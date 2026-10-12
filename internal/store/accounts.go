package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Account is an upstream provider subscription (a Claude or Codex login).
type Account struct {
	ID             int64      `json:"id"`
	Provider       string     `json:"provider"`
	Label          string     `json:"label"`
	Alias          string     `json:"alias"`
	Email          string     `json:"email"`
	ExternalID     string     `json:"externalId"`
	Plan           string     `json:"plan"`
	Credentials    string     `json:"-"`
	Disabled       bool       `json:"disabled"`
	Priority       int        `json:"priority"`
	AutoRedeem     bool       `json:"-"` // superseded by RedeemPolicy
	RedeemPolicy   string     `json:"-"`
	PrimerPolicy   string     `json:"-"`
	Status         string     `json:"status"`
	StatusMessage  string     `json:"statusMessage"`
	CooldownUntil  *time.Time `json:"cooldownUntil"`
	Quota          string     `json:"-"`
	Subscription   string     `json:"-"`
	HasWebSession  bool       `json:"hasWebSession"`
	QuotaUpdatedAt *time.Time `json:"quotaUpdatedAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type AccountPatch struct {
	Label    *string `json:"label"`
	Alias    *string `json:"alias"`
	Disabled *bool   `json:"disabled"`
	Priority *int    `json:"priority"`
	// RedeemPolicy is the account's banked reset override as JSON ("" clears it).
	RedeemPolicy *string `json:"-"`
	// PrimerPolicy is the account's session primer override as JSON ("" clears it).
	PrimerPolicy *string `json:"-"`
}

const accountColumns = `id, provider, label, alias, email, external_id, plan, credentials, disabled, priority,
	auto_redeem, status, status_message, cooldown_until, quota, quota_updated_at, last_used_at, created_at, updated_at,
	subscription, web_session != '', redeem_policy, primer_policy`

func scanAccount(sc interface{ Scan(...any) error }) (*Account, error) {
	var a Account
	var cooldown, quotaAt, lastUsed sql.NullInt64
	var created, updated int64
	err := sc.Scan(&a.ID, &a.Provider, &a.Label, &a.Alias, &a.Email, &a.ExternalID, &a.Plan, &a.Credentials,
		&a.Disabled, &a.Priority, &a.AutoRedeem, &a.Status, &a.StatusMessage, &cooldown, &a.Quota, &quotaAt,
		&lastUsed, &created, &updated, &a.Subscription, &a.HasWebSession, &a.RedeemPolicy, &a.PrimerPolicy)
	if err != nil {
		return nil, err
	}
	a.CooldownUntil = nullTime(cooldown)
	a.QuotaUpdatedAt = nullTime(quotaAt)
	a.LastUsedAt = nullTime(lastUsed)
	a.CreatedAt = fromUnix(created)
	a.UpdatedAt = fromUnix(updated)
	return &a, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+accountColumns+" FROM accounts ORDER BY provider, priority DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) GetAccount(ctx context.Context, id int64) (*Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// UpsertAccount inserts a new account, or refreshes the credentials of an existing one with the
// same provider and external ID (re-logging into the same subscription shouldn't duplicate it).
func (s *Store) UpsertAccount(ctx context.Context, a *Account) (*Account, error) {
	now := unix(time.Now())
	if a.ExternalID != "" {
		var id int64
		err := s.db.QueryRowContext(ctx,
			"SELECT id FROM accounts WHERE provider = ? AND external_id = ?", a.Provider, a.ExternalID).Scan(&id)
		if err == nil {
			_, err = s.db.ExecContext(ctx,
				`UPDATE accounts SET credentials = ?, email = ?, plan = CASE WHEN ? = '' THEN plan ELSE ? END,
					status = 'ok', status_message = '', cooldown_until = NULL, updated_at = ?
				 WHERE id = ?`,
				a.Credentials, a.Email, a.Plan, a.Plan, now, id)
			if err != nil {
				return nil, err
			}
			return s.GetAccount(ctx, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts (provider, label, email, external_id, plan, credentials, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Provider, a.Label, a.Email, a.ExternalID, a.Plan, a.Credentials, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetAccount(ctx, id)
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, p AccountPatch) (*Account, error) {
	err := affected(s.db.ExecContext(ctx,
		`UPDATE accounts SET
			label = COALESCE(?, label),
			alias = COALESCE(?, alias),
			disabled = COALESCE(?, disabled),
			priority = COALESCE(?, priority),
			redeem_policy = COALESCE(?, redeem_policy),
			primer_policy = COALESCE(?, primer_policy),
			updated_at = ?
		 WHERE id = ?`,
		p.Label, p.Alias, p.Disabled, p.Priority, p.RedeemPolicy, p.PrimerPolicy, unix(time.Now()), id))
	if err != nil {
		return nil, err
	}
	return s.GetAccount(ctx, id)
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", id))
}

func (s *Store) SaveAccountCredentials(ctx context.Context, id int64, creds, plan string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE accounts SET credentials = ?, plan = CASE WHEN ? = '' THEN plan ELSE ? END, updated_at = ? WHERE id = ?`,
		creds, plan, plan, unix(time.Now()), id)
	return err
}

func (s *Store) SaveAccountStatus(ctx context.Context, id int64, status, message string, cooldownUntil *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE accounts SET status = ?, status_message = ?, cooldown_until = ?, updated_at = ? WHERE id = ?",
		status, message, nullUnix(cooldownUntil), unix(time.Now()), id)
	return err
}

func (s *Store) SaveAccountQuota(ctx context.Context, id int64, quota string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE accounts SET quota = ?, quota_updated_at = ? WHERE id = ?", quota, unix(at), id)
	return err
}

// SetAccountWebSession stores (or with "" clears) an account's web session cookie.
func (s *Store) SetAccountWebSession(ctx context.Context, id int64, session string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE accounts SET web_session = ? WHERE id = ?", session, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AccountWebSession(ctx context.Context, id int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT web_session FROM accounts WHERE id = ?", id).Scan(&v)
	return v, err
}

func (s *Store) SaveAccountSubscription(ctx context.Context, id int64, sub string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE accounts SET subscription = ? WHERE id = ?", sub, id)
	return err
}

func (s *Store) TouchAccount(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE accounts SET last_used_at = ? WHERE id = ?", unix(at), id)
	return err
}
