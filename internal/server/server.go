// Package server 实现 html-site 的服务端：API 写操作 + HTML 查看托管。
//
// 路由：
//   - /api/*  需要 X-API-Token，校验 owner 身份（写操作权限隔离）
//   - /v/{slug}      查看 HTML；有分享码则需先通过 /v/{slug}/verify
//   - /healthz 健康检查
package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"html-site/internal/model"
	"html-site/internal/store"
)

// MaxUploadBytes 限制单次上传体积（10MB）。单文件 HTML 足够。
const MaxUploadBytes = 10 * 1024 * 1024

// Server 聚合存储层与运行配置。
type Server struct {
	store        *store.Store
	addr         string        // 监听地址，用于未配置 publicURL 时推导默认 base URL
	publicURL    string        // 对外可访问的基础 URL，如 https://site.example.com ；用于拼访问链接
	mux          *http.ServeMux
	loginLimit   *loginLimiter // 后台登录失败计数(防暴力枚举)
}

// Options 构造 Server 的可选参数。
type Options struct {
	Addr      string // 监听地址
	PublicURL string // 留空则用请求 Host 头动态推断
}

// New 创建 Server 并注册路由。
func New(st *store.Store, opt Options) *Server {
	s := &Server{
		store:      st,
		addr:       opt.Addr,
		publicURL:  strings.TrimRight(opt.PublicURL, "/"),
		loginLimit: newLoginLimiter(),
	}
	s.mux = http.NewServeMux()
	s.register()
	return s
}

// Handler 暴露底层 mux，供测试或自定义 Server 复用。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.mux.ServeHTTP)
}

// ListenAndServe 启动 HTTP 服务。
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr: addr,
		// 中间件包裹顺序(从外到内):安全响应头 → 访问日志 → 路由。
		Handler:           s.secureHeaders(s.logging(s.mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("html-site listening on %s", addr)
	return srv.ListenAndServe()
}

// secureHeaders 注入通用安全响应头。全部为附加项,不改变业务行为。
//
//   - X-Content-Type-Options: nosniff  —— 阻止浏览器对响应做 MIME 嗅探
//   - X-Frame-Options: SAMEORIGIN      —— 仅允许同源页面 iframe 嵌入(后台编辑器
//     的实时预览、同源托管页仍可正常工作,只挡跨站点击劫持)
//   - Referrer-Policy: strict-origin-when-cross-origin —— 跨站请求只带 origin,不带完整路径
//
// 不设 CSP:后台模板与托管 HTML 内联脚本较多,内网场景收益有限且配置不当易破坏 UI。
func (s *Server) secureHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.ServeHTTP(w, r)
	})
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

// register 注册全部路由。
func (s *Server) register() {
	// API：所有 /api/ 走 token 鉴权中间件
	s.mux.HandleFunc("/api/pages", s.withToken(s.handlePagesCollection))
	s.mux.HandleFunc("/api/pages/", s.withToken(s.handlePageItem))

	// 查看层
	s.mux.HandleFunc("/v/", s.handleView)

	// 后台管理
	s.registerAdmin()

	// 静态资源
	s.mux.HandleFunc("/static/", serveStatic)

	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body><h2>html-site</h2><p>运行中。使用 CLI 上传 HTML 后即可获得访问链接。</p></body></html>`))
			return
		}
		http.NotFound(w, r)
	})
}

// logging 简单的访问日志中间件。
func (s *Server) logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// ----------------------------------------------------------------------------
// 通用响应工具
// ----------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

// publicBaseURL 返回用于拼接对外访问链接的基础 URL。
// 优先级：配置的 publicURL > 请求 Host（含端口，反代场景下取 X-Forwarded-*）> 监听 addr 推导。
func (s *Server) publicBaseURL(r *http.Request) string {
	if s.publicURL != "" {
		return s.publicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		scheme = xfp
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	// r.Host 本身就含端口（如 127.0.0.1:18080），直接可用
	if host != "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	// 兜底：用监听 addr 推导（保证带端口）
	host = deriveHostFromAddr(s.addr)
	return fmt.Sprintf("%s://%s", scheme, host)
}

// deriveHostFromAddr 从监听地址（如 ":18080" / "127.0.0.1:8080" / ":80"）推导出带端口的主机串。
func deriveHostFromAddr(addr string) string {
	const defaultHost = "localhost"
	if addr == "" {
		return defaultHost
	}
	// 形如 ":port"
	if strings.HasPrefix(addr, ":") {
		return defaultHost + addr
	}
	// 形如 "host:port"；host 为 0.0.0.0 时换成 localhost（更友好）
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "[::]" {
		host = defaultHost
	}
	return host + ":" + port
}

// ownerFromContext 从请求上下文取出已认证 owner。
type ctxKey string

const ownerCtxKey ctxKey = "owner"

func ownerFromContext(r *http.Request) *model.User {
	if v, ok := r.Context().Value(ownerCtxKey).(*model.User); ok {
		return v
	}
	return nil
}

// 友好的错误码映射：把 store 层错误转成 HTTP 状态。
func statusForError(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "page not found"
	case errors.Is(err, store.ErrUserNotFound):
		return http.StatusNotFound, "user not found"
	case errors.Is(err, store.ErrSlugTaken):
		return http.StatusConflict, "slug already taken"
	case errors.Is(err, store.ErrUserExists):
		return http.StatusConflict, "user already exists"
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// genRandomHex 生成 n 字节的随机十六进制串（2n 个十六进制字符）。
func genRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
