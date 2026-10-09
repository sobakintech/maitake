package quota

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildFiltersServerSide(t *testing.T) {
	now := time.Now()
	reset := now.Add(2 * time.Hour)
	exp := now.Add(48 * time.Hour)
	accounts := []AccountInfo{
		{ID: 1, Provider: "claude", Label: "alice@secret.com", Status: "ok", Plan: "Max",
			Snapshot: &Snapshot{Windows: []Window{{ID: "5h", Kind: KindSession, Label: "5-hour", UsedPercent: 30, ResetsAt: &reset},
				{ID: "7d", Kind: KindWeekly, Label: "Weekly", UsedPercent: 80}}}},
		{ID: 2, Provider: "claude", Label: "bob@secret.com", Alias: "Bob", Status: "ok",
			Snapshot: &Snapshot{Windows: []Window{{ID: "5h", Kind: KindSession, Label: "5-hour", UsedPercent: 70}}}},
		{ID: 3, Provider: "codex", Label: "hidden@secret.com", Status: "ok",
			Snapshot: &Snapshot{Banked: &Banked{Available: 2, Credits: []BankedCredit{{ID: "c", ExpiresAt: &exp}}}}},
		{ID: 4, Provider: "codex", Label: "carol@secret.com", Status: "ok",
			Snapshot: &Snapshot{Banked: &Banked{Available: 1}}},
	}
	p := DefaultProfile()
	p.Mode = "both"
	p.NumberFormat = "level"
	p.ResetFormat = "hidden"
	p.HiddenAccounts = []int64{3}
	p.Windows.Weekly = false
	p.Banked = "count"

	view := Build(p, DefaultPageSettings(), accounts, nil, now)
	raw, _ := json.Marshal(view)
	out := string(raw)
	for _, leak := range []string{"secret.com", `"value"`, "resetsAt", "Weekly", `"expiries"`} {
		if strings.Contains(out, leak) {
			t.Errorf("view leaks %q: %s", leak, out)
		}
	}
	if len(view.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(view.Providers))
	}
	claudeView := view.Providers[0]
	if claudeView.Accounts[0].Name != "Account 1" || claudeView.Pool == nil || claudeView.Pool.Windows[0].Level != "ok" {
		t.Errorf("unexpected claude view: %+v", claudeView)
	}
	if b := view.Providers[1].Banked; b == nil || b.Available != 1 {
		t.Errorf("hidden account counted in banked total: %+v", b)
	}
}

func TestPoolAggregates(t *testing.T) {
	now := time.Now()
	mk := func(used float64) AccountInfo {
		return AccountInfo{Provider: "claude", Status: "ok",
			Snapshot: &Snapshot{Windows: []Window{{ID: "5h", Kind: KindSession, UsedPercent: used}}}}
	}
	accts := []AccountInfo{mk(20), mk(60)}
	for agg, want := range map[string]float64{"average": 60, "min": 40, "max": 80, "total": 1.2} {
		p := DefaultProfile()
		p.PoolAggregate = agg
		v := Build(p, DefaultPageSettings(), accts, nil, now)
		if got := *v.Providers[0].Pool.Windows[0].Value; got != want {
			t.Errorf("%s: got %v want %v", agg, got, want)
		}
	}
}
