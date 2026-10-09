package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneAudit(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	var entries []AuditEntry
	for i := range 10 {
		entries = append(entries, AuditEntry{UsageEvent: UsageEvent{Time: now.Add(time.Duration(i-10) * 24 * time.Hour),
			Status: 200}, Payload: make([]byte, 100)})
	}
	if err := st.InsertAudit(ctx, entries); err != nil {
		t.Fatal(err)
	}
	count := func() int64 {
		s, _ := st.AuditStats(ctx)
		return s.Entries
	}
	if err := st.PruneAudit(ctx, now.Add(-5*24*time.Hour-time.Hour), 0, 0); err != nil || count() != 5 {
		t.Fatalf("age prune: %v, %d left", err, count())
	}
	if err := st.PruneAudit(ctx, time.Time{}, 4, 0); err != nil || count() != 4 {
		t.Fatalf("count prune: %v, %d left", err, count())
	}
	if err := st.PruneAudit(ctx, time.Time{}, 0, 250); err != nil || count() != 2 {
		t.Fatalf("size prune: %v, %d left", err, count())
	}
	list, _ := st.ListAudit(ctx, AuditFilter{}, 10)
	if len(list) != 2 || list[0].ID != 10 || list[1].ID != 9 {
		t.Fatalf("kept the wrong entries: %+v", list)
	}
}
