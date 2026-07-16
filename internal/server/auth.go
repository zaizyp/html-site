// auth.go：API token 鉴权中间件 + owner 校验工具 + 后台 session 鉴权。
package server

import (
	"context"
	"net/http"
	"strings"

	"html-site/internal/model"
)

// sessionCookieName 后台登录 cookie 名。
const sessionCookieName = "hs_session"

// withToken 是 /api/* 的统一中间件：校验 X-API-Token，把 owner 放进 context。
// 未带 token 或 token 无效 → 401。
func (s *Server) withToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get("X-API-Token"))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing X-API-Token header")
			return
		}
		user, err := s.store.UserByToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ownerCtxKey, user)
		next(w, r.WithContext(ctx))
	}
}

// requirePageOwner 加载 slug 对应页面，并校验其 owner 是否为当前请求用户。
// 失败时已写入 HTTP 响应，调用方应直接 return。成功时返回 page。
//
// 注意：对无权访问的资源统一返回 404（不暴露存在性）。
func (s *Server) requirePageOwner(w http.ResponseWriter, r *http.Request, slug string) (*model.Page, bool) {
	p, err := s.store.PageBySlug(slug)
	if err != nil {
		code, msg := statusForError(err)
		writeError(w, code, msg)
		return nil, false
	}
	user := ownerFromContext(r)
	if user == nil || p.OwnerID != user.ID {
		writeError(w, http.StatusNotFound, "page not found")
		return nil, false
	}
	return p, true
}

// sessionUserKey 后台登录用户在 context 中的键。
type sessionUserKey struct{}

// sessionCSRFKey CSRF token 在 context 中的键。
type sessionCSRFKey struct{}

// currentUser 从 context 取已登录用户（后台）。未登录返回 nil。
func currentUser(r *http.Request) *model.User {
	if v, ok := r.Context().Value(sessionUserKey{}).(*model.User); ok {
		return v
	}
	return nil
}

// currentCSRF 从 context 取当前 session 的 CSRF token。
func currentCSRF(r *http.Request) string {
	if v, ok := r.Context().Value(sessionCSRFKey{}).(string); ok {
		return v
	}
	return ""
}

// requireLogin 要求已登录后台；未登录跳转登录页。
func (s *Server) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, csrf, ok := s.resolveSession(w, r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), sessionUserKey{}, u)
		ctx = context.WithValue(ctx, sessionCSRFKey{}, csrf)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin 要求已登录且为管理员。
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			http.Error(w, "需要管理员权限", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// resolveSession 解析 cookie 中的 session，返回 (user, csrf, ok)。
// ok=false 表示未登录（已清失效 cookie）。
func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) (*model.User, string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, "", false
	}
	sess, err := s.store.SessionByToken(c.Value)
	if err != nil {
		// 失效 session：清 cookie
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
		return nil, "", false
	}
	user, err := s.store.UserByID(sess.UserID)
	if err != nil {
		return nil, "", false
	}
	return user, sess.CSRF, true
}

// startSession 登录成功后创建 session 并种 cookie。
func (s *Server) startSession(w http.ResponseWriter, userID int64) error {
	sess, err := s.store.CreateSession(userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.Token,
		Path:     "/",
		MaxAge:   int(model.SessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// clearSession 登出：删 session + 清 cookie。
func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
}

// verifyCSRF 校验 POST 表单里的 csrf 字段与 session 绑定的 csrf 是否一致。
// 不一致返回 true（已写过响应）。
func (s *Server) verifyCSRF(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad form")
		return true
	}
	got := r.FormValue("csrf")
	want := currentCSRF(r)
	if got == "" || got != want {
		http.Error(w, "CSRF 校验失败", http.StatusForbidden)
		return true
	}
	return false
}
