// admin_pages_ops.go：后台页面操作 —— 在线编辑、上传新建、元信息修改、批量删除/移动。
// 路由在 registerAdmin 中注册（/admin/pages/new、/admin/pages/{id}/edit、/admin/pages/batch）。
package server

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"html-site/internal/model"
	"html-site/internal/store"
)

// pageSize 后台列表每页条数。
const adminPageSize = 15

// adminPageNew GET 展示上传/新建页；POST 创建页面。
func (s *Server) adminPageNew(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if r.Method == http.MethodGet {
		d := adminData{Title: "发布页面", CurrentUser: u, CSRF: currentCSRF(r)}
		d.Flash = s.popFlash(w, r)
		if u.IsAdmin() {
			d.Groups, _ = s.store.ListAllGroups()
		} else {
			d.Groups, _ = s.store.ListGroupsByOwner(u.ID)
		}
		s.renderAdmin(w, "page_upload.html", d)
		return
	}
	// POST：创建
	s.adminPageCreate(w, r)
}

// adminPageCreate 处理新建页面（表单提交：content + 可选 file 上传）。
func (s *Server) adminPageCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
		// 退化为普通表单
		_ = r.ParseForm()
	}

	content := r.FormValue("content")
	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	shareCode := strings.TrimSpace(r.FormValue("share_code"))
	groupID, _ := strconv.ParseInt(r.FormValue("group_id"), 10, 64)

	// 若上传了文件，用文件内容覆盖
	if file, hdr, err := r.FormFile("file"); err == nil {
		if data, rerr := io.ReadAll(io.LimitReader(file, MaxUploadBytes)); rerr == nil && len(data) > 0 {
			content = string(data)
			if title == "" {
				title = strings.TrimSuffix(strings.TrimSuffix(hdr.Filename, ".html"), ".htm")
			}
		}
	}

	if strings.TrimSpace(content) == "" {
		s.setFlash(w, "err", "HTML 内容不能为空")
		http.Redirect(w, r, "/admin/pages/new", http.StatusSeeOther)
		return
	}

	// 把 group_id 解析成分组名（CreatePage 接受分组名；不存在则自动建）
	groupName := ""
	if groupID > 0 {
		if g, err := s.store.GroupByID(groupID); err == nil && g.OwnerID == u.ID {
			groupName = g.Name
		}
	}

	if _, err := s.store.CreatePage(u.ID, content, title, shareCode, slug, groupName); err != nil {
		code, msg := statusForError(err)
		_ = code
		s.setFlash(w, "err", "发布失败："+msg)
		http.Redirect(w, r, "/admin/pages/new", http.StatusSeeOther)
		return
	}
	s.setFlash(w, "ok", "页面已发布")
	http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
}

// adminPageEdit GET 展示编辑页；POST 保存（内容 + 元信息）。
func (s *Server) adminPageEdit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64) // /admin/pages/{id}/edit
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.PageByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !u.IsAdmin() && p.OwnerID != u.ID {
		http.Error(w, "无权操作", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodGet {
		d := adminData{Title: "编辑页面", CurrentUser: u, CSRF: currentCSRF(r), EditPage: p}
		d.Flash = s.popFlash(w, r)
		content, _ := s.store.ReadPageContent(p)
		d.Content = string(content)
		if u.IsAdmin() {
			d.Groups, _ = s.store.ListAllGroups()
		} else {
			d.Groups, _ = s.store.ListGroupsByOwner(u.ID)
		}
		s.renderAdmin(w, "page_upload.html", d)
		return
	}

	// POST：保存
	if s.verifyCSRF(w, r) {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	newSlug := strings.TrimSpace(r.FormValue("slug"))
	shareCode := r.FormValue("share_code")
	groupID, _ := strconv.ParseInt(r.FormValue("group_id"), 10, 64)
	content := r.FormValue("content")

	// 1. 改元信息
	meta := store.PageMetaUpdate{
		Title:        &title,
		GroupIDSet:   true,
		GroupID:      groupID,
		ShareCodeSet: true,
		ShareCode:    shareCode,
	}
	if newSlug != p.Slug {
		ns := newSlug
		meta.NewSlug = &ns
	}
	if _, err := s.store.UpdatePageMeta(p.ID, meta); err != nil {
		s.setFlash(w, "err", "保存失败："+err.Error())
		http.Redirect(w, r, "/admin/pages/"+strconv.FormatInt(p.ID, 10)+"/edit", http.StatusSeeOther)
		return
	}

	// 2. 改内容（若有）
	if strings.TrimSpace(content) != "" {
		if _, err := s.store.UpdatePage(p.ID, store.UpdatePageOptions{Content: &content}); err != nil {
			s.setFlash(w, "err", "内容保存失败："+err.Error())
			http.Redirect(w, r, "/admin/pages/"+strconv.FormatInt(p.ID, 10)+"/edit", http.StatusSeeOther)
			return
		}
	}
	s.setFlash(w, "ok", "已保存")
	http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
}

// adminPagesBatch 批量删除 / 批量移动分组。
// 表单字段：action=delete|move，ids=...（多个），move 时 group=目标分组ID。
// 注意：ids 通过单独的隐藏表单 __batch__ 收集（checkbox 的 form 属性指向它），
// 但实际提交表单是批量栏的两个 form；为简化，我们让提交按钮把选中 id 动态写入。
// 这里改为：读取所有名为 ids 的字段（前端在提交前会克隆 checkbox 到提交表单）。
// 简化方案：前端在批量表单提交事件里把选中的 checkbox append 进表单。
func (s *Server) adminPagesBatch(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	r.ParseMultipartForm(1 << 20)
	if err := r.ParseForm(); err == nil {
	}
	action := r.FormValue("action")
	ids := parseIDs(r.Form["ids"])

	switch action {
	case "delete":
		// 仅删自己的（管理员可删任意）
		n := 0
		for _, id := range ids {
			p, err := s.store.PageByID(id)
			if err != nil || (!u.IsAdmin() && p.OwnerID != u.ID) {
				continue
			}
			if err := s.store.DeletePage(id); err == nil {
				n++
			}
		}
		s.setFlash(w, "ok", "已删除 "+strconv.Itoa(n)+" 个页面")
	case "move":
		gid, _ := strconv.ParseInt(r.FormValue("group"), 10, 64)
		// 校验目标分组属于自己
		if gid > 0 {
			if g, err := s.store.GroupByID(gid); err != nil || (!u.IsAdmin() && g.OwnerID != u.ID) {
				s.setFlash(w, "err", "目标分组无效")
				http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
				return
			}
		}
		scope := u.ID
		if u.IsAdmin() {
			scope = 0 // 管理员批量移动不限制 owner（仅对选中项）
		}
		// admin 无 owner 限制：逐项校验
		n := 0
		for _, id := range ids {
			p, err := s.store.PageByID(id)
			if err != nil || (!u.IsAdmin() && p.OwnerID != u.ID) {
				continue
			}
			if err := s.store.MovePageToGroup(id, gid); err == nil {
				n++
			}
		}
		_ = scope
		s.setFlash(w, "ok", "已移动 "+strconv.Itoa(n)+" 个页面")
	default:
		s.setFlash(w, "err", "未知操作")
	}
	http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
}

// parseIDs 把字符串切片解析为 int64 切片，忽略非法值。
func parseIDs(ss []string) []int64 {
	var out []int64
	for _, s := range ss {
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// 占位：确保 model 包被引用（PageMetaUpdate 等已在 store 中）。
var _ = model.RoleUser
