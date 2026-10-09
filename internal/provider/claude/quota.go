package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/quota"
)

func parseResetTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
		t := time.Unix(int64(f), 0).UTC()
		return &t
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123} {
		if t, err := time.Parse(layout, raw); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// WindowsFromHeaders reads the anthropic-ratelimit-unified-* headers sent on every response.
// Utilization headers are 0-1 fractions; resets are unix seconds.
func WindowsFromHeaders(h http.Header) []quota.Window {
	var out []quota.Window
	add := func(prefix, id, kind, label string, minutes int) {
		util := h.Get("anthropic-ratelimit-unified-" + prefix + "-utilization")
		status := strings.ToLower(h.Get("anthropic-ratelimit-unified-" + prefix + "-status"))
		reset := parseResetTime(h.Get("anthropic-ratelimit-unified-" + prefix + "-reset"))
		if util == "" && status == "" && reset == nil {
			return
		}
		w := quota.Window{ID: id, Kind: kind, Label: label, ResetsAt: reset, WindowMinutes: minutes,
			Limited: status == "rejected"}
		if f, err := strconv.ParseFloat(util, 64); err == nil {
			w.UsedPercent = clampPct(f * 100)
		}
		if w.Limited {
			w.UsedPercent = 100
		}
		out = append(out, w)
	}
	add("5h", "5h", quota.KindSession, "5-hour", 300)
	add("7d", "7d", quota.KindWeekly, "Weekly", 10080)
	add("7d_oi", "7d_oi", quota.KindModelWeekly, "Weekly (model limit)", 10080)
	return out
}

// CooldownFromHeaders returns when a rate-limited account becomes usable again.
func CooldownFromHeaders(h http.Header, now time.Time) *time.Time {
	var latest *time.Time
	consider := func(t *time.Time) {
		if t != nil && t.After(now) && t.Sub(now) < 8*24*time.Hour && (latest == nil || t.After(*latest)) {
			latest = t
		}
	}
	for _, prefix := range []string{"5h", "7d"} {
		if strings.EqualFold(h.Get("anthropic-ratelimit-unified-"+prefix+"-status"), "rejected") {
			consider(parseResetTime(h.Get("anthropic-ratelimit-unified-" + prefix + "-reset")))
		}
	}
	if latest == nil && strings.EqualFold(h.Get("anthropic-ratelimit-unified-status"), "rejected") &&
		!overageBoundary(h) {
		consider(parseResetTime(h.Get("anthropic-ratelimit-unified-reset")))
	}
	if latest == nil {
		if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs > 0 {
			t := now.Add(time.Duration(secs) * time.Second)
			consider(&t)
		}
	}
	return latest
}

// overageBoundary reports a unified reset that is just the end of the overage billing period
// (the representative claim is overage and both resets match), not when the account recovers.
func overageBoundary(h http.Header) bool {
	if !strings.Contains(strings.ToLower(h.Get("anthropic-ratelimit-unified-representative-claim")), "overage") {
		return false
	}
	unified := parseResetTime(h.Get("anthropic-ratelimit-unified-reset"))
	overage := parseResetTime(h.Get("anthropic-ratelimit-unified-overage-reset"))
	return unified != nil && overage != nil && unified.Equal(*overage)
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// FetchQuota polls Claude's OAuth usage endpoint (the one behind Claude Code's /usage).
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) (*quota.Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.APIBase+"/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", userAgent)
	var raw map[string]json.RawMessage
	if err := provider.DoJSON(p.http(), req, &raw); err != nil {
		return nil, err
	}
	return snapshotFromUsage(raw), nil
}

type usageWindow struct {
	Utilization     *float64 `json:"utilization"`
	ResetsAt        *string  `json:"resets_at"`
	LimitDollars    *float64 `json:"limit_dollars"`
	UsedDollars     *float64 `json:"used_dollars"`
	RemainingDollar *float64 `json:"remaining_dollars"`
}

// usageLimit is one entry of the "limits" list, which carries proper display names.
type usageLimit struct {
	Kind     string   `json:"kind"`
	Group    string   `json:"group"`
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Surface *struct {
			DisplayName string `json:"display_name"`
		} `json:"surface"`
	} `json:"scope"`
}

var usageLabels = map[string]string{
	"seven_day_opus":       "Weekly · Opus",
	"seven_day_sonnet":     "Weekly · Sonnet",
	"seven_day_fable":      "Weekly · Fable",
	"seven_day_oauth_apps": "Weekly · OAuth apps",
}

func slug(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
}

func (l usageLimit) scopeName() string {
	if l.Scope == nil {
		return ""
	}
	if l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
		return l.Scope.Model.DisplayName
	}
	if l.Scope.Surface != nil {
		return l.Scope.Surface.DisplayName
	}
	return ""
}

func (l usageLimit) window() quota.Window {
	w := quota.Window{}
	if l.Percent != nil {
		w.UsedPercent = clampPct(*l.Percent)
	}
	if l.ResetsAt != nil {
		w.ResetsAt = parseResetTime(*l.ResetsAt)
	}
	name := l.scopeName()
	switch {
	case l.Kind == "session":
		w.ID, w.Kind, w.Label, w.WindowMinutes = "5h", quota.KindSession, "5-hour", 300
	case l.Kind == "weekly_all":
		w.ID, w.Kind, w.Label, w.WindowMinutes = "7d", quota.KindWeekly, "Weekly", 10080
	case l.Group == "weekly":
		w.ID, w.Kind, w.WindowMinutes = "7d_"+slug(name), quota.KindModelWeekly, 10080
		w.Label = "Weekly · " + name
		if name == "" {
			w.ID, w.Label = "7d_"+l.Kind, "Weekly · "+titleCase(strings.ReplaceAll(l.Kind, "_", " "))
		}
	default:
		w.ID, w.Kind = l.Kind, quota.KindOther
		w.Label = titleCase(strings.ReplaceAll(l.Kind, "_", " "))
		if name != "" {
			w.ID += "_" + slug(name)
			w.Label += " · " + name
		}
	}
	w.Limited = w.UsedPercent >= 100
	return w
}

func snapshotFromUsage(raw map[string]json.RawMessage) *quota.Snapshot {
	now := time.Now().UTC()
	s := &quota.Snapshot{UpdatedAt: now, Source: "poll", FetchedAt: &now, Windows: []quota.Window{}}

	var limits []usageLimit
	_ = json.Unmarshal(raw["limits"], &limits)
	for _, l := range limits {
		s.Windows = append(s.Windows, l.window())
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var w usageWindow
		if json.Unmarshal(raw[k], &w) != nil || w.Utilization == nil {
			continue
		}
		// Dollar-denominated allowances sit under internal code names; label them by what they are.
		if w.LimitDollars != nil {
			used := 0.0
			if w.UsedDollars != nil {
				used = *w.UsedDollars
			}
			win := quota.Window{ID: "credit_" + k, Kind: quota.KindOther, UsedPercent: clampPct(*w.Utilization),
				Label:  "Credit allowance",
				Amount: &quota.Amount{Currency: "USD", Limit: *w.LimitDollars, Used: used}}
			if w.ResetsAt != nil {
				win.ResetsAt = parseResetTime(*w.ResetsAt)
			}
			win.Limited = win.UsedPercent >= 100
			s.Windows = append(s.Windows, win)
			continue
		}
		if len(limits) > 0 {
			continue // the limits list already covers percentage windows
		}
		win := quota.Window{ID: k, UsedPercent: clampPct(*w.Utilization)}
		if w.ResetsAt != nil {
			win.ResetsAt = parseResetTime(*w.ResetsAt)
		}
		switch {
		case k == "five_hour":
			win.ID, win.Kind, win.Label, win.WindowMinutes = "5h", quota.KindSession, "5-hour", 300
		case k == "seven_day":
			win.ID, win.Kind, win.Label, win.WindowMinutes = "7d", quota.KindWeekly, "Weekly", 10080
		case strings.HasPrefix(k, "seven_day_"):
			win.Kind, win.WindowMinutes = quota.KindModelWeekly, 10080
			win.Label = usageLabels[k]
			if win.Label == "" {
				win.Label = "Weekly · " + titleCase(strings.ReplaceAll(strings.TrimPrefix(k, "seven_day_"), "_", " "))
			}
		default:
			win.Kind, win.Label = quota.KindOther, titleCase(strings.ReplaceAll(k, "_", " "))
		}
		win.Limited = win.UsedPercent >= 100
		s.Windows = append(s.Windows, win)
	}
	order := map[string]int{quota.KindSession: 0, quota.KindWeekly: 1, quota.KindModelWeekly: 2, quota.KindOther: 3}
	sort.SliceStable(s.Windows, func(i, j int) bool { return order[s.Windows[i].Kind] < order[s.Windows[j].Kind] })

	if extra, ok := raw["extra_usage"]; ok {
		var e struct {
			IsEnabled    bool     `json:"is_enabled"`
			MonthlyLimit *float64 `json:"monthly_limit"`
			UsedCredits  *float64 `json:"used_credits"`
			Utilization  *float64 `json:"utilization"`
		}
		if json.Unmarshal(extra, &e) == nil {
			s.Extra = &quota.Extra{Enabled: e.IsEnabled, Used: e.UsedCredits, Limit: e.MonthlyLimit, UsedPercent: e.Utilization}
		}
	}
	return s
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// ModelScopedLimit reports a 429 caused only by a per-model or overage limit, where the account
// can still serve other models.
func ModelScopedLimit(h http.Header) bool {
	rejected := func(prefix string) bool {
		return strings.EqualFold(h.Get("anthropic-ratelimit-unified-"+prefix+"-status"), "rejected")
	}
	if rejected("5h") || rejected("7d") {
		return false
	}
	return rejected("7d_oi") || strings.EqualFold(h.Get("anthropic-ratelimit-unified-overage-status"), "rejected")
}
