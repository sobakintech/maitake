// Package accounts keeps the live pool of upstream accounts: selection, token refresh,
// cooldowns, quota polling and banked-reset redemption.
package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/quota"
	"github.com/sobakintech/maitake/internal/store"
)

const (
	StatusOK       = "ok"
	StatusCooldown = "cooldown"
	StatusError    = "error"
	StatusReauth   = "reauth"
)

// Settings are the admin-tunable knobs for the pool.
type Settings struct {
	QuotaPollMinutes int `json:"quotaPollMinutes"`
	// AutoRedeemMinWaitMinutes is the pre-RedeemPolicy setting, read once into Redeem.
	AutoRedeemMinWaitMinutes int `json:"autoRedeemMinWaitMinutes,omitempty"`
	// Redeem is the default banked reset policy. Only Codex accounts spend resets automatically.
	Redeem     RedeemPolicy `json:"redeem"`
	MaxRetries int          `json:"maxRetries"`
	// BootstrapSeconds is how long to hold a stream's opening events, so an error that arrives
	// inside a 200 response can still move to another account before the client sees anything.
	BootstrapSeconds int `json:"bootstrapSeconds"`
	// KeepaliveSeconds sends an SSE comment when the upstream is quiet that long (0 = off).
	KeepaliveSeconds int         `json:"keepaliveSeconds"`
	ErrorRules       []ErrorRule `json:"errorRules"`
}

func DefaultSettings() Settings {
	return Settings{QuotaPollMinutes: 5, Redeem: DefaultRedeemPolicy(), MaxRetries: 3, BootstrapSeconds: 20,
		KeepaliveSeconds: 15, ErrorRules: DefaultErrorRules()}
}

func (s *Settings) Normalize() {
	if s.QuotaPollMinutes < 1 {
		s.QuotaPollMinutes = 1
	}
	if s.AutoRedeemMinWaitMinutes > 0 {
		s.Redeem.MinWaitMinutes, s.AutoRedeemMinWaitMinutes = s.AutoRedeemMinWaitMinutes, 0
	}
	s.Redeem.Normalize()
	if s.MaxRetries < 1 {
		s.MaxRetries = 1
	}
	if s.MaxRetries > 10 {
		s.MaxRetries = 10
	}
	if s.BootstrapSeconds < 0 {
		s.BootstrapSeconds = 0
	}
	if s.KeepaliveSeconds < 0 {
		s.KeepaliveSeconds = 0
	}
	if s.ErrorRules == nil {
		s.ErrorRules = []ErrorRule{}
	}
	s.ErrorRules = normalizeRules(s.ErrorRules)
}

var ErrNoAccounts = errors.New("no accounts configured for this provider")

// ErrAllCooling means every account is rate limited; Until is the soonest recovery.
type ErrAllCooling struct{ Until time.Time }

func (e *ErrAllCooling) Error() string {
	return fmt.Sprintf("all accounts are rate limited until %s", e.Until.Format(time.RFC3339))
}

type entry struct {
	acct       store.Account
	creds      provider.Credentials
	snap       *quota.Snapshot
	sub        *provider.Subscription
	modelCool  map[string]time.Time // per-model cooldowns
	lastPicked time.Time
	lastRedeem time.Time
	redeems    []time.Time // automatic uses, for the daily cap
	refreshMu  sync.Mutex
}

type Manager struct {
	store     *store.Store
	providers map[string]provider.Provider
	settings  func() Settings

	mu      sync.Mutex
	entries map[int64]*entry
	sticky  map[string]stickyEntry
	wake    chan struct{}
}

func New(st *store.Store, settings func() Settings, providers ...provider.Provider) *Manager {
	m := &Manager{store: st, settings: settings, providers: map[string]provider.Provider{},
		entries: map[int64]*entry{}, sticky: map[string]stickyEntry{}, wake: make(chan struct{}, 1)}
	for _, p := range providers {
		m.providers[p.ID()] = p
	}
	return m
}

func (m *Manager) Load(ctx context.Context) error {
	accts, err := m.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = map[int64]*entry{}
	for _, a := range accts {
		m.entries[a.ID] = newEntry(a)
	}
	return nil
}

func newEntry(a store.Account) *entry {
	creds, _ := provider.ParseCredentials(a.Credentials)
	e := &entry{acct: a, creds: creds, snap: quota.Parse(a.Quota)}
	if a.Subscription != "" {
		var sub provider.Subscription
		if json.Unmarshal([]byte(a.Subscription), &sub) == nil {
			e.sub = &sub
		}
	}
	return e
}

// View is an account as shown in the admin dashboard.
type View struct {
	store.Account
	Quota          *quota.Snapshot        `json:"quota"`
	Subscription   *provider.Subscription `json:"subscription"`
	ModelCooldowns map[string]time.Time   `json:"modelCooldowns,omitempty"`
	// Redeem is the account's own banked reset rules; RedeemEffective adds the pool defaults.
	Redeem          RedeemOverride `json:"redeem"`
	RedeemEffective RedeemPolicy   `json:"redeemEffective"`
	TokenExpiresAt  *time.Time     `json:"tokenExpiresAt"`
	LastRefresh     *time.Time     `json:"lastRefresh"`
}

func (m *Manager) viewOf(e *entry) View {
	v := View{Account: e.acct, Quota: e.snap, Subscription: e.sub, Redeem: ParseRedeemOverride(e.acct.RedeemPolicy)}
	v.RedeemEffective = v.Redeem.Apply(m.settings().Redeem)
	now := time.Now()
	for model, until := range e.modelCool {
		if until.After(now) {
			if v.ModelCooldowns == nil {
				v.ModelCooldowns = map[string]time.Time{}
			}
			v.ModelCooldowns[model] = until
		}
	}
	if !e.creds.ExpiresAt.IsZero() {
		t := e.creds.ExpiresAt
		v.TokenExpiresAt = &t
	}
	if !e.creds.LastRefresh.IsZero() {
		t := e.creds.LastRefresh
		v.LastRefresh = &t
	}
	return v
}

func (m *Manager) List() []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]View, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, m.viewOf(e))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) Get(id int64) (View, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return View{}, false
	}
	return m.viewOf(e), true
}

// QuotaInfos feeds the public quota page.
func (m *Manager) QuotaInfos() []quota.AccountInfo {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []quota.AccountInfo
	for _, e := range m.entries {
		status := e.acct.Status
		if e.acct.Disabled {
			status = "disabled"
		} else if status == StatusCooldown && e.acct.CooldownUntil != nil && !e.acct.CooldownUntil.After(now) {
			status = StatusOK
		}
		plan := e.acct.Plan
		if e.snap != nil && e.snap.Plan != "" {
			plan = e.snap.Plan
		}
		out = append(out, quota.AccountInfo{ID: e.acct.ID, Provider: e.acct.Provider, Label: e.acct.Label,
			Alias: e.acct.Alias, Plan: plan, Status: status, Snapshot: e.snap})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Add stores a freshly logged-in or imported account and fetches its quota in the background.
func (m *Manager) Add(ctx context.Context, providerID string, creds provider.Credentials, id provider.Identity) (*store.Account, error) {
	label := id.Label
	if label == "" {
		label = providerID + " account"
	}
	a, err := m.store.UpsertAccount(ctx, &store.Account{Provider: providerID, Label: label, Email: id.Email,
		ExternalID: id.ExternalID, Plan: id.Plan, Credentials: creds.Encode()})
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.entries[a.ID] = newEntry(*a)
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.RefreshQuota(ctx, a.ID)
		_ = m.RefreshSubscription(ctx, a.ID)
	}()
	return a, nil
}

func (m *Manager) Update(ctx context.Context, id int64, p store.AccountPatch) (*store.Account, error) {
	a, err := m.store.UpdateAccount(ctx, id, p)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if e, ok := m.entries[id]; ok {
		e.acct = *a
	}
	m.mu.Unlock()
	return a, nil
}

// Reload re-reads an account row changed outside the manager.
func (m *Manager) Reload(ctx context.Context, id int64) error {
	a, err := m.store.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if e, ok := m.entries[id]; ok {
		e.acct = *a
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Delete(ctx context.Context, id int64) error {
	if err := m.store.DeleteAccount(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.entries, id)
	m.mu.Unlock()
	return nil
}

// Lease is an account handed out for one request.
type Lease struct {
	ID       int64
	Provider string
	Label    string
	Plan     string
	Creds    provider.Credentials
}

func (e *entry) available(now time.Time, model string) (bool, time.Time) {
	if e.acct.Disabled || e.acct.Status == StatusReauth {
		return false, time.Time{}
	}
	if e.acct.CooldownUntil != nil && e.acct.CooldownUntil.After(now) {
		return false, *e.acct.CooldownUntil
	}
	if until, ok := e.modelCool[model]; ok && model != "" && until.After(now) {
		return false, until
	}
	if e.snap != nil {
		if hit, until := e.snap.Exhausted(now); hit {
			if until != nil {
				return false, *until
			}
			// Exhausted without a known reset: allow, the upstream will tell us.
		}
	}
	return true, time.Time{}
}

// PickOptions shape account selection for one request.
type PickOptions struct {
	// Strategy: "round_robin" (default), "fill", "most_remaining" or "random".
	Strategy string
	// Accounts restricts the pool to these IDs; empty means every account of the provider.
	Accounts []int64
	// StickyKey keeps requests with the same key on the same account for StickyFor.
	StickyKey string
	StickyFor time.Duration
	// Model skips accounts where this model is cooling down.
	Model string
}

type stickyEntry struct {
	id    int64
	until time.Time
}

// remainingPercent estimates how much of an account's tightest account-wide limit is left.
func (e *entry) remainingPercent(now time.Time) float64 {
	if e.snap == nil {
		return 50
	}
	left := 100.0
	for _, w := range e.snap.Windows {
		if w.Kind != quota.KindSession && w.Kind != quota.KindWeekly {
			continue
		}
		if w.ResetsAt != nil && !w.ResetsAt.After(now) {
			continue
		}
		if r := 100 - w.UsedPercent; r < left {
			left = r
		}
	}
	return left
}

// Pick chooses an account among the highest available priority tier, using the strategy.
func (m *Manager) Pick(ctx context.Context, providerID string, exclude map[int64]bool, opts PickOptions) (*Lease, error) {
	now := time.Now()
	m.mu.Lock()
	var candidates []*entry
	var soonest time.Time
	total := 0
	for _, e := range m.entries {
		if e.acct.Provider != providerID || exclude[e.acct.ID] {
			continue
		}
		if len(opts.Accounts) > 0 && !slices.Contains(opts.Accounts, e.acct.ID) {
			continue
		}
		if !e.acct.Disabled && e.acct.Status != StatusReauth {
			total++
		}
		ok, until := e.available(now, opts.Model)
		if ok {
			candidates = append(candidates, e)
		} else if !until.IsZero() && (soonest.IsZero() || until.Before(soonest)) {
			soonest = until
		}
	}
	if len(candidates) == 0 {
		m.mu.Unlock()
		if total == 0 && len(exclude) == 0 {
			return nil, ErrNoAccounts
		}
		if !soonest.IsZero() {
			return nil, &ErrAllCooling{Until: soonest}
		}
		return nil, ErrNoAccounts
	}

	var e *entry
	stickyID := opts.StickyKey + "|" + providerID
	if opts.StickyKey != "" {
		if s, ok := m.sticky[stickyID]; ok && now.Before(s.until) {
			for _, c := range candidates {
				if c.acct.ID == s.id {
					e = c
				}
			}
		}
	}
	if e == nil {
		top := candidates[0].acct.Priority
		for _, c := range candidates {
			top = max(top, c.acct.Priority)
		}
		tier := slices.DeleteFunc(candidates, func(c *entry) bool { return c.acct.Priority != top })
		switch opts.Strategy {
		case "fill":
			sort.Slice(tier, func(i, j int) bool { return tier[i].acct.ID < tier[j].acct.ID })
		case "most_remaining":
			sort.Slice(tier, func(i, j int) bool {
				ri, rj := tier[i].remainingPercent(now), tier[j].remainingPercent(now)
				if ri != rj {
					return ri > rj
				}
				return tier[i].lastPicked.Before(tier[j].lastPicked)
			})
		case "random":
			rand.Shuffle(len(tier), func(i, j int) { tier[i], tier[j] = tier[j], tier[i] })
		default:
			sort.Slice(tier, func(i, j int) bool { return tier[i].lastPicked.Before(tier[j].lastPicked) })
		}
		e = tier[0]
	}
	e.lastPicked = now
	if opts.StickyKey != "" && opts.StickyFor > 0 {
		m.sticky[stickyID] = stickyEntry{id: e.acct.ID, until: now.Add(opts.StickyFor)}
		if len(m.sticky) > 10000 {
			for k, v := range m.sticky {
				if now.After(v.until) {
					delete(m.sticky, k)
				}
			}
		}
	}
	id := e.acct.ID
	label := e.acct.Label
	plan := e.acct.Plan
	if e.snap != nil && e.snap.Plan != "" {
		plan = e.snap.Plan
	}
	m.mu.Unlock()

	creds, err := m.Credentials(ctx, id, false)
	if err != nil {
		return nil, err
	}
	return &Lease{ID: id, Provider: providerID, Label: label, Plan: plan, Creds: creds}, nil
}

// Credentials returns usable credentials, refreshing them when close to expiry or when forced.
func (m *Manager) Credentials(ctx context.Context, id int64, force bool) (provider.Credentials, error) {
	m.mu.Lock()
	e, ok := m.entries[id]
	m.mu.Unlock()
	if !ok {
		return provider.Credentials{}, store.ErrNotFound
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()

	m.mu.Lock()
	creds := e.creds
	providerID := e.acct.Provider
	m.mu.Unlock()
	if !force && creds.AccessToken != "" && time.Until(creds.ExpiresAt) > 5*time.Minute {
		return creds, nil
	}
	if !force && creds.AccessToken != "" && creds.ExpiresAt.IsZero() {
		return creds, nil
	}
	return m.refreshLocked(ctx, e, providerID, creds)
}

func (m *Manager) refreshLocked(ctx context.Context, e *entry, providerID string, creds provider.Credentials) (provider.Credentials, error) {
	p := m.providers[providerID]
	if p == nil {
		return creds, fmt.Errorf("unknown provider %q", providerID)
	}
	fresh, ident, err := p.Refresh(ctx, creds)
	if err != nil {
		status, until := StatusError, time.Now().Add(5*time.Minute)
		if errors.Is(err, provider.ErrInvalidGrant) {
			status = StatusReauth
		}
		m.setStatus(ctx, e, status, "token refresh failed: "+err.Error(), &until)
		return creds, err
	}
	if err := m.store.SaveAccountCredentials(ctx, e.acct.ID, fresh.Encode(), ident.Plan); err != nil {
		slog.Error("save credentials", "account", e.acct.ID, "err", err)
	}
	m.mu.Lock()
	e.creds = fresh
	if ident.Plan != "" {
		e.acct.Plan = ident.Plan
	}
	m.mu.Unlock()
	if e.acct.Status == StatusReauth || e.acct.Status == StatusError {
		m.setStatus(ctx, e, StatusOK, "", nil)
	}
	if src, ok := p.(provider.SubscriptionSource); ok && providerID == "codex" {
		if sub, err := src.Subscription(ctx, fresh); err == nil {
			m.mu.Lock()
			e.sub = sub
			m.mu.Unlock()
			raw, _ := json.Marshal(sub)
			_ = m.store.SaveAccountSubscription(ctx, e.acct.ID, string(raw))
		}
	}
	return fresh, nil
}

func (m *Manager) setStatus(ctx context.Context, e *entry, status, msg string, until *time.Time) {
	m.mu.Lock()
	e.acct.Status, e.acct.StatusMessage, e.acct.CooldownUntil = status, msg, until
	id := e.acct.ID
	m.mu.Unlock()
	if err := m.store.SaveAccountStatus(ctx, id, status, msg, until); err != nil {
		slog.Error("save account status", "account", id, "err", err)
	}
}

func (m *Manager) entry(id int64) *entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries[id]
}

// MarkSuccess clears any error state after a successful upstream call.
func (m *Manager) MarkSuccess(ctx context.Context, id int64) {
	e := m.entry(id)
	if e == nil {
		return
	}
	m.mu.Lock()
	needsReset := e.acct.Status != StatusOK
	touch := e.acct.LastUsedAt == nil || time.Since(*e.acct.LastUsedAt) > time.Minute
	now := time.Now().UTC()
	if touch {
		e.acct.LastUsedAt = &now
	}
	m.mu.Unlock()
	if needsReset {
		m.setStatus(ctx, e, StatusOK, "", nil)
	}
	if touch {
		_ = m.store.TouchAccount(ctx, id, now)
	}
}

// MarkCooldown takes an account out of rotation until the given time.
func (m *Manager) MarkCooldown(ctx context.Context, id int64, until time.Time, msg string) {
	if e := m.entry(id); e != nil {
		m.setStatus(ctx, e, StatusCooldown, msg, &until)
	}
}

// MarkModelCooldown benches one model on one account, leaving its other models usable.
func (m *Manager) MarkModelCooldown(id int64, model string, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	if e == nil {
		return
	}
	if e.modelCool == nil {
		e.modelCool = map[string]time.Time{}
	}
	e.modelCool[model] = until
}

// Unstick drops any affinity binding for a key so the next request picks fresh.
func (m *Manager) Unstick(stickyKey, providerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sticky, stickyKey+"|"+providerID)
}

func (m *Manager) MarkError(ctx context.Context, id int64, until time.Time, msg string) {
	if e := m.entry(id); e != nil {
		m.setStatus(ctx, e, StatusError, msg, &until)
	}
}

// ObserveQuota merges windows learned from response headers into the account's snapshot.
func (m *Manager) ObserveQuota(ctx context.Context, id int64, windows []quota.Window, plan string) {
	if len(windows) == 0 {
		return
	}
	e := m.entry(id)
	if e == nil {
		return
	}
	now := time.Now().UTC()
	m.mu.Lock()
	snap := &quota.Snapshot{}
	if e.snap != nil {
		cp := *e.snap
		cp.Windows = append([]quota.Window(nil), e.snap.Windows...)
		snap = &cp
	}
	snap.Merge(windows)
	snap.UpdatedAt = now
	snap.Source = "headers"
	if plan != "" {
		snap.Plan = plan
	}
	e.snap = snap
	persist := e.acct.QuotaUpdatedAt == nil || now.Sub(*e.acct.QuotaUpdatedAt) > 30*time.Second
	if persist {
		e.acct.QuotaUpdatedAt = &now
	}
	m.mu.Unlock()
	if persist {
		_ = m.store.SaveAccountQuota(ctx, id, snap.Encode(), now)
	}
}

// RefreshQuota polls the provider's usage endpoint for one account.
func (m *Manager) RefreshQuota(ctx context.Context, id int64) error {
	e := m.entry(id)
	if e == nil {
		return store.ErrNotFound
	}
	creds, err := m.Credentials(ctx, id, false)
	if err != nil {
		return err
	}
	p := m.providers[e.acct.Provider]
	snap, err := p.FetchQuota(ctx, creds)
	now := time.Now().UTC()
	m.mu.Lock()
	if err != nil {
		if e.snap != nil {
			cp := *e.snap
			cp.Error = err.Error()
			e.snap = &cp
		} else {
			e.snap = &quota.Snapshot{Error: err.Error(), Windows: []quota.Window{}}
		}
	} else {
		if snap.Plan == "" && e.snap != nil {
			snap.Plan = e.snap.Plan
		}
		e.snap = snap
	}
	e.acct.QuotaUpdatedAt = &now
	encoded := e.snap.Encode()
	plan := e.snap.Plan
	m.mu.Unlock()
	if plan != "" && plan != e.acct.Plan {
		_ = m.store.SaveAccountCredentials(ctx, id, creds.Encode(), plan)
		m.mu.Lock()
		e.acct.Plan = plan
		m.mu.Unlock()
	}
	if serr := m.store.SaveAccountQuota(ctx, id, encoded, now); serr != nil {
		return serr
	}
	return err
}

// RefreshSubscription updates what's known about an account's plan period.
func (m *Manager) RefreshSubscription(ctx context.Context, id int64) error {
	e := m.entry(id)
	if e == nil {
		return store.ErrNotFound
	}
	src, ok := m.providers[e.acct.Provider].(provider.SubscriptionSource)
	if !ok {
		return nil
	}
	creds, err := m.Credentials(ctx, id, false)
	if err != nil {
		return err
	}
	creds.WebSession, _ = m.store.AccountWebSession(ctx, id)
	sub, err := src.Subscription(ctx, creds)
	if err != nil {
		return err
	}
	if sub.RenewedSession != "" {
		if err := m.store.SetAccountWebSession(ctx, id, sub.RenewedSession); err != nil {
			slog.Error("save renewed web session", "account", id, "err", err)
		} else {
			slog.Info("web session renewed", "account", id)
		}
	}
	m.mu.Lock()
	e.sub = sub
	m.mu.Unlock()
	raw, _ := json.Marshal(sub)
	return m.store.SaveAccountSubscription(ctx, id, string(raw))
}

// Redeem spends one banked reset.
func (m *Manager) Redeem(ctx context.Context, id int64, creditID string) (*provider.RedeemResult, error) {
	e := m.entry(id)
	if e == nil {
		return nil, store.ErrNotFound
	}
	r, ok := m.providers[e.acct.Provider].(provider.Redeemer)
	if !ok {
		return nil, errors.New("this provider has no banked resets")
	}
	creds, err := m.Credentials(ctx, id, false)
	if err != nil {
		return nil, err
	}
	res, err := r.Redeem(ctx, creds, provider.UUID(), creditID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	e.lastRedeem = time.Now()
	m.mu.Unlock()
	if res.Code == "reset" || res.Code == "already_redeemed" {
		m.setStatus(ctx, e, StatusOK, "", nil)
	}
	_ = m.RefreshQuota(ctx, id)
	return res, nil
}

// Run keeps tokens fresh, polls quotas and handles auto-redeem until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	m.maintain(ctx, false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.maintain(ctx, false)
		case <-m.wake:
			m.maintain(ctx, true)
		}
	}
}

func (m *Manager) maintain(ctx context.Context, force bool) {
	s := m.settings()
	s.Normalize()
	now := time.Now()
	m.mu.Lock()
	var work []*entry
	for _, e := range m.entries {
		if !e.acct.Disabled {
			work = append(work, e)
		}
	}
	m.mu.Unlock()

	for _, e := range work {
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		acct, creds := e.acct, e.creds
		m.mu.Unlock()
		if acct.Status == StatusReauth {
			continue
		}
		if acct.Status == StatusCooldown && acct.CooldownUntil != nil && !acct.CooldownUntil.After(now) {
			m.setStatus(ctx, e, StatusOK, "", nil)
		}
		p := m.providers[acct.Provider]
		if p != nil && !creds.ExpiresAt.IsZero() && time.Until(creds.ExpiresAt) < p.RefreshLead() {
			cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			if _, err := m.Credentials(cctx, acct.ID, true); err != nil {
				slog.Warn("background token refresh failed", "account", acct.ID, "err", err)
			}
			cancel()
		}
		due := force || acct.QuotaUpdatedAt == nil || now.Sub(*acct.QuotaUpdatedAt) >= time.Duration(s.QuotaPollMinutes)*time.Minute
		if due {
			cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			if err := m.RefreshQuota(cctx, acct.ID); err != nil {
				slog.Debug("quota poll failed", "account", acct.ID, "err", err)
			}
			cancel()
		}
		m.mu.Lock()
		sub := e.sub
		m.mu.Unlock()
		if force || sub == nil || now.Sub(sub.CheckedAt) > 6*time.Hour {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := m.RefreshSubscription(cctx, acct.ID); err != nil {
				slog.Debug("subscription refresh failed", "account", acct.ID, "err", err)
			}
			cancel()
		}
		m.autoRedeem(ctx, e, s, now)
	}
}

func (m *Manager) autoRedeem(ctx context.Context, e *entry, s Settings, now time.Time) {
	m.mu.Lock()
	acct, snap, last := e.acct, e.snap, e.lastRedeem
	recent := 0
	for _, t := range e.redeems {
		if now.Sub(t) < 24*time.Hour {
			recent++
		}
	}
	m.mu.Unlock()
	if acct.Provider != "codex" || snap == nil || snap.Banked == nil || snap.Banked.Available == 0 {
		return
	}
	p := ParseRedeemOverride(acct.RedeemPolicy).Apply(s.Redeem)
	if !p.Enabled || now.Sub(last) < time.Duration(p.GapMinutes)*time.Minute || (p.MaxPerDay > 0 && recent >= p.MaxPerDay) {
		return
	}
	creditID, reason, ok := pickRedeem(p, snap, now)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res, err := m.Redeem(cctx, acct.ID, creditID)
	m.mu.Lock()
	e.lastRedeem = now
	if err == nil {
		kept := e.redeems[:0]
		for _, t := range e.redeems {
			if now.Sub(t) < 24*time.Hour {
				kept = append(kept, t)
			}
		}
		e.redeems = append(kept, now)
	}
	m.mu.Unlock()
	if err != nil {
		slog.Warn("auto-redeem failed", "account", acct.ID, "err", err)
		return
	}
	slog.Info("auto-redeemed banked reset", "account", acct.ID, "reason", reason, "result", res.Code)
}
