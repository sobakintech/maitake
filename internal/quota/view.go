package quota

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/sobakintech/maitake/internal/resets"
)

// AccountInfo is the input for one upstream account.
type AccountInfo struct {
	ID       int64
	Provider string
	Label    string
	Alias    string
	Plan     string
	Status   string // ok, cooldown, error, disabled
	Snapshot *Snapshot
}

var providerNames = map[string]string{"claude": "Claude", "codex": "Codex"}

type View struct {
	Title         string         `json:"title"`
	Notice        string         `json:"notice"`
	NumberFormat  string         `json:"numberFormat"`
	ResetFormat   string         `json:"resetFormat"`
	ShowUpdatedAt bool           `json:"showUpdatedAt"`
	Providers     []ProviderView `json:"providers"`
	CodexResets   *ResetsView    `json:"codexResets,omitempty"`
	GeneratedAt   time.Time      `json:"generatedAt"`
}

type ProviderView struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Pool     *EntryView  `json:"pool,omitempty"`
	Accounts []EntryView `json:"accounts,omitempty"`
	Banked   *BankedView `json:"banked,omitempty"`
}

type EntryView struct {
	Name         string       `json:"name,omitempty"`
	Plan         string       `json:"plan,omitempty"`
	Status       string       `json:"status,omitempty"`
	AccountCount int          `json:"accountCount,omitempty"`
	Windows      []WindowView `json:"windows"`
	Banked       *BankedView  `json:"banked,omitempty"`
	Extra        *ExtraView   `json:"extra,omitempty"`
	UpdatedAt    *time.Time   `json:"updatedAt,omitempty"`
}

type WindowView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Value is in Unit: "percent_remaining", "percent_used" or "accounts" (pooled total). Omitted for level format.
	Value    *float64   `json:"value,omitempty"`
	Unit     string     `json:"unit,omitempty"`
	Level    string     `json:"level"` // plenty, ok, low, out
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
	// Amount is shown for money allowances when the profile allows it.
	Amount *Amount `json:"amount,omitempty"`
}

type BankedView struct {
	Available int         `json:"available"`
	Expiries  []time.Time `json:"expiries,omitempty"`
}

type ExtraView struct {
	Enabled     bool     `json:"enabled"`
	Unlimited   bool     `json:"unlimited,omitempty"`
	UsedPercent *float64 `json:"usedPercent,omitempty"`
}

type ResetsView struct {
	Latest    *resets.Reset `json:"latest,omitempty"`
	Scheduled *resets.Reset `json:"scheduled,omitempty"`
	Forecast  *resets.Watch `json:"forecast,omitempty"`
	Stats     *resets.Stats `json:"stats,omitempty"`
}

func level(remaining float64) string {
	switch {
	case remaining <= 0:
		return "out"
	case remaining <= 20:
		return "low"
	case remaining <= 50:
		return "ok"
	default:
		return "plenty"
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// remaining returns the remaining percent of w, treating windows whose reset has passed as fresh.
func remaining(w Window, now time.Time) float64 {
	if w.ResetsAt != nil && !w.ResetsAt.After(now) {
		return 100
	}
	r := 100 - w.UsedPercent
	if w.Limited {
		r = 0
	}
	return math.Max(0, math.Min(100, r))
}

func (p *Profile) windowView(w Window, rem float64, now time.Time) WindowView {
	v := WindowView{ID: w.ID, Kind: w.Kind, Label: w.Label, Level: level(rem)}
	switch p.NumberFormat {
	case "remaining":
		val := round1(rem)
		v.Value, v.Unit = &val, "percent_remaining"
	case "used":
		val := round1(100 - rem)
		v.Value, v.Unit = &val, "percent_used"
	case "bar":
		val := math.Round(rem/10) * 10
		v.Value, v.Unit = &val, "percent_remaining"
	}
	if p.ResetFormat != "hidden" && w.ResetsAt != nil && w.ResetsAt.After(now) {
		t := *w.ResetsAt
		v.ResetsAt = &t
	}
	if p.ShowAmounts && w.Amount != nil && (p.NumberFormat == "remaining" || p.NumberFormat == "used") {
		a := *w.Amount
		if rem >= 100 {
			a.Used = 0 // the window has reset since it was last read
		}
		v.Amount = &a
	}
	return v
}

func (p *Profile) bankedView(banks []*Banked, now time.Time) *BankedView {
	if p.Banked == "hidden" {
		return nil
	}
	var out BankedView
	seen := false
	for _, b := range banks {
		if b == nil {
			continue
		}
		seen = true
		out.Available += b.Available
		if p.Banked == "count_expiry" {
			for _, c := range b.Credits {
				if c.ExpiresAt != nil && c.ExpiresAt.After(now) {
					out.Expiries = append(out.Expiries, *c.ExpiresAt)
				}
			}
		}
	}
	if !seen {
		return nil
	}
	sort.Slice(out.Expiries, func(i, j int) bool { return out.Expiries[i].Before(out.Expiries[j]) })
	if len(out.Expiries) > 20 {
		out.Expiries = out.Expiries[:20]
	}
	return &out
}

func (p *Profile) extraView(e *Extra) *ExtraView {
	if !p.Extra || e == nil {
		return nil
	}
	v := &ExtraView{Enabled: e.Enabled, Unlimited: e.Unlimited}
	if p.NumberFormat != "level" && e.UsedPercent != nil {
		u := round1(*e.UsedPercent)
		v.UsedPercent = &u
	}
	return v
}

func (p *Profile) entryName(a AccountInfo, index int) string {
	switch p.AccountNaming {
	case "label":
		return a.Label
	case "alias":
		if a.Alias != "" {
			return a.Alias
		}
	case "hidden":
		return ""
	}
	return "Account " + itoa(index+1)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// Build renders the quota page for one profile. Accounts the profile hides never reach the output.
func Build(p Profile, page PageSettings, accounts []AccountInfo, codexResets *resets.Snapshot, now time.Time) View {
	view := View{
		Title:         page.Title,
		Notice:        page.Notice,
		NumberFormat:  p.NumberFormat,
		ResetFormat:   p.ResetFormat,
		ShowUpdatedAt: p.ShowUpdatedAt,
		Providers:     []ProviderView{},
		GeneratedAt:   now,
	}

	for _, provider := range []string{"claude", "codex"} {
		if !slices.Contains(p.Providers, provider) {
			continue
		}
		var visible []AccountInfo
		for _, a := range accounts {
			if a.Provider == provider && a.Status != "disabled" && !slices.Contains(p.HiddenAccounts, a.ID) {
				visible = append(visible, a)
			}
		}
		if len(visible) == 0 {
			continue
		}
		pv := ProviderView{ID: provider, Name: providerNames[provider]}

		if p.Mode != "pooled" {
			for i, a := range visible {
				pv.Accounts = append(pv.Accounts, p.accountEntry(a, i, now))
			}
		}
		if p.Mode != "accounts" {
			pv.Pool = p.poolEntry(visible, now)
		}
		if p.BankedMode == "pooled" {
			var banks []*Banked
			for _, a := range visible {
				if a.Snapshot != nil {
					banks = append(banks, a.Snapshot.Banked)
				}
			}
			pv.Banked = p.bankedView(banks, now)
		}
		view.Providers = append(view.Providers, pv)
	}

	if slices.Contains(p.Providers, "codex") && codexResets != nil && codexResets.Status != nil {
		st := codexResets.Status
		rv := &ResetsView{}
		if p.CodexResets.Latest {
			rv.Latest = st.Latest
		}
		if p.CodexResets.Scheduled {
			rv.Scheduled = st.Scheduled
		}
		if p.CodexResets.Forecast && st.Watch != nil && st.Watch.ExpiresAt.After(now) {
			rv.Forecast = st.Watch
		}
		if p.CodexResets.Stats {
			stats := st.Stats
			rv.Stats = &stats
		}
		if rv.Latest != nil || rv.Scheduled != nil || rv.Forecast != nil || rv.Stats != nil {
			view.CodexResets = rv
		}
	}
	return view
}

func (p *Profile) accountEntry(a AccountInfo, index int, now time.Time) EntryView {
	e := EntryView{Name: p.entryName(a, index), Windows: []WindowView{}}
	if p.ShowPlan {
		e.Plan = a.Plan
	}
	s := a.Snapshot
	if p.ShowStatus {
		e.Status = a.Status
		if s != nil && a.Status == "ok" {
			if hit, _ := s.Exhausted(now); hit {
				e.Status = "exhausted"
			}
		}
	}
	if s == nil {
		return e
	}
	for _, w := range s.Windows {
		if p.showsKind(w.Kind) {
			e.Windows = append(e.Windows, p.windowView(w, remaining(w, now), now))
		}
	}
	if p.BankedMode == "accounts" {
		e.Banked = p.bankedView([]*Banked{s.Banked}, now)
	}
	e.Extra = p.extraView(s.Extra)
	if p.ShowUpdatedAt && !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		e.UpdatedAt = &t
	}
	return e
}

func (p *Profile) poolEntry(accounts []AccountInfo, now time.Time) *EntryView {
	type agg struct {
		w     Window
		rems  []float64
		reset *time.Time
	}
	byID := map[string]*agg{}
	var order []string
	var updated *time.Time
	for _, a := range accounts {
		s := a.Snapshot
		if s == nil {
			continue
		}
		if !s.UpdatedAt.IsZero() && (updated == nil || s.UpdatedAt.Before(*updated)) {
			t := s.UpdatedAt
			updated = &t
		}
		for _, w := range s.Windows {
			if !p.showsKind(w.Kind) {
				continue
			}
			g, ok := byID[w.ID]
			if !ok {
				g = &agg{w: w}
				byID[w.ID] = g
				order = append(order, w.ID)
			}
			g.rems = append(g.rems, remaining(w, now))
			if w.ResetsAt != nil && w.ResetsAt.After(now) && (g.reset == nil || w.ResetsAt.Before(*g.reset)) {
				g.reset = w.ResetsAt
			}
		}
	}

	e := &EntryView{Name: "All accounts", AccountCount: len(accounts), Windows: []WindowView{}}
	for _, id := range order {
		g := byID[id]
		var rem float64
		switch p.PoolAggregate {
		case "min":
			rem = slices.Min(g.rems)
		case "max":
			rem = slices.Max(g.rems)
		default:
			for _, r := range g.rems {
				rem += r
			}
			rem /= float64(len(g.rems))
		}
		w := g.w
		w.ResetsAt = g.reset
		w.Limited = false
		w.Amount = nil // amounts differ per account
		w.UsedPercent = 100 - rem
		v := p.windowView(w, rem, now)
		if p.PoolAggregate == "total" && p.NumberFormat != "level" {
			total := 0.0
			for _, r := range g.rems {
				total += r / 100
			}
			total = round1(total)
			v.Value, v.Unit = &total, "accounts"
		}
		e.Windows = append(e.Windows, v)
	}
	if p.ShowUpdatedAt {
		e.UpdatedAt = updated
	}
	return e
}
