package quota

import (
	"math"
	"testing"
	"time"
)

var capNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func weekly(used float64, resetIn time.Duration) *Snapshot {
	r := capNow.Add(resetIn)
	return &Snapshot{Windows: []Window{
		{ID: "5h", Kind: KindSession, UsedPercent: 10, WindowMinutes: 300},
		{ID: "7d", Kind: KindWeekly, UsedPercent: used, ResetsAt: &r, WindowMinutes: 10080},
	}}
}

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestBuildCapacityProjection(t *testing.T) {
	// Two equal accounts, each 6 days into its week: average burn is 60/144 and 30/144 percent per hour.
	in := CapacityInput{Kind: KindWeekly, Usage: UsageAverage, Now: capNow, From: capNow.Add(-24 * time.Hour),
		To: capNow.Add(10 * 24 * time.Hour), Accounts: []CapacityAccount{
			{ID: 1, Weight: 1, Snapshot: weekly(60, 24*time.Hour)},
			{ID: 2, Weight: 1, Snapshot: weekly(30, 24*time.Hour)},
		}}
	p := BuildCapacity("claude", in)
	if p.Remaining == nil || !near(*p.Remaining, 55, 1e-9) {
		t.Fatalf("remaining = %v", p.Remaining)
	}
	if p.Stats.BurnSource != UsageAverage || !near(p.Stats.BurnRate, (60.0+30.0)/144/2, 1e-9) {
		t.Fatalf("burn = %v %s", p.Stats.BurnRate, p.Stats.BurnSource)
	}
	if p.Stats.NextReset == nil || !p.Stats.NextReset.T.Equal(capNow.Add(24*time.Hour)) || !near(p.Stats.NextReset.Gain, 45+90.0/144*24/2, 1e-6) {
		t.Fatalf("next reset = %+v", p.Stats.NextReset)
	}
	if p.Stats.PoolEmptyAt != nil || p.Stats.FirstEmpty != nil {
		t.Fatalf("nothing should run out: %+v", p.Stats)
	}
	// After the first reset both windows repeat weekly.
	if len(p.Resets) != 2 || !p.Resets[1].T.Equal(capNow.Add(8*24*time.Hour)) {
		t.Fatalf("resets = %+v", p.Resets)
	}
}

func TestBuildCapacityDemandMovesWhenAccountEmpties(t *testing.T) {
	// Account 1 burns 10%/h with 20% left; account 2 is idle with a full window that hasn't started.
	snap2 := &Snapshot{Windows: []Window{{ID: "7d", Kind: KindWeekly, WindowMinutes: 10080}}}
	reset := capNow.Add(100 * time.Hour)
	snap1 := &Snapshot{Windows: []Window{{ID: "7d", Kind: KindWeekly, UsedPercent: 80, ResetsAt: &reset, WindowMinutes: 10080}}}
	in := CapacityInput{Kind: KindWeekly, Usage: UsageRecent, BurnWindow: 3 * time.Hour, Now: capNow,
		From: capNow.Add(-24 * time.Hour), To: capNow.Add(7 * 24 * time.Hour),
		Accounts: []CapacityAccount{{ID: 1, Weight: 1, Snapshot: snap1}, {ID: 2, Weight: 1, Snapshot: snap2}},
		Samples: []Sample{
			{AccountID: 1, WindowID: "7d", Time: capNow.Add(-4 * time.Hour), Used: 40, ResetsAt: &reset},
			{AccountID: 1, WindowID: "7d", Time: capNow, Used: 80, ResetsAt: &reset},
		}}
	p := BuildCapacity("codex", in)
	if p.Stats.BurnSource != "mixed" || !near(p.Stats.BurnRate, 5, 0.01) {
		t.Fatalf("burn = %v %s", p.Stats.BurnRate, p.Stats.BurnSource)
	}
	// Requests split evenly: 5%/h each, so account 1 runs dry in 4h, and account 2's window opens.
	if p.Stats.FirstEmpty == nil || p.Stats.FirstEmpty.AccountID != 1 || p.Stats.FirstEmpty.At.Sub(capNow.Add(4*time.Hour)).Abs() > time.Second {
		t.Fatalf("first empty = %+v", p.Stats.FirstEmpty)
	}
	// Account 2 then takes the full 10%/h with 80% left: empty 8h later.
	if p.Stats.PoolEmptyAt == nil || p.Stats.PoolEmptyAt.Sub(capNow.Add(12*time.Hour)).Abs() > time.Second {
		t.Fatalf("pool empty = %v", p.Stats.PoolEmptyAt)
	}
	acct2 := p.Accounts[1]
	if acct2.ID != 2 || acct2.EmptyAt == nil || acct2.ResetsAt != nil {
		t.Fatalf("account 2 = %+v", acct2)
	}

	// A higher priority account takes all the demand first.
	in.Accounts[1].Priority = 1
	p = BuildCapacity("codex", in)
	if p.Stats.FirstEmpty == nil || p.Stats.FirstEmpty.AccountID != 2 || p.Stats.FirstEmpty.At.Sub(capNow.Add(10*time.Hour)).Abs() > time.Second {
		t.Fatalf("priority first empty = %+v", p.Stats.FirstEmpty)
	}
}

func TestRecordedSeriesAndFlaps(t *testing.T) {
	r1 := capNow.Add(-10 * time.Hour)
	r2 := r1.Add(7 * 24 * time.Hour)
	samples := []Sample{
		{Time: capNow.Add(-20 * time.Hour), Used: 50, ResetsAt: &r1},
		{Time: capNow.Add(-19 * time.Hour), Used: 90, ResetsAt: &r1}, // flap
		{Time: capNow.Add(-18 * time.Hour), Used: 52, ResetsAt: &r1},
		{Time: capNow.Add(-2 * time.Hour), Used: 5, ResetsAt: &r2},
	}
	s := recordedSeries(filterFlaps(samples), capNow)
	want := []Point{
		{capNow.Add(-20 * time.Hour), 50}, {capNow.Add(-18 * time.Hour), 48},
		{r1, 48}, {r1, 100}, {capNow.Add(-2 * time.Hour), 95},
	}
	if len(s) != len(want) {
		t.Fatalf("series = %+v", s)
	}
	for i := range want {
		if !s[i].T.Equal(want[i].T) || !near(s[i].V, want[i].V, 1e-9) {
			t.Fatalf("point %d = %+v, want %+v", i, s[i], want[i])
		}
	}
}

func TestPoolRecordedSteps(t *testing.T) {
	a := &capAcct{w: 1, series: []Point{{capNow.Add(-2 * time.Hour), 40}, {capNow.Add(-time.Hour), 20}, {capNow.Add(-time.Hour), 100}, {capNow, 100}}}
	a.view.ID = 1
	b := &capAcct{w: 3, series: []Point{{capNow.Add(-90 * time.Minute), 50}, {capNow, 50}}}
	pts, resets := poolRecorded([]*capAcct{a, b}, capNow.Add(-3*time.Hour))
	if len(resets) != 1 || !near(resets[0].Gain, 20, 1e-9) || resets[0].Accounts[0] != 1 {
		t.Fatalf("resets = %+v", resets)
	}
	// Before account b's first reading only a counts; at the step the pool goes from 42.5 to 62.5.
	if !near(pts[0].V, 40, 1e-9) {
		t.Fatalf("first = %+v", pts[0])
	}
	var step []float64
	for _, p := range pts {
		if p.T.Equal(capNow.Add(-time.Hour)) {
			step = append(step, p.V)
		}
	}
	if len(step) != 2 || !near(step[0], 42.5, 1e-9) || !near(step[1], 62.5, 1e-9) {
		t.Fatalf("step = %v (points %+v)", step, pts)
	}
}

func TestPlanWeight(t *testing.T) {
	if PlanWeight("claude", "Max 20x") != 20 || PlanWeight("codex", "Pro") != 6 || PlanWeight("codex", "Team") != 1 {
		t.Fatal("unexpected plan weights")
	}
}

func TestBuildCapacityIdleAndEmpty(t *testing.T) {
	// A (priority 1) is 8h into its week with 30% used, so it burns 3.75%/h and runs dry in 18h40m.
	// B is full with a reset in an hour; its window shouldn't refill on that schedule but open when
	// A runs dry, and last a week from then.
	ra := capNow.Add(160 * time.Hour)
	rb := capNow.Add(time.Hour)
	snapA := &Snapshot{Windows: []Window{{ID: "7d", Kind: KindWeekly, UsedPercent: 30, ResetsAt: &ra, WindowMinutes: 10080}}}
	snapB := &Snapshot{Windows: []Window{{ID: "7d", Kind: KindWeekly, ResetsAt: &rb, WindowMinutes: 10080}}}
	in := CapacityInput{Kind: KindWeekly, Usage: UsageAverage, Now: capNow, From: capNow, To: capNow.Add(14 * 24 * time.Hour),
		Accounts: []CapacityAccount{{ID: 1, Priority: 1, Weight: 1, Snapshot: snapA}, {ID: 2, Weight: 1, Snapshot: snapB}}}
	p := BuildCapacity("claude", in)
	if p.Resets == nil {
		t.Fatal("resets must be an empty list, not null")
	}
	var bReset *time.Time
	for _, r := range p.Resets {
		if r.Accounts[0] == 2 && r.Gain > 0 {
			bReset = &r.T
			break
		}
	}
	if bReset == nil || bReset.Sub(capNow.Add(18*time.Hour+40*time.Minute+7*24*time.Hour)).Abs() > time.Minute {
		t.Fatalf("B refilled at %v, resets %+v", bReset, p.Resets)
	}

	// Already empty and waiting for a reset: no "runs dry" time.
	reset := capNow.Add(10 * time.Hour)
	spent := &Snapshot{Windows: []Window{{ID: "7d", Kind: KindWeekly, UsedPercent: 100, Limited: true, ResetsAt: &reset, WindowMinutes: 10080}}}
	in.Accounts = []CapacityAccount{{ID: 3, Weight: 1, Snapshot: spent}}
	in.Usage, in.BurnWindow = UsageRecent, 3*time.Hour
	in.Samples = []Sample{{AccountID: 3, WindowID: "7d", Time: capNow.Add(-4 * time.Hour), Used: 90, ResetsAt: &reset}}
	p = BuildCapacity("claude", in)
	if p.Stats.EmptyNow != 1 || (p.Stats.PoolEmptyAt != nil && !p.Stats.PoolEmptyAt.After(reset)) {
		t.Fatalf("stats = %+v", p.Stats)
	}
}
