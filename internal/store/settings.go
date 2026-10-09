package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// GetSetting decodes the JSON value stored under key into dst, leaving dst untouched if unset.
func (s *Store) GetSetting(ctx context.Context, key string, dst any) error {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), dst)
}

func (s *Store) PutSetting(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, string(raw))
	return err
}

type DisplayProfile struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	KeyCount  int             `json:"keyCount"`
}

const profileColumns = `p.id, p.name, p.config, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM api_keys k WHERE k.display_profile_id = p.id)`

func scanProfile(sc interface{ Scan(...any) error }) (*DisplayProfile, error) {
	var p DisplayProfile
	var cfg string
	var created, updated int64
	if err := sc.Scan(&p.ID, &p.Name, &cfg, &created, &updated, &p.KeyCount); err != nil {
		return nil, err
	}
	p.Config = json.RawMessage(cfg)
	p.CreatedAt = fromUnix(created)
	p.UpdatedAt = fromUnix(updated)
	return &p, nil
}

func (s *Store) ListProfiles(ctx context.Context) ([]DisplayProfile, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+profileColumns+" FROM display_profiles p ORDER BY p.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DisplayProfile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetProfile(ctx context.Context, id int64) (*DisplayProfile, error) {
	p, err := scanProfile(s.db.QueryRowContext(ctx, "SELECT "+profileColumns+" FROM display_profiles p WHERE p.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) CreateProfile(ctx context.Context, name string, config json.RawMessage) (*DisplayProfile, error) {
	now := unix(time.Now())
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO display_profiles (name, config, created_at, updated_at) VALUES (?, ?, ?, ?)",
		name, string(config), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetProfile(ctx, id)
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, name *string, config json.RawMessage) (*DisplayProfile, error) {
	var cfg *string
	if config != nil {
		v := string(config)
		cfg = &v
	}
	err := affected(s.db.ExecContext(ctx,
		"UPDATE display_profiles SET name = COALESCE(?, name), config = COALESCE(?, config), updated_at = ? WHERE id = ?",
		name, cfg, unix(time.Now()), id))
	if err != nil {
		return nil, err
	}
	return s.GetProfile(ctx, id)
}

func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM display_profiles WHERE id = ?", id))
}
