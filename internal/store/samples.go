package store

import (
	"context"
	"database/sql"
	"time"
)

// QuotaSample is one recorded reading of an account's quota window.
type QuotaSample struct {
	AccountID int64
	WindowID  string
	Kind      string
	Time      time.Time
	Used      float64
	ResetsAt  *time.Time
	Minutes   int
}

func (s *Store) InsertQuotaSamples(ctx context.Context, samples []QuotaSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO quota_samples
		(account_id, window_id, ts, kind, used, resets_at, minutes) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, q := range samples {
		if _, err := stmt.ExecContext(ctx, q.AccountID, q.WindowID, unix(q.Time), q.Kind, q.Used,
			nullUnix(q.ResetsAt), q.Minutes); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// QuotaSamples returns samples of one window kind in [since, until], oldest first, plus the
// last sample of each account's window before since so a series can start at since.
func (s *Store) QuotaSamples(ctx context.Context, kind string, since, until time.Time) ([]QuotaSample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.account_id, q.window_id, q.ts, q.kind, q.used, q.resets_at, q.minutes
		FROM quota_samples q
		JOIN (SELECT account_id, window_id, MAX(ts) AS ts FROM quota_samples
			WHERE kind = ? AND ts < ? GROUP BY account_id, window_id) seed
		ON q.account_id = seed.account_id AND q.window_id = seed.window_id AND q.ts = seed.ts
		WHERE q.kind = ?
		UNION ALL
		SELECT account_id, window_id, ts, kind, used, resets_at, minutes FROM quota_samples
		WHERE kind = ? AND ts >= ? AND ts <= ?
		ORDER BY 3, 1, 2`, kind, unix(since), kind, kind, unix(since), unix(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuotaSample
	for rows.Next() {
		var q QuotaSample
		var ts int64
		var resets sql.NullInt64
		if err := rows.Scan(&q.AccountID, &q.WindowID, &ts, &q.Kind, &q.Used, &resets, &q.Minutes); err != nil {
			return nil, err
		}
		q.Time, q.ResetsAt = fromUnix(ts), nullTime(resets)
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) PruneQuotaSamples(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM quota_samples WHERE ts < ?", unix(before))
	return err
}
