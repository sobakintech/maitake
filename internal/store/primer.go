package store

import (
	"context"
	"database/sql"
	"time"
)

// PrimerRun is one attempt by the session primer to start an account's session window.
type PrimerRun struct {
	ID        int64      `json:"id"`
	AccountID int64      `json:"accountId"`
	Time      time.Time  `json:"time"`
	Trigger   string     `json:"trigger"` // "slot" or "manual"
	PlannedAt *time.Time `json:"plannedAt,omitempty"`
	// Outcome is "started", "sent" (not verified), "skipped" or "failed".
	Outcome  string     `json:"outcome"`
	Message  string     `json:"message"`
	Pings    int        `json:"pings"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

func (s *Store) InsertPrimerRun(ctx context.Context, r *PrimerRun) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO primer_runs
		(account_id, ts, trigger, planned_at, outcome, message, pings, resets_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.AccountID, unix(r.Time), r.Trigger, nullUnix(r.PlannedAt), r.Outcome, r.Message, r.Pings, nullUnix(r.ResetsAt))
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

// PrimerRuns lists runs newest first, since a time, optionally for one account (0 = all).
func (s *Store) PrimerRuns(ctx context.Context, accountID int64, since time.Time, limit int) ([]PrimerRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, account_id, ts, trigger, planned_at, outcome, message, pings, resets_at
		FROM primer_runs WHERE ts >= ? AND (? = 0 OR account_id = ?) ORDER BY ts DESC, id DESC LIMIT ?`,
		unix(since), accountID, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrimerRun{}
	for rows.Next() {
		var r PrimerRun
		var ts int64
		var planned, resets sql.NullInt64
		if err := rows.Scan(&r.ID, &r.AccountID, &ts, &r.Trigger, &planned, &r.Outcome, &r.Message, &r.Pings, &resets); err != nil {
			return nil, err
		}
		r.Time, r.PlannedAt, r.ResetsAt = fromUnix(ts), nullTime(planned), nullTime(resets)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PrunePrimerRuns(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM primer_runs WHERE ts < ?", unix(before))
	return err
}
