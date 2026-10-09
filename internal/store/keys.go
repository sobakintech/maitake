package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type APIKey struct {
	ID int64 `json:"id"`
	// Exactly one of UserID and AdminID is set.
	UserID     *int64     `json:"userId"`
	AdminID    *int64     `json:"adminId"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Disabled   bool       `json:"disabled"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// AllowedModels holds glob patterns; empty means every model is allowed.
	AllowedModels    []string `json:"allowedModels"`
	RPMLimit         int      `json:"rpmLimit"`
	DisplayProfileID *int64   `json:"displayProfileId"`
	// DisplayConfig is the key's own quota page view (quota.Profile as JSON); it overrides the
	// profile. Empty when the key has none.
	DisplayConfig json.RawMessage `json:"displayConfig"`
	// ModelRules is the key's own model mapping (routing.KeyRules as JSON).
	ModelRules json.RawMessage `json:"modelRules"`
	// Copyable is true when an encrypted copy of the full key is stored.
	Copyable bool `json:"copyable"`
}

// KeyPatch updates only the non-nil fields. DisplayProfileID 0 resets the key to the default profile.
type KeyPatch struct {
	Name             *string   `json:"name"`
	Disabled         *bool     `json:"disabled"`
	AllowedModels    *[]string `json:"allowedModels"`
	RPMLimit         *int      `json:"rpmLimit"`
	DisplayProfileID *int64    `json:"displayProfileId"`
	// DisplayConfig sets the key's own view; JSON null removes it.
	DisplayConfig *json.RawMessage `json:"displayConfig"`
	ModelRules    *json.RawMessage `json:"modelRules"`
	// ExpiresAt: absent leaves the expiry alone, null removes it, a time sets it.
	ExpiresAt OptionalTime `json:"expiresAt"`
}

// OptionalTime tells an absent JSON field apart from an explicit null.
type OptionalTime struct {
	Set   bool
	Value *time.Time
}

func (o *OptionalTime) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	o.Value = &t
	return nil
}

// KeyOwner is the result of authenticating a client API key. User is zero for admin-owned keys.
type KeyOwner struct {
	Key       APIKey
	User      User
	OwnerName string
}

// OwnedKey is a key with a display name for whoever owns it.
type OwnedKey struct {
	APIKey
	OwnerType string `json:"ownerType"` // "user" or "admin"
	OwnerName string `json:"ownerName"`
}

const keyColumns = `k.id, k.user_id, k.admin_id, k.name, k.prefix, k.disabled, k.created_at, k.expires_at, k.last_used_at,
	k.allowed_models, k.rpm_limit, k.display_profile_id, k.model_rules, k.secret_enc IS NOT NULL, k.display_config`

func scanKey(sc interface{ Scan(...any) error }, extra ...any) (*APIKey, error) {
	var k APIKey
	var created int64
	var expires, lastUsed, profile, userID, adminID sql.NullInt64
	var allowed, rules, display string
	dest := append([]any{&k.ID, &userID, &adminID, &k.Name, &k.Prefix, &k.Disabled, &created, &expires, &lastUsed,
		&allowed, &k.RPMLimit, &profile, &rules, &k.Copyable, &display}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	if userID.Valid {
		k.UserID = &userID.Int64
	}
	if adminID.Valid {
		k.AdminID = &adminID.Int64
	}
	k.CreatedAt = fromUnix(created)
	k.AllowedModels = splitLines(allowed)
	k.ModelRules = json.RawMessage("null")
	if rules != "" {
		k.ModelRules = json.RawMessage(rules)
	}
	if profile.Valid {
		k.DisplayProfileID = &profile.Int64
	}
	if display != "" {
		k.DisplayConfig = json.RawMessage(display)
	}
	k.ExpiresAt = nullTime(expires)
	k.LastUsedAt = nullTime(lastUsed)
	return &k, nil
}

func (s *Store) ListKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+keyColumns+" FROM api_keys k WHERE k.user_id = ? ORDER BY k.created_at DESC, k.id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []APIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

// ListAllKeys returns every key with its owner's name, newest first.
func (s *Store) ListAllKeys(ctx context.Context) ([]OwnedKey, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+keyColumns+`,
		CASE WHEN k.admin_id IS NULL THEN 'user' ELSE 'admin' END, COALESCE(u.name, a.username, '')
		FROM api_keys k LEFT JOIN users u ON u.id = k.user_id LEFT JOIN admins a ON a.id = k.admin_id
		ORDER BY k.created_at DESC, k.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []OwnedKey{}
	for rows.Next() {
		var ok OwnedKey
		k, err := scanKey(rows, &ok.OwnerType, &ok.OwnerName)
		if err != nil {
			return nil, err
		}
		ok.APIKey = *k
		keys = append(keys, ok)
	}
	return keys, rows.Err()
}

func (s *Store) GetKey(ctx context.Context, id int64) (*APIKey, error) {
	k, err := scanKey(s.db.QueryRowContext(ctx, "SELECT "+keyColumns+" FROM api_keys k WHERE k.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

// CreateKey stores a new user key. enc is the encrypted full key, or nil to keep only the hash.
func (s *Store) CreateKey(ctx context.Context, userID int64, name, prefix string, hash, enc []byte, expiresAt *time.Time) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, name, prefix, key_hash, secret_enc, created_at, expires_at)
		 SELECT id, ?, ?, ?, ?, ?, ? FROM users WHERE id = ?`,
		name, prefix, hash, enc, unix(time.Now()), nullUnix(expiresAt), userID)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetKey(ctx, id)
}

func (s *Store) CreateAdminKey(ctx context.Context, adminID int64, name, prefix string, hash, enc []byte, expiresAt *time.Time) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (admin_id, name, prefix, key_hash, secret_enc, created_at, expires_at)
		 SELECT id, ?, ?, ?, ?, ?, ? FROM admins WHERE id = ?`,
		name, prefix, hash, enc, unix(time.Now()), nullUnix(expiresAt), adminID)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetKey(ctx, id)
}

func (s *Store) UpdateKey(ctx context.Context, id int64, p KeyPatch) (*APIKey, error) {
	var allowed *string
	if p.AllowedModels != nil {
		v := joinLines(*p.AllowedModels)
		allowed = &v
	}
	var rules *string
	if p.ModelRules != nil {
		v := string(*p.ModelRules)
		if v == "null" {
			v = ""
		}
		rules = &v
	}
	var display *string
	if p.DisplayConfig != nil {
		v := string(*p.DisplayConfig)
		if v == "null" {
			v = ""
		}
		display = &v
	}
	setProfile := p.DisplayProfileID != nil
	var profile sql.NullInt64
	if setProfile && *p.DisplayProfileID > 0 {
		profile = sql.NullInt64{Int64: *p.DisplayProfileID, Valid: true}
	}
	err := affected(s.db.ExecContext(ctx,
		`UPDATE api_keys SET
			name = COALESCE(?, name),
			disabled = COALESCE(?, disabled),
			allowed_models = COALESCE(?, allowed_models),
			rpm_limit = COALESCE(?, rpm_limit),
			model_rules = COALESCE(?, model_rules),
			display_config = COALESCE(?, display_config),
			display_profile_id = CASE WHEN ? THEN ? ELSE display_profile_id END,
			expires_at = CASE WHEN ? THEN ? ELSE expires_at END
		 WHERE id = ?`,
		p.Name, p.Disabled, allowed, p.RPMLimit, rules, display, setProfile, profile, p.ExpiresAt.Set, nullUnix(p.ExpiresAt.Value), id))
	if err != nil {
		return nil, err
	}
	return s.GetKey(ctx, id)
}

// KeySecret returns the encrypted copy of a key, or nil if none is stored.
func (s *Store) KeySecret(ctx context.Context, id int64) ([]byte, error) {
	var enc []byte
	err := s.db.QueryRowContext(ctx, "SELECT secret_enc FROM api_keys WHERE id = ?", id).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return enc, err
}

// RotateKey replaces a key's secret, keeping its settings. The old secret stops working.
func (s *Store) RotateKey(ctx context.Context, id int64, prefix string, hash, enc []byte) (*APIKey, error) {
	err := affected(s.db.ExecContext(ctx,
		"UPDATE api_keys SET prefix = ?, key_hash = ?, secret_enc = ?, last_used_at = NULL WHERE id = ?",
		prefix, hash, enc, id))
	if err != nil {
		return nil, err
	}
	return s.GetKey(ctx, id)
}

// ClearKeySecrets deletes every stored key copy, leaving only hashes.
func (s *Store) ClearKeySecrets(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE api_keys SET secret_enc = NULL WHERE secret_enc IS NOT NULL")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DeleteKey(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE id = ?", id))
}

// AuthenticateKey returns the key and owner for an active key, or ErrNotFound if the key
// is unknown, disabled, expired, or belongs to a disabled user.
func (s *Store) AuthenticateKey(ctx context.Context, hash []byte) (*KeyOwner, error) {
	now := time.Now()
	var o KeyOwner
	var userID, userCreated sql.NullInt64
	var userName, userNote, adminName sql.NullString
	var userDisabled sql.NullBool
	k, err := scanKey(s.db.QueryRowContext(ctx,
		"SELECT "+keyColumns+`, u.id, u.name, u.note, u.disabled, u.created_at, a.username
		 FROM api_keys k
		 LEFT JOIN users u ON u.id = k.user_id
		 LEFT JOIN admins a ON a.id = k.admin_id
		 WHERE k.key_hash = ? AND k.disabled = 0 AND (u.id IS NOT NULL OR a.id IS NOT NULL)
		   AND (u.id IS NULL OR u.disabled = 0)
		   AND (k.expires_at IS NULL OR k.expires_at > ?)`,
		hash, unix(now)),
		&userID, &userName, &userNote, &userDisabled, &userCreated, &adminName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Key = *k
	if userID.Valid {
		o.User = User{ID: userID.Int64, Name: userName.String, Note: userNote.String,
			Disabled: userDisabled.Bool, CreatedAt: fromUnix(userCreated.Int64)}
		o.OwnerName = userName.String
	} else {
		o.OwnerName = adminName.String
	}

	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
		_, _ = s.db.ExecContext(ctx, "UPDATE api_keys SET last_used_at = ? WHERE id = ?", unix(now), k.ID)
	}
	return &o, nil
}

func splitLines(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func joinLines(items []string) string {
	clean := make([]string, 0, len(items))
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			clean = append(clean, it)
		}
	}
	return strings.Join(clean, "\n")
}
