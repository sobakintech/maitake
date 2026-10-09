package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/store"
)

const oauthTTL = 15 * time.Minute

// oauthSession is an in-progress login started from the dashboard.
type oauthSession struct {
	id        string
	provider  string
	verifier  string
	state     string
	created   time.Time
	device    *codex.DeviceCode
	status    string // pending, done, error
	err       string
	accountID int64
}

type oauthSessions struct {
	mu       sync.Mutex
	sessions map[string]*oauthSession
}

func (o *oauthSessions) put(s *oauthSession) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sessions == nil {
		o.sessions = map[string]*oauthSession{}
	}
	for id, existing := range o.sessions {
		if time.Since(existing.created) > oauthTTL {
			delete(o.sessions, id)
		}
	}
	o.sessions[s.id] = s
}

func (o *oauthSessions) get(id string) *oauthSession {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := o.sessions[id]
	if s == nil || time.Since(s.created) > oauthTTL {
		return nil
	}
	cp := *s
	return &cp
}

func (o *oauthSessions) update(id string, fn func(*oauthSession)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s := o.sessions[id]; s != nil {
		fn(s)
	}
}

func (o *oauthSessions) remove(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.sessions, id)
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"accounts": s.accounts.List()})
}

func (s *Server) accountResponse(w http.ResponseWriter, status int, id int64) {
	v, ok := s.accounts.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, status, map[string]any{"account": v})
}

func (s *Server) startOAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	verifier, challenge := provider.PKCE()
	sess := &oauthSession{id: provider.RandomHex(16), provider: body.Provider, verifier: verifier,
		state: provider.RandomHex(16), created: time.Now(), status: "pending"}
	var authURL, redirect string
	switch body.Provider {
	case "claude":
		authURL, redirect = s.claude.AuthURL(sess.state, challenge), claude.RedirectURI
	case "codex":
		authURL, redirect = s.codex.AuthURL(sess.state, challenge), codex.RedirectURI
	default:
		writeError(w, http.StatusBadRequest, "provider must be claude or codex")
		return
	}
	s.oauth.put(sess)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sess.id, "authUrl": authURL, "redirectUri": redirect})
}

func (s *Server) completeOAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"sessionId"`
		Callback  string `json:"callback"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	sess := s.oauth.get(body.SessionID)
	if sess == nil || sess.device != nil {
		writeError(w, http.StatusBadRequest, "login session expired; start again")
		return
	}
	code, state := provider.ParseCallback(body.Callback)
	if code == "" {
		writeError(w, http.StatusBadRequest, "couldn't find an authorization code in what you pasted")
		return
	}
	if state != "" && state != sess.state {
		writeError(w, http.StatusBadRequest, "state mismatch: this callback belongs to a different login attempt")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var creds provider.Credentials
	var ident provider.Identity
	var err error
	if sess.provider == "claude" {
		creds, ident, err = s.claude.Exchange(ctx, code, sess.state, sess.verifier)
	} else {
		creds, ident, err = s.codex.Exchange(ctx, code, sess.verifier)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "login failed: "+err.Error())
		return
	}
	acct, err := s.accounts.Add(r.Context(), sess.provider, creds, ident)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	s.oauth.remove(sess.id)
	s.accountResponse(w, http.StatusCreated, acct.ID)
}

func (s *Server) startDevice(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	dc, err := s.codex.StartDevice(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "couldn't start device login: "+err.Error())
		return
	}
	sess := &oauthSession{id: provider.RandomHex(16), provider: "codex", created: time.Now(), device: dc, status: "pending"}
	s.oauth.put(sess)
	go s.pollDevice(sess.id, dc)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sess.id, "userCode": dc.UserCode,
		"verificationUrl": codex.DeviceVerifyURL, "expiresAt": sess.created.Add(oauthTTL)})
}

func (s *Server) pollDevice(id string, dc *codex.DeviceCode) {
	deadline := time.Now().Add(oauthTTL)
	for time.Now().Before(deadline) {
		time.Sleep(dc.Interval)
		if s.oauth.get(id) == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		creds, ident, err := s.codex.PollDevice(ctx, dc)
		cancel()
		if errors.Is(err, codex.ErrPending) {
			continue
		}
		if err != nil {
			s.oauth.update(id, func(o *oauthSession) { o.status, o.err = "error", err.Error() })
			return
		}
		acct, err := s.accounts.Add(context.Background(), "codex", creds, ident)
		if err != nil {
			s.oauth.update(id, func(o *oauthSession) { o.status, o.err = "error", err.Error() })
			return
		}
		s.oauth.update(id, func(o *oauthSession) { o.status, o.accountID = "done", acct.ID })
		return
	}
	s.oauth.update(id, func(o *oauthSession) { o.status, o.err = "error", "device code expired" })
}

func (s *Server) deviceStatus(w http.ResponseWriter, r *http.Request) {
	sess := s.oauth.get(chi.URLParam(r, "session"))
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "error": "login session expired"})
		return
	}
	resp := map[string]any{"status": sess.status}
	if sess.err != "" {
		resp["error"] = sess.err
	}
	if sess.accountID > 0 {
		if v, ok := s.accounts.Get(sess.accountID); ok {
			resp["account"] = v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) cancelDevice(w http.ResponseWriter, r *http.Request) {
	s.oauth.remove(chi.URLParam(r, "session"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) importAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		JSON     string `json:"json"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	raw := []byte(strings.TrimSpace(body.JSON))
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var creds provider.Credentials
	var ident provider.Identity
	var err error
	switch body.Provider {
	case "claude":
		creds, err = claude.ImportCredentials(raw)
		if err == nil {
			// Refreshing fills in the profile (email, account id, plan) and proves the tokens work.
			creds, ident, err = s.claude.Refresh(ctx, creds)
		}
	case "codex":
		creds, ident, err = codex.ImportCredentials(raw)
		if err == nil && creds.RefreshToken != "" {
			var refreshed provider.Identity
			if creds, refreshed, err = s.codex.Refresh(ctx, creds); err == nil && refreshed.ExternalID != "" {
				ident = refreshed
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "provider must be claude or codex")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "import failed: "+err.Error())
		return
	}
	acct, err := s.accounts.Add(r.Context(), body.Provider, creds, ident)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	s.accountResponse(w, http.StatusCreated, acct.ID)
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		store.AccountPatch
		// Redeem replaces the account's banked reset override; null fields use the pool default.
		Redeem *accounts.RedeemOverride `json:"redeem"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Redeem != nil {
		raw, _ := json.Marshal(body.Redeem)
		policy := string(raw)
		if policy == "{}" {
			policy = ""
		}
		body.RedeemPolicy = &policy
	}
	if body.Label != nil {
		*body.Label = strings.TrimSpace(*body.Label)
		if *body.Label == "" || len(*body.Label) > 100 {
			writeError(w, http.StatusBadRequest, "label must be 1-100 characters")
			return
		}
	}
	if body.Alias != nil && len(*body.Alias) > 100 {
		writeError(w, http.StatusBadRequest, "alias must be at most 100 characters")
		return
	}
	if _, err := s.accounts.Update(r.Context(), id, body.AccountPatch); err != nil {
		writeStoreError(w, r, err)
		return
	}
	s.accountResponse(w, http.StatusOK, id)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.accounts.Delete(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refreshAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if _, err := s.accounts.Credentials(ctx, id, true); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusBadGateway, "token refresh failed: "+err.Error())
		return
	}
	if err := s.accounts.RefreshQuota(ctx, id); err != nil {
		writeError(w, http.StatusBadGateway, "quota refresh failed: "+err.Error())
		return
	}
	_ = s.accounts.RefreshSubscription(ctx, id)
	s.accountResponse(w, http.StatusOK, id)
}

func (s *Server) redeemAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		CreditID string `json:"creditId"`
	}
	if r.ContentLength > 0 && !readJSON(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.accounts.Redeem(ctx, id, body.CreditID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusBadGateway, "redeem failed: "+err.Error())
		return
	}
	v, _ := s.accounts.Get(id)
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "account": v})
}

// refreshAllAccounts refreshes every enabled account's limits and subscription and reports
// which ones failed.
func (s *Server) refreshAllAccounts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	type failed struct {
		ID    int64  `json:"id"`
		Label string `json:"label"`
		Error string `json:"error"`
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	failures := []failed{}
	refreshed := 0
	for _, a := range s.accounts.List() {
		if a.Disabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.accounts.RefreshQuota(ctx, a.ID)
			if err == nil {
				_ = s.accounts.RefreshSubscription(ctx, a.ID)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, failed{a.ID, a.Label, err.Error()})
			} else {
				refreshed++
			}
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": refreshed, "failed": failures})
}

// setWebSession stores a claude.ai session cookie for an account (empty clears it) and reads
// the subscription with it right away, so the result shows immediately.
func (s *Server) setWebSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Session string `json:"session"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	if a.Provider != "claude" {
		writeError(w, http.StatusBadRequest, "web sessions are only used for Claude accounts")
		return
	}
	session := strings.TrimSpace(body.Session)
	if len(session) > 4096 {
		writeError(w, http.StatusBadRequest, "session is too long")
		return
	}
	if err := s.store.SetAccountWebSession(r.Context(), id, session); err != nil {
		writeStoreError(w, r, err)
		return
	}
	if err := s.accounts.Reload(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := s.accounts.RefreshSubscription(ctx, id); err != nil {
		writeError(w, http.StatusBadGateway, "subscription check failed: "+err.Error())
		return
	}
	s.accountResponse(w, http.StatusOK, id)
}
