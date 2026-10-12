package accounts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/store"
)

// PrimerPlan is an account's next planned ping.
type PrimerPlan struct {
	At time.Time `json:"at"`
	// ReadyAt is when the session it starts will reset.
	ReadyAt time.Time `json:"readyAt"`
}

// primerState is the primer's bookkeeping for one account.
type primerState struct {
	busy    bool
	handled map[int64]bool // planned slot pings already acted on, by unix second
	next    *PrimerPlan
	last    *store.PrimerRun
}

type primerJob struct {
	trigger string
	planned *time.Time
	loc     *time.Location // the planned time's zone, for messages
}

// planPrimer picks what is due now, if anything, and the next ping after that.
func planPrimer(p PrimerPolicy, e *entry, now time.Time) (*primerJob, *PrimerPlan) {
	st := &e.primer
	w := sessionWindow(e.snap)
	length := sessionLength(w)
	ends := sessionEnds(w, now)
	delay := time.Duration(p.DelaySeconds) * time.Second
	grace := max(time.Duration(p.GraceMinutes)*time.Minute, time.Minute)

	var job *primerJob
	var next *PrimerPlan
	consider := func(at time.Time) {
		if next == nil || at.Before(next.At) {
			next = &PrimerPlan{At: at, ReadyAt: at.Add(length)}
		}
	}
	for _, sp := range slotPings(p, length, now.Add(-grace), now.Add(8*24*time.Hour)) {
		if st.handled[sp.at.Unix()] {
			continue
		}
		if sp.at.After(now) {
			consider(sp.at)
			continue
		}
		// The session before is still running: ping once it ends, if that's within the grace period.
		if ends != nil && ends.Add(delay).Before(sp.at.Add(grace)) {
			consider(ends.Add(delay))
			continue
		}
		if job == nil {
			at := sp.at
			job = &primerJob{trigger: "slot", planned: &at, loc: sp.loc}
		}
	}
	return job, next
}

// WakePrimer re-plans right away, after settings or an account's rules change.
func (m *Manager) WakePrimer() {
	select {
	case m.primerWake <- struct{}{}:
	default:
	}
}

// RunPrimer starts accounts' session windows on their schedules until ctx ends.
func (m *Manager) RunPrimer(ctx context.Context) {
	m.loadPrimerHistory(ctx)
	for {
		wait := min(max(time.Until(m.primerTick(ctx, time.Now())), time.Second), 30*time.Second)
		select {
		case <-ctx.Done():
			return
		case <-m.primerWake:
		case <-time.After(wait):
		}
	}
}

func (m *Manager) loadPrimerHistory(ctx context.Context) {
	runs, err := m.store.PrimerRuns(ctx, 0, time.Now().Add(-48*time.Hour), 5000)
	if err != nil {
		slog.Warn("load primer history", "err", err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(runs) - 1; i >= 0; i-- { // oldest first
		r := runs[i]
		e := m.entries[r.AccountID]
		if e == nil {
			continue
		}
		if r.PlannedAt != nil {
			if e.primer.handled == nil {
				e.primer.handled = map[int64]bool{}
			}
			e.primer.handled[r.PlannedAt.Unix()] = true
		}
		e.primer.last = &r
	}
}

// primerTick starts every due run and returns when the next one is planned.
func (m *Manager) primerTick(ctx context.Context, now time.Time) time.Time {
	s := m.settings()
	s.Normalize()
	earliest := now.Add(time.Minute)
	type start struct {
		id     int64
		policy PrimerEffective
		job    primerJob
	}
	var starts []start
	m.mu.Lock()
	for _, e := range m.entries {
		p := s.Primer.Effective(e.acct.Provider, ParseAccountPrimer(e.acct.PrimerPolicy))
		if !p.Enabled || e.acct.Disabled || e.acct.Status == StatusReauth {
			e.primer.next = nil
			continue
		}
		job, next := planPrimer(p.PrimerPolicy, e, now)
		e.primer.next = next
		if next != nil && next.At.Before(earliest) {
			earliest = next.At
		}
		if job == nil || e.primer.busy {
			continue
		}
		e.primer.busy = true
		if job.planned != nil {
			if e.primer.handled == nil {
				e.primer.handled = map[int64]bool{}
			}
			e.primer.handled[job.planned.Unix()] = true
		}
		starts = append(starts, start{e.acct.ID, p, *job})
	}
	m.mu.Unlock()
	for _, st := range starts {
		go m.runPrimer(ctx, st.id, st.policy, s.Primer.Prompt, st.job)
	}
	return earliest
}

// PrimeNow pings an account right away, whatever its schedule, and reports what happened.
func (m *Manager) PrimeNow(ctx context.Context, id int64) (store.PrimerRun, error) {
	e := m.entry(id)
	if e == nil {
		return store.PrimerRun{}, store.ErrNotFound
	}
	s := m.settings()
	s.Normalize()
	m.mu.Lock()
	if e.primer.busy {
		m.mu.Unlock()
		return store.PrimerRun{}, errors.New("a ping is already in progress for this account")
	}
	e.primer.busy = true
	p := s.Primer.Effective(e.acct.Provider, ParseAccountPrimer(e.acct.PrimerPolicy))
	m.mu.Unlock()
	p.Retries, p.Verify = 0, true
	return m.runPrimer(ctx, id, p, s.Primer.Prompt, primerJob{trigger: "manual"}), nil
}

func (m *Manager) runPrimer(ctx context.Context, id int64, p PrimerEffective, prompt string, job primerJob) store.PrimerRun {
	run := m.prime(ctx, id, p, prompt, job)
	record := run.Outcome != "deferred"
	m.mu.Lock()
	if e := m.entries[id]; e != nil {
		st := &e.primer
		st.busy = false
		if run.Outcome == "deferred" && job.planned != nil {
			delete(st.handled, job.planned.Unix())
		}
		if record {
			st.last = &run
		}
	}
	m.mu.Unlock()
	if record {
		if err := m.store.InsertPrimerRun(context.WithoutCancel(ctx), &run); err != nil {
			slog.Error("save primer run", "account", id, "err", err)
		}
		slog.Info("session primer", "account", id, "trigger", job.trigger, "outcome", run.Outcome, "pings", run.Pings,
			"message", run.Message)
	}
	m.WakePrimer()
	return run
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func clock(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 15:04")
}

// prime runs one attempt to start a session: check it's idle, ping, verify, retry.
func (m *Manager) prime(ctx context.Context, id int64, p PrimerEffective, prompt string, job primerJob) store.PrimerRun {
	run := store.PrimerRun{AccountID: id, Time: time.Now().UTC(), Trigger: job.trigger, PlannedAt: job.planned}
	finish := func(outcome, format string, args ...any) store.PrimerRun {
		run.Outcome, run.Message = outcome, fmt.Sprintf(format, args...)
		return run
	}
	e := m.entry(id)
	if e == nil {
		return finish("failed", "account not found")
	}
	m.mu.Lock()
	providerID := e.acct.Provider
	m.mu.Unlock()
	pinger, ok := m.providers[providerID].(provider.Pinger)
	if !ok {
		return finish("failed", "%s accounts can't be primed", providerID)
	}
	model := p.Model
	loc := job.loc
	if loc == nil {
		loc = time.Local
	}

	snapshot := func() {
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if err := m.RefreshQuota(cctx, id); err != nil {
			slog.Debug("primer quota check failed", "account", id, "err", err)
		}
	}
	snapshot()
	now := time.Now()
	m.mu.Lock()
	snap, acct := e.snap, e.acct
	m.mu.Unlock()
	w := sessionWindow(snap)
	length := sessionLength(w)
	if ends := sessionEnds(w, now); ends != nil {
		grace := max(time.Duration(p.GraceMinutes)*time.Minute, time.Minute)
		if job.planned != nil && ends.Add(time.Duration(p.DelaySeconds)*time.Second).Before(job.planned.Add(grace)) {
			return finish("deferred", "")
		}
		run.ResetsAt = ends
		return finish("skipped", "A session was already running until %s", clock(*ends, loc))
	}
	if snap != nil {
		if hit, until := snap.Exhausted(now); hit {
			if until != nil {
				return finish("skipped", "A limit is used up until %s", clock(*until, loc))
			}
			return finish("skipped", "A limit is used up")
		}
	}
	if acct.CooldownUntil != nil && acct.CooldownUntil.After(now) {
		return finish("skipped", "The account is cooling down until %s", clock(*acct.CooldownUntil, loc))
	}

	var lastErr string
	for attempt := 0; attempt <= p.Retries; attempt++ {
		pingAt := time.Now()
		run.Pings++
		err := m.ping(ctx, id, pinger, model, prompt)
		var he *provider.HTTPError
		switch {
		case err != nil && errors.As(err, &he) && he.Status == http.StatusTooManyRequests:
			return finish("failed", "Rate limited: %v", err)
		case err != nil:
			lastErr = err.Error()
		case !p.Verify:
			run.Outcome = "sent"
		default:
			if !sleepCtx(ctx, time.Duration(p.VerifySeconds)*time.Second) {
				return finish("failed", "stopped")
			}
			snapshot()
			m.mu.Lock()
			sw := sessionWindow(e.snap)
			m.mu.Unlock()
			if ends := sessionEnds(sw, time.Now()); ends != nil && ends.After(pingAt.Add(length/2)) {
				run.ResetsAt = ends
				return finish("started", "Session started; it resets %s", clock(*ends, loc))
			}
			lastErr = "the ping went through but the session didn't start yet"
		}
		if attempt < p.Retries && !sleepCtx(ctx, time.Until(pingAt.Add(time.Duration(p.RetrySeconds)*time.Second))) {
			return finish("failed", "stopped")
		}
	}
	if run.Outcome == "sent" {
		snapshot()
		m.mu.Lock()
		run.ResetsAt = sessionEnds(sessionWindow(e.snap), time.Now())
		m.mu.Unlock()
		if run.ResetsAt != nil {
			return finish("sent", "Sent %d pings; the session resets %s", run.Pings, clock(*run.ResetsAt, loc))
		}
		return finish("sent", "Sent %d pings", run.Pings)
	}
	return finish("failed", "%s", lastErr)
}

// ping sends one request, retrying once with fresh credentials if the token was rejected.
func (m *Manager) ping(ctx context.Context, id int64, pinger provider.Pinger, model, prompt string) error {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	creds, err := m.Credentials(cctx, id, false)
	if err != nil {
		return err
	}
	res, err := pinger.Ping(cctx, creds, model, prompt)
	var he *provider.HTTPError
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
		if creds, err = m.Credentials(cctx, id, true); err != nil {
			return err
		}
		res, err = pinger.Ping(cctx, creds, model, prompt)
	}
	if res != nil {
		m.ObserveQuota(cctx, id, res.Windows, res.Plan)
	}
	return err
}
