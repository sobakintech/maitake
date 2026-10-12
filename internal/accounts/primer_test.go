package accounts

import (
	"testing"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
)

func TestSlotPings(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Vilnius")
	p := DefaultPrimerPolicy()
	p.Schedules = []PrimerSchedule{
		{Times: []string{"14:00", "9:00"}, Timezone: "Europe/Vilnius"},
		{Times: []string{"02:00"}, Days: []int{1}, Timezone: "Europe/Vilnius"},
		{Times: []string{"12:00"}, Timezone: "Asia/Tokyo"},
	}
	p.Normalize()
	// Sunday 2026-10-11 00:00 in Vilnius.
	from := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)
	got := slotPings(p, 5*time.Hour, from, from.Add(24*time.Hour))
	want := []struct{ at, ready string }{
		{"Sun 01:00", "Sun 06:00"}, // noon in Tokyo
		{"Sun 04:00", "Sun 09:00"},
		{"Sun 09:00", "Sun 14:00"},
		{"Sun 21:00", "Mon 02:00"}, // Monday's time pings the evening before
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pings, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if a, r := got[i].at.In(loc).Format("Mon 15:04"), got[i].ready.In(loc).Format("Mon 15:04"); a != w.at || r != w.ready {
			t.Errorf("ping %d = %s → %s, want %s → %s", i, a, r, w.at, w.ready)
		}
	}
}

func TestPlanPrimer(t *testing.T) {
	now := time.Date(2026, 10, 10, 4, 0, 10, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	session := func(resets *time.Time) *entry {
		return &entry{snap: &quota.Snapshot{Windows: []quota.Window{
			{ID: "5h", Kind: quota.KindSession, WindowMinutes: 300, ResetsAt: resets},
		}}}
	}
	slot := DefaultPrimerPolicy()
	slot.Schedules = []PrimerSchedule{{Times: []string{"09:00"}, Timezone: "UTC"}}
	slot.Normalize()

	job, next := planPrimer(slot, session(nil), now)
	if job == nil || job.trigger != "slot" || job.planned.Hour() != 4 {
		t.Fatalf("idle account should ping for 09:00 at 04:00, got %+v", job)
	}
	if next == nil || next.At.Day() != 11 {
		t.Errorf("next ping should be tomorrow's, got %+v", next)
	}

	// The session before ends in 10 minutes, inside the grace period: wait for it.
	job, next = planPrimer(slot, session(at(10*time.Minute)), now)
	if job != nil || next == nil || !next.At.Equal(now.Add(10*time.Minute+2*time.Second)) {
		t.Fatalf("should wait for the running session, got job %+v next %+v", job, next)
	}

	// Already handled: nothing to do until tomorrow.
	e := session(nil)
	e.primer.handled = map[int64]bool{now.Truncate(time.Hour).Unix(): true}
	if job, _ = planPrimer(slot, e, now); job != nil {
		t.Fatalf("handled slot ran again: %+v", job)
	}
}

func TestPrimerEffective(t *testing.T) {
	ps := DefaultPrimerSettings()
	ps.Schedule.Schedules = []PrimerSchedule{{Times: []string{"14:00", "9:00", "09:00", "nope"}, Timezone: "Mars/Base"}}
	ps.Models = map[string]string{"claude": "sonnet"}
	ps.Normalize()
	p := ps.Effective("claude", AccountPrimer{Enabled: true})
	if !p.Enabled || p.Model != "sonnet" || len(p.Schedules) != 1 {
		t.Fatalf("unexpected effective policy %+v", p)
	}
	if s := p.Schedules[0]; len(s.Times) != 2 || s.Times[0] != "09:00" || s.Times[1] != "14:00" || s.Timezone != "" {
		t.Fatalf("times should be sorted and deduplicated and an unknown zone dropped, got %+v", s)
	}
	if c := ps.Effective("codex", AccountPrimer{}); c.Enabled || c.Model != DefaultPrimerModels["codex"] {
		t.Fatalf("codex should be off with the default model, got %+v", c)
	}
	own := DefaultPrimerPolicy()
	own.Schedules = []PrimerSchedule{{Times: []string{"7:30"}}}
	o := ps.Effective("claude", AccountPrimer{Enabled: true, Model: "opus", Schedule: &own})
	if o.Model != "opus" || len(o.Schedules) != 1 || o.Schedules[0].Times[0] != "07:30" {
		t.Fatalf("account setup should win, got %+v", o)
	}
}
