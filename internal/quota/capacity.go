package quota

import (
	"math"
	"sort"
	"strings"
	"time"
)

// planWeights is each plan's quota relative to the provider's other plans; unlisted plans count 1.
var planWeights = map[string]map[string]float64{
	"claude": {"pro": 1, "max": 5, "max 5x": 5, "max 20x": 20},
	"codex":  {"plus": 1, "pro": 6},
}

// PlanWeight is a plan's relative capacity; plan names match case-insensitively.
func PlanWeight(provider, plan string) float64 {
	if w, ok := planWeights[provider][strings.ToLower(plan)]; ok {
		return w
	}
	return 1
}

// Usage assumptions for the projection.
const (
	UsageRecent  = "recent"  // the burn rate of the last few hours continues
	UsageAverage = "average" // each window keeps its average rate since it opened
)

// Sample is one recorded reading of a quota window.
type Sample struct {
	AccountID int64
	WindowID  string
	Time      time.Time
	Used      float64
	ResetsAt  *time.Time
	Minutes   int
}

type CapacityAccount struct {
	ID       int64
	Priority int
	Label    string
	Alias    string
	Plan     string
	Status   string
	Weight   float64
	Snapshot *Snapshot
}

type CapacityInput struct {
	Kind       string // KindWeekly or KindSession
	Usage      string
	Now        time.Time
	From, To   time.Time
	BurnWindow time.Duration
	Accounts   []CapacityAccount
	Samples    []Sample // oldest first
}

type Point struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

type CapacityReset struct {
	T time.Time `json:"t"`
	// Gain is how much of the pool comes back, in percent of its capacity.
	Gain      float64 `json:"gain"`
	Projected bool    `json:"projected"`
	Accounts  []int64 `json:"accounts"`
}

type CapacityEmpty struct {
	AccountID int64     `json:"accountId"`
	At        time.Time `json:"at"`
}

type CapacityAccountView struct {
	ID     int64   `json:"id"`
	Label  string  `json:"label"`
	Alias  string  `json:"alias"`
	Plan   string  `json:"plan"`
	Status string  `json:"status"`
	Weight float64 `json:"weight"`
	// Remaining is the account's own window, in percent; nil when it reports no such window.
	Remaining *float64   `json:"remaining"`
	ResetsAt  *time.Time `json:"resetsAt,omitempty"`
	// Rate is the account's burn in percent of its own window per hour.
	Rate      float64    `json:"rate"`
	EmptyAt   *time.Time `json:"emptyAt,omitempty"`
	Recorded  []Point    `json:"recorded"`
	Projected []Point    `json:"projected"`
}

type CapacityStats struct {
	// BurnRate is pool percent per hour; BurnSource says which rates went into it.
	BurnRate    float64        `json:"burnRate"`
	BurnSource  string         `json:"burnSource"` // recent, average or mixed
	PoolEmptyAt *time.Time     `json:"poolEmptyAt"`
	FirstEmpty  *CapacityEmpty `json:"firstEmpty"`
	EmptyNow    int            `json:"emptyNow"`
	NextReset   *CapacityReset `json:"nextReset"`
	// Horizon is how far ahead the stats looked.
	Horizon time.Time `json:"horizon"`
}

type CapacityProvider struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Capacity float64               `json:"capacity"` // sum of the counted accounts' weights
	Accounts []CapacityAccountView `json:"accounts"`
	// Remaining is the pool now, in percent of Capacity; nil without data.
	Remaining *float64        `json:"remaining"`
	Recorded  []Point         `json:"recorded"`
	Projected []Point         `json:"projected"`
	Resets    []CapacityReset `json:"resets"`
	Stats     CapacityStats   `json:"stats"`
}

// CapacityHorizon is how far ahead the stats look for a window kind.
func CapacityHorizon(kind string) time.Duration {
	if kind == KindSession {
		return 2 * 24 * time.Hour
	}
	return 14 * 24 * time.Hour
}

// capAcct is one counted account while building the chart.
type capAcct struct {
	view     CapacityAccountView
	w        float64
	priority int
	period   time.Duration
	// series is the recorded remaining percent; two points may share a time to draw a step.
	series []Point
	rem    float64
	reset  *time.Time
	rate   float64
	recent bool
}

// BuildCapacity charts one provider's pool: what was recorded, and where it goes from now.
func BuildCapacity(id string, in CapacityInput) CapacityProvider {
	out := CapacityProvider{ID: id, Name: providerNames[id], Accounts: []CapacityAccountView{},
		Recorded: []Point{}, Projected: []Point{}, Resets: []CapacityReset{}}
	if out.Name == "" {
		out.Name = id
	}
	horizon := in.Now.Add(CapacityHorizon(in.Kind))
	if in.To.After(horizon) {
		horizon = in.To
	}
	out.Stats.Horizon = horizon

	byAccount := map[int64][]Sample{}
	for _, s := range in.Samples {
		byAccount[s.AccountID] = append(byAccount[s.AccountID], s)
	}

	var accts []*capAcct
	for _, a := range in.Accounts {
		v := CapacityAccountView{ID: a.ID, Label: a.Label, Alias: a.Alias, Plan: a.Plan, Status: a.Status,
			Weight: a.Weight, Recorded: []Point{}, Projected: []Point{}}
		win := pickWindow(a.Snapshot, in.Kind)
		if win == nil {
			out.Accounts = append(out.Accounts, v)
			continue
		}
		c := &capAcct{view: v, w: a.Weight, priority: a.Priority, period: time.Duration(win.WindowMinutes) * time.Minute}
		if c.w <= 0 {
			c.w = 1
		}
		used := math.Max(0, math.Min(100, win.UsedPercent))
		if win.Limited {
			used = 100
		}
		c.rem, c.reset = 100-used, win.ResetsAt
		// A reset that has passed since the last reading has already refilled the window; the next
		// one opens on first use.
		if c.reset != nil && !c.reset.After(in.Now) {
			c.rem, c.reset = 100, nil
		}
		c.series = recordedSeries(filterFlaps(windowSamples(byAccount[a.ID], win.ID)), in.Now)
		c.series = append(c.series, Point{T: in.Now, V: c.rem})
		c.rate, c.recent = burnRate(c, used, in)
		rem := c.rem
		c.view.Remaining, c.view.ResetsAt, c.view.Rate = &rem, c.reset, c.rate
		accts = append(accts, c)
	}
	if len(accts) == 0 {
		return out
	}

	total := 0.0
	recent := 0
	for _, c := range accts {
		total += c.w
		out.Stats.BurnRate += c.w * c.rate
		if c.recent {
			recent++
		}
		if c.rem <= 0 {
			out.Stats.EmptyNow++
		}
	}
	out.Capacity = total
	out.Stats.BurnRate /= total
	switch recent {
	case len(accts):
		out.Stats.BurnSource = UsageRecent
	case 0:
		out.Stats.BurnSource = UsageAverage
	default:
		out.Stats.BurnSource = "mixed"
	}

	out.Recorded, out.Resets = poolRecorded(accts, in.From)
	now := poolValue(accts, total)
	out.Remaining = &now

	projected, resets := project(accts, total, in.Now, horizon, &out.Stats)
	out.Projected = clipAfter(projected, in.To)
	for _, r := range groupResets(resets) {
		if out.Stats.NextReset == nil || (out.Stats.NextReset.Gain < 0.1 && r.Gain >= 0.1) {
			cp := r
			out.Stats.NextReset = &cp
		}
		if r.T.Before(in.To) {
			out.Resets = append(out.Resets, r)
		}
	}

	for _, c := range accts {
		c.view.Recorded = simplify(clipBefore(c.view.Recorded, in.From), 0.2)
		c.view.Projected = clipAfter(c.view.Projected, in.To)
		out.Accounts = append(out.Accounts, c.view)
	}
	sort.SliceStable(out.Accounts, func(i, j int) bool {
		return (out.Accounts[i].Remaining != nil) && (out.Accounts[j].Remaining == nil)
	})
	return out
}

// pickWindow is the account-wide window of a kind; the first one wins if a provider sends two.
func pickWindow(s *Snapshot, kind string) *Window {
	if s == nil {
		return nil
	}
	for i := range s.Windows {
		if s.Windows[i].Kind == kind {
			return &s.Windows[i]
		}
	}
	return nil
}

func windowSamples(samples []Sample, windowID string) []Sample {
	var out []Sample
	for _, s := range samples {
		if s.WindowID == windowID {
			out = append(out, s)
		}
	}
	return out
}

// filterFlaps drops single readings that jump away and straight back, which some upstreams
// report now and then.
func filterFlaps(s []Sample) []Sample {
	if len(s) < 3 {
		return s
	}
	out := []Sample{s[0]}
	for i := 1; i < len(s)-1; i++ {
		prev, next := out[len(out)-1], s[i+1]
		if math.Abs(s[i].Used-prev.Used) >= 10 && math.Abs(next.Used-prev.Used) <= 3 && sameResetTime(prev.ResetsAt, next.ResetsAt) {
			continue
		}
		out = append(out, s[i])
	}
	return append(out, s[len(s)-1])
}

func sameResetTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Sub(*b).Abs() < 5*time.Minute
}

// recordedSeries turns samples into remaining percent over time. A reset, or a big drop in usage
// (a banked reset), is drawn as a step at the reading that saw it; a reset that passed with no
// reading after it is drawn at its scheduled time. Small dips are left as slopes.
func recordedSeries(samples []Sample, now time.Time) []Point {
	var out []Point
	for i, s := range samples {
		rem := 100 - s.Used
		if n := len(out); n > 0 && rem > out[n-1].V+1 &&
			(rem >= out[n-1].V+5 || !sameResetTime(samples[i-1].ResetsAt, s.ResetsAt)) {
			out = append(out, Point{T: s.Time, V: out[n-1].V})
		}
		out = append(out, Point{T: s.Time, V: rem})
		end := now
		if i+1 < len(samples) {
			end = samples[i+1].Time
		}
		if s.ResetsAt != nil && s.ResetsAt.After(s.Time) && s.ResetsAt.Before(end) && rem < 100 {
			out = append(out, Point{T: *s.ResetsAt, V: rem}, Point{T: *s.ResetsAt, V: 100})
		}
	}
	return out
}

// burnRate is the account's own percent per hour, from recent history when there is enough of
// it, otherwise from the window's average so far.
func burnRate(c *capAcct, used float64, in CapacityInput) (float64, bool) {
	if in.Usage != UsageAverage && in.BurnWindow > 0 && len(c.series) > 1 {
		start := in.Now.Add(-in.BurnWindow)
		first := c.series[0].T
		if from := maxTime(start, first); in.Now.Sub(from) >= in.BurnWindow/2 {
			spent := 0.0
			for i := 1; i < len(c.series); i++ {
				a, b := c.series[i-1], c.series[i]
				if !b.T.After(a.T) || b.V >= a.V || !b.T.After(from) {
					continue
				}
				span := b.T.Sub(a.T).Seconds()
				overlap := b.T.Sub(maxTime(a.T, from)).Seconds()
				spent += (a.V - b.V) * overlap / span
			}
			return spent / in.Now.Sub(from).Hours(), true
		}
	}
	if c.reset == nil || c.period <= 0 {
		return 0, false
	}
	elapsed := c.period - c.reset.Sub(in.Now)
	if elapsed < 15*time.Minute || c.rem >= 100 {
		return 0, false
	}
	return (100 - c.rem) / elapsed.Hours(), false
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// poolRecorded sums the accounts' recorded series into the pool line and finds the recorded resets.
// Accounts count from their first reading, so the pool doesn't look drained before history began.
func poolRecorded(accts []*capAcct, from time.Time) ([]Point, []CapacityReset) {
	var times []time.Time
	for _, c := range accts {
		for _, p := range c.series {
			times = append(times, p.T)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	var out []Point
	var resets []CapacityReset
	idx := make([]int, len(accts))
	for k, t := range times {
		if k > 0 && t.Equal(times[k-1]) {
			continue
		}
		var left, right, known float64
		var stepped []int64
		gain := 0.0
		for i, c := range accts {
			l, r, ok := valueAt(c.series, &idx[i], t)
			if !ok {
				continue
			}
			known += c.w
			left += c.w * l
			right += c.w * r
			if r > l+0.5 {
				gain += c.w * (r - l)
				stepped = append(stepped, c.view.ID)
			}
		}
		if known == 0 {
			continue
		}
		if !t.Before(from) && gain > 0 {
			resets = append(resets, CapacityReset{T: t, Gain: gain / known, Accounts: stepped})
		}
		lv, rv := left/known, right/known
		out = append(out, Point{T: t, V: lv})
		if math.Abs(rv-lv) > 1e-9 {
			out = append(out, Point{T: t, V: rv})
		}
	}
	for _, c := range accts {
		c.view.Recorded = c.series
	}
	return simplify(clipBefore(out, from), 0.1), groupResets(resets)
}

// valueAt returns a series' value just before and at t, interpolating between points and holding
// the last one. cursor carries the search position across increasing t.
func valueAt(s []Point, cursor *int, t time.Time) (left, right float64, ok bool) {
	if len(s) == 0 || t.Before(s[0].T) {
		return 0, 0, false
	}
	i := *cursor
	for i+1 < len(s) && !s[i+1].T.After(t) {
		i++
	}
	*cursor = i
	if s[i].T.Equal(t) {
		j := i
		for j > 0 && s[j-1].T.Equal(t) {
			j--
		}
		return s[j].V, s[i].V, true
	}
	v := s[i].V
	if i+1 < len(s) {
		a, b := s[i], s[i+1]
		v = a.V + (b.V-a.V)*t.Sub(a.T).Seconds()/b.T.Sub(a.T).Seconds()
	}
	return v, v, true
}

func poolValue(accts []*capAcct, total float64) float64 {
	sum := 0.0
	for _, c := range accts {
		sum += c.w * c.rem
	}
	return sum / total
}

// project plays the pool forward. Demand is the accounts' combined burn, in plan-weighted units,
// and stays constant. Like routing, it goes to the highest priority accounts with quota left,
// split evenly between them by request, so a smaller plan drains faster; when one runs dry its
// share moves to the rest.
func project(accts []*capAcct, total float64, now, horizon time.Time, stats *CapacityStats) ([]Point, []CapacityReset) {
	demand := 0.0
	for _, c := range accts {
		demand += c.w * c.rate
	}
	t := now
	last := poolValue(accts, total)
	points := []Point{{T: t, V: last}}
	for _, c := range accts {
		c.view.Projected = append(c.view.Projected, Point{T: t, V: c.rem})
	}
	var resets []CapacityReset
	drain := make([]float64, len(accts))
	for step := 0; step < 20000 && t.Before(horizon); step++ {
		top, tier := math.MinInt, 0
		for _, c := range accts {
			if c.rem > 0 {
				if c.priority > top {
					top, tier = c.priority, 0
				}
				if c.priority == top {
					tier++
				}
			}
		}
		next := horizon
		for i, c := range accts {
			drain[i] = 0
			if c.rem > 0 && c.priority == top && demand > 0 {
				drain[i] = demand / float64(tier) / c.w
			}
			// An idle window opens when it's first used.
			if drain[i] > 0 && c.reset == nil && c.period > 0 {
				r := t.Add(c.period)
				c.reset = &r
			}
			if c.reset != nil && c.reset.Before(next) {
				next = *c.reset
			}
			if drain[i] > 0 {
				if h := c.rem / drain[i]; h < next.Sub(t).Hours() {
					next = t.Add(time.Duration(h * float64(time.Hour)))
				}
			}
		}
		hours := next.Sub(t).Hours()
		t = next
		for i, c := range accts {
			if drain[i] == 0 {
				continue
			}
			c.rem -= drain[i] * hours
			if c.rem < 1e-6 {
				c.rem = 0
				if c.view.EmptyAt == nil {
					at := t
					c.view.EmptyAt = &at
					if stats.FirstEmpty == nil {
						stats.FirstEmpty = &CapacityEmpty{AccountID: c.view.ID, At: at}
					}
				}
			}
		}
		v := poolValue(accts, total)
		points = append(points, Point{T: t, V: v})
		// Only a pool that runs dry counts, not one that's empty now and waiting for a reset.
		if v <= 1e-6 && last > 1e-6 && stats.PoolEmptyAt == nil {
			at := t
			stats.PoolEmptyAt = &at
		}
		last = v
		gain := 0.0
		var stepped []int64
		for _, c := range accts {
			c.view.Projected = append(c.view.Projected, Point{T: t, V: c.rem})
			if c.reset == nil || c.reset.After(t) {
				continue
			}
			if c.rem < 100 {
				gain += c.w * (100 - c.rem)
				stepped = append(stepped, c.view.ID)
			}
			// The next window opens when the account is next used.
			c.rem, c.reset = 100, nil
			c.view.Projected = append(c.view.Projected, Point{T: t, V: c.rem})
		}
		if len(stepped) > 0 {
			resets = append(resets, CapacityReset{T: t, Gain: gain / total, Projected: true, Accounts: stepped})
			last = poolValue(accts, total)
			points = append(points, Point{T: t, V: last})
		}
	}
	return points, resets
}

// groupResets merges resets less than half an hour apart into one annotation.
func groupResets(in []CapacityReset) []CapacityReset {
	out := []CapacityReset{}
	for _, r := range in {
		if n := len(out); n > 0 && r.T.Sub(out[n-1].T) < 30*time.Minute && r.Projected == out[n-1].Projected {
			out[n-1].Gain += r.Gain
			out[n-1].Accounts = append(out[n-1].Accounts, r.Accounts...)
			continue
		}
		r.Accounts = append([]int64(nil), r.Accounts...)
		out = append(out, r)
	}
	return out
}

// clipBefore drops points before from, keeping the last one before it so the line starts at the edge.
func clipBefore(s []Point, from time.Time) []Point {
	i := sort.Search(len(s), func(i int) bool { return !s[i].T.Before(from) })
	if i > 0 {
		i--
	}
	return append([]Point{}, s[i:]...)
}

// clipAfter drops points after to, keeping the first one after it so the line reaches the edge.
func clipAfter(s []Point, to time.Time) []Point {
	i := sort.Search(len(s), func(i int) bool { return s[i].T.After(to) })
	if i < len(s) {
		i++
	}
	return append([]Point{}, s[:i]...)
}

// simplify drops points that lie within eps percent of the line through their neighbours. Steps
// (two points at one time) are always kept.
func simplify(s []Point, eps float64) []Point {
	if len(s) < 3 {
		return s
	}
	out := []Point{s[0]}
	for i := 1; i < len(s)-1; i++ {
		a, p, b := out[len(out)-1], s[i], s[i+1]
		if p.T.Equal(a.T) || p.T.Equal(b.T) || !b.T.After(a.T) {
			out = append(out, p)
			continue
		}
		f := p.T.Sub(a.T).Seconds() / b.T.Sub(a.T).Seconds()
		if math.Abs(a.V+(b.V-a.V)*f-p.V) > eps {
			out = append(out, p)
		}
	}
	return append(out, s[len(s)-1])
}
