package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	KeyCount  int       `json:"keyCount"`
}

type UserPatch struct {
	Name     *string `json:"name"`
	Note     *string `json:"note"`
	Disabled *bool   `json:"disabled"`
}

const userColumns = `u.id, u.name, u.note, u.disabled, u.created_at,
	(SELECT COUNT(*) FROM api_keys k WHERE k.user_id = u.id)`

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
	var u User
	var created int64
	if err := sc.Scan(&u.ID, &u.Name, &u.Note, &u.Disabled, &created, &u.KeyCount); err != nil {
		return nil, err
	}
	u.CreatedAt = fromUnix(created)
	return &u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users u ORDER BY u.name COLLATE NOCASE, u.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users u WHERE u.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) CreateUser(ctx context.Context, name, note string) (*User, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO users (name, note, created_at) VALUES (?, ?, ?)", name, note, unix(time.Now()))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) UpdateUser(ctx context.Context, id int64, p UserPatch) (*User, error) {
	err := affected(s.db.ExecContext(ctx,
		`UPDATE users SET
			name = COALESCE(?, name),
			note = COALESCE(?, note),
			disabled = COALESCE(?, disabled)
		 WHERE id = ?`,
		p.Name, p.Note, p.Disabled, id))
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id))
}
