// Package quota holds the provider-neutral shape of upstream subscription limits and
// builds the filtered views shown on the public quota page.
package quota

import (
	"encoding/json"
	"time"
)

// Window kinds group provider-specific limits so display profiles can toggle them generically.
const (
	KindSession     = "session"      // short rolling window, e.g. 5 hours
	KindWeekly      = "weekly"       // weekly window shared by all models
	KindModelWeekly = "model_weekly" // weekly window for a specific model family
	KindOther       = "other"
)

type Snapshot struct {
	Plan      string     `json:"plan,omitempty"`
	Windows   []Window   `json:"windows"`
	Banked    *Banked    `json:"banked,omitempty"`
	Extra     *Extra     `json:"extra,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Source    string     `json:"source,omitempty"` // "poll" or "headers"
	Error     string     `json:"error,omitempty"`
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
}

type Window struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Label         string     `json:"label"`
	UsedPercent   float64    `json:"usedPercent"`
	ResetsAt      *time.Time `json:"resetsAt,omitempty"`
	WindowMinutes int        `json:"windowMinutes,omitempty"`
	Limited       bool       `json:"limited,omitempty"`
	// Detail is admin-only extra context. It never reaches the quota page.
	Detail string `json:"detail,omitempty"`
	// Amount is set for allowances measured in money rather than a percentage of a limit.
	Amount *Amount `json:"amount,omitempty"`
	// Pace is computed when serving the admin dashboard and never stored.
	Pace *Pace `json:"pace,omitempty"`
}

type Amount struct {
	Currency string  `json:"currency"`
	Limit    float64 `json:"limit"`
	Used     float64 `json:"used"`
}

// Banked describes saved rate-limit resets.
type Banked struct {
	Available int            `json:"available"`
	Credits   []BankedCredit `json:"credits,omitempty"`
	// CooldownUntil is when another reset can be spent after the last one (Claude).
	CooldownUntil *time.Time `json:"cooldownUntil,omitempty"`
}

type BankedCredit struct {
	ID        string     `json:"id"`
	Type      string     `json:"type,omitempty"`
	Title     string     `json:"title,omitempty"`
	GrantedAt *time.Time `json:"grantedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Count is how many resets this credit holds; 0 means one.
	Count int `json:"count,omitempty"`
	// Clears names the windows a reset clears, when the provider says.
	Clears []string `json:"clears,omitempty"`
	// RequiresLimit means it can only be spent while a limit is hit.
	RequiresLimit bool `json:"requiresLimit,omitempty"`
}

func (c BankedCredit) Resets() int {
	if c.Count > 0 {
		return c.Count
	}
	return 1
}

// Extra covers paid usage beyond the plan: Claude extra usage / Codex credits.
type Extra struct {
	Enabled     bool     `json:"enabled"`
	Unlimited   bool     `json:"unlimited,omitempty"`
	UsedPercent *float64 `json:"usedPercent,omitempty"`
	Used        *float64 `json:"used,omitempty"`
	Limit       *float64 `json:"limit,omitempty"`
	Balance     string   `json:"balance,omitempty"`
	Currency    string   `json:"currency,omitempty"`
}

func Parse(raw string) *Snapshot {
	if raw == "" {
		return nil
	}
	var s Snapshot
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

func (s *Snapshot) Encode() string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s *Snapshot) Window(id string) *Window {
	for i := range s.Windows {
		if s.Windows[i].ID == id {
			return &s.Windows[i]
		}
	}
	return nil
}

// Merge overlays fresh windows onto s by ID, keeping windows only present in s. A per-model
// weekly window with an unknown ID (headers don't name the model) updates the snapshot's only
// per-model weekly window instead of adding a duplicate.
func (s *Snapshot) Merge(fresh []Window) {
	for _, w := range fresh {
		if existing := s.Window(w.ID); existing != nil {
			*existing = w
			continue
		}
		if w.Kind == KindModelWeekly {
			var only *Window
			count := 0
			for i := range s.Windows {
				if s.Windows[i].Kind == KindModelWeekly {
					only = &s.Windows[i]
					count++
				}
			}
			if count == 1 {
				only.UsedPercent, only.ResetsAt, only.Limited = w.UsedPercent, w.ResetsAt, w.Limited
				continue
			}
		}
		s.Windows = append(s.Windows, w)
	}
}

// Exhausted reports whether any window is at its limit, returning the latest reset among them.
func (s *Snapshot) Exhausted(now time.Time) (bool, *time.Time) {
	var latest *time.Time
	hit := false
	for _, w := range s.Windows {
		if w.ResetsAt != nil && !w.ResetsAt.After(now) {
			continue
		}
		if w.Limited || w.UsedPercent >= 100 {
			// Only account-wide limits bench the whole account; model and credit limits don't.
			if w.Kind != KindSession && w.Kind != KindWeekly {
				continue
			}
			hit = true
			if w.ResetsAt != nil && (latest == nil || w.ResetsAt.After(*latest)) {
				latest = w.ResetsAt
			}
		}
	}
	return hit, latest
}
