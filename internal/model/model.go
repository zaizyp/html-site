// Package model 定义贯穿全系统的数据结构。
package model

import "time"

// 角色常量。
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User 表示一个拥有者（人或 agent）。
//   - 通过 Token 访问 /api/*（CLI / skill 用）
//   - 通过 PasswordHash 登录后台（浏览器用）
type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Token        string    `json:"token,omitempty"`        // 仅在创建/重置时回显一次
	PasswordHash string    `json:"-"`                      // 永不对外暴露
	Role         string    `json:"role"`                   // admin | user
	HasPassword  bool      `json:"has_password"`           // 是否已设密码（决定能否登录后台）
	CreatedAt    time.Time `json:"created_at"`
}

// IsAdmin 是否管理员。
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// Page 表示一份托管的单文件 HTML 页面。
type Page struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	OwnerID   int64     `json:"owner_id"`
	OwnerName string    `json:"owner_name,omitempty"`
	Title     string    `json:"title"`
	ShareCode string    `json:"share_code,omitempty"`
	HasCode   bool      `json:"has_share_code"`
	FilePath  string    `json:"-"`
	SizeBytes int64     `json:"size_bytes"`
	GroupID   int64     `json:"group_id,omitempty"` // 0 = 未分组
	GroupName string    `json:"group_name,omitempty"`
	Views     int64     `json:"views,omitempty"`  // PV，列表展示用（按需填充）
	UV        int64     `json:"uv,omitempty"`     // UV，列表展示用（按需填充）
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreatePageRequest 是 POST /api/pages 的请求体。
type CreatePageRequest struct {
	Content   string `json:"content"`
	Title     string `json:"title,omitempty"`
	ShareCode string `json:"share_code,omitempty"` // 空字符串 = 公开
	Slug      string `json:"slug,omitempty"`       // 留空则随机生成
	GroupName string `json:"group,omitempty"`      // 可选；分组不存在则自动创建
}

// CreatePageResponse 是 POST /api/pages 的响应体。
type CreatePageResponse struct {
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	ShareCode string `json:"share_code,omitempty"`
	Public    bool   `json:"public"`
	Group     string `json:"group,omitempty"`
}

// UpdatePageRequest 是 PUT /api/pages/{slug} 的请求体。
type UpdatePageRequest struct {
	Content      *string `json:"content,omitempty"`
	Title        *string `json:"title,omitempty"`
	ShareCodeSet bool    `json:"share_code_set,omitempty"`
	ShareCode    string  `json:"share_code,omitempty"`
}

// Group 单层分组。
type Group struct {
	ID        int64     `json:"id"`
	OwnerID   int64     `json:"owner_id"`
	OwnerName string    `json:"owner_name,omitempty"`
	Name      string    `json:"name"`
	PageCount int       `json:"page_count,omitempty"` // 列表展示用
	CreatedAt time.Time `json:"created_at"`
}

// Session 后台登录会话。
type Session struct {
	ID        int64     `json:"-"`
	UserID    int64     `json:"user_id"`
	Token     string    `json:"-"`
	CSRF      string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionTTL session 有效期。
const SessionTTL = 30 * 24 * time.Hour

// GroupCount 用于仪表盘分组页面数条形图。
type GroupCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AccessSplit 用于仪表盘访问权限环形图。
type AccessSplit struct {
	Public   int `json:"public"`
	Protected int `json:"protected"`
}
