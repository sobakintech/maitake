package server

import (
	"net/http"
	"time"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/quota"
)

var burnWindows = map[string]time.Duration{"1h": time.Hour, "3h": 3 * time.Hour, "6h": 6 * time.Hour}

// capacity charts each provider's pool for the admin dashboard: recorded history and a projection.
func (s *Server) capacity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := quota.KindWeekly
	if q.Get("window") == "session" {
		kind = quota.KindSession
	}
	burn, ok := burnWindows[q.Get("burn")]
	if !ok {
		burn = burnWindows["3h"]
	}
	usage := quota.UsageRecent
	if q.Get("usage") == quota.UsageAverage {
		usage = quota.UsageAverage
	}
	now := time.Now().UTC()
	from, to := now.Add(-3*24*time.Hour), now.Add(11*24*time.Hour)
	if kind == quota.KindSession {
		from, to = now.Add(-24*time.Hour), now.Add(2*24*time.Hour)
	}
	if v, err := time.Parse(time.RFC3339, q.Get("from")); err == nil {
		from = v.UTC()
	}
	if v, err := time.Parse(time.RFC3339, q.Get("to")); err == nil {
		to = v.UTC()
	}
	if !to.After(from) || to.Sub(from) > 62*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid range")
		return
	}

	since := from
	if b := now.Add(-burn); b.Before(since) {
		since = b
	}
	rows, err := s.store.QuotaSamples(r.Context(), kind, since, now)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	samples := make([]quota.Sample, len(rows))
	for i, row := range rows {
		samples[i] = quota.Sample{AccountID: row.AccountID, WindowID: row.WindowID, Time: row.Time, Used: row.Used,
			ResetsAt: row.ResetsAt, Minutes: row.Minutes}
	}

	inputs := map[string]*quota.CapacityInput{}
	var order []string
	for _, a := range s.accounts.List() {
		if a.Disabled {
			continue
		}
		in := inputs[a.Provider]
		if in == nil {
			in = &quota.CapacityInput{Kind: kind, Usage: usage, Now: now, From: from, To: to, BurnWindow: burn,
				Samples: samples}
			inputs[a.Provider] = in
			order = append(order, a.Provider)
		}
		plan := a.Plan
		if a.Quota != nil && a.Quota.Plan != "" {
			plan = a.Quota.Plan
		}
		snap := a.Quota
		if a.Status == accounts.StatusReauth {
			snap = nil // routing skips it, so its quota isn't part of the pool
		}
		in.Accounts = append(in.Accounts, quota.CapacityAccount{ID: a.ID, Priority: a.Priority, Label: a.Label,
			Alias: a.Alias, Plan: plan, Status: a.Status, Weight: quota.PlanWeight(a.Provider, plan), Snapshot: snap})
	}
	providers := []quota.CapacityProvider{}
	for _, id := range order {
		providers = append(providers, quota.BuildCapacity(id, *inputs[id]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": kind, "usage": usage, "burn": burn.Hours(),
		"now": now, "from": from, "to": to, "providers": providers})
}
