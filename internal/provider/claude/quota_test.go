package claude

import (
	"encoding/json"
	"testing"

	"github.com/sobakintech/maitake/internal/quota"
)

// Trimmed real response from /api/oauth/usage (Oct 2026).
const usageSample = `{"five_hour":{"utilization":25.0,"resets_at":"2026-10-09T16:50:00.811064+00:00","limit_dollars":null,"used_dollars":null},"seven_day":{"utilization":33.0,"resets_at":"2026-10-09T18:00:00.811083+00:00","limit_dollars":null},"seven_day_opus":null,"tangelo":null,"iguana_necktie":{"utilization":0.0,"resets_at":"2026-11-05T07:59:00+00:00","limit_dollars":250,"used_dollars":0.0,"remaining_dollars":250.0},"extra_usage":{"is_enabled":false,"monthly_limit":null,"used_credits":null,"utilization":null},"limits":[{"kind":"session","group":"session","percent":25,"severity":"normal","resets_at":"2026-10-09T16:50:00.811064+00:00","scope":null,"is_active":false},{"kind":"weekly_all","group":"weekly","percent":33,"severity":"normal","resets_at":"2026-10-09T18:00:00.811083+00:00","scope":null,"is_active":true},{"kind":"weekly_scoped","group":"weekly","percent":0,"severity":"normal","resets_at":"2026-10-09T18:00:00+00:00","scope":{"model":{"id":null,"display_name":"Fable"},"surface":null},"is_active":false}]}`

func TestSnapshotFromUsage(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(usageSample), &raw); err != nil {
		t.Fatal(err)
	}
	s := snapshotFromUsage(raw)
	want := []struct{ id, kind, label string }{
		{"5h", quota.KindSession, "5-hour"},
		{"7d", quota.KindWeekly, "Weekly"},
		{"7d_fable", quota.KindModelWeekly, "Weekly · Fable"},
		{"credit_iguana_necktie", quota.KindOther, "Credit allowance"},
	}
	if len(s.Windows) != len(want) {
		t.Fatalf("got %d windows: %+v", len(s.Windows), s.Windows)
	}
	for i, w := range want {
		got := s.Windows[i]
		if got.ID != w.id || got.Kind != w.kind || got.Label != w.label {
			t.Errorf("window %d = %s/%s/%q, want %s/%s/%q", i, got.ID, got.Kind, got.Label, w.id, w.kind, w.label)
		}
	}
	if a := s.Window("credit_iguana_necktie").Amount; a == nil || a.Limit != 250 || a.Used != 0 || a.Currency != "USD" {
		t.Errorf("amount = %+v", a)
	}

	// Response headers only know "a per-model weekly limit"; it should update Fable, not add a twin.
	s.Merge([]quota.Window{{ID: "7d_oi", Kind: quota.KindModelWeekly, Label: "Weekly (model limit)", UsedPercent: 40}})
	if len(s.Windows) != 4 || s.Window("7d_fable").UsedPercent != 40 {
		t.Errorf("header merge duplicated or missed: %+v", s.Windows)
	}
}
