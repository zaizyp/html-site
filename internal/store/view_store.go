// view_store.go：页面访问统计（PV/UV）。
//
// 隐私设计：IP 与 User-Agent 只存 sha256 前 16 个十六进制字符，不存明文。
// UV 去重用 ip_hash（同一 IP 视为一个访客；受限于反代是否透传真实 IP）。
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"html-site/internal/model"
)

// hashShort 对字符串取 sha256 前 16 个 hex 字符（8 字节），用于隐私去重。
func hashShort(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// RecordView 记录一次页面访问。异步友好：调用方可放进 goroutine。
// pageID 已由调用方校验存在。
func (s *Store) RecordView(pageID int64, ip, ua string) {
	_, _ = s.db.Exec(
		`INSERT INTO page_views(page_id, ip_hash, ua_hash) VALUES(?, ?, ?)`,
		pageID, hashShort(ip), hashShort(ua),
	)
}

// RecordViewCtx 带 context 的记录，便于在超时/取消时放弃写入。
func (s *Store) RecordViewCtx(ctx context.Context, pageID int64, ip, ua string) {
	s.db.ExecContext(ctx,
		`INSERT INTO page_views(page_id, ip_hash, ua_hash) VALUES(?, ?, ?)`,
		pageID, hashShort(ip), hashShort(ua),
	)
}

// PageViewStats 单页面的 PV/UV。
type PageViewStats struct {
	PageID int64
	PV     int64
	UV     int64
}

// ViewStats 返回单页面的 PV 与 UV。
func (s *Store) ViewStats(pageID int64) (PageViewStats, error) {
	st := PageViewStats{PageID: pageID}
	err := s.db.QueryRow(
		`SELECT COUNT(1), COUNT(DISTINCT ip_hash) FROM page_views WHERE page_id = ?`,
		pageID,
	).Scan(&st.PV, &st.UV)
	if err != nil {
		return st, err
	}
	return st, nil
}

// ViewStatsBatch 批量查询多个页面的 PV/UV，返回 pageID -> stats 的映射。
// 未有访问记录的页面不会出现在结果里（调用方按需补 0）。
func (s *Store) ViewStatsBatch(pageIDs []int64) (map[int64]PageViewStats, error) {
	out := make(map[int64]PageViewStats, len(pageIDs))
	if len(pageIDs) == 0 {
		return out, nil
	}
	// 用 IN 查询；pageIDs 来自内部数据，安全。
	q, args := inQuery(
		`SELECT page_id, COUNT(1), COUNT(DISTINCT ip_hash) FROM page_views WHERE page_id IN (`+placeholders(len(pageIDs))+`) GROUP BY page_id`,
		pageIDs,
	)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st PageViewStats
		if err := rows.Scan(&st.PageID, &st.PV, &st.UV); err != nil {
			return nil, err
		}
		out[st.PageID] = st
	}
	return out, rows.Err()
}

// TotalViews 返回全站总 PV 与总 UV。
func (s *Store) TotalViews() (pv, uv int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(1), COUNT(DISTINCT ip_hash) FROM page_views`).Scan(&pv, &uv)
	return
}

// TopPageByViews 是按访问量排序的页面条目。
type TopPageByViews struct {
	PageID   int64
	Slug     string
	Title    string
	PV       int64
	UV       int64
	OwnerID  int64
	OwnerNm  string
}

// TopPagesByViews 返回访问量最高的 limit 个页面（含 slug/标题/owner）。
// scopeOwner=0 表示全站；非 0 表示限定该 owner。
func (s *Store) TopPagesByViews(limit int, scopeOwner int64) ([]TopPageByViews, error) {
	if limit <= 0 {
		limit = 10
	}
	q := `SELECT p.id, p.slug, p.title, v.pv, v.uv, p.owner_id, u.name
	      FROM (
	        SELECT page_id, COUNT(1) AS pv, COUNT(DISTINCT ip_hash) AS uv
	        FROM page_views GROUP BY page_id
	      ) v
	      JOIN pages p ON p.id = v.page_id
	      JOIN users u ON u.id = p.owner_id`
	args := []any{}
	if scopeOwner != 0 {
		q += " WHERE p.owner_id = ?"
		args = append(args, scopeOwner)
	}
	q += " ORDER BY v.pv DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopPageByViews
	for rows.Next() {
		var t TopPageByViews
		if err := rows.Scan(&t.PageID, &t.Slug, &t.Title, &t.PV, &t.UV, &t.OwnerID, &t.OwnerNm); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DailyViewPoint 一天的聚合点。
type DailyViewPoint struct {
	Day string // YYYY-MM-DD
	PV  int64
	UV  int64
}

// DailyViews 返回最近 days 天每天的 PV/UV。
func (s *Store) DailyViews(days int) ([]DailyViewPoint, error) {
	if days <= 0 {
		days = 7
	}
	// SQLite 的 DATE 视图：把 viewed_at 转成 YYYY-MM-DD 分组
	q := `SELECT substr(viewed_at,1,4)||'-'||substr(viewed_at,6,2)||'-'||substr(viewed_at,9,2) AS d,
	             COUNT(1) AS pv, COUNT(DISTINCT ip_hash) AS uv
	      FROM page_views
	      WHERE viewed_at >= datetime('now', ?)
	      GROUP BY d ORDER BY d`
	rows, err := s.db.Query(q, "-"+itoa(days)+" days")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyViewPoint
	for rows.Next() {
		var p DailyViewPoint
		if err := rows.Scan(&p.Day, &p.PV, &p.UV); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// itoa 简易 int->string，避免引入 strconv（本文件目前未用）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// placeholders 生成 n 个 ? 占位符，用 , 分隔。
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// inQuery 把 []int64 拼成 IN 查询的参数切片。
func inQuery(q string, ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return q, args
}

// 触发 model 包的 time 引用，避免 import 未用（DailyViews 等暂用字符串日期）。
var _ = time.Now

// AnnotatePagesWithViews 把 PV/UV 批量回填到一批 Page 的 Views/UV 字段。
func (s *Store) AnnotatePagesWithViews(pages []*model.Page) error {
	if len(pages) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(pages))
	for _, p := range pages {
		ids = append(ids, p.ID)
	}
	stats, err := s.ViewStatsBatch(ids)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if st, ok := stats[p.ID]; ok {
			p.Views = st.PV
			p.UV = st.UV
		}
	}
	return nil
}
