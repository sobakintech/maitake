package accounts

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/provider"
)

// modelListRetry is the wait after a failed fetch, so a broken account isn't asked every minute.
const modelListRetry = 5 * time.Minute

// usableForModels leaves out accounts that can't take requests whatever they report.
func (e *entry) usableForModels() bool {
	return !e.acct.Disabled && e.acct.Status != StatusReauth
}

// RefreshModelLists asks accounts for their models when their list is older than maxAge.
// A maxAge of 0 asks every account, ignoring the retry wait. A failure keeps the last good list.
func (m *Manager) RefreshModelLists(ctx context.Context, maxAge time.Duration) {
	type job struct {
		e      *entry
		id     int64
		lister provider.ModelLister
	}
	now := time.Now()
	var jobs []job
	m.mu.Lock()
	for id, e := range m.entries {
		lister, ok := m.providers[e.acct.Provider].(provider.ModelLister)
		if !ok || !e.usableForModels() {
			continue
		}
		if maxAge > 0 && (now.Sub(e.modelsAt) < maxAge || now.Sub(e.modelsTried) < modelListRetry) {
			continue
		}
		e.modelsTried = now
		jobs = append(jobs, job{e, id, lister})
	}
	m.mu.Unlock()

	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		creds, err := m.Credentials(cctx, j.id, false)
		var list []models.Listed
		if err == nil {
			list, err = j.lister.ListModels(cctx, creds)
		}
		cancel()
		if err == nil && len(list) == 0 {
			err = errors.New("empty list")
		}
		if err != nil {
			slog.Debug("model list fetch failed", "account", j.id, "err", err)
			continue
		}
		m.mu.Lock()
		j.e.listed, j.e.modelsAt = list, time.Now()
		m.mu.Unlock()
	}
}

// RunModelLists keeps model lists fresh until ctx ends, while interval returns more than 0.
func (m *Manager) RunModelLists(ctx context.Context, interval func() time.Duration) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if d := interval(); d > 0 {
			m.RefreshModelLists(ctx, d)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Inventory gathers what the usable accounts have reported about their models.
func (m *Manager) Inventory() models.Inventory {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv := models.Inventory{Unreported: map[string][]int64{}}
	served := map[string]*models.Served{}
	for id, e := range m.entries {
		if !e.usableForModels() {
			continue
		}
		p := e.acct.Provider
		if e.modelsAt.IsZero() {
			inv.Unreported[p] = append(inv.Unreported[p], id)
			continue
		}
		if e.modelsAt.After(inv.CheckedAt) {
			inv.CheckedAt = e.modelsAt
		}
		for _, l := range e.listed {
			key := p + "/" + l.ID
			s := served[key]
			if s == nil {
				s = &models.Served{ID: l.ID, Provider: p}
				served[key] = s
			}
			if l.Preview {
				s.PreviewAccounts = append(s.PreviewAccounts, id)
			} else {
				s.Accounts = append(s.Accounts, id)
			}
			if l.Created.After(s.Created) {
				s.Created = l.Created
			}
		}
	}
	for _, s := range served {
		slices.Sort(s.Accounts)
		slices.Sort(s.PreviewAccounts)
		inv.Models = append(inv.Models, *s)
	}
	slices.SortFunc(inv.Models, func(a, b models.Served) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, ids := range inv.Unreported {
		slices.Sort(ids)
	}
	return inv
}

// CanServe reports whether any of the accounts could take a request for the model right now.
// Empty accounts means any of the provider's.
func (m *Manager) CanServe(providerID, model string, accounts []int64) bool {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.entries {
		if e.acct.Provider != providerID || (len(accounts) > 0 && !slices.Contains(accounts, id)) {
			continue
		}
		if ok, _ := e.available(now, model); ok {
			return true
		}
	}
	return false
}
