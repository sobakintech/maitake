package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaSamplesSeed(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	reset := base.Add(48 * time.Hour)
	var rows []QuotaSample
	for i := range 5 {
		rows = append(rows, QuotaSample{AccountID: 1, WindowID: "7d", Kind: "weekly", Time: base.Add(time.Duration(i) * time.Hour),
			Used: float64(i * 10), ResetsAt: &reset, Minutes: 10080})
	}
	rows = append(rows, QuotaSample{AccountID: 1, WindowID: "5h", Kind: "session", Time: base, Used: 1})
	if err := st.InsertQuotaSamples(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got, err := st.QuotaSamples(ctx, "weekly", base.Add(150*time.Minute), base.Add(10*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// The 02:00 reading seeds the series, then 03:00 and 04:00.
	if len(got) != 3 || got[0].Used != 20 || got[2].Used != 40 || got[0].ResetsAt == nil || !got[0].ResetsAt.Equal(reset) {
		t.Fatalf("got %+v", got)
	}
	if err := st.PruneQuotaSamples(ctx, base.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.QuotaSamples(ctx, "weekly", base, base.Add(10*time.Hour))
	if len(got) != 1 {
		t.Fatalf("after prune: %+v", got)
	}
}
