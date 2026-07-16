// Package store 提供 SQLite 元数据存储 + 磁盘 HTML 文件存储。
//
// 设计要点：
//   - 单一 Store 持有 *sql.DB（SQLite），整个进程共享。
//   - HTML 内容落盘到 pagesDir，元数据进 SQLite，便于整体备份（拷 data/ 目录）。
//   - slug 默认随机 base62(6 位)；token 随机 32 字节 hex。
package store

import (
	"crypto/rand"
	"database/sql"
	_ "embed" // 用于编译期嵌入 schema.sql
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，免 CGO；通过 init 注册 "sqlite" 驱动名到 database/sql
	"golang.org/x/crypto/bcrypt"

	"html-site/internal/model"
)

//go:embed schema.sql
var schemaSQL string

// 常见错误，供上层做语义判断。
var (
	ErrNotFound        = errors.New("not found")
	ErrSlugTaken       = errors.New("slug already taken")
	ErrUserExists      = errors.New("user already exists")
	ErrUserNotFound    = errors.New("user not found")
)

const (
	base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	slugLength     = 6
	tokenBytes     = 32 // → 64 个十六进制字符
)

// Store 聚合了数据库句柄与磁盘根目录。
type Store struct {
	db       *sql.DB
	dataDir  string // 例如 .../data
	pagesDir string // = dataDir/pages
}

// Open 打开（或创建）位于 dataDir 的数据库，并执行 schema 迁移。
// dataDir 不存在时会自动创建。
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	pagesDir := filepath.Join(dataDir, "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create pages dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "app.db")
	// modernc.org/sqlite 注册的驱动名是 "sqlite"。
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite 并发写时建议开启 WAL，并限制连接数为 1（写串行化，避免锁错误）。
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	st := &Store{db: db, dataDir: dataDir, pagesDir: pagesDir}
	// 对历史库补齐新列/索引（新建库无影响，幂等）
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error { return s.db.Close() }

// PagesDir 返回 HTML 文件存放目录（绝对路径）。
func (s *Store) PagesDir() string { return s.pagesDir }

// AbsFilePath 把存储的相对 file_path 转成绝对路径。
func (s *Store) AbsFilePath(rel string) string {
	return filepath.Join(s.pagesDir, rel)
}

// ----------------------------------------------------------------------------
// 随机串生成
// ----------------------------------------------------------------------------

// randomToken 生成 32 字节的十六进制 token（64 字符）。
func randomToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// randomSlug 生成 6 位 base62 slug。
func randomSlug() (string, error) {
	b := make([]byte, slugLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, slugLength)
	for i, v := range b {
		out[i] = base62Alphabet[int(v)%len(base62Alphabet)]
	}
	return string(out), nil
}

// UniqueSlug 不断重试直到拿到一个数据库中尚不存在的随机 slug。
func (s *Store) UniqueSlug() (string, error) {
	for i := 0; i < 20; i++ {
		slug, err := randomSlug()
		if err != nil {
			return "", err
		}
		var exists int
		err = s.db.QueryRow(`SELECT COUNT(1) FROM pages WHERE slug = ?`, slug).Scan(&exists)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			return slug, nil
		}
	}
	return "", errors.New("failed to generate unique slug after retries")
}

// NormalizeSlug 校验自定义 slug：只允许字母数字与 -_，长度 1..64。
func NormalizeSlug(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil // 空表示走随机
	}
	if len(s) > 64 {
		return "", errors.New("slug too long (max 64)")
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", fmt.Errorf("slug contains invalid character %q (allowed: A-Z a-z 0-9 - _)", r)
		}
	}
	return s, nil
}

// ----------------------------------------------------------------------------
// User 操作
// ----------------------------------------------------------------------------

// CreateUser 新建 owner，返回带 token 的完整对象。token 仅此处回显一次。
//
// role 为空时默认 'user'；若当前库无任何用户则自动升为首个 admin。
// password 明文，内部 bcrypt 后存储；为空则不设密码（仅能用 token，不能登录后台）。
func (s *Store) CreateUser(name, role, password string) (*model.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("user name required")
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	hash := ""
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		hash = string(h)
	}
	if role == "" {
		role = model.RoleUser
	}
	// 首个用户自动成为管理员
	if role != model.RoleAdmin {
		var cnt int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&cnt); err != nil {
			return nil, err
		}
		if cnt == 0 {
			role = model.RoleAdmin
		}
	}
	res, err := s.db.Exec(
		`INSERT INTO users(name, token, password_hash, role) VALUES(?, ?, ?, ?)`,
		name, token, hash, role,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			if strings.Contains(err.Error(), "name") {
				return nil, ErrUserExists
			}
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.User{ID: id, Name: name, Token: token, Role: role, HasPassword: password != "", CreatedAt: time.Now()}, nil
}

// scanUser 是统一的 user 行扫描，含 role/password_hash。
func scanUser(scanner interface {
	Scan(dest ...any) error
}, u *model.User) error {
	var hash string
	err := scanner.Scan(&u.ID, &u.Name, &u.Token, &hash, &u.Role, &u.CreatedAt)
	u.PasswordHash = hash
	u.HasPassword = hash != ""
	return err
}

// UserByToken 通过 token 查找 owner。命中返回用户，否则 ErrUserNotFound。
func (s *Store) UserByToken(token string) (*model.User, error) {
	if token == "" {
		return nil, ErrUserNotFound
	}
	u := &model.User{}
	err := scanUser(s.db.QueryRow(
		`SELECT id, name, token, password_hash, role, created_at FROM users WHERE token = ?`, token,
	), u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UserByName 按名查找（管理命令用）。
func (s *Store) UserByName(name string) (*model.User, error) {
	u := &model.User{}
	err := scanUser(s.db.QueryRow(
		`SELECT id, name, token, password_hash, role, created_at FROM users WHERE name = ?`, name,
	), u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UserByID 按主键查找。
func (s *Store) UserByID(id int64) (*model.User, error) {
	u := &model.User{}
	err := scanUser(s.db.QueryRow(
		`SELECT id, name, token, password_hash, role, created_at FROM users WHERE id = ?`, id,
	), u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers 列出全部 owner（不含 token，避免泄漏）。
func (s *Store) ListUsers() ([]*model.User, error) {
	rows, err := s.db.Query(
		`SELECT id, name, token, password_hash, role, created_at FROM users ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := scanUser(rows, u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// VerifyPassword 校验用户密码。未设密码或错误均返回 false。
func (s *Store) VerifyPassword(u *model.User, password string) bool {
	if u.PasswordHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// SetPassword 设置/修改用户密码（明文传入，内部 bcrypt）。
func (s *Store) SetPassword(userID int64, password string) error {
	if password == "" {
		return errors.New("password required")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(h), userID)
	return err
}

// SetRole 修改用户角色。
func (s *Store) SetRole(userID int64, role string) error {
	_, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, userID)
	return err
}

// RegenerateToken 重新生成用户 API token 并返回新 token。
func (s *Store) RegenerateToken(userID int64) (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`UPDATE users SET token = ? WHERE id = ?`, tok, userID); err != nil {
		return "", err
	}
	return tok, nil
}

// AdminCount 返回管理员数量（用于防止删除最后一个管理员）。
func (s *Store) AdminCount() (int, error) {
	var cnt int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE role = ?`, model.RoleAdmin).Scan(&cnt)
	return cnt, err
}

// DeleteUser 删除用户。关联的 pages/groups/sessions 由 ON DELETE CASCADE 处理。
// 磁盘 HTML 文件需要调用方单独清理（按 owner）。
func (s *Store) DeleteUser(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	return err
}

// ----------------------------------------------------------------------------
// Page 操作
// ----------------------------------------------------------------------------

// CreatePage 把 HTML 内容落盘并写入元数据，返回新建的 Page。
//
// shareCode 为空字符串表示“公开访问”。slug 为空时自动生成。
// groupName 非空时把页面归入该分组（不存在则创建）。
func (s *Store) CreatePage(ownerID int64, content, title, shareCode, slug, groupName string) (*model.Page, error) {
	// 1. 解析 slug
	slug, err := NormalizeSlug(slug)
	if err != nil {
		return nil, err
	}
	if slug == "" {
		slug, err = s.UniqueSlug()
		if err != nil {
			return nil, err
		}
	} else {
		// 自定义 slug 需先确认未占用
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM pages WHERE slug=?`, slug).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			return nil, ErrSlugTaken
		}
	}

	// 2. 解析分组（可选）
	var groupID any // 传 nil 给 NULL 列
	if groupName = strings.TrimSpace(groupName); groupName != "" {
		gid, err := s.ensureGroup(ownerID, groupName)
		if err != nil {
			return nil, err
		}
		groupID = gid
	}

	relPath := slug + ".html"
	absPath := s.AbsFilePath(relPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write html file: %w", err)
	}

	res, err := s.db.Exec(
		`INSERT INTO pages(slug, owner_id, title, share_code, file_path, size_bytes, group_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		slug, ownerID, title, shareCode, relPath, len(content), groupID,
	)
	if err != nil {
		// 写库失败要回滚磁盘文件
		_ = os.Remove(absPath)
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.getPageByCond(`p.id = ?`, id)
}

// PageBySlug 按 slug 查询（含 owner 名）。
func (s *Store) PageBySlug(slug string) (*model.Page, error) {
	return s.getPageByCond(`p.slug = ?`, slug)
}

// PageByID 按主键查询。
func (s *Store) PageByID(id int64) (*model.Page, error) {
	return s.getPageByCond(`p.id = ?`, id)
}

func (s *Store) getPageByCond(where string, arg any) (*model.Page, error) {
	q := `SELECT p.id, p.slug, p.owner_id, u.name, p.title, p.share_code, p.file_path,
	             p.size_bytes, p.group_id, g.name, p.created_at, p.updated_at
	      FROM pages p
	      JOIN users u ON u.id = p.owner_id
	      LEFT JOIN groups g ON g.id = p.group_id
	      WHERE ` + where
	p := &model.Page{}
	var shareCode, groupName sql.NullString
	var groupID sql.NullInt64
	err := s.db.QueryRow(q, arg).Scan(
		&p.ID, &p.Slug, &p.OwnerID, &p.OwnerName, &p.Title, &shareCode, &p.FilePath,
		&p.SizeBytes, &groupID, &groupName, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.ShareCode = shareCode.String
	p.HasCode = shareCode.String != ""
	p.GroupID = groupID.Int64
	p.GroupName = groupName.String
	return p, nil
}

// ListPagesByOwner 列出某 owner 的全部页面（按更新时间倒序）。
// groupFilter 非空时只返回属于该 groupID 的页面；为 -1 时只返回未分组页面；为 0 不筛选。
func (s *Store) ListPagesByOwner(ownerID int64, groupFilter int64) ([]*model.Page, error) {
	q := `SELECT p.id, p.slug, p.owner_id, u.name, p.title, p.share_code, p.file_path,
	              p.size_bytes, p.group_id, g.name, p.created_at, p.updated_at
	       FROM pages p
	       JOIN users u ON u.id = p.owner_id
	       LEFT JOIN groups g ON g.id = p.group_id
	       WHERE p.owner_id = ?`
	args := []any{ownerID}
	if groupFilter > 0 {
		q += " AND p.group_id = ?"
		args = append(args, groupFilter)
	} else if groupFilter == UnGrouped {
		q += " AND p.group_id IS NULL"
	}
	q += " ORDER BY p.updated_at DESC"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Page
	for rows.Next() {
		p := &model.Page{}
		var shareCode, groupName sql.NullString
		var groupID sql.NullInt64
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.OwnerID, &p.OwnerName, &p.Title, &shareCode, &p.FilePath,
			&p.SizeBytes, &groupID, &groupName, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.ShareCode = shareCode.String
		p.HasCode = shareCode.String != ""
		p.GroupID = groupID.Int64
		p.GroupName = groupName.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// UnGrouped 是 ListPagesByOwner 的特殊筛选值：只看未分组页面。
const UnGrouped int64 = -1

// ListAllPages 列出全部用户的页面（仅管理员用），按更新时间倒序。
func (s *Store) ListAllPages() ([]*model.Page, error) {
	q := `SELECT p.id, p.slug, p.owner_id, u.name, p.title, p.share_code, p.file_path,
	              p.size_bytes, p.group_id, g.name, p.created_at, p.updated_at
	       FROM pages p
	       JOIN users u ON u.id = p.owner_id
	       LEFT JOIN groups g ON g.id = p.group_id
	       ORDER BY p.updated_at DESC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Page
	for rows.Next() {
		p := &model.Page{}
		var shareCode, groupName sql.NullString
		var groupID sql.NullInt64
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.OwnerID, &p.OwnerName, &p.Title, &shareCode, &p.FilePath,
			&p.SizeBytes, &groupID, &groupName, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.ShareCode = shareCode.String
		p.HasCode = shareCode.String != ""
		p.GroupID = groupID.Int64
		p.GroupName = groupName.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePageOptions 描述一次更新涉及哪些字段。
// Content/Title 为 nil 表示不改；ShareCode 相关：当 SetShareCode=true 时
// 使用 ShareCode 的值（空字符串 = 改为公开）。
type UpdatePageOptions struct {
	Content      *string
	Title        *string
	SetShareCode bool
	ShareCode    string
}

// UpdatePage 修改已存在页面。返回更新后的 Page。
func (s *Store) UpdatePage(pageID int64, opts UpdatePageOptions) (*model.Page, error) {
	cur, err := s.PageByID(pageID)
	if err != nil {
		return nil, err
	}

	// 内容变更 → 覆盖磁盘文件
	if opts.Content != nil {
		abs := s.AbsFilePath(cur.FilePath)
		if err := os.WriteFile(abs, []byte(*opts.Content), 0o644); err != nil {
			return nil, fmt.Errorf("overwrite html file: %w", err)
		}
	}

	// 拼接 UPDATE 语句：只 SET 提供了的字段
	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	if opts.Content != nil {
		sets = append(sets, "size_bytes = ?")
		args = append(args, len(*opts.Content))
	}
	if opts.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *opts.Title)
	}
	if opts.SetShareCode {
		sets = append(sets, "share_code = ?")
		args = append(args, opts.ShareCode)
	}
	args = append(args, pageID)
	q := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := s.db.Exec(q, args...); err != nil {
		return nil, err
	}
	return s.PageByID(pageID)
}

// DeletePage 删除页面：先删磁盘文件，再删数据库记录。
func (s *Store) DeletePage(pageID int64) error {
	cur, err := s.PageByID(pageID)
	if err != nil {
		return err
	}
	abs := s.AbsFilePath(cur.FilePath)
	_ = os.Remove(abs) // 文件缺失不算致命错误
	if _, err := s.db.Exec(`DELETE FROM pages WHERE id = ?`, pageID); err != nil {
		return err
	}
	return nil
}

// ReadPageContent 读取某页面对应的 HTML 文件原始内容。
func (s *Store) ReadPageContent(p *model.Page) ([]byte, error) {
	return os.ReadFile(s.AbsFilePath(p.FilePath))
}

// DeleteUserFiles 删除某用户拥有的全部 HTML 磁盘文件（删用户前调用）。
func (s *Store) DeleteUserFiles(ownerID int64) error {
	rows, err := s.db.Query(`SELECT file_path FROM pages WHERE owner_id = ?`, ownerID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, p)
	}
	rows.Close()
	for _, p := range paths {
		_ = os.Remove(s.AbsFilePath(p))
	}
	return nil
}
