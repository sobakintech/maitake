package accounts

import (
	"testing"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
)

func TestPickRedeem(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	snap := func(sessionUsed, weeklyUsed float64, weeklyReset time.Duration, credits ...quota.BankedCredit) *quota.Snapshot {
		return &quota.Snapshot{
			Windows: []quota.Window{
				{ID: "5h", Kind: quota.KindSession, UsedPercent: sessionUsed, ResetsAt: at(2 * time.Hour)},
				{ID: "7d", Kind: quota.KindWeekly, UsedPercent: weeklyUsed, ResetsAt: at(weeklyReset)},
			},
			Banked: &quota.Banked{Available: len(credits), Credits: credits},
		}
	}
	soon := quota.BankedCredit{ID: "soon", ExpiresAt: at(3 * time.Hour)}
	late := quota.BankedCredit{ID: "late", ExpiresAt: at(20 * 24 * time.Hour)}
	base := DefaultRedeemPolicy()
	base.Enabled = true

	if id, _, ok := pickRedeem(base, snap(100, 50, 72*time.Hour, soon, late), now); !ok || id != "soon" {
		t.Fatalf("session limit should spend the soonest-expiring reset, got %q %v", id, ok)
	}
	weekly := base
	weekly.Trigger = "weekly"
	if _, _, ok := pickRedeem(weekly, snap(100, 50, 72*time.Hour, soon, late), now); ok {
		t.Fatal("weekly trigger must ignore the 5-hour limit")
	}
	latest := base
	latest.Order = "latest_expiry"
	if id, _, _ := pickRedeem(latest, snap(50, 100, 72*time.Hour, soon, late), now); id != "late" {
		t.Fatalf("latest_expiry should pick the later reset, got %q", id)
	}
	reserve := base
	reserve.Reserve = 2
	if _, _, ok := pickRedeem(reserve, snap(50, 100, 72*time.Hour, soon, late), now); ok {
		t.Fatal("reserve should keep both resets")
	}
	if _, _, ok := pickRedeem(base, snap(50, 100, 30*time.Minute, soon, late), now); ok {
		t.Fatal("a limit resetting within the minimum wait shouldn't spend a reset")
	}
	expiring := base
	expiring.ExpiringHours = 6
	if id, reason, ok := pickRedeem(expiring, snap(20, 60, 72*time.Hour, soon, late), now); !ok || id != "soon" || reason != "about to expire" {
		t.Fatalf("an expiring reset should be used once usage passes the threshold, got %q %q %v", id, reason, ok)
	}
	if _, _, ok := pickRedeem(expiring, snap(20, 30, 72*time.Hour, soon, late), now); ok {
		t.Fatal("below the usage threshold the expiring reset should wait")
	}

	on, wait := true, 5
	p := RedeemOverride{Enabled: &on, MinWaitMinutes: &wait}.Apply(DefaultRedeemPolicy())
	if !p.Enabled || p.MinWaitMinutes != 5 || p.Trigger != "any" {
		t.Fatalf("override not applied: %+v", p)
	}
}
