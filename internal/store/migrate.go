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
	"fmt"
	"os"
	"path/filepath"
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
		`CREATE INDEX IF NOT EXISTS idx_views_page ON page_views(page_id);`,
		`CREATE INDEX IF NOT EXISTS idx_views_time ON page_views(viewed_at);`,
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

	// 5. 文件路径迁移：把旧的平铺 file_path（形如 "abc123.html"，不含 /）
	// 搬到二级目录 u<owner>/g<group>/<slug>.html，并更新 file_path 列。
	// 幂等：已是二级路径（含 /）的记录跳过。
	if err := s.migrateFilePaths(); err != nil {
		return err
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

// migrateFilePaths 把历史平铺的 page file_path（如 "abc123.html"）搬到
// 二级目录 u<owner>/g<group>/<slug>.html。已是二级路径的记录跳过，幂等。
//
// 逐行处理：读出 owner_id/group_id/slug/file_path，若 file_path 不含 '/'，
// 则构造新路径、mkdir、移动文件、UPDATE。
func (s *Store) migrateFilePaths() error {
	rows, err := s.db.Query(`SELECT id, owner_id, group_id, slug, file_path FROM pages`)
	if err != nil {
		return err
	}
	type rec struct {
		id      int64
		owner   int64
		groupID int64
		slug    string
		oldRel  string
	}
	var todo []rec
	for rows.Next() {
		var r rec
		var gid sql.NullInt64
		if err := rows.Scan(&r.id, &r.owner, &gid, &r.slug, &r.oldRel); err != nil {
			rows.Close()
			return err
		}
		if gid.Valid {
			r.groupID = gid.Int64
		}
		if !isNestedPath(r.oldRel) {
			todo = append(todo, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range todo {
		newRel := pageRelPath(r.owner, r.groupID, r.slug)
		oldAbs := s.AbsFilePath(r.oldRel)
		newAbs := s.AbsFilePath(newRel)
		if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
			return fmt.Errorf("migrate: mkdir %s: %w", filepath.Dir(newAbs), err)
		}
		// 文件不存在（历史数据丢失）则跳过物理移动，只更新路径
		if _, statErr := os.Stat(oldAbs); statErr == nil {
			if err := os.Rename(oldAbs, newAbs); err != nil {
				data, rerr := os.ReadFile(oldAbs)
				if rerr == nil {
					if werr := os.WriteFile(newAbs, data, 0o644); werr == nil {
						_ = os.Remove(oldAbs)
					}
				}
			}
		}
		if _, err := s.db.Exec(`UPDATE pages SET file_path = ? WHERE id = ?`, newRel, r.id); err != nil {
			return err
		}
	}
	return nil
}
