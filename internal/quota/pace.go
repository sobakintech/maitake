package quota

import (
	"math"
	"time"
)

// Pace statuses.
const (
	PaceOnTrack = "on_track" // projected to finish with room to spare
	PaceClose   = "close"    // projected to finish just under the limit
	PaceOver    = "over"     // projected to hit the limit before the window resets
)

// Pace projects where a window ends up at reset if usage keeps the rate it has had so far.
type Pace struct {
	Status string `json:"status"`
	// ProjectedUsed is the percent used at reset at the current rate; it can exceed 100.
	ProjectedUsed float64 `json:"projectedUsed,omitempty"`
	// Elapsed is the percent of the window that has passed, i.e. where even usage would be now.
	Elapsed float64 `json:"elapsed,omitempty"`
	// RunsOutAt is when the limit is hit at the current rate, if that's before the reset.
	RunsOutAt *time.Time `json:"runsOutAt,omitempty"`
}

// ProjectPace returns nil when there's no useful projection: the window is spent, unused, has no
// known span or reset, or is too young to say anything about the rate.
func ProjectPace(w Window, now time.Time) *Pace {
	used := math.Max(0, math.Min(100, w.UsedPercent))
	if w.Limited || math.Round(100-used) <= 0 || used <= 0 {
		return nil
	}
	if w.ResetsAt == nil || w.WindowMinutes <= 0 || !w.ResetsAt.After(now) {
		return nil
	}
	period := time.Duration(w.WindowMinutes) * time.Minute
	start := w.ResetsAt.Add(-period)
	elapsed := now.Sub(start)
	if elapsed < max(time.Minute, period/100) {
		return nil
	}
	progress := math.Min(1, elapsed.Seconds()/period.Seconds())
	projected := used / progress
	if projected > 90 && used < 5 {
		return nil // a few early requests aren't a trend
	}
	p := &Pace{Status: paceStatus(projected), ProjectedUsed: projected, Elapsed: progress * 100}
	if projected > 100 {
		at := start.Add(time.Duration(float64(elapsed) * 100 / used))
		if at.After(now) && at.Before(*w.ResetsAt) {
			p.RunsOutAt = &at
		}
	}
	return p
}

func paceStatus(projected float64) string {
	switch {
	case projected <= 90:
		return PaceOnTrack
	case math.Round(100-projected) >= 1:
		return PaceClose
	default:
		return PaceOver
	}
}

// WithPace returns a copy of s with each window's pace filled in.
func (s *Snapshot) WithPace(now time.Time) *Snapshot {
	if s == nil {
		return nil
	}
	out := *s
	out.Windows = make([]Window, len(s.Windows))
	for i, w := range s.Windows {
		w.Pace = ProjectPace(w, now)
		out.Windows[i] = w
	}
	return &out
}
