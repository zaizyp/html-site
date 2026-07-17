// admin.go：后台 handlers —— 登录/登出、仪表盘、页面管理、分组管理。
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"html-site/internal/model"
	"html-site/internal/store"
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
	s.mux.HandleFunc("/admin/pages/new", s.requireLogin(s.adminPageNew))
	s.mux.HandleFunc("/admin/pages/batch", s.requireLogin(s.adminPagesBatch))
	s.mux.HandleFunc("/admin/pages/", s.requireLogin(s.adminPagesAction)) // {id}/delete | {id}/edit
	s.mux.HandleFunc("/admin/groups", s.requireLogin(s.adminGroups))
	s.mux.HandleFunc("/admin/groups/", s.requireLogin(s.adminGroupsAction)) // create/rename/delete
	s.mux.HandleFunc("/admin/account", s.requireLogin(s.adminAccount))
	s.mux.HandleFunc("/admin/account/", s.requireLogin(s.adminAccountAction)) // password/regenerate-token

	// 仅管理员
	s.mux.HandleFunc("/admin/users", s.requireAdmin(s.adminUsers))
	s.mux.HandleFunc("/admin/users/", s.requireAdmin(s.adminUsersAction))
}

// adminPagesAction 处理 /admin/pages/{id}/delete 与 /admin/pages/{id}/edit。
func (s *Server) adminPagesAction(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/delete"):
		s.adminPageDelete(w, r)
	case strings.HasSuffix(p, "/edit"):
		s.adminPageEdit(w, r)
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
	StatPV, StatUV                   int64
	StatBytes                        int64
	StatToday                        int
	RecentPages                      []*model.Page
	Pages                            []*model.Page
	Groups                           []*model.Group
	Users                            []*model.User
	UserPageCount                    map[int64]int
	FilterOwner, FilterGroup         int64
	Query                            string
	// 文件夹浏览（进入式目录）
	ViewedUser    *model.User   // 当前正在浏览其目录的用户（管理员视角）
	CurrentGroup  *model.Group  // 当前所在分组（nil=根目录）
	SubGroups     []*model.Group // 当前目录下的子文件夹
	Breadcrumbs   []*model.Group // 面包屑：根 → ... → 当前
	Username                         string
	Error                            string
	MaskedToken                      string
	NewToken                         string
	// 仪表盘图表数据（JSON 字符串，由模板 data-chart-* 消费）
	ChartViews  string
	ChartAccess string
	ChartGroups []*model.GroupCount
	MaxGroupCount int
	TopPages    []store.TopPageByViews
	// 分页
	Page       int
	TotalPages int
	Total      int
	PageNums   []int
	// 编辑/上传页
	EditPage *model.Page
	Content  string
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

	scope := int64(0) // 全站
	if !u.IsAdmin() {
		scope = u.ID
	}

	d.StatPages, _ = s.store.CountPages(scope, 0)
	if u.IsAdmin() {
		users, _ := s.store.ListUsers()
		d.StatUsers = len(users)
		d.StatGroups = s.countAllGroups()
	} else {
		groups, _ := s.store.ListGroupsByOwner(u.ID)
		d.StatGroups = len(groups)
	}
	d.StatBytes, _ = s.store.TotalStorage(scope)
	d.StatToday, _ = s.store.CountToday(scope)
	d.StatPV, d.StatUV, _ = s.store.TotalViews()

	// 最近页面
	var recent []*model.Page
	recent, _ = s.store.ListPagesPaged(scope, 0, 1, 5)
	_ = s.store.AnnotatePagesWithViews(recent)
	d.RecentPages = recent

	// 访问趋势（近 14 天）——把 PV/UV 合并到同一组数据
	type dv struct {
		Label string `json:"label"`
		PV    int64  `json:"pv"`
		UV    int64  `json:"uv"`
	}
	viewPoints, _ := s.store.DailyViews(14)
	// 用最近 14 天的日期补齐缺失天
	merged := buildDailySeries(14, func(day string) dv {
		for _, p := range viewPoints {
			if p.Day == day {
				return dv{Label: day, PV: p.PV, UV: p.UV}
			}
		}
		return dv{Label: day}
	})
	if b, err := json.Marshal(merged); err == nil {
		d.ChartViews = string(b)
	}

	// 访问权限环形
	sp, _ := s.store.AccessSplit(scope)
	if b, err := json.Marshal([]map[string]any{
		{"label": "公开", "val": sp.Public, "color": "var(--ok)"},
		{"label": "受保护", "val": sp.Protected, "color": "var(--warn)"},
	}); err == nil {
		d.ChartAccess = string(b)
	}

	// 分组页面数条形
	d.ChartGroups = s.buildGroupCounts(u)
	for _, g := range d.ChartGroups {
		if g.Count > d.MaxGroupCount {
			d.MaxGroupCount = g.Count
		}
	}
	if d.MaxGroupCount == 0 {
		d.MaxGroupCount = 1
	}

	// 热门页面
	d.TopPages, _ = s.store.TopPagesByViews(5, scope)

	s.renderAdmin(w, "dashboard.html", d)
}

// buildGroupCounts 组装仪表盘分组页面数列表。
func (s *Server) buildGroupCounts(u *model.User) []*model.GroupCount {
	var gs []*model.Group
	if u.IsAdmin() {
		gs, _ = s.store.ListAllGroups()
	} else {
		gs, _ = s.store.ListGroupsByOwner(u.ID)
	}
	out := make([]*model.GroupCount, 0, len(gs))
	for _, g := range gs {
		out = append(out, &model.GroupCount{Name: g.Name, Count: g.PageCount})
	}
	return out
}

// CountAllGroups 返回全部分组数。
func (s *Server) countAllGroups() int {
	gs, _ := s.store.ListAllGroups()
	return len(gs)
}

// buildDailySeries 生成最近 n 天的连续日期序列，并对每天调用 pick 填充数据点。
func buildDailySeries[T any](n int, pick func(day string) T) []T {
	out := make([]T, 0, n)
	now := time.Now()
	for i := n - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, pick(day))
	}
	return out
}

// ----------------------------------------------------------------------------
// 页面管理
// ----------------------------------------------------------------------------

func (s *Server) adminPages(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "页面管理", CurrentUser: u, CSRF: currentCSRF(r)}
	d.Flash = s.popFlash(w, r)

	// 管理员且未指定 owner：顶层显示「用户列表」（用户=顶层文件夹）
	if u.IsAdmin() && r.URL.Query().Get("owner") == "" {
		d.Users, _ = s.store.ListUsers()
		d.UserPageCount = s.fillUserPageCount(d.Users)
		d.Title = "用户目录"
		s.renderAdmin(w, "pages.html", d)
		return
	}

	// 解析 owner / group 参数，确定"当前所在目录"
	ownerID := u.ID // 普通用户固定看自己
	if u.IsAdmin() {
		if v := r.URL.Query().Get("owner"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				ownerID = id
				d.FilterOwner = id
				if vu, _ := s.store.UserByID(id); vu != nil {
					d.ViewedUser = vu
				}
			}
		}
	}
	var groupID int64
	if v := r.URL.Query().Get("group"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			groupID = id
			d.FilterGroup = id
		}
	}
	d.Query = strings.TrimSpace(r.URL.Query().Get("q"))

	// 安全校验：普通用户不能进入他人目录或他人分组
	if !u.IsAdmin() {
		if groupID != 0 {
			if g, _ := s.store.GroupByID(groupID); g == nil || g.OwnerID != u.ID {
				http.Error(w, "无权访问", http.StatusForbidden)
				return
			}
		}
	} else if groupID != 0 {
		// 管理员进入分组时，校验分组归属与 owner 参数一致
		if g, _ := s.store.GroupByID(groupID); g != nil {
			d.CurrentGroup = g
			if g.OwnerID != ownerID {
				ownerID = g.OwnerID // 以分组实际归属为准
				d.FilterOwner = ownerID
				if vu, _ := s.store.UserByID(ownerID); vu != nil {
					d.ViewedUser = vu
				}
			}
		}
	}

	// 面包屑：根 → ... → 当前分组
	d.Breadcrumbs, _ = s.store.GroupPath(groupID)
	// 当前目录下的子文件夹
	d.SubGroups, _ = s.store.ListGroupsByParent(ownerID, groupID)

	// 当前目录下的直接页面（分页）
	d.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if d.Page < 1 {
		d.Page = 1
	}
	const pageSize = 15
	groupFilter := groupID
	if groupID == 0 {
		groupFilter = store.UnGrouped // 根目录：只看未分组页面（group_id IS NULL）
	}
	pages, _ := s.store.ListPagesPaged(ownerID, groupFilter, d.Page, pageSize)
	pages = filterSearch(pages, d.Query)
	_ = s.store.AnnotatePagesWithViews(pages)
	d.Pages = pages
	d.Total, _ = s.store.CountPages(ownerID, groupFilter)
	d.TotalPages = (d.Total + pageSize - 1) / pageSize
	if d.TotalPages < 1 {
		d.TotalPages = 1
	}
	d.PageNums = pageNumbers(d.Page, d.TotalPages)

	// 兼容：模板里移动分组下拉用（全部分组，扁平）
	if u.IsAdmin() {
		d.Groups, _ = s.store.ListAllGroups()
	} else {
		d.Groups, _ = s.store.ListGroupsByOwner(u.ID)
	}

	s.renderAdmin(w, "pages.html", d)
}

// pageNumbers 生成分页器页码数组（含 -1 表示省略号 …）。
func pageNumbers(cur, total int) []int {
	if total <= 7 {
		out := make([]int, total)
		for i := 0; i < total; i++ {
			out[i] = i + 1
		}
		return out
	}
	out := []int{1}
	left := cur - 2
	right := cur + 2
	if left <= 1 {
		left = 2
	}
	if right >= total {
		right = total - 1
	}
	if left > 2 {
		out = append(out, -1)
	}
	for i := left; i <= right; i++ {
		out = append(out, i)
	}
	if right < total-1 {
		out = append(out, -1)
	}
	out = append(out, total)
	return out
}

// fillUserPageCount 为用户列表填充每人页面数。
func (s *Server) fillUserPageCount(users []*model.User) map[int64]int {
	m := make(map[int64]int, len(users))
	for _, u := range users {
		n, _ := s.store.CountPages(u.ID, 0)
		m[u.ID] = n
	}
	return m
}

// filterSearch 按 q 过滤 slug/标题/owner。
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

// adminGroups 已废弃：分组管理与页面管理合并为文件夹浏览。
// 老链接重定向到 /admin/pages（保持向后兼容）。
func (s *Server) adminGroups(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
}

func (s *Server) adminGroupCreate(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
		return
	}
	parentID, _ := strconv.ParseInt(r.FormValue("parent"), 10, 64)
	// 管理员可在指定 owner 下建分组；普通用户只能在自己的 owner 下
	ownerID := u.ID
	if u.IsAdmin() {
		if v, _ := strconv.ParseInt(r.FormValue("owner"), 10, 64); v > 0 {
			ownerID = v
		}
	}
	if _, err := s.store.CreateGroup(ownerID, name, parentID); err != nil {
		s.setFlash(w, "err", "创建失败："+err.Error())
	}
	// 回到创建时的目录视图
	back := "/admin/pages"
	if u.IsAdmin() {
		back += "?owner=" + strconv.FormatInt(ownerID, 10)
		if parentID > 0 {
			back += "&group=" + strconv.FormatInt(parentID, 10)
		}
	} else if parentID > 0 {
		back += "?group=" + strconv.FormatInt(parentID, 10)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
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
	// 取分组的真实 owner（管理员可操作任意用户的分组；普通用户只能操作自己的）
	g, _ := s.store.GroupByID(gid)
	if g == nil {
		http.NotFound(w, r)
		return
	}
	if !u.IsAdmin() && g.OwnerID != u.ID {
		http.Error(w, "无权操作", http.StatusForbidden)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if err := s.store.RenameGroup(gid, g.OwnerID, name); err != nil {
		s.setFlash(w, "err", "改名失败："+err.Error())
	}
	http.Redirect(w, r, groupBackURL(u, g), http.StatusSeeOther)
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
	g, _ := s.store.GroupByID(gid)
	if g == nil {
		http.NotFound(w, r)
		return
	}
	if !u.IsAdmin() && g.OwnerID != u.ID {
		http.Error(w, "无权操作", http.StatusForbidden)
		return
	}
	// 删除后回到父目录（子树会被删，页面上移到父级）
	parentID := g.ParentID
	deleteOK := true
	if err := s.store.DeleteGroup(gid, g.OwnerID); err != nil {
		s.setFlash(w, "err", "删除失败："+err.Error())
		deleteOK = false
	}
	// 删除失败回到当前分组；成功则回到父目录
	var back *model.Group
	if !deleteOK {
		back = g
	} else if parentID != 0 {
		back, _ = s.store.GroupByID(parentID)
	}
	http.Redirect(w, r, groupBackURL(u, back), http.StatusSeeOther)
}

// groupBackURL 生成"回到某分组所在目录"的 URL（管理员带上 owner 参数）。
// parent 为 nil 表示回到根目录。
func groupBackURL(u *model.User, parent *model.Group) string {
	q := ""
	var ownerID int64
	if parent != nil {
		ownerID = parent.OwnerID
		q = "?group=" + strconv.FormatInt(parent.ID, 10)
	}
	if u.IsAdmin() {
		if q == "" {
			q = "?owner=" + strconv.FormatInt(ownerID, 10)
		} else if ownerID != 0 {
			q += "&owner=" + strconv.FormatInt(ownerID, 10)
		}
	}
	return "/admin/pages" + q
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
