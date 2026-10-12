package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
)

// PrimerSchedule is a set of times a fresh session should be ready, on some days. Each ping
// goes out one session length before its time.
type PrimerSchedule struct {
	// Times are local "HH:MM".
	Times []string `json:"times"`
	// Days are weekdays (0 = Sunday) of the times; empty means every day.
	Days []int `json:"days"`
	// Timezone reads the times in this IANA zone instead of the server's.
	Timezone string `json:"timezone,omitempty"`
}

// PrimerPolicy decides when maitake starts an account's session window on its own, by sending
// a tiny request while the window is idle. The pool settings hold the schedule accounts share;
// an account can have its own instead.
type PrimerPolicy struct {
	Schedules []PrimerSchedule `json:"schedules"`
	// DelaySeconds waits this long after the previous session ends before a ping that had to
	// wait for it, in case the provider's clock runs a little behind.
	DelaySeconds int `json:"delaySeconds"`
	// Verify checks that the session started after each ping and only retries when it didn't.
	// Off, every follow-up ping is sent regardless.
	Verify        bool `json:"verify"`
	VerifySeconds int  `json:"verifySeconds"`
	// Retries is how many follow-up pings may be sent, RetrySeconds apart.
	Retries      int `json:"retries"`
	RetrySeconds int `json:"retrySeconds"`
	// GraceMinutes still pings a planned slot this late, e.g. after a restart or while the
	// previous session runs out.
	GraceMinutes int `json:"graceMinutes"`
}

// PrimerSettings are the primer's pool-wide settings.
type PrimerSettings struct {
	Schedule PrimerPolicy `json:"schedule"`
	// Models are the ping model per provider; missing ones use DefaultPrimerModels. Claude takes
	// a line name like "haiku" for its newest model; Codex models may carry an effort suffix,
	// e.g. "gpt-5.6-luna(low)".
	Models map[string]string `json:"models"`
	Prompt string            `json:"prompt"`
}

// AccountPrimer is one account's primer setup. Accounts are only primed once turned on.
type AccountPrimer struct {
	Enabled bool `json:"enabled"`
	// Model replaces the provider's ping model when set.
	Model string `json:"model,omitempty"`
	// Schedule replaces the pool schedule when set.
	Schedule *PrimerPolicy `json:"schedule,omitempty"`
}

// PrimerEffective is what the primer does for one account.
type PrimerEffective struct {
	PrimerPolicy
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

// ValidZone reports whether name is an IANA time zone this build knows.
func ValidZone(name string) bool {
	_, err := time.LoadLocation(name)
	return name != "" && err == nil
}

// ZoneName names loc for people. The server's own zone is often just "Local" (always on
// Windows), so it falls back to $TZ and then to the current UTC offset.
func ZoneName(loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	if tz := os.Getenv("TZ"); ValidZone(tz) {
		return tz
	}
	_, off := time.Now().In(loc).Zone()
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, off/3600, off%3600/60)
}

func zone(name string) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

// DefaultPrimerModels are the cheapest models each provider's subscription accepts. "haiku"
// resolves to the newest Haiku the account can use.
var DefaultPrimerModels = map[string]string{"claude": "haiku", "codex": "gpt-5.6-luna(low)"}

func DefaultPrimerPolicy() PrimerPolicy {
	return PrimerPolicy{Schedules: []PrimerSchedule{}, DelaySeconds: 2, Verify: true, VerifySeconds: 15, Retries: 1,
		RetrySeconds: 60, GraceMinutes: 30}
}

func DefaultPrimerSettings() PrimerSettings {
	return PrimerSettings{Schedule: DefaultPrimerPolicy(), Models: map[string]string{}, Prompt: "hi"}
}

func (s *PrimerSettings) Normalize() {
	s.Schedule.Normalize()
	models := map[string]string{}
	for k, v := range s.Models {
		if v = strings.TrimSpace(v); v != "" {
			models[k] = v
		}
	}
	s.Models = models
	s.Prompt = strings.TrimSpace(s.Prompt)
	if s.Prompt == "" {
		s.Prompt = "hi"
	}
}

// Effective is the account's own setup filled in from the pool settings.
func (s PrimerSettings) Effective(providerID string, a AccountPrimer) PrimerEffective {
	p := s.Schedule
	if a.Schedule != nil {
		p = *a.Schedule
		p.Normalize()
	}
	model := strings.TrimSpace(a.Model)
	if model == "" {
		model = s.Models[providerID]
	}
	if model == "" {
		model = DefaultPrimerModels[providerID]
	}
	return PrimerEffective{PrimerPolicy: p, Enabled: a.Enabled, Model: model}
}

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }

// parseClock reads "HH:MM" as minutes after midnight.
func parseClock(s string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func formatClock(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

func normalizeDays(days []int) []int {
	out := []int{}
	for _, d := range days {
		if d >= 0 && d <= 6 && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	if len(out) == 7 {
		return []int{}
	}
	return out
}

func (p *PrimerPolicy) Normalize() {
	schedules := []PrimerSchedule{}
	for _, s := range p.Schedules {
		times := []string{}
		for _, t := range s.Times {
			if at, ok := parseClock(t); ok && !slices.Contains(times, formatClock(at)) {
				times = append(times, formatClock(at))
			}
		}
		slices.Sort(times)
		s.Times = times
		s.Days = normalizeDays(s.Days)
		if s.Timezone = strings.TrimSpace(s.Timezone); !ValidZone(s.Timezone) {
			s.Timezone = ""
		}
		schedules = append(schedules, s)
	}
	p.Schedules = schedules
	p.DelaySeconds = clampInt(p.DelaySeconds, 0, 600)
	p.VerifySeconds = clampInt(p.VerifySeconds, 5, 300)
	p.Retries = clampInt(p.Retries, 0, 10)
	p.RetrySeconds = clampInt(p.RetrySeconds, 10, 3600)
	p.GraceMinutes = clampInt(p.GraceMinutes, 0, 12*60)
}

func ParseAccountPrimer(raw string) AccountPrimer {
	var a AccountPrimer
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &a)
	}
	return a
}

// sessionWindow is the account's short rolling window, if known.
func sessionWindow(s *quota.Snapshot) *quota.Window {
	if s == nil {
		return nil
	}
	for i := range s.Windows {
		if s.Windows[i].Kind == quota.KindSession {
			return &s.Windows[i]
		}
	}
	return nil
}

// sessionLength is how long a session window lasts, five hours unless the provider says otherwise.
func sessionLength(w *quota.Window) time.Duration {
	if w != nil && w.WindowMinutes > 0 {
		return time.Duration(w.WindowMinutes) * time.Minute
	}
	return 5 * time.Hour
}

// sessionEnds is when the running session resets, or nil when none is running.
func sessionEnds(w *quota.Window, now time.Time) *time.Time {
	if w == nil || w.ResetsAt == nil || !w.ResetsAt.After(now) {
		return nil
	}
	return w.ResetsAt
}

func onDay(days []int, t time.Time) bool {
	return len(days) == 0 || slices.Contains(days, int(t.Weekday()))
}

func atClock(day time.Time, minutes int, loc *time.Location) time.Time {
	y, m, d := day.In(loc).Date()
	return time.Date(y, m, d, minutes/60, minutes%60, 0, 0, loc)
}

// slotPing is a planned ping for one occurrence of a scheduled time.
type slotPing struct {
	at    time.Time
	ready time.Time
	loc   *time.Location
}

// slotPings lists the pings the schedules plan between from and to.
func slotPings(p PrimerPolicy, length time.Duration, from, to time.Time) []slotPing {
	var out []slotPing
	for _, s := range p.Schedules {
		loc := zone(s.Timezone)
		for day := from.In(loc).AddDate(0, 0, -1); !day.After(to.In(loc).AddDate(0, 0, 1)); day = day.AddDate(0, 0, 1) {
			for _, c := range s.Times {
				at, ok := parseClock(c)
				if !ok {
					continue
				}
				ready := atClock(day, at, loc)
				if !onDay(s.Days, ready) {
					continue
				}
				if sp := (slotPing{at: ready.Add(-length), ready: ready, loc: loc}); !sp.at.Before(from) && !sp.at.After(to) {
					out = append(out, sp)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b slotPing) int { return a.at.Compare(b.at) })
	return slices.CompactFunc(out, func(a, b slotPing) bool { return a.at.Equal(b.at) })
}
