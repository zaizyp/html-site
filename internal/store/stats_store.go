// stats_store.go：仪表盘统计 + 分页 + 批量 + 页面元信息修改 等查询。
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"html-site/internal/model"
)

// DailyCreatePoint 一天的新增页面数。
type DailyCreatePoint struct {
	Day   string
	Count int
}

// DailyCreates 返回最近 days 天每天的新增页面数（按 created_at 聚合）。
// scopeOwner=0 表示全站；非 0 限定该 owner。
func (s *Store) DailyCreates(days, scopeOwner int64) ([]DailyCreatePoint, error) {
	if days <= 0 {
		days = 14
	}
	q := `SELECT substr(created_at,1,4)||'-'||substr(created_at,6,2)||'-'||substr(created_at,9,2) AS d, COUNT(1)
	      FROM pages WHERE created_at >= datetime('now', ?)`
	args := []any{"-" + intToStr(days) + " days"}
	if scopeOwner != 0 {
		q += " AND owner_id = ?"
		args = append(args, scopeOwner)
	}
	q += " GROUP BY d ORDER BY d"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyCreatePoint
	for rows.Next() {
		var p DailyCreatePoint
		if err := rows.Scan(&p.Day, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// intToStr 简易 int64 -> 字符串（用于拼 SQLite datetime 参数）。
func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TotalStorage 返回全部页面占用字节数（SUM(size_bytes)）。scopeOwner=0 全站。
func (s *Store) TotalStorage(scopeOwner int64) (int64, error) {
	q := `SELECT COALESCE(SUM(size_bytes),0) FROM pages`
	args := []any{}
	if scopeOwner != 0 {
		q += " WHERE owner_id = ?"
		args = append(args, scopeOwner)
	}
	var n int64
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// CountToday 返回今天新增的页面数。scopeOwner=0 全站。
func (s *Store) CountToday(scopeOwner int64) (int, error) {
	q := `SELECT COUNT(1) FROM pages WHERE date(created_at)=date('now')`
	args := []any{}
	if scopeOwner != 0 {
		q += " AND owner_id = ?"
		args = append(args, scopeOwner)
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// AccessSplit 返回公开 / 受保护页面数量。
func (s *Store) AccessSplit(scopeOwner int64) (model.AccessSplit, error) {
	q := `SELECT
	    SUM(CASE WHEN share_code='' OR share_code IS NULL THEN 1 ELSE 0 END),
	    SUM(CASE WHEN share_code!='' AND share_code IS NOT NULL THEN 1 ELSE 0 END)
	    FROM pages`
	args := []any{}
	if scopeOwner != 0 {
		q += " WHERE owner_id = ?"
		args = append(args, scopeOwner)
	}
	var sp model.AccessSplit
	err := s.db.QueryRow(q, args...).Scan(&sp.Public, &sp.Protected)
	return sp, err
}

// CountPages 返回（按筛选条件的）页面总数。ownerID=0 全站；groupFilter 语义同 ListPagesByOwner。
func (s *Store) CountPages(ownerID, groupFilter int64) (int, error) {
	q := `SELECT COUNT(1) FROM pages WHERE 1=1`
	args := []any{}
	if ownerID != 0 {
		q += " AND owner_id = ?"
		args = append(args, ownerID)
	}
	if groupFilter > 0 {
		q += " AND group_id = ?"
		args = append(args, groupFilter)
	} else if groupFilter == UnGrouped {
		q += " AND group_id IS NULL"
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// scanPageRows 把公共的 SELECT 列扫描成 Page（含 owner 名 / 分组名）。
func scanPageRows(rows *sql.Rows) ([]*model.Page, error) {
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

// pageSelectCols 与 scanPageRows 对应的列。
const pageSelectCols = `p.id, p.slug, p.owner_id, u.name, p.title, p.share_code, p.file_path,
              p.size_bytes, p.group_id, g.name, p.created_at, p.updated_at`

// ListPagesPaged 分页列出页面。ownerID=0 全站；返回当前页数据。
func (s *Store) ListPagesPaged(ownerID, groupFilter int64, page, pageSize int) ([]*model.Page, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := `SELECT ` + pageSelectCols + `
	       FROM pages p JOIN users u ON u.id = p.owner_id
	       LEFT JOIN groups g ON g.id = p.group_id WHERE 1=1`
	args := []any{}
	if ownerID != 0 {
		q += " AND p.owner_id = ?"
		args = append(args, ownerID)
	}
	if groupFilter > 0 {
		q += " AND p.group_id = ?"
		args = append(args, groupFilter)
	} else if groupFilter == UnGrouped {
		q += " AND p.group_id IS NULL"
	}
	q += " ORDER BY p.updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPageRows(rows)
}

// UpdatePageMeta 修改页面元信息：title / slug / group_id / share_code。
// 各字段为零值/空字符串时按规则处理（见实现）。
// 改 slug 时会同时改 file_path 并物理重命名文件。
type PageMetaUpdate struct {
	Title       *string
	NewSlug     *string // nil 表示不改 slug
	GroupIDSet  bool
	GroupID     int64 // 0 = 移出分组
	ShareCodeSet bool
	ShareCode   string
}

// UpdatePageMeta 修改页面元信息，返回更新后的 Page。
func (s *Store) UpdatePageMeta(pageID int64, m PageMetaUpdate) (*model.Page, error) {
	cur, err := s.PageByID(pageID)
	if err != nil {
		return nil, err
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}

	if m.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *m.Title)
	}

	// 分组
	if m.GroupIDSet {
		var g any = m.GroupID
		if m.GroupID == 0 {
			g = nil
		}
		sets = append(sets, "group_id = ?")
		args = append(args, g)
	}

	// 分享码
	if m.ShareCodeSet {
		sets = append(sets, "share_code = ?")
		args = append(args, m.ShareCode)
	}

	// slug 改动：校验唯一 + 改 file_path + 物理重命名
	if m.NewSlug != nil && *m.NewSlug != cur.Slug {
		newSlug, err := NormalizeSlug(*m.NewSlug)
		if err != nil {
			return nil, err
		}
		if newSlug == "" {
			return nil, ErrNotFound
		}
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM pages WHERE slug=? AND id<>?`, newSlug, pageID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			return nil, ErrSlugTaken
		}
		// 物理重命名文件（保持目录，只换文件名）
		if isNestedPath(cur.FilePath) {
			dir := dirOf(cur.FilePath)
			newRel := dir + "/" + newSlug + ".html"
			oldAbs := s.AbsFilePath(cur.FilePath)
			newAbs := s.AbsFilePath(newRel)
			if data, rerr := os.ReadFile(oldAbs); rerr == nil {
				if werr := os.WriteFile(newAbs, data, 0o644); werr == nil {
					_ = os.Remove(oldAbs)
				}
			}
			sets = append(sets, "file_path = ?")
			args = append(args, newRel)
		}
		sets = append(sets, "slug = ?")
		args = append(args, newSlug)
	}

	args = append(args, pageID)
	q := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := s.db.Exec(q, args...); err != nil {
		return nil, err
	}
	// 若改了 group_id，需要把文件物理移到新分组目录
	if m.GroupIDSet && isNestedPath(cur.FilePath) {
		// 重新读取以拿到最新 slug/file_path
		updated, _ := s.PageByID(pageID)
		if updated != nil {
			newRel := pageRelPath(updated.OwnerID, m.GroupID, updated.Slug)
			oldAbs := s.AbsFilePath(updated.FilePath)
			newAbs := s.AbsFilePath(newRel)
			if oldAbs != newAbs {
				_ = os.MkdirAll(filepath.Dir(newAbs), 0o755)
				if data, rerr := os.ReadFile(oldAbs); rerr == nil {
					if werr := os.WriteFile(newAbs, data, 0o644); werr == nil {
						_ = os.Remove(oldAbs)
						s.db.Exec(`UPDATE pages SET file_path = ? WHERE id = ?`, newRel, pageID)
					}
				}
			}
		}
	}
	return s.PageByID(pageID)
}

// dirOf 返回相对路径的目录部分（不含文件名）。
func dirOf(rel string) string {
	for i := len(rel) - 1; i >= 0; i-- {
		if rel[i] == '/' {
			return rel[:i]
		}
	}
	return ""
}

// BatchDeletePages 批量删除页面（含磁盘文件）。
func (s *Store) BatchDeletePages(ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		if err := s.DeletePage(id); err == nil {
			n++
		}
	}
	return n, nil
}

// BatchMovePages 批量移动页面到指定分组（groupID=0 移出分组）。
// 仅对当前用户拥有的页面生效，返回实际移动数。
func (s *Store) BatchMovePages(ids []int64, ownerID, groupID int64) (int, error) {
	n := 0
	for _, id := range ids {
		// 校验归属，避免越权
		p, err := s.PageByID(id)
		if err != nil || p.OwnerID != ownerID {
			continue
		}
		if err := s.MovePageToGroup(id, groupID); err == nil {
			n++
		}
	}
	return n, nil
}
