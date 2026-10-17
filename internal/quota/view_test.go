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

	view := Build(p, DefaultPageSettings(), accounts, Feeds{}, now)
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
		v := Build(p, DefaultPageSettings(), accts, Feeds{}, now)
		if got := *v.Providers[0].Pool.Windows[0].Value; got != want {
			t.Errorf("%s: got %v want %v", agg, got, want)
		}
	}
}

func TestBankedFullList(t *testing.T) {
	now := time.Now()
	soon, later, gone := now.Add(time.Hour), now.Add(48*time.Hour), now.Add(-time.Hour)
	accts := []AccountInfo{
		{ID: 1, Provider: "codex", Label: "a@secret.com", Status: "ok", Snapshot: &Snapshot{Banked: &Banked{Available: 3,
			Credits: []BankedCredit{{ID: "x", Title: "Reset", ExpiresAt: &later, Count: 2}, {ID: "y"}, {ID: "z", ExpiresAt: &gone}}}}},
		{ID: 2, Provider: "codex", Label: "b@secret.com", Status: "ok", Snapshot: &Snapshot{Banked: &Banked{Available: 1,
			Credits: []BankedCredit{{ID: "w", ExpiresAt: &soon}}}}},
	}
	p := DefaultProfile()
	p.Banked = "full"
	b := Build(p, DefaultPageSettings(), accts, Feeds{}, now).Providers[0].Banked
	if b == nil || b.Available != 4 || len(b.Credits) != 3 {
		t.Fatalf("unexpected banked view: %+v", b)
	}
	if b.Credits[0].Account != "Account 2" || b.Credits[1].Count != 2 || b.Credits[2].ExpiresAt != nil {
		t.Errorf("credits not sorted by expiry with owners: %+v", b.Credits)
	}
	if len(b.Expiries) != 3 {
		t.Errorf("expected 3 expiries, got %v", b.Expiries)
	}

	p.BankedMode = "accounts"
	p.Mode = "accounts"
	v := Build(p, DefaultPageSettings(), accts, Feeds{}, now)
	if a := v.Providers[0].Accounts[0].Banked; a == nil || len(a.Credits) != 2 || a.Credits[0].Account != "" {
		t.Errorf("unexpected per-account banked view: %+v", a)
	}
}

func TestAccountSortAndLevels(t *testing.T) {
	now := time.Now()
	mk := func(id int64, alias string, used float64) AccountInfo {
		return AccountInfo{ID: id, Provider: "claude", Alias: alias, Status: "ok",
			Snapshot: &Snapshot{Windows: []Window{{ID: "5h", Kind: KindSession, UsedPercent: used}}}}
	}
	accts := []AccountInfo{mk(1, "b", 10), mk(2, "c", 90), mk(3, "a", 50), {ID: 4, Provider: "claude", Alias: "d", Status: "ok"}}
	p := DefaultProfile()
	p.Mode = "accounts"
	p.AccountNaming = "alias"
	for sort, want := range map[string]string{"default": "bcad", "name": "abcd", "most_left": "bacd", "least_left": "cabd"} {
		p.AccountSort = sort
		got := ""
		for _, e := range Build(p, DefaultPageSettings(), accts, Feeds{}, now).Providers[0].Accounts {
			got += e.Name
		}
		if got != want {
			t.Errorf("%s: got %s want %s", sort, got, want)
		}
	}

	p.AccountSort = "default"
	p.Levels.Low, p.Levels.OK = 60, 95
	got := ""
	for _, e := range Build(p, DefaultPageSettings(), accts[:3], Feeds{}, now).Providers[0].Accounts {
		got += e.Windows[0].Level + " "
	}
	if got != "ok low low " {
		t.Errorf("custom levels: got %q", got)
	}
}
