// group_store.go：树形目录（邻接表）的 CRUD + 路径解析。
//
// 数据模型：groups(parent_id) 指向父分组，0=根。depth=0 为根，最深 MaxGroupDepth。
// 磁盘路径仍用单个 group_id（u<owner>/g<group>/<slug>.html），层级关系只在 DB。
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

// MaxGroupDepth 目录最大深度：根 depth=0，最深子节点 depth=4，共 5 层。
const MaxGroupDepth = 4

// ErrGroupExists 同一父分组下名字已存在。
var ErrGroupExists = errors.New("group already exists")

// ErrGroupTooDeep 超过最大目录深度。
var ErrGroupTooDeep = errors.New("目录层级超过 5 层限制")

// ----------------------------------------------------------------------------
// 路径解析（upload --group a/b/c 用）
// ----------------------------------------------------------------------------

// ensureGroup 查找或创建某 owner 下由路径串定位的分组，返回最终 group_id。
//
// path 可为 "a/b/c"（多级，逐层查找或创建）或 "a"（根下单层，向后兼容）。
// 空串返回 0（根/未分组）。分隔符固定为 '/'。
func (s *Store) ensureGroup(ownerID int64, path string) (int64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, nil
	}
	// 拆分路径，逐级查找或创建。parentID 从根(0)开始累积。
	parentID := int64(0)
	depth := 0
	for _, seg := range strings.Split(path, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue // 跳过空段（如 "a//b"）
		}
		// 查找现有子分组
		var gid int64
		err := s.db.QueryRow(
			`SELECT id FROM groups WHERE owner_id=? AND parent_id=? AND name=?`,
			ownerID, parentID, seg,
		).Scan(&gid)
		if err == nil {
			parentID = gid
			depth++
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		// 不存在则创建（受深度限制）
		if depth >= MaxGroupDepth {
			return 0, fmt.Errorf("%w: 不能在 depth=%d 下再建子目录", ErrGroupTooDeep, depth)
		}
		res, err := s.db.Exec(
			`INSERT INTO groups(owner_id, parent_id, name, depth) VALUES (?, ?, ?, ?)`,
			ownerID, parentID, seg, depth+1,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				// 并发创建，重新查一次
				if e2 := s.db.QueryRow(
					`SELECT id FROM groups WHERE owner_id=? AND parent_id=? AND name=?`,
					ownerID, parentID, seg,
				).Scan(&gid); e2 == nil {
					parentID = gid
					depth++
					continue
				}
				return 0, ErrGroupExists
			}
			return 0, err
		}
		gid, _ = res.LastInsertId()
		parentID = gid
		depth++
	}
	return parentID, nil
}

// ----------------------------------------------------------------------------
// 创建 / 查询
// ----------------------------------------------------------------------------

// CreateGroup 显式创建分组。parentID=0 表示在根下创建。
// 校验：父分组归属同一 owner、深度不超限、同父下名字唯一。
func (s *Store) CreateGroup(ownerID int64, name string, parentID int64) (*model.Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name required")
	}
	// 校验父分组（若非根）
	parentDepth := 0
	if parentID != 0 {
		parent, err := s.groupByIDRow(parentID)
		if err != nil {
			return nil, err
		}
		if parent.OwnerID != ownerID {
			return nil, ErrNotFound // 不属于该 owner，统一 404 不暴露
		}
		parentDepth = parent.Depth
	}
	if parentDepth >= MaxGroupDepth {
		return nil, fmt.Errorf("%w: 不能在 depth=%d 下再建子目录", ErrGroupTooDeep, parentDepth)
	}
	res, err := s.db.Exec(
		`INSERT INTO groups(owner_id, parent_id, name, depth) VALUES (?, ?, ?, ?)`,
		ownerID, parentID, name, parentDepth+1,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrGroupExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GroupByID(id)
}

// GroupByID 查询分组（含 owner 名）。返回的 Group 仅填充基础字段（不含 PageCount/ChildCount）。
func (s *Store) GroupByID(id int64) (*model.Group, error) {
	return s.groupByIDRow(id)
}

// groupByIDRow 查询单行基础字段（内部复用）。
func (s *Store) groupByIDRow(id int64) (*model.Group, error) {
	g := &model.Group{}
	err := s.db.QueryRow(
		`SELECT g.id, g.owner_id, g.parent_id, g.depth, u.name, g.name, g.created_at
		 FROM groups g JOIN users u ON u.id = g.owner_id
		 WHERE g.id = ?`, id,
	).Scan(&g.ID, &g.OwnerID, &g.ParentID, &g.Depth, &g.OwnerName, &g.Name, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// ListGroupsByParent 列出某 owner、某父分组下的直接子分组（文件夹浏览用）。
// 同时填充每个子分组的直接子页面数(PageCount)与直接子分组数(ChildCount)。
// parentID=0 表示根。
func (s *Store) ListGroupsByParent(ownerID, parentID int64) ([]*model.Group, error) {
	rows, err := s.db.Query(
		`SELECT g.id, g.owner_id, g.parent_id, g.depth, u.name, g.name, g.created_at,
		        (SELECT COUNT(1) FROM pages p WHERE p.group_id = g.id) AS page_cnt,
		        (SELECT COUNT(1) FROM groups c WHERE c.parent_id = g.id) AS child_cnt
		 FROM groups g JOIN users u ON u.id = g.owner_id
		 WHERE g.owner_id = ? AND g.parent_id = ?
		 ORDER BY g.name`, ownerID, parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Group
	for rows.Next() {
		g := &model.Group{}
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.ParentID, &g.Depth, &g.OwnerName, &g.Name, &g.CreatedAt, &g.PageCount, &g.ChildCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListGroupsByOwner 列出某 owner 的全部分组（扁平带 parent_id，前端可自行组装树）。
// 仅填充基础字段 + 直接子页面数，不填 ChildCount。
func (s *Store) ListGroupsByOwner(ownerID int64) ([]*model.Group, error) {
	rows, err := s.db.Query(
		`SELECT g.id, g.owner_id, g.parent_id, g.depth, u.name, g.name, g.created_at,
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
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.ParentID, &g.Depth, &g.OwnerName, &g.Name, &g.CreatedAt, &g.PageCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListAllGroups 列出全部分组（管理员视角，扁平带 parent_id）。
func (s *Store) ListAllGroups() ([]*model.Group, error) {
	rows, err := s.db.Query(
		`SELECT g.id, g.owner_id, g.parent_id, g.depth, u.name, g.name, g.created_at,
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
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.ParentID, &g.Depth, &g.OwnerName, &g.Name, &g.CreatedAt, &g.PageCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GroupPath 返回从根到该分组的祖先链（含自身），用于面包屑。
// 例：a/b/c 返回 [a, b, c]。groupID=0 返回空切片。
func (s *Store) GroupPath(groupID int64) ([]*model.Group, error) {
	if groupID == 0 {
		return nil, nil
	}
	var chain []*model.Group
	cur := groupID
	// 防御性循环上限（避免环导致死循环）
	for i := 0; i < MaxGroupDepth+2 && cur != 0; i++ {
		g, err := s.groupByIDRow(cur)
		if err != nil {
			return nil, err
		}
		chain = append(chain, g)
		cur = g.ParentID
	}
	// 反转为根在前
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// DescendantGroupIDs 递归收集 groupID 及其所有后代分组的 id（含自身）。
// Go 层递归（不依赖 SQLite CTE，兼容性好）。用于"查看子树全部页面"。
func (s *Store) DescendantGroupIDs(groupID int64) ([]int64, error) {
	result := []int64{groupID}
	stack := []int64{groupID}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		rows, err := s.db.Query(`SELECT id FROM groups WHERE parent_id = ?`, cur)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return nil, err
			}
			result = append(result, cid)
			stack = append(stack, cid)
		}
		rows.Close()
	}
	return result, nil
}

// ----------------------------------------------------------------------------
// 改名 / 删除
// ----------------------------------------------------------------------------

// RenameGroup 重命名分组（同 owner + 同父下名字唯一）。
func (s *Store) RenameGroup(groupID, ownerID int64, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("group name required")
	}
	// 取 parent_id 用于唯一约束判断
	g, err := s.groupByIDRow(groupID)
	if err != nil {
		return err
	}
	if g.OwnerID != ownerID {
		return ErrNotFound
	}
	res, err := s.db.Exec(
		`UPDATE groups SET name = ? WHERE id = ? AND owner_id = ? AND parent_id = ?`,
		newName, groupID, ownerID, g.ParentID,
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

// DeleteGroup 删除分组及其整棵子树。
//
// 页面处理（页面上移）：先把子树（含自身）下所有页面的 group_id 置为该分组的
// parent_id（根分组则置 NULL），再删分组（子分组由应用层逐个删除以同步清理磁盘空目录）。
// 全程在事务内，保证不留孤儿数据。
//
// 注意：不走 DB 的 ON DELETE CASCADE，因为我们要先上移页面再删分组；
// 且 pages.group_id 的外键是 ON DELETE SET NULL（会丢上移目标），故必须在删之前 UPDATE。
func (s *Store) DeleteGroup(groupID, ownerID int64) error {
	g, err := s.groupByIDRow(groupID)
	if err != nil {
		return err
	}
	if g.OwnerID != ownerID {
		return ErrNotFound
	}

	// 收集子树全部 id
	descIDs, err := s.DescendantGroupIDs(groupID)
	if err != nil {
		return err
	}

	// 注意：store 设了 SetMaxOpenConns(1)，单连接模式下 SQL 语句天然序列化，
	// 无需显式事务即可保证原子性语义（且 BeginTx 在单连接+驱动状态下可能 panic）。
	// 故这里按顺序执行：先上移页面，再删子树分组。

	// 1. 把子树下所有页面的 group_id 上移到父级（根则置 NULL）
	newParent := g.ParentID
	var arg any = newParent
	if newParent == 0 {
		arg = nil
	}
	// 用 IN (?, ?, ...) 占位
	phPages := make([]string, len(descIDs))
	argsPages := make([]any, 0, len(descIDs)+1)
	argsPages = append(argsPages, arg)
	for i, id := range descIDs {
		phPages[i] = "?"
		argsPages = append(argsPages, id)
	}
	qUpd := `UPDATE pages SET group_id = ?, updated_at = CURRENT_TIMESTAMP WHERE group_id IN (` + strings.Join(phPages, ",") + `)`
	if _, err := s.db.Exec(qUpd, argsPages...); err != nil {
		return err
	}

	// 2. 删除子树全部分组（groups 表对 parent_id 无自引用外键，可直接按 id 批量删）
	phDel := make([]string, len(descIDs))
	argsDel := make([]any, 0, len(descIDs))
	for i, id := range descIDs {
		phDel[i] = "?"
		argsDel = append(argsDel, id)
	}
	qDel := `DELETE FROM groups WHERE id IN (` + strings.Join(phDel, ",") + `)`
	if _, err := s.db.Exec(qDel, argsDel...); err != nil {
		return err
	}
	return nil
}

// ----------------------------------------------------------------------------
// 页面移动（磁盘文件随 group_id 迁移）
// ----------------------------------------------------------------------------

// MovePageToGroup 把页面移到某分组（groupID=0 表示移出分组/回到根）。
// 同时把磁盘 HTML 文件从旧的 g<oldGroup> 目录物理移动到新 g<groupID> 目录，
// 并更新 file_path 列，保持「文件按 用户+分组 二级目录」的存储约定。
//
// 层级关系不影响磁盘路径（始终用单个 group_id），故此方法无需因树形改造而改动。
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
