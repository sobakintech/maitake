package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// AuditEntry is one fully recorded request. Payload is the gzip-compressed detail (headers and
// bodies); it is only loaded by GetAudit.
type AuditEntry struct {
	ID int64 `json:"id"`
	UsageEvent
	ClientIP    string          `json:"clientIp"`
	UserAgent   string          `json:"userAgent"`
	Attempts    json.RawMessage `json:"attempts"`
	PayloadSize int64           `json:"payloadSize"`
	Payload     []byte          `json:"-"`
}

const auditColumns = `id, ts, COALESCE(user_id, 0), COALESCE(key_id, 0), COALESCE(account_id, 0), provider, model,
	requested_model, endpoint, status, stream, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
	reasoning_tokens, duration_ms, ttft_ms, error, service_tier, served_tier, client_ip, user_agent, attempts, payload_size`

func scanAudit(sc interface{ Scan(...any) error }, extra ...any) (*AuditEntry, error) {
	var e AuditEntry
	var ts int64
	var attempts string
	dest := append([]any{&e.ID, &ts, &e.UserID, &e.KeyID, &e.AccountID, &e.Provider, &e.Model, &e.RequestedModel,
		&e.Endpoint, &e.Status, &e.Stream, &e.InputTokens, &e.OutputTokens, &e.CacheReadTokens, &e.CacheWriteTokens,
		&e.ReasoningTokens, &e.DurationMS, &e.TTFTMS, &e.Error, &e.ServiceTier, &e.ServedTier, &e.ClientIP,
		&e.UserAgent, &attempts, &e.PayloadSize}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	e.Time = time.UnixMilli(ts).UTC()
	e.Attempts = json.RawMessage(attempts)
	if attempts == "" {
		e.Attempts = json.RawMessage("[]")
	}
	return &e, nil
}

func (s *Store) InsertAudit(ctx context.Context, entries []AuditEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO audit_log
		(ts, user_id, key_id, account_id, provider, model, requested_model, endpoint, status, stream, input_tokens,
		 output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens, duration_ms, ttft_ms, error,
		 service_tier, served_tier, client_ip, user_agent, attempts, payload, payload_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		attempts := string(e.Attempts)
		if attempts == "" || attempts == "null" {
			attempts = "[]"
		}
		_, err := stmt.ExecContext(ctx, e.Time.UnixMilli(), nullID(e.UserID), nullID(e.KeyID), nullID(e.AccountID),
			e.Provider, e.Model, e.RequestedModel, e.Endpoint, e.Status, e.Stream, e.InputTokens, e.OutputTokens,
			e.CacheReadTokens, e.CacheWriteTokens, e.ReasoningTokens, e.DurationMS, e.TTFTMS, e.Error,
			e.ServiceTier, e.ServedTier, e.ClientIP, e.UserAgent, attempts, e.Payload, len(e.Payload))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type AuditFilter struct {
	Since     time.Time
	Until     time.Time
	Before    int64 // only entries with a smaller id, for paging
	UserID    int64
	KeyID     int64
	AccountID int64
	Model     string
	// Status is "errors", "ok", or a specific status code.
	Status string
	// Search matches the error, endpoint, models, client IP and user agent.
	Search string
}

func (f AuditFilter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, a ...any) {
		conds = append(conds, cond)
		args = append(args, a...)
	}
	if !f.Since.IsZero() {
		add("ts >= ?", f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		add("ts < ?", f.Until.UnixMilli())
	}
	if f.Before > 0 {
		add("id < ?", f.Before)
	}
	if f.UserID > 0 {
		add("user_id = ?", f.UserID)
	}
	if f.KeyID > 0 {
		add("key_id = ?", f.KeyID)
	}
	if f.AccountID > 0 {
		add("account_id = ?", f.AccountID)
	}
	if f.Model != "" {
		add("(model = ? OR requested_model = ?)", f.Model, f.Model)
	}
	switch f.Status {
	case "":
	case "errors":
		add("status >= 400")
	case "ok":
		add("status < 400")
	default:
		if code, err := strconv.Atoi(f.Status); err == nil {
			add("status = ?", code)
		}
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		add(`(error LIKE ? ESCAPE '\' OR endpoint LIKE ? ESCAPE '\' OR model LIKE ? ESCAPE '\'
			OR requested_model LIKE ? ESCAPE '\' OR client_ip LIKE ? ESCAPE '\' OR user_agent LIKE ? ESCAPE '\')`,
			like, like, like, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *Store) ListAudit(ctx context.Context, f AuditFilter, limit int) ([]AuditEntry, error) {
	where, args := f.where()
	rows, err := s.db.QueryContext(ctx, "SELECT "+auditColumns+" FROM audit_log"+where+" ORDER BY id DESC LIMIT ?",
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) GetAudit(ctx context.Context, id int64) (*AuditEntry, error) {
	var payload []byte
	e, err := scanAudit(s.db.QueryRowContext(ctx, "SELECT "+auditColumns+", payload FROM audit_log WHERE id = ?", id), &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Payload = payload
	return e, nil
}

type AuditStats struct {
	Entries int64 `json:"entries"`
	Bytes   int64 `json:"bytes"`
}

func (s *Store) AuditStats(ctx context.Context) (AuditStats, error) {
	var st AuditStats
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(payload_size), 0) FROM audit_log").Scan(&st.Entries, &st.Bytes)
	return st, err
}

func (s *Store) DeleteAudit(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE id = ?", id))
}

// ClearAudit deletes every entry matching f and reports how many went.
func (s *Store) ClearAudit(ctx context.Context, f AuditFilter) (int64, error) {
	where, args := f.where()
	res, err := s.db.ExecContext(ctx, "DELETE FROM audit_log"+where, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneAudit drops entries older than before (unless zero) and all but the newest maxEntries
// (unless 0), then those beyond maxBytes of payload (unless 0).
func (s *Store) PruneAudit(ctx context.Context, before time.Time, maxEntries int, maxBytes int64) error {
	if !before.IsZero() {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE ts < ?", before.UnixMilli()); err != nil {
			return err
		}
	}
	if maxEntries > 0 {
		_, err := s.db.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id <= (SELECT id FROM audit_log ORDER BY id DESC LIMIT 1 OFFSET ?)", maxEntries)
		if err != nil {
			return err
		}
	}
	if maxBytes > 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id <= (
			SELECT id FROM (SELECT id, SUM(payload_size) OVER (ORDER BY id DESC) AS running FROM audit_log)
			WHERE running > ? ORDER BY id DESC LIMIT 1)`, maxBytes)
		if err != nil {
			return err
		}
	}
	return nil
}
