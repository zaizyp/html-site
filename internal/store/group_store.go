// group_store.go：分组（单层）的 CRUD。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"html-site/internal/model"
)

// ErrGroupExists 同一 owner 下分组名已存在。
var ErrGroupExists = errors.New("group already exists")

// ensureGroup 查找或创建某 owner 下的分组，返回 group_id。
func (s *Store) ensureGroup(ownerID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("group name required")
	}
	var gid int64
	err := s.db.QueryRow(`SELECT id FROM groups WHERE owner_id=? AND name=?`, ownerID, name).Scan(&gid)
	if err == nil {
		return gid, nil // 已存在
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO groups(owner_id, name) VALUES(?, ?)`, ownerID, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrGroupExists
		}
		return 0, err
	}
	gid, _ = res.LastInsertId()
	return gid, nil
}

// CreateGroup 显式创建分组（已存在则报错）。
func (s *Store) CreateGroup(ownerID int64, name string) (*model.Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name required")
	}
	res, err := s.db.Exec(`INSERT INTO groups(owner_id, name) VALUES(?, ?)`, ownerID, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrGroupExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GroupByID(id)
}

// GroupByID 查询分组（含 owner 名 + 页面计数）。
func (s *Store) GroupByID(id int64) (*model.Group, error) {
	g := &model.Group{}
	err := s.db.QueryRow(
		`SELECT g.id, g.owner_id, u.name, g.name, g.created_at
		 FROM groups g JOIN users u ON u.id = g.owner_id
		 WHERE g.id = ?`, id,
	).Scan(&g.ID, &g.OwnerID, &g.OwnerName, &g.Name, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// ListGroupsByOwner 列出某 owner 的全部分组（含每组页面计数）。
func (s *Store) ListGroupsByOwner(ownerID int64) ([]*model.Group, error) {
	rows, err := s.db.Query(
		`SELECT g.id, g.owner_id, u.name, g.name, g.created_at,
		        (SELECT COUNT(1) FROM pages p WHERE p.group_id = g.id)
		 FROM groups g JOIN users u ON u.id = g.owner_id
		 WHERE g.owner_id = ?
		 ORDER BY g.name`, ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Group
	for rows.Next() {
		g := &model.Group{}
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.OwnerName, &g.Name, &g.CreatedAt, &g.PageCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListAllGroups 列出全部分组（管理员视角）。
func (s *Store) ListAllGroups() ([]*model.Group, error) {
	rows, err := s.db.Query(
		`SELECT g.id, g.owner_id, u.name, g.name, g.created_at,
		        (SELECT COUNT(1) FROM pages p WHERE p.group_id = g.id)
		 FROM groups g JOIN users u ON u.id = g.owner_id
		 ORDER BY u.name, g.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Group
	for rows.Next() {
		g := &model.Group{}
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.OwnerName, &g.Name, &g.CreatedAt, &g.PageCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RenameGroup 重命名分组（同 owner 下名字唯一）。
func (s *Store) RenameGroup(groupID, ownerID int64, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("group name required")
	}
	res, err := s.db.Exec(
		`UPDATE groups SET name = ? WHERE id = ? AND owner_id = ?`,
		newName, groupID, ownerID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrGroupExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGroup 删除分组。其下页面的 group_id 由 ON DELETE SET NULL 自动置空。
func (s *Store) DeleteGroup(groupID, ownerID int64) error {
	res, err := s.db.Exec(`DELETE FROM groups WHERE id = ? AND owner_id = ?`, groupID, ownerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MovePageToGroup 把页面移到某分组（groupID=0 表示移出分组）。
// 同时把磁盘 HTML 文件从旧的 g<oldGroup> 目录物理移动到新 g<groupID> 目录，
// 并更新 file_path 列，保持「文件按 用户+分组 二级目录」的存储约定。
func (s *Store) MovePageToGroup(pageID, groupID int64) error {
	cur, err := s.PageByID(pageID)
	if err != nil {
		return err
	}

	var arg any = groupID
	if groupID == 0 {
		arg = nil
	}

	// 物理移动文件（仅在当前 file_path 是二级路径时才移动；
	// 平铺路径属于尚未迁移的旧库，交给 migrate.go 处理，这里跳过物理移动避免误删）。
	if isNestedPath(cur.FilePath) {
		newRel := pageRelPath(cur.OwnerID, groupID, cur.Slug)
		oldAbs := s.AbsFilePath(cur.FilePath)
		newAbs := s.AbsFilePath(newRel)
		if oldAbs != newAbs {
			if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
				return fmt.Errorf("create target dir: %w", err)
			}
			if err := os.Rename(oldAbs, newAbs); err != nil {
				// Rename 跨设备会失败，回退到 复制+删除
				data, rerr := os.ReadFile(oldAbs)
				if rerr != nil {
					return fmt.Errorf("read page file for move: %w", rerr)
				}
				if werr := os.WriteFile(newAbs, data, 0o644); werr != nil {
					return fmt.Errorf("write page file for move: %w", werr)
				}
				_ = os.Remove(oldAbs)
			}
			// 更新 file_path
			if _, err := s.db.Exec(`UPDATE pages SET file_path = ? WHERE id = ?`, newRel, pageID); err != nil {
				return err
			}
		}
	}

	res, err := s.db.Exec(
		`UPDATE pages SET group_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		arg, pageID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
