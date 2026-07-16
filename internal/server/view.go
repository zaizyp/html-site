// view.go：/v/{slug} 查看 HTML，含分享码校验流程。
//
// 访问流程：
//  1. GET /v/{slug}
//     · 页面无分享码 → 直接返回 HTML
//     · 有分享码 → 检查 cookie 是否已通过；通过则返回 HTML，否则返回输入页
//  2. POST /v/{slug}/verify  body: share_code=xxx
//     · 校验通过 → 种 cookie 后 302 跳回 GET /v/{slug}
//     · 失败 → 返回输入页并提示错误
//
// cookie 设计：键名 hs_v_<slug>，值为固定签名串（这里是 'ok' + base 分享码哈希前 8 位）。
// HttpOnly + SameSite=Lax，路径 /v/<slug>。有效期 30 天。
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"html-site/internal/model"
	"html-site/internal/store"
)

const (
	shareCookieMaxAge = 30 * 24 * 3600 // 30 天
)

// handleView 是 /v/ 前缀的总入口，按子路径分发到「查看」或「校验」。
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v/")
	// 去掉可能的尾部斜杠
	path = strings.TrimRight(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	// 区分 /v/{slug} 与 /v/{slug}/verify
	slug, rest, _ := strings.Cut(path, "/")
	switch rest {
	case "":
		s.viewPage(w, r, slug)
	case "verify":
		s.verifyShareCode(w, r, slug)
	default:
		http.NotFound(w, r)
	}
}

// viewPage 渲染页面 HTML。
func (s *Server) viewPage(w http.ResponseWriter, r *http.Request, slug string) {
	p, err := s.store.PageBySlug(slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 公开页面：直接返回
	if !p.HasCode {
		s.serveHTML(w, r, p)
		return
	}

	// 非公开：校验 cookie
	if validShareCookie(r, slug, p.ShareCode) {
		s.serveHTML(w, r, p)
		return
	}

	// 未通过：返回输入页
	renderCodePrompt(w, slug, "")
}

// verifyShareCode 处理 POST /v/{slug}/verify。
func (s *Server) verifyShareCode(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, err := s.store.PageBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderCodePrompt(w, slug, "表单解析失败")
		return
	}
	code := strings.TrimSpace(r.FormValue("share_code"))
	if p.HasCode && constantTimeEqualString(code, p.ShareCode) {
		// 通过：种 cookie
		http.SetCookie(w, &http.Cookie{
			Name:     shareCookieName(slug),
			Value:    shareCookieValue(p.ShareCode),
			Path:     "/v/" + slug,
			MaxAge:   shareCookieMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/v/"+slug, http.StatusSeeOther)
		return
	}
	renderCodePrompt(w, slug, "分享码不正确")
}

// serveHTML 读取磁盘文件并作为 text/html 返回。
func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request, p *model.Page) {
	content, err := s.store.ReadPageContent(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}

// ----------------------------------------------------------------------------
// 分享码 cookie 工具
// ----------------------------------------------------------------------------

func shareCookieName(slug string) string {
	return "hs_v_" + slug
}

// shareCookieValue 用分享码的 sha256 前 16 个十六进制字符作为 token，
// 不存放明文分享码，避免通过 cookie 泄漏码本身。
func shareCookieValue(shareCode string) string {
	sum := sha256.Sum256([]byte(shareCode))
	return hex.EncodeToString(sum[:8])
}

// validShareCookie 检查请求里的 cookie 是否匹配该页面的分享码。
func validShareCookie(r *http.Request, slug, shareCode string) bool {
	c, err := r.Cookie(shareCookieName(slug))
	if err != nil || c.Value == "" {
		return false
	}
	return constantTimeEqualString(c.Value, shareCookieValue(shareCode))
}

// constantTimeEqualString 常量时间字符串比较，规避时序攻击。
func constantTimeEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// renderCodePrompt 输出分享码输入页。errMsg 非空时显示错误提示。
func renderCodePrompt(w http.ResponseWriter, slug, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	errBlock := ""
	if errMsg != "" {
		errBlock = `<p class="err">` + htmlEscape(errMsg) + `</p>`
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>需要分享码</title>
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f5f6f8;color:#222;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
  .card{background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.08);padding:32px;width:320px;max-width:90vw}
  h1{font-size:18px;margin:0 0 8px}
  p.sub{color:#888;font-size:13px;margin:0 0 20px}
  label{display:block;font-size:13px;margin-bottom:6px;color:#555}
  input{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #ddd;border-radius:8px;font-size:15px}
  input:focus{outline:none;border-color:#4a6cf7;box-shadow:0 0 0 3px rgba(74,108,247,.15)}
  button{margin-top:16px;width:100%;padding:11px;background:#4a6cf7;color:#fff;border:0;border-radius:8px;font-size:15px;cursor:pointer}
  button:hover{background:#3a5be0}
  .err{color:#e5484d;background:#fdecee;padding:8px 12px;border-radius:6px;margin:0 0 12px;font-size:13px}
</style>
</head>
<body>
<form class="card" method="POST" action="/v/` + slug + `/verify">
  <h1>该页面受分享码保护</h1>
  <p class="sub">请输入分享码以查看内容</p>
  ` + errBlock + `
  <label for="code">分享码</label>
  <input id="code" name="share_code" type="text" autocomplete="off" autofocus>
  <button type="submit">查看</button>
</form>
</body>
</html>`))
}

// htmlEscape 简易 HTML 转义，避免错误信息里的特殊字符破坏页面。
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
