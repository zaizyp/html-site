// migrate.go：数据库幂等迁移。
//
// 职责：
//  1. 给历史库（第一期建表）补齐新列：users.role、users.password_hash、pages.group_id
//  2. 创建全部索引（schema.sql 不再建索引，统一在此管理，避免旧库缺列时 CREATE INDEX 失败）
//
// 执行顺序：schema.sql（建表，对旧库无影响）→ migrate（补列 + 建索引）。
// 幂等，可安全地在每次启动时调用。
package store

import (
	"database/sql"
	"strings"

	"html-site/internal/model"
)

// migrate 在已应用 schema.sql 的基础上，补齐历史库缺失的列与索引。
func (s *Store) migrate() error {
	// 1. users 表补列
	if err := addColumnIfMissing(s.db, "users", "role", "TEXT NOT NULL DEFAULT 'user'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "users", "password_hash", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 2. pages 表补 group_id 列（必须在引用它的索引创建之前）
	if err := addColumnIfMissing(s.db, "pages", "group_id", "INTEGER REFERENCES groups(id) ON DELETE SET NULL"); err != nil {
		return err
	}
	// 3. 补建全部索引（幂等，CREATE INDEX IF NOT EXISTS）
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_pages_owner ON pages(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_pages_slug  ON pages(slug);`,
		`CREATE INDEX IF NOT EXISTS idx_pages_group ON pages(group_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_exp  ON sessions(expires_at);`,
	}
	for _, idx := range indexes {
		if _, err := s.db.Exec(idx); err != nil {
			return err
		}
	}
	// 4. 迁移兜底：若没有任何管理员（旧库迁移后 role 全为 user），把最早的用户提升为 admin。
	// 否则旧库的唯一用户迁移后无法进入用户管理后台。
	var adminCnt int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE role = ?`, model.RoleAdmin).Scan(&adminCnt); err != nil {
		return err
	}
	if adminCnt == 0 {
		if _, err := s.db.Exec(
			`UPDATE users SET role = ? WHERE id = (SELECT MIN(id) FROM users)`,
			model.RoleAdmin,
		); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing 检测列是否存在，缺失则 ALTER TABLE ADD COLUMN。
// SQLite 的 ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS，需手动检测。
func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	exists, err := hasColumn(db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	q := "ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl
	if _, err := db.Exec(q); err != nil {
		// 并发启动多实例时可能同时 ALTER，忽略“列已存在”类错误
		if strings.Contains(err.Error(), "duplicate column") {
			return nil
		}
		return err
	}
	return nil
}

// hasColumn 用 PRAGMA table_info 查列是否存在。
func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
