package quota

import (
	"testing"
	"time"
)

func sessionAt(used float64, now time.Time, elapsed time.Duration) Window {
	reset := now.Add(5*time.Hour - elapsed)
	return Window{ID: "5h", Kind: KindSession, UsedPercent: used, ResetsAt: &reset, WindowMinutes: 300}
}

func TestProjectPace(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		w       Window
		status  string
		runsOut bool
	}{
		{"on track", sessionAt(20, now, 2*time.Hour+30*time.Minute), PaceOnTrack, false},
		{"close", sessionAt(47, now, 2*time.Hour+30*time.Minute), PaceClose, false},
		{"over", sessionAt(80, now, 2*time.Hour+30*time.Minute), PaceOver, true},
		{"unused", sessionAt(0, now, time.Hour), "", false},
		{"spent", sessionAt(100, now, time.Hour), "", false},
		{"too young", sessionAt(10, now, 2*time.Minute), "", false},
		{"few early requests", sessionAt(4, now, 10*time.Minute), "", false},
		{"no span", Window{UsedPercent: 50, ResetsAt: sessionAt(0, now, 0).ResetsAt}, "", false},
	}
	for _, c := range cases {
		p := ProjectPace(c.w, now)
		if c.status == "" {
			if p != nil {
				t.Errorf("%s: expected no pace, got %+v", c.name, p)
			}
			continue
		}
		if p == nil || p.Status != c.status || (p.RunsOutAt != nil) != c.runsOut {
			t.Errorf("%s: got %+v", c.name, p)
		}
	}

	p := ProjectPace(sessionAt(80, now, 2*time.Hour+30*time.Minute), now)
	if p.Elapsed != 50 || p.ProjectedUsed != 160 {
		t.Errorf("unexpected projection: %+v", p)
	}
	if want := now.Add(-2*time.Hour - 30*time.Minute).Add(time.Duration(float64(150*time.Minute) * 100 / 80)); !p.RunsOutAt.Equal(want) {
		t.Errorf("runs out at %v, want %v", p.RunsOutAt, want)
	}
}

func TestPaceInView(t *testing.T) {
	now := time.Now()
	accts := []AccountInfo{
		{ID: 1, Provider: "claude", Status: "ok", Snapshot: &Snapshot{Windows: []Window{sessionAt(20, now, 150*time.Minute)}}},
		{ID: 2, Provider: "claude", Status: "ok", Snapshot: &Snapshot{Windows: []Window{sessionAt(76, now, 150*time.Minute)}}},
	}
	p := DefaultProfile()
	p.Mode = "both"
	view := Build(p, DefaultPageSettings(), accts, Feeds{}, now)
	pv := view.Providers[0]
	if pv.Accounts[0].Windows[0].Pace != nil {
		t.Errorf("on-track pace shown in warnings mode: %+v", pv.Accounts[0].Windows[0].Pace)
	}
	if pc := pv.Accounts[1].Windows[0].Pace; pc == nil || pc.Status != PaceOver || pc.RunsOutAt == nil {
		t.Errorf("expected over pace with run-out time: %+v", pc)
	}
	// Average of 40% and 152% projected.
	if pc := pv.Pool.Windows[0].Pace; pc == nil || pc.Status != PaceClose || pc.ProjectedUsed != 96 || pc.RunsOutAt != nil {
		t.Errorf("unexpected pooled pace: %+v", pc)
	}

	p.PoolAggregate = "max"
	p.Pace = "always"
	if pc := Build(p, DefaultPageSettings(), accts, Feeds{}, now).Providers[0].Pool.Windows[0].Pace; pc == nil || pc.Status != PaceOnTrack {
		t.Errorf("max pool should follow the account with most left: %+v", pc)
	}

	p.NumberFormat = "level"
	pc := Build(p, DefaultPageSettings(), accts, Feeds{}, now).Providers[0].Accounts[1].Windows[0].Pace
	if pc == nil || pc.ProjectedUsed != 0 || pc.Elapsed != 0 || pc.RunsOutAt != nil {
		t.Errorf("level format leaks pace numbers: %+v", pc)
	}

	p.Pace = "hidden"
	if pc := Build(p, DefaultPageSettings(), accts, Feeds{}, now).Providers[0].Accounts[1].Windows[0].Pace; pc != nil {
		t.Errorf("hidden pace shown: %+v", pc)
	}
}
