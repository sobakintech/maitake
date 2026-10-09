package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Admin struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&n)
	return n, err
}

// CreateFirstAdmin inserts an admin only if none exist yet, so concurrent setup requests can't both succeed.
func (s *Store) CreateFirstAdmin(ctx context.Context, username, passwordHash string) (*Admin, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash, created_at)
		 SELECT ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM admins)`,
		username, passwordHash, unix(now))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Admin{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: fromUnix(unix(now))}, nil
}

func (s *Store) AdminByUsername(ctx context.Context, username string) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, created_at FROM admins WHERE username = ?", username))
}

func (s *Store) scanAdmin(row *sql.Row) (*Admin, error) {
	var a Admin
	var created int64
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.CreatedAt = fromUnix(created)
	return &a, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, adminID int64, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, admin_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		tokenHash, adminID, unix(now), unix(now.Add(ttl)))
	return err
}

func (s *Store) AdminBySession(ctx context.Context, tokenHash []byte) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRowContext(ctx,
		`SELECT a.id, a.username, a.password_hash, a.created_at
		 FROM sessions s JOIN admins a ON a.id = s.admin_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, unix(time.Now())))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", unix(time.Now()))
	return err
}
