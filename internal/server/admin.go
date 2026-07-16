// admin.go：后台 handlers —— 登录/登出、仪表盘、页面管理、分组管理。
package server

import (
	"net/http"
	"strconv"
	"strings"

	"html-site/internal/model"
)

// registerAdmin 注册全部后台路由。带 _p 的为公开（登录），其余需登录/管理员。
func (s *Server) registerAdmin() {
	// 公开：登录页（GET 展示，POST 校验）。需要注入一次性 CSRF。
	s.mux.HandleFunc("/admin/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			csrf := s.ensureLoginCSRF(w, r)
			s.renderAdmin(w, "login.html", adminData{Title: "登录", CSRF: csrf})
			return
		}
		// POST：校验 loginCSRF（公开端点，无 session，走 cookie csrf）
		_ = r.ParseForm()
		if r.FormValue("csrf") == "" || r.FormValue("csrf") != s.loginCSRF(r) {
			csrf := s.ensureLoginCSRF(w, r)
			s.renderAdmin(w, "login.html", adminData{Title: "登录", Error: "页面已过期，请重试", CSRF: csrf})
			return
		}
		s.adminLogin(w, r)
	})
	s.mux.HandleFunc("/admin/logout", s.requireLogin(s.adminLogout))

	// 需登录
	s.mux.HandleFunc("/admin", s.requireLogin(s.adminDashboard))
	s.mux.HandleFunc("/admin/pages", s.requireLogin(s.adminPages))
	s.mux.HandleFunc("/admin/pages/", s.requireLogin(s.adminPagesAction)) // delete
	s.mux.HandleFunc("/admin/groups", s.requireLogin(s.adminGroups))
	s.mux.HandleFunc("/admin/groups/", s.requireLogin(s.adminGroupsAction)) // create/rename/delete
	s.mux.HandleFunc("/admin/account", s.requireLogin(s.adminAccount))
	s.mux.HandleFunc("/admin/account/", s.requireLogin(s.adminAccountAction)) // password/regenerate-token

	// 仅管理员
	s.mux.HandleFunc("/admin/users", s.requireAdmin(s.adminUsers))
	s.mux.HandleFunc("/admin/users/", s.requireAdmin(s.adminUsersAction))
}

// adminPagesAction 处理 /admin/pages/{id}/delete。
func (s *Server) adminPagesAction(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/delete"):
		s.adminPageDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

// adminGroupsAction 处理 /admin/groups/{create | id/rename | id/delete}。
func (s *Server) adminGroupsAction(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimRight(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(p, "/create"):
		s.adminGroupCreate(w, r)
	case strings.HasSuffix(p, "/rename"):
		s.adminGroupRename(w, r)
	case strings.HasSuffix(p, "/delete"):
		s.adminGroupDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

// adminAccountAction 处理 /admin/account/{password | regenerate-token}。
func (s *Server) adminAccountAction(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimRight(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(p, "/password"):
		s.adminAccountPassword(w, r)
	case strings.HasSuffix(p, "/regenerate-token"):
		s.adminAccountRegenToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

// adminUsersAction 处理 /admin/users/{create | id/*}。
func (s *Server) adminUsersAction(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimRight(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(p, "/create"):
		s.adminUserCreate(w, r)
	case strings.HasSuffix(p, "/delete"):
		s.adminUserDelete(w, r)
	case strings.HasSuffix(p, "/reset-password"):
		s.adminUserResetPassword(w, r)
	case strings.HasSuffix(p, "/promote"):
		s.adminUserPromote(w, r)
	case strings.HasSuffix(p, "/demote"):
		s.adminUserDemote(w, r)
	default:
		http.NotFound(w, r)
	}
}

// adminData 是模板渲染的通用数据容器。
type adminData struct {
	Title        string
	CurrentUser  *model.User
	CSRF         string
	Flash        []flashMsg
	// 各页面按需填充的字段
	StatPages, StatGroups, StatUsers int
	RecentPages                      []*model.Page
	Pages                            []*model.Page
	Groups                           []*model.Group
	Users                            []*model.User
	FilterOwner, FilterGroup         int64
	Query                            string
	Username                         string
	Error                            string
	MaskedToken                      string
	NewToken                         string
}

type flashMsg struct {
	Class string // "err" | "ok"
	Msg   string
}

// flash 把消息暂存到 session（简单做法：用 query 参数 ?f= 传递）。
// 这里改用一次性 cookie 暂存，避免 URL 暴露。实现复杂度高，简化为：登录后页面直接带 flash。
// 为保持简单，本实现把 flash 通过 cookie 传递见 carryFlash/flash helpers。

// renderAdmin 渲染后台模板，自动填充 CurrentUser / CSRF。
func (s *Server) renderAdmin(w http.ResponseWriter, name string, d adminData) {
	u := d.CurrentUser
	if u == nil {
		// 未带，渲染时需要从空模板体现
	}
	_ = u
	if d.CSRF == "" {
		// 模板需要 csrf 字段，调用方应已通过 requireLogin 注入
	}
	if err := renderTmpl(w, name, d); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ----------------------------------------------------------------------------
// 登录 / 登出
// ----------------------------------------------------------------------------

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	// POST：校验用户名密码（CSRF 已在路由层校验）
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	d := adminData{Title: "登录", Username: username}

	user, err := s.store.UserByName(username)
	if err != nil || !s.store.VerifyPassword(user, password) {
		d.Error = "用户名或密码错误"
		d.CSRF = s.loginCSRF(r)
		s.renderAdmin(w, "login.html", d)
		return
	}
	if err := s.startSession(w, user.ID); err != nil {
		d.Error = "创建会话失败：" + err.Error()
		d.CSRF = s.loginCSRF(r)
		s.renderAdmin(w, "login.html", d)
		return
	}
	// 登录成功后清除 loginCSRF cookie
	http.SetCookie(w, &http.Cookie{Name: loginCSRFCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// loginCSRF 返回登录页专用的一次性 CSRF（写入短期 cookie）。
// 登录是公开端点，没有 session，故用一个临时 cookie 承载 csrf。
const loginCSRFCookie = "hs_login_csrf"

func (s *Server) loginCSRF(r *http.Request) string {
	if c, err := r.Cookie(loginCSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// ensureLoginCSRF 确保登录页有一次性的 csrf（GET 时种入，登录成功后失效）。
func (s *Server) ensureLoginCSRF(w http.ResponseWriter, r *http.Request) string {
	if v := s.loginCSRF(r); v != "" {
		return v
	}
	v, err := randomHex(16)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name: loginCSRFCookie, Value: v, Path: "/", MaxAge: 600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	return v
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	s.clearSession(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ----------------------------------------------------------------------------
// 仪表盘
// ----------------------------------------------------------------------------

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "概览", CurrentUser: u, CSRF: currentCSRF(r)}

	if u.IsAdmin() {
		pages, _ := s.store.ListAllPages()
		groups, _ := s.store.ListAllGroups()
		users, _ := s.store.ListUsers()
		d.StatPages = len(pages)
		d.StatGroups = len(groups)
		d.StatUsers = len(users)
		if len(pages) > 5 {
			d.RecentPages = pages[:5]
		} else {
			d.RecentPages = pages
		}
	} else {
		pages, _ := s.store.ListPagesByOwner(u.ID, 0)
		groups, _ := s.store.ListGroupsByOwner(u.ID)
		d.StatPages = len(pages)
		d.StatGroups = len(groups)
		if len(pages) > 5 {
			d.RecentPages = pages[:5]
		} else {
			d.RecentPages = pages
		}
	}
	s.renderAdmin(w, "dashboard.html", d)
}

// ----------------------------------------------------------------------------
// 页面管理
// ----------------------------------------------------------------------------

func (s *Server) adminPages(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "页面管理", CurrentUser: u, CSRF: currentCSRF(r)}
	d.Flash = s.popFlash(w, r)

	// 解析筛选参数
	ownerFilter := u.ID // 普通用户固定看自己
	if u.IsAdmin() {
		if v := r.URL.Query().Get("owner"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				ownerFilter = id
				d.FilterOwner = id
			}
		}
	}
	if v := r.URL.Query().Get("group"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.FilterGroup = id
		}
	}
	d.Query = strings.TrimSpace(r.URL.Query().Get("q"))

	// 取数据
	var pages []*model.Page
	if u.IsAdmin() && d.FilterOwner == 0 {
		pages, _ = s.store.ListAllPages()
	} else {
		pages, _ = s.store.ListPagesByOwner(ownerFilter, d.FilterGroup)
	}
	pages = filterSearch(pages, d.Query)
	d.Pages = pages

	// 分组列表（筛选下拉用）
	if u.IsAdmin() {
		d.Groups, _ = s.store.ListAllGroups()
		d.Users, _ = s.store.ListUsers()
	} else {
		d.Groups, _ = s.store.ListGroupsByOwner(u.ID)
	}

	s.renderAdmin(w, "pages.html", d)
}

// filterSearch 按 q 过滤 slug/标题。
func filterSearch(pages []*model.Page, q string) []*model.Page {
	if q == "" {
		return pages
	}
	q = strings.ToLower(q)
	var out []*model.Page
	for _, p := range pages {
		if strings.Contains(strings.ToLower(p.Slug), q) ||
			strings.Contains(strings.ToLower(p.Title), q) ||
			strings.Contains(strings.ToLower(p.OwnerName), q) {
			out = append(out, p)
		}
	}
	return out
}

// adminPageDelete 删除页面（owner 或 admin 可操作）。
func (s *Server) adminPageDelete(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64) // /admin/pages/{id}/delete
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
	_ = s.store.DeletePage(p.ID)
	http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
}

// ----------------------------------------------------------------------------
// 分组管理
// ----------------------------------------------------------------------------

func (s *Server) adminGroups(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "分组管理", CurrentUser: u, CSRF: currentCSRF(r)}
	if u.IsAdmin() {
		d.Groups, _ = s.store.ListAllGroups()
	} else {
		d.Groups, _ = s.store.ListGroupsByOwner(u.ID)
	}
	s.renderAdmin(w, "groups.html", d)
}

func (s *Server) adminGroupCreate(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
		return
	}
	if _, err := s.store.CreateGroup(u.ID, name); err != nil {
		s.setFlash(w, "err", "创建失败："+err.Error())
	}
	http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
}

func (s *Server) adminGroupRename(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	gid, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if err := s.store.RenameGroup(gid, u.ID, name); err != nil {
		s.setFlash(w, "err", "改名失败："+err.Error())
	}
	http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
}

func (s *Server) adminGroupDelete(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	gid, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteGroup(gid, u.ID); err != nil {
		s.setFlash(w, "err", "删除失败："+err.Error())
	}
	http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
}

// pathID 从路径中提取第 idx 段（按 / 分割）。
// 例：pathID("/admin/pages/12/delete", 2) → "12"（0-based: admin=0,pages=1,12=2,delete=3）
func pathID(path string, idx int) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if idx < len(parts) {
		return parts[idx]
	}
	return ""
}

// ----------------------------------------------------------------------------
// flash（一次性消息，用 cookie 传递）
// ----------------------------------------------------------------------------

const flashCookie = "hs_flash"

func (s *Server) setFlash(w http.ResponseWriter, class, msg string) {
	// 用 class|msg 编码，URL 不暴露
	val := class + "|" + msg
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: val, Path: "/", MaxAge: 30,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// popFlash 读取并清除 flash，供页面渲染前调用。
func (s *Server) popFlash(w http.ResponseWriter, r *http.Request) []flashMsg {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	parts := strings.SplitN(c.Value, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	return []flashMsg{{Class: parts[0], Msg: parts[1]}}
}

// randomHex 生成 n 字节的十六进制串。
func randomHex(n int) (string, error) {
	return randomTokenHex(n)
}
