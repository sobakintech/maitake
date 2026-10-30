package accounts

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
	"github.com/sobakintech/maitake/internal/store"
)

// A window is sampled again once it moves this much, its reset changes, or the last sample is
// this old. The heartbeat keeps flat stretches visible as data rather than a gap.
const (
	sampleMinDelta  = 0.5
	sampleResetSkew = 2 * time.Minute
	sampleHeartbeat = 30 * time.Minute
)

type sampleMark struct {
	used   float64
	resets *time.Time
	at     time.Time
}

// recordSamples stores the snapshot's account-wide windows for the capacity chart.
func (m *Manager) recordSamples(ctx context.Context, e *entry, snap *quota.Snapshot, now time.Time) {
	if snap == nil || snap.Error != "" {
		return
	}
	m.mu.Lock()
	if e.samples == nil {
		e.samples = map[string]sampleMark{}
	}
	var out []store.QuotaSample
	for _, w := range snap.Windows {
		if w.Kind != quota.KindSession && w.Kind != quota.KindWeekly {
			continue
		}
		// A window past its reset is stale, e.g. one the latest headers didn't mention.
		if w.ResetsAt != nil && !w.ResetsAt.After(now) {
			continue
		}
		used := math.Max(0, math.Min(100, w.UsedPercent))
		if w.Limited {
			used = 100
		}
		// An unused Codex window's reset slides forward on every poll, so it only counts once used.
		if prev, ok := e.samples[w.ID]; ok && math.Abs(prev.used-used) < sampleMinDelta &&
			(sameReset(prev.resets, w.ResetsAt) || used == 0 && prev.used == 0) &&
			now.Sub(prev.at) < sampleHeartbeat {
			continue
		}
		e.samples[w.ID] = sampleMark{used: used, resets: w.ResetsAt, at: now}
		out = append(out, store.QuotaSample{AccountID: e.acct.ID, WindowID: w.ID, Kind: w.Kind, Time: now,
			Used: used, ResetsAt: w.ResetsAt, Minutes: w.WindowMinutes})
	}
	m.mu.Unlock()
	if err := m.store.InsertQuotaSamples(ctx, out); err != nil {
		slog.Warn("save quota samples", "account", e.acct.ID, "err", err)
	}
}

func sameReset(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	d := a.Sub(*b)
	return d < sampleResetSkew && d > -sampleResetSkew
}
