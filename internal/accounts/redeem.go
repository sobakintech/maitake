package accounts

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
)

// RedeemPolicy decides when a Codex account spends its banked resets on its own. The pool
// settings hold the default; each account can override any field.
type RedeemPolicy struct {
	Enabled bool `json:"enabled"`
	// Trigger is which limit being hit spends a reset: "any" (5-hour or weekly), "weekly" or "session".
	Trigger string `json:"trigger"`
	// MinWaitMinutes skips limits that reset sooner than this anyway.
	MinWaitMinutes int `json:"minWaitMinutes"`
	// Reserve keeps this many banked resets unspent by limit triggers.
	Reserve int `json:"reserve"`
	// Order picks which reset to spend: "soonest_expiry" or "latest_expiry".
	Order string `json:"order"`
	// GapMinutes is the least time between two automatic uses on one account.
	GapMinutes int `json:"gapMinutes"`
	// MaxPerDay caps automatic uses per account in 24 hours; 0 is unlimited.
	MaxPerDay int `json:"maxPerDay"`
	// ExpiringHours spends a reset that expires within this many hours (0 = off), so it isn't
	// lost, once the account has used at least ExpiringMinUsedPercent of a limit.
	ExpiringHours          int `json:"expiringHours"`
	ExpiringMinUsedPercent int `json:"expiringMinUsedPercent"`
}

func DefaultRedeemPolicy() RedeemPolicy {
	return RedeemPolicy{Trigger: "any", MinWaitMinutes: 60, Order: "soonest_expiry", GapMinutes: 15,
		ExpiringMinUsedPercent: 50}
}

func oneOf(v string, allowed ...string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return allowed[0]
}

func atLeast(v, min int) int {
	if v < min {
		return min
	}
	return v
}

func (p *RedeemPolicy) Normalize() {
	p.Trigger = oneOf(p.Trigger, "any", "weekly", "session")
	p.Order = oneOf(p.Order, "soonest_expiry", "latest_expiry")
	p.MinWaitMinutes = atLeast(p.MinWaitMinutes, 0)
	p.Reserve = atLeast(p.Reserve, 0)
	p.GapMinutes = atLeast(p.GapMinutes, 1)
	p.MaxPerDay = atLeast(p.MaxPerDay, 0)
	p.ExpiringHours = atLeast(p.ExpiringHours, 0)
	p.ExpiringMinUsedPercent = min(atLeast(p.ExpiringMinUsedPercent, 0), 100)
}

// RedeemOverride is an account's own redeem rules; nil fields use the pool default.
type RedeemOverride struct {
	Enabled                *bool   `json:"enabled,omitempty"`
	Trigger                *string `json:"trigger,omitempty"`
	MinWaitMinutes         *int    `json:"minWaitMinutes,omitempty"`
	Reserve                *int    `json:"reserve,omitempty"`
	Order                  *string `json:"order,omitempty"`
	GapMinutes             *int    `json:"gapMinutes,omitempty"`
	MaxPerDay              *int    `json:"maxPerDay,omitempty"`
	ExpiringHours          *int    `json:"expiringHours,omitempty"`
	ExpiringMinUsedPercent *int    `json:"expiringMinUsedPercent,omitempty"`
}

func ParseRedeemOverride(raw string) RedeemOverride {
	var o RedeemOverride
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &o)
	}
	return o
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// Apply layers the override on top of base.
func (o RedeemOverride) Apply(base RedeemPolicy) RedeemPolicy {
	p := base
	set(&p.Enabled, o.Enabled)
	set(&p.Trigger, o.Trigger)
	set(&p.MinWaitMinutes, o.MinWaitMinutes)
	set(&p.Reserve, o.Reserve)
	set(&p.Order, o.Order)
	set(&p.GapMinutes, o.GapMinutes)
	set(&p.MaxPerDay, o.MaxPerDay)
	set(&p.ExpiringHours, o.ExpiringHours)
	set(&p.ExpiringMinUsedPercent, o.ExpiringMinUsedPercent)
	p.Normalize()
	return p
}

// limitHit reports whether a limit matching trigger is used up, with the latest of their resets.
func limitHit(s *quota.Snapshot, trigger string, now time.Time) (bool, *time.Time) {
	var latest *time.Time
	hit := false
	for _, w := range s.Windows {
		if w.ResetsAt != nil && !w.ResetsAt.After(now) {
			continue
		}
		matches := w.Kind == quota.KindSession && trigger != "weekly" || w.Kind == quota.KindWeekly && trigger != "session"
		if !matches || !(w.Limited || w.UsedPercent >= 100) {
			continue
		}
		hit = true
		if w.ResetsAt != nil && (latest == nil || w.ResetsAt.After(*latest)) {
			latest = w.ResetsAt
		}
	}
	return hit, latest
}

// mostUsed is the highest usage among account-wide limits that haven't reset.
func mostUsed(s *quota.Snapshot, now time.Time) float64 {
	top := 0.0
	for _, w := range s.Windows {
		if (w.Kind == quota.KindSession || w.Kind == quota.KindWeekly) && (w.ResetsAt == nil || w.ResetsAt.After(now)) {
			top = max(top, w.UsedPercent)
		}
	}
	return top
}

// pickRedeem decides whether to spend a reset now, returning the credit to use ("" for any)
// and why, or ok=false.
func pickRedeem(p RedeemPolicy, s *quota.Snapshot, now time.Time) (creditID, reason string, ok bool) {
	credits := s.Banked.Credits // soonest-expiring first
	if hit, until := limitHit(s, p.Trigger, now); hit && s.Banked.Available > p.Reserve &&
		(until == nil || until.Sub(now) >= time.Duration(p.MinWaitMinutes)*time.Minute) {
		if len(credits) > 0 {
			creditID = credits[0].ID
			if p.Order == "latest_expiry" {
				creditID = credits[len(credits)-1].ID
			}
		}
		return creditID, "limit reached", true
	}
	if p.ExpiringHours > 0 && len(credits) > 0 {
		c := credits[0]
		if c.ExpiresAt != nil && c.ExpiresAt.Sub(now) <= time.Duration(p.ExpiringHours)*time.Hour &&
			mostUsed(s, now) >= float64(p.ExpiringMinUsedPercent) {
			return c.ID, "about to expire", true
		}
	}
	return "", "", false
}
