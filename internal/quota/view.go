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
	Title         string `json:"title"`
	Notice        string `json:"notice"`
	NumberFormat  string `json:"numberFormat"`
	ResetFormat   string `json:"resetFormat"`
	ShowUpdatedAt bool   `json:"showUpdatedAt"`

	Layout            string `json:"layout"`
	AllowLayoutSwitch bool   `json:"allowLayoutSwitch"`
	AllowFlip         bool   `json:"allowFlip"`
	Width             string `json:"width"`
	FeedsPosition     string `json:"feedsPosition"`
	ShowAccountCount  bool   `json:"showAccountCount"`
	RefreshSeconds    int    `json:"refreshSeconds"`

	Providers    []ProviderView `json:"providers"`
	CodexResets  *ResetsView    `json:"codexResets,omitempty"`
	ClaudeResets *ResetsView    `json:"claudeResets,omitempty"`
	GeneratedAt  time.Time      `json:"generatedAt"`
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
	Pace   *Pace   `json:"pace,omitempty"`
}

type BankedView struct {
	Available int         `json:"available"`
	Expiries  []time.Time `json:"expiries,omitempty"`
	// Credits lists each reset when the profile shows them in full.
	Credits []BankedCreditView `json:"credits,omitempty"`
}

type BankedCreditView struct {
	Title         string     `json:"title,omitempty"`
	Count         int        `json:"count"`
	GrantedAt     *time.Time `json:"grantedAt,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	RequiresLimit bool       `json:"requiresLimit,omitempty"`
	// Account names the account holding it, in a provider total.
	Account string `json:"account,omitempty"`
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

func (p *Profile) level(remaining float64) string {
	switch {
	case remaining <= 0:
		return "out"
	case remaining <= p.Levels.Low:
		return "low"
	case remaining <= p.Levels.OK:
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

func (p *Profile) windowView(w Window, rem float64, pace *Pace, now time.Time) WindowView {
	v := WindowView{ID: w.ID, Kind: w.Kind, Label: w.Label, Level: p.level(rem)}
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
	v.Pace = p.paceView(pace)
	return v
}

// paceView trims a projection down to what the profile's number and reset formats allow.
func (p *Profile) paceView(pc *Pace) *Pace {
	if pc == nil || p.Pace == "hidden" || (p.Pace == "warnings" && pc.Status == PaceOnTrack) {
		return nil
	}
	v := Pace{Status: pc.Status}
	switch p.NumberFormat {
	case "level":
		return &v
	case "bar":
		v.ProjectedUsed = math.Round(pc.ProjectedUsed/10) * 10
	default:
		v.ProjectedUsed = round1(pc.ProjectedUsed)
	}
	if p.ResetFormat != "hidden" {
		v.Elapsed = round1(pc.Elapsed)
	}
	if pc.RunsOutAt != nil {
		t := pc.RunsOutAt.Truncate(time.Minute)
		v.RunsOutAt = &t
	}
	return &v
}

// poolPace combines per-account projections for the average and total aggregates. Accounts
// without a projection are assumed to stay where they are.
func poolPace(useds []float64, paces []*Pace) *Pace {
	var projected, elapsed float64
	tracked := 0
	for i, pc := range paces {
		if pc == nil {
			projected += useds[i]
			continue
		}
		projected += pc.ProjectedUsed
		elapsed += pc.Elapsed
		tracked++
	}
	if tracked == 0 {
		return nil
	}
	projected /= float64(len(paces))
	return &Pace{Status: paceStatus(projected), ProjectedUsed: projected, Elapsed: elapsed / float64(tracked)}
}

// bankedView sums the banked resets of some accounts. owners names each bank's account for the
// full list; it's nil for a single account.
func (p *Profile) bankedView(banks []*Banked, owners []string, now time.Time) *BankedView {
	if p.Banked == "hidden" {
		return nil
	}
	var out BankedView
	seen := false
	for i, b := range banks {
		if b == nil {
			continue
		}
		seen = true
		out.Available += b.Available
		if p.Banked == "count" {
			continue
		}
		for _, c := range b.Credits {
			if c.ExpiresAt != nil && !c.ExpiresAt.After(now) {
				continue
			}
			if c.ExpiresAt != nil {
				for range min(c.Resets(), 20) {
					out.Expiries = append(out.Expiries, *c.ExpiresAt)
				}
			}
			if p.Banked == "full" {
				v := BankedCreditView{Title: c.Title, Count: c.Resets(), GrantedAt: c.GrantedAt,
					ExpiresAt: c.ExpiresAt, RequiresLimit: c.RequiresLimit}
				if owners != nil {
					v.Account = owners[i]
				}
				out.Credits = append(out.Credits, v)
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
	// Soonest to expire first; resets that never expire go last.
	sort.SliceStable(out.Credits, func(i, j int) bool {
		a, b := out.Credits[i].ExpiresAt, out.Credits[j].ExpiresAt
		return a != nil && (b == nil || a.Before(*b))
	})
	if len(out.Credits) > 100 {
		out.Credits = out.Credits[:100]
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
// Feeds are the latest reset feed snapshots; either can be nil.
type Feeds struct {
	Codex, Claude *resets.Snapshot
}

func Build(p Profile, page PageSettings, accounts []AccountInfo, feeds Feeds, now time.Time) View {
	view := View{
		Title:         page.Title,
		Notice:        page.Notice,
		NumberFormat:  p.NumberFormat,
		ResetFormat:   p.ResetFormat,
		ShowUpdatedAt: p.ShowUpdatedAt,

		Layout:            p.Layout,
		AllowLayoutSwitch: p.AllowLayoutSwitch,
		AllowFlip:         p.AllowFlip && (p.NumberFormat == "remaining" || p.NumberFormat == "used"),
		Width:             p.Width,
		FeedsPosition:     p.FeedsPosition,
		ShowAccountCount:  p.ShowAccountCount,
		RefreshSeconds:    page.RefreshSeconds,

		Providers:   []ProviderView{},
		GeneratedAt: now,
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
			p.sortAccounts(pv.Accounts, visible, now)
		}
		if p.Mode != "accounts" {
			pv.Pool = p.poolEntry(visible, now)
		}
		if p.BankedMode == "pooled" {
			var banks []*Banked
			var owners []string
			for i, a := range visible {
				if a.Snapshot != nil {
					banks = append(banks, a.Snapshot.Banked)
					owners = append(owners, p.entryName(a, i))
				}
			}
			pv.Banked = p.bankedView(banks, owners, now)
		}
		view.Providers = append(view.Providers, pv)
	}

	if slices.Contains(p.Providers, "claude") {
		view.ClaudeResets = resetsView(p.ClaudeResets, feeds.Claude, now)
	}
	if slices.Contains(p.Providers, "codex") {
		view.CodexResets = resetsView(p.CodexResets, feeds.Codex, now)
	}
	return view
}

func resetsView(d ResetsDisplay, snap *resets.Snapshot, now time.Time) *ResetsView {
	if snap == nil || snap.Status == nil {
		return nil
	}
	st := snap.Status
	rv := &ResetsView{}
	if d.Latest {
		rv.Latest = st.Latest
	}
	if d.Scheduled {
		rv.Scheduled = st.Scheduled
	}
	if d.Forecast && st.Watch != nil && st.Watch.ExpiresAt.After(now) {
		rv.Forecast = st.Watch
	}
	if d.Stats {
		stats := st.Stats
		rv.Stats = &stats
	}
	if rv.Latest == nil && rv.Scheduled == nil && rv.Forecast == nil && rv.Stats == nil {
		return nil
	}
	return rv
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
			e.Windows = append(e.Windows, p.windowView(w, remaining(w, now), ProjectPace(w, now), now))
		}
	}
	if p.BankedMode == "accounts" {
		e.Banked = p.bankedView([]*Banked{s.Banked}, nil, now)
	}
	e.Extra = p.extraView(s.Extra)
	if p.ShowUpdatedAt && !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		e.UpdatedAt = &t
	}
	return e
}

// sortAccounts orders account entries, which line up with visible, by the profile's sort.
func (p *Profile) sortAccounts(entries []EntryView, visible []AccountInfo, now time.Time) {
	if p.AccountSort == "default" || len(entries) < 2 {
		return
	}
	// least is the lowest share left across the windows the profile shows; no data sorts last.
	least := make([]float64, len(visible))
	for i, a := range visible {
		least[i] = -1
		if a.Snapshot == nil {
			continue
		}
		for _, w := range a.Snapshot.Windows {
			if p.showsKind(w.Kind) {
				if r := remaining(w, now); least[i] < 0 || r < least[i] {
					least[i] = r
				}
			}
		}
	}
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		i, j := idx[x], idx[y]
		switch p.AccountSort {
		case "name":
			return entries[i].Name < entries[j].Name
		case "most_left":
			return least[i] > least[j]
		default:
			if (least[i] < 0) != (least[j] < 0) {
				return least[j] < 0
			}
			return least[i] < least[j]
		}
	})
	sorted := make([]EntryView, len(entries))
	for x, i := range idx {
		sorted[x] = entries[i]
	}
	copy(entries, sorted)
}

func (p *Profile) poolEntry(accounts []AccountInfo, now time.Time) *EntryView {
	type agg struct {
		w     Window
		rems  []float64
		paces []*Pace
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
			g.paces = append(g.paces, ProjectPace(w, now))
			if w.ResetsAt != nil && w.ResetsAt.After(now) && (g.reset == nil || w.ResetsAt.Before(*g.reset)) {
				g.reset = w.ResetsAt
			}
		}
	}

	e := &EntryView{Name: "All accounts", AccountCount: len(accounts), Windows: []WindowView{}}
	for _, id := range order {
		g := byID[id]
		var rem float64
		var pace *Pace
		switch p.PoolAggregate {
		case "min", "max":
			pick := slices.Index(g.rems, slices.Min(g.rems))
			if p.PoolAggregate == "max" {
				pick = slices.Index(g.rems, slices.Max(g.rems))
			}
			rem, pace = g.rems[pick], g.paces[pick]
		default:
			useds := make([]float64, len(g.rems))
			for i, r := range g.rems {
				rem += r
				useds[i] = 100 - r
			}
			rem /= float64(len(g.rems))
			pace = poolPace(useds, g.paces)
		}
		w := g.w
		w.ResetsAt = g.reset
		w.Limited = false
		w.Amount = nil // amounts differ per account
		w.UsedPercent = 100 - rem
		v := p.windowView(w, rem, pace, now)
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
