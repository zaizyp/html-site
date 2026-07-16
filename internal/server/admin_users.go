// admin_users.go：用户管理（管理员）+ 个人设置（所有登录用户）。
package server

import (
	"net/http"
	"strconv"
	"strings"

	"html-site/internal/model"
)

// ----------------------------------------------------------------------------
// 用户管理（仅管理员）
// ----------------------------------------------------------------------------

// adminUsers 用户列表页。
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "用户管理", CurrentUser: u, CSRF: currentCSRF(r)}
	d.Flash = s.popFlash(w, r)
	users, _ := s.store.ListUsers()
	d.Users = users
	s.renderAdmin(w, "users.html", d)
}

// adminUserCreate 创建用户。
func (s *Server) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	pwd := r.FormValue("password")
	role := r.FormValue("role")
	if role != model.RoleAdmin {
		role = model.RoleUser
	}
	if name == "" || pwd == "" {
		s.setFlash(w, "err", "用户名和密码不能为空")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if _, err := s.store.CreateUser(name, role, pwd); err != nil {
		s.setFlash(w, "err", "创建失败："+err.Error())
	} else {
		s.setFlash(w, "ok", "已创建用户 "+name)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminUserDelete 删除用户。
func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	me := currentUser(r)
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if id == me.ID {
		s.setFlash(w, "err", "不能删除自己")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	target, err := s.store.UserByID(id)
	if err != nil {
		s.setFlash(w, "err", "用户不存在")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	// 防止删除最后一个管理员
	if target.IsAdmin() {
		cnt, _ := s.store.AdminCount()
		if cnt <= 1 {
			s.setFlash(w, "err", "至少保留一个管理员")
			http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			return
		}
	}
	_ = s.store.DeleteUserFiles(id)
	if err := s.store.DeleteUser(id); err != nil {
		s.setFlash(w, "err", "删除失败："+err.Error())
	} else {
		s.setFlash(w, "ok", "已删除用户 "+target.Name)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminUserResetPassword 管理员重置某用户密码（生成随机密码并 flash 显示）。
func (s *Server) adminUserResetPassword(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := s.store.UserByID(id)
	if err != nil {
		s.setFlash(w, "err", "用户不存在")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	pwd, err := randomTokenHex(8) // 16 位
	if err != nil {
		s.setFlash(w, "err", "生成密码失败")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if err := s.store.SetPassword(t.ID, pwd); err != nil {
		s.setFlash(w, "err", "重置失败："+err.Error())
	} else {
		s.setFlash(w, "ok", t.Name+" 的新密码："+pwd)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminUserPromote 升为管理员。
func (s *Server) adminUserPromote(w http.ResponseWriter, r *http.Request) {
	s.changeRole(w, r, model.RoleAdmin)
}
func (s *Server) adminUserDemote(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err == nil && id == me.ID {
		s.setFlash(w, "err", "不能降级自己")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	s.changeRole(w, r, model.RoleUser)
}

func (s *Server) changeRole(w http.ResponseWriter, r *http.Request, role string) {
	if s.verifyCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(pathID(r.URL.Path, 2), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if role == model.RoleAdmin {
		if _, err := s.store.UserByID(id); err != nil {
			s.setFlash(w, "err", "用户不存在")
		} else if err := s.store.SetRole(id, role); err != nil {
			s.setFlash(w, "err", err.Error())
		}
	} else {
		// 降级前确保不删最后一个管理员
		t, err := s.store.UserByID(id)
		if err != nil {
			s.setFlash(w, "err", "用户不存在")
		} else if t.IsAdmin() {
			cnt, _ := s.store.AdminCount()
			if cnt <= 1 {
				s.setFlash(w, "err", "至少保留一个管理员")
			} else if err := s.store.SetRole(id, role); err != nil {
				s.setFlash(w, "err", err.Error())
			}
		} else if err := s.store.SetRole(id, role); err != nil {
			s.setFlash(w, "err", err.Error())
		}
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// ----------------------------------------------------------------------------
// 个人设置
// ----------------------------------------------------------------------------

func (s *Server) adminAccount(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d := adminData{Title: "个人设置", CurrentUser: u, CSRF: currentCSRF(r)}
	d.Flash = s.popFlash(w, r)
	d.MaskedToken = maskToken(u.Token)
	s.renderAdmin(w, "account.html", d)
}

// adminAccountPassword 修改自己密码。
func (s *Server) adminAccountPassword(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	current := r.FormValue("current")
	new := r.FormValue("new")
	confirm := r.FormValue("confirm")
	if !s.store.VerifyPassword(u, current) {
		s.setFlash(w, "err", "当前密码错误")
		http.Redirect(w, r, "/admin/account", http.StatusSeeOther)
		return
	}
	if len(new) < 6 {
		s.setFlash(w, "err", "新密码至少 6 位")
		http.Redirect(w, r, "/admin/account", http.StatusSeeOther)
		return
	}
	if new != confirm {
		s.setFlash(w, "err", "两次输入的新密码不一致")
		http.Redirect(w, r, "/admin/account", http.StatusSeeOther)
		return
	}
	if err := s.store.SetPassword(u.ID, new); err != nil {
		s.setFlash(w, "err", "修改失败："+err.Error())
	} else {
		s.setFlash(w, "ok", "密码已更新")
	}
	http.Redirect(w, r, "/admin/account", http.StatusSeeOther)
}

// adminAccountRegenToken 重新生成自己的 API token，并把新 token flash 给用户。
func (s *Server) adminAccountRegenToken(w http.ResponseWriter, r *http.Request) {
	if s.verifyCSRF(w, r) {
		return
	}
	u := currentUser(r)
	tok, err := s.store.RegenerateToken(u.ID)
	if err != nil {
		s.setFlash(w, "err", "重置失败："+err.Error())
	} else {
		// 注意：重置后当前请求 context 里的 user 还是旧 token，但 session 不受影响
		s.setFlash(w, "ok", "新 token："+tok)
	}
	http.Redirect(w, r, "/admin/account", http.StatusSeeOther)
}

// maskToken 脱敏显示 token（前 8 位 + …）。
func maskToken(t string) string {
	if len(t) <= 8 {
		return t
	}
	return t[:8] + "…"
}

// randomTokenHex 生成 n 字节的十六进制串。定义在此避免与 store 同名冲突。
func randomTokenHex(n int) (string, error) {
	return genRandomHex(n)
}
