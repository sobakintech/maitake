package resets

import (
	"strings"
	"testing"
	"time"
)

// Trimmed from claude-resets.com/data/resets.json (Oct 2026).
const claudeSample = `{"providers":{"claude":{"name":"Claude","account":"ClaudeDevs","events":[
{"id":"1","date":"2026-09-01T18:35:27Z","kind":"reset","scope":"all","note":"Shipped Fable 5.1 and reset limits for all users.","url":"https://x.com/ClaudeDevs/status/1"},
{"id":"2","date":"2026-09-04T20:08:45Z","kind":"reset","account":"lydiahallie","scope":"Max","note":"Reset weekly limits for Max.","url":"https://x.com/lydiahallie/status/2"},
{"id":"p","date":"2026-09-10T00:00:00Z","kind":"policy","scope":"paid plans","note":"Raised limits."},
{"id":"3","date":"2026-09-22T16:44:06Z","kind":"reset","resetType":"banked","usableUntil":"2026-10-22T23:59:59Z","scope":"Pro, Max + Team","note":"Gave a banked reset.","url":"https://x.com/ClaudeDevs/status/3"}
]},"codex":{"events":[{"id":"c","date":"2026-10-08T03:44:55Z","kind":"reset"}]}}}`

func TestDecodeClaude(t *testing.T) {
	now := time.Date(2026, 10, 2, 16, 44, 6, 0, time.UTC)
	st, err := decodeClaude(strings.NewReader(claudeSample), now)
	if err != nil {
		t.Fatal(err)
	}
	l := st.Latest
	if l == nil || l.ID != "3" || l.ResetType != "banked" || l.Source.Author != "ClaudeDevs" ||
		l.Scope != "Pro, Max + Team" || l.UsableUntil == nil || l.Text != "Gave a banked reset." {
		t.Fatalf("latest = %+v", l)
	}
	if st.Stats.Total != 3 || *st.Stats.DaysSinceLast != 10 || *st.Stats.AvgIntervalDays != 10.5 {
		t.Errorf("stats = total %d, since %v, avg %v", st.Stats.Total, *st.Stats.DaysSinceLast, *st.Stats.AvgIntervalDays)
	}
	if st.Scheduled != nil || st.Watch != nil {
		t.Errorf("claude feed has no schedule or forecast: %+v", st)
	}

	if _, err := decodeClaude(strings.NewReader(`{"providers":{}}`), now); err == nil {
		t.Error("missing claude provider should fail")
	}
}
