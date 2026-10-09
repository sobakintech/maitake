package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

type UsageEvent struct {
	Time             time.Time `json:"time"`
	UserID           int64     `json:"userId"`
	KeyID            int64     `json:"keyId"`
	AccountID        int64     `json:"accountId"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	RequestedModel   string    `json:"requestedModel"`
	Endpoint         string    `json:"endpoint"`
	Status           int       `json:"status"`
	Stream           bool      `json:"stream"`
	InputTokens      int64     `json:"inputTokens"`
	OutputTokens     int64     `json:"outputTokens"`
	CacheReadTokens  int64     `json:"cacheReadTokens"`
	CacheWriteTokens int64     `json:"cacheWriteTokens"`
	ReasoningTokens  int64     `json:"reasoningTokens"`
	DurationMS       int64     `json:"durationMs"`
	Error            string    `json:"error"`
	// ServiceTier is what the client asked for (e.g. "priority"); ServedTier what the provider reports.
	ServiceTier string `json:"serviceTier"`
	ServedTier  string `json:"servedTier"`
	// TTFTMS is the time until the first output arrived from upstream; 0 when there was none.
	TTFTMS int64 `json:"ttftMs"`
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id > 0}
}

func (s *Store) InsertUsage(ctx context.Context, events []UsageEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO usage_events
		(ts, user_id, key_id, account_id, provider, model, endpoint, status, stream, input_tokens, output_tokens,
		 cache_read_tokens, cache_write_tokens, reasoning_tokens, duration_ms, error, requested_model,
		 service_tier, served_tier, ttft_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range events {
		_, err := stmt.ExecContext(ctx, e.Time.UnixMilli(), nullID(e.UserID), nullID(e.KeyID), nullID(e.AccountID),
			e.Provider, e.Model, e.Endpoint, e.Status, e.Stream, e.InputTokens, e.OutputTokens,
			e.CacheReadTokens, e.CacheWriteTokens, e.ReasoningTokens, e.DurationMS, e.Error, e.RequestedModel,
			e.ServiceTier, e.ServedTier, e.TTFTMS)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type UsageFilter struct {
	Since     time.Time
	Until     time.Time
	UserID    int64
	KeyID     int64
	AccountID int64
}

func (f UsageFilter) where() (string, []any) {
	conds := []string{"ts >= ?", "ts < ?"}
	args := []any{f.Since.UnixMilli(), f.Until.UnixMilli()}
	if f.UserID > 0 {
		conds = append(conds, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.KeyID > 0 {
		conds = append(conds, "key_id = ?")
		args = append(args, f.KeyID)
	}
	if f.AccountID > 0 {
		conds = append(conds, "account_id = ?")
		args = append(args, f.AccountID)
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

type UsageTotals struct {
	Requests         int64 `json:"requests"`
	Errors           int64 `json:"errors"`
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens"`
	// AvgTTFTMS averages time to first token over requests that produced output.
	AvgTTFTMS int64 `json:"avgTtftMs"`
}

const totalsColumns = `COUNT(*), COALESCE(SUM(status >= 400), 0), COALESCE(SUM(input_tokens), 0),
	COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
	COALESCE(SUM(reasoning_tokens), 0), CAST(COALESCE(AVG(NULLIF(ttft_ms, 0)), 0) AS INTEGER)`

func scanTotals(sc interface{ Scan(...any) error }, extra ...any) (UsageTotals, error) {
	var t UsageTotals
	dest := append(extra, &t.Requests, &t.Errors, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens,
		&t.CacheWriteTokens, &t.ReasoningTokens, &t.AvgTTFTMS)
	return t, sc.Scan(dest...)
}

func (s *Store) UsageTotals(ctx context.Context, f UsageFilter) (UsageTotals, error) {
	where, args := f.where()
	return scanTotals(s.db.QueryRowContext(ctx, "SELECT "+totalsColumns+" FROM usage_events"+where, args...))
}

type UsageBucket struct {
	Key string `json:"key"`
	UsageTotals
}

// UsageBreakdown groups usage by one dimension: "day", "hour", "model", "user", "key", "account" or "provider".
// For "user", requests made with an admin key are keyed "k<key id>".
func (s *Store) UsageBreakdown(ctx context.Context, f UsageFilter, by string, tzOffsetMinutes int) ([]UsageBucket, error) {
	var group string
	offsetMS := int64(tzOffsetMinutes) * 60_000
	switch by {
	case "day":
		group = "CAST(((ts + " + strconv.FormatInt(offsetMS, 10) + ") / 86400000) * 86400000 - " + strconv.FormatInt(offsetMS, 10) + " AS TEXT)"
	case "hour":
		group = "CAST((ts / 3600000) * 3600000 AS TEXT)"
	case "model":
		group = "model"
	case "provider":
		group = "provider"
	case "user":
		group = "CASE WHEN user_id IS NULL THEN 'k' || COALESCE(key_id, 0) ELSE CAST(user_id AS TEXT) END"
	case "key":
		group = "CAST(COALESCE(key_id, 0) AS TEXT)"
	case "account":
		group = "CAST(COALESCE(account_id, 0) AS TEXT)"
	default:
		group = "'all'"
	}
	where, args := f.where()
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+group+" AS k, "+totalsColumns+" FROM usage_events"+where+" GROUP BY k ORDER BY k", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageBucket{}
	for rows.Next() {
		var b UsageBucket
		t, err := scanTotals(rows, &b.Key)
		if err != nil {
			return nil, err
		}
		b.UsageTotals = t
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) RecentUsage(ctx context.Context, f UsageFilter, limit int) ([]UsageEvent, error) {
	where, args := f.where()
	rows, err := s.db.QueryContext(ctx, `SELECT ts, COALESCE(user_id, 0), COALESCE(key_id, 0), COALESCE(account_id, 0),
		provider, model, endpoint, status, stream, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		reasoning_tokens, duration_ms, error, requested_model, service_tier, served_tier, ttft_ms FROM usage_events`+where+" ORDER BY ts DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageEvent{}
	for rows.Next() {
		var e UsageEvent
		var ts int64
		if err := rows.Scan(&ts, &e.UserID, &e.KeyID, &e.AccountID, &e.Provider, &e.Model, &e.Endpoint, &e.Status,
			&e.Stream, &e.InputTokens, &e.OutputTokens, &e.CacheReadTokens, &e.CacheWriteTokens, &e.ReasoningTokens,
			&e.DurationMS, &e.Error, &e.RequestedModel, &e.ServiceTier, &e.ServedTier, &e.TTFTMS); err != nil {
			return nil, err
		}
		e.Time = time.UnixMilli(ts).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) PruneUsage(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM usage_events WHERE ts < ?", before.UnixMilli())
	return err
}
