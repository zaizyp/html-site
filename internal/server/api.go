// api.go：/api/pages 与 /api/pages/{slug} 的请求处理。
package server

import (
	"io"
	"net/http"
	"strings"

	"html-site/internal/model"
	"html-site/internal/store"
)

// handlePagesCollection 处理 /api/pages：
//   - GET    列出当前 owner 的全部页面
//   - POST   上传新页面
func (s *Server) handlePagesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPages(w, r)
	case http.MethodPost:
		s.createPage(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePageItem 处理 /api/pages/{slug}：
//   - GET    查看元信息（含 content，方便 agent 拉回内容做修改）
//   - PUT    修改（内容/标题/分享码）
//   - DELETE 删除
func (s *Server) handlePageItem(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /api/pages/{slug}[/...]；提取 {slug}
	path := strings.TrimPrefix(r.URL.Path, "/api/pages/")
	slug, _, _ := strings.Cut(path, "/")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "missing slug")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getPage(w, r, slug)
	case http.MethodPut:
		s.updatePage(w, r, slug)
	case http.MethodDelete:
		s.deletePage(w, r, slug)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ----------------------------------------------------------------------------
// GET /api/pages —— 列出当前 owner 的页面
// ----------------------------------------------------------------------------

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	user := ownerFromContext(r)
	pages, err := s.store.ListPagesByOwner(user.ID, 0) // groupFilter=0 不过滤
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pages == nil {
		pages = []*model.Page{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

// ----------------------------------------------------------------------------
// POST /api/pages —— 上传新页面
// ----------------------------------------------------------------------------

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	// 限制请求体大小，避免被超大上传拖垮
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	user := ownerFromContext(r)

	var req model.CreatePageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	p, err := s.store.CreatePage(user.ID, req.Content, req.Title, req.ShareCode, req.Slug, req.GroupName)
	if err != nil {
		code, msg := statusForError(err)
		writeError(w, code, msg)
		return
	}

	resp := model.CreatePageResponse{
		Slug:      p.Slug,
		URL:       s.publicBaseURL(r) + "/v/" + p.Slug,
		ShareCode: p.ShareCode,
		Public:    p.ShareCode == "",
		Group:     p.GroupName,
	}
	writeJSON(w, http.StatusCreated, resp)
}

// ----------------------------------------------------------------------------
// GET /api/pages/{slug} —— 元信息（含 content）
// ----------------------------------------------------------------------------

func (s *Server) getPage(w http.ResponseWriter, r *http.Request, slug string) {
	p, ok := s.requirePageOwner(w, r, slug)
	if !ok {
		return
	}
	content, err := s.store.ReadPageContent(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read content: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page":    p,
		"content": string(content),
	})
}

// ----------------------------------------------------------------------------
// PUT /api/pages/{slug} —— 修改
// ----------------------------------------------------------------------------

func (s *Server) updatePage(w http.ResponseWriter, r *http.Request, slug string) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	p, ok := s.requirePageOwner(w, r, slug)
	if !ok {
		return
	}
	var req model.UpdatePageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	opts := store.UpdatePageOptions{
		Content:      req.Content,
		Title:        req.Title,
		SetShareCode: req.ShareCodeSet,
		ShareCode:    req.ShareCode,
	}
	updated, err := s.store.UpdatePage(p.ID, opts)
	if err != nil {
		code, msg := statusForError(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page": updated,
		"url":  s.publicBaseURL(r) + "/v/" + updated.Slug,
	})
}

// ----------------------------------------------------------------------------
// DELETE /api/pages/{slug} —— 删除
// ----------------------------------------------------------------------------

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request, slug string) {
	p, ok := s.requirePageOwner(w, r, slug)
	if !ok {
		return
	}
	if err := s.store.DeletePage(p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "slug": slug})
}

// ----------------------------------------------------------------------------
// 工具
// ----------------------------------------------------------------------------

// decodeJSON 解析请求体。同时支持 application/json 直接读，便于 CLI。
func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return decodeJSONBytes(body, dst)
}
