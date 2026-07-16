// session_store.go：后台登录 session 的 CRUD。
package store

import (
	"database/sql"
	"errors"
	"time"

	"html-site/internal/model"
)

// ErrSessionNotFound session 不存在或已过期。
var ErrSessionNotFound = errors.New("session not found")

// CreateSession 为 userID 创建一个新 session，返回带 token/csrf 的 Session。
func (s *Store) CreateSession(userID int64) (*model.Session, error) {
	tok, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	exp := now.Add(model.SessionTTL)
	res, err := s.db.Exec(
		`INSERT INTO sessions(user_id, token, csrf, expires_at) VALUES(?, ?, ?, ?)`,
		userID, tok, csrf, exp,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Session{
		ID: id, UserID: userID, Token: tok, CSRF: csrf, ExpiresAt: exp, CreatedAt: now,
	}, nil
}

// SessionByToken 通过 cookie token 查找有效 session（含 user 信息）。
// 已过期的 session 视为不存在（并顺带清理）。
func (s *Store) SessionByToken(token string) (*model.Session, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}
	sess := &model.Session{}
	err := s.db.QueryRow(
		`SELECT id, user_id, token, csrf, expires_at, created_at
		 FROM sessions WHERE token = ?`, token,
	).Scan(&sess.ID, &sess.UserID, &sess.Token, &sess.CSRF, &sess.ExpiresAt, &sess.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.DeleteSession(token)
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// DeleteSession 删除某个 session（按 token）。
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// PurgeExpiredSessions 清理所有过期 session。建议定期调用。
func (s *Store) PurgeExpiredSessions() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
