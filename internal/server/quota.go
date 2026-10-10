package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
	"github.com/sobakintech/maitake/internal/resets"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/settings"
	"github.com/sobakintech/maitake/internal/store"
)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	// outboundProxy tells the UI that Claude TLS fingerprinting is off (it needs a direct connection).
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.settings.Get(), "outboundProxy": s.cfg.ProxyURL != ""})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settings.Settings
	if !readJSON(w, r, &body) {
		return
	}
	if id := body.QuotaPage.PublicProfileID; id > 0 {
		if _, err := s.store.GetProfile(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "the public profile does not exist")
			return
		}
	}
	prev := s.settings.Get()
	saved, err := s.settings.Put(r.Context(), body)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	if saved.CodexResets != prev.CodexResets {
		s.resets.Refresh()
	}
	if saved.ClaudeResets != prev.ClaudeResets {
		s.claudeResets.Refresh()
	}
	if saved.Updates != prev.Updates {
		s.updates.Wake()
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": saved})
}

type profileBody struct {
	Name   *string         `json:"name"`
	Config json.RawMessage `json:"config"`
}

// normalizedConfig validates a profile config and returns it with defaults filled in.
func normalizedConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, errors.New("config must be a JSON object")
	}
	p := quota.ParseProfile(raw)
	return json.Marshal(p)
}

type profileView struct {
	store.DisplayProfile
	Config   quota.Profile `json:"config"`
	IsPublic bool          `json:"isPublic"`
}

func (s *Server) profileView(p store.DisplayProfile) profileView {
	return profileView{DisplayProfile: p, Config: quota.ParseProfile(p.Config),
		IsPublic: p.ID == s.settings.Get().QuotaPage.PublicProfileID}
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.ListProfiles(r.Context())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	out := make([]profileView, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, s.profileView(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out, "defaults": quota.DefaultProfile()})
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var body profileBody
	if !readJSON(w, r, &body) {
		return
	}
	if body.Name == nil || !validName(w, body.Name, true) {
		if body.Name == nil {
			writeError(w, http.StatusBadRequest, "name is required")
		}
		return
	}
	cfg, err := normalizedConfig(body.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.store.CreateProfile(r.Context(), *body.Name, cfg)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": s.profileView(*p)})
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body profileBody
	if !readJSON(w, r, &body) || !validName(w, body.Name, true) {
		return
	}
	var cfg json.RawMessage
	if body.Config != nil {
		var err error
		if cfg, err = normalizedConfig(body.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	p, err := s.store.UpdateProfile(r.Context(), id, body.Name, cfg)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": s.profileView(*p)})
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteProfile(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	if cur := s.settings.Get(); cur.QuotaPage.PublicProfileID == id {
		cur.QuotaPage.PublicProfileID = 0
		_, _ = s.settings.Put(r.Context(), cur)
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewProfile renders the quota page for an unsaved config so the editor can show it live.
func (s *Server) previewProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config json.RawMessage `json:"config"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	view := quota.Build(quota.ParseProfile(body.Config), s.settings.Get().QuotaPage, s.accounts.QuotaInfos(), s.feeds(), time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"view": view})
}

func (s *Server) feeds() quota.Feeds {
	codex, claude := s.resets.Snapshot(), s.claudeResets.Snapshot()
	return quota.Feeds{Codex: &codex, Claude: &claude}
}

func resetFeed(p *resets.Poller, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{key: p.Snapshot()})
	}
}

func refreshResetFeed(p *resets.Poller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.Refresh()
		w.WriteHeader(http.StatusAccepted)
	}
}

// quotaMeta tells the public page whether it's enabled and whether it needs a key, without auth.
func (s *Server) quotaMeta(w http.ResponseWriter, r *http.Request) {
	page := s.settings.Get().QuotaPage
	access := page.Access
	if page.PublicProfileID == 0 {
		access = "key"
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": page.Enabled, "access": access, "title": page.Title})
}

// publicQuota serves the quota page. Each key sees its own view or its profile; keys with
// neither see nothing. In public mode, visitors without a key see the public profile.
func (s *Server) publicQuota(w http.ResponseWriter, r *http.Request) {
	page := s.settings.Get().QuotaPage
	if !page.Enabled {
		writeError(w, http.StatusNotFound, "the quota page is disabled")
		return
	}
	var profile *quota.Profile
	load := func(id int64) {
		if p, err := s.store.GetProfile(r.Context(), id); err == nil {
			v := quota.ParseProfile(p.Config)
			profile = &v
		}
	}
	if key := clientKey(r); key != "" {
		owner, err := s.store.AuthenticateKey(r.Context(), secret.Hash(key))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		if err != nil {
			writeStoreError(w, r, err)
			return
		}
		if len(owner.Key.DisplayConfig) > 0 {
			v := quota.ParseProfile(owner.Key.DisplayConfig)
			profile = &v
		} else if id := owner.Key.DisplayProfileID; id != nil {
			load(*id)
		}
		if profile == nil {
			writeError(w, http.StatusForbidden, "this key has no quota page access")
			return
		}
	} else if page.Access == "public" && page.PublicProfileID > 0 {
		load(page.PublicProfileID)
	}
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "an API key is required to view this page")
		return
	}
	view := quota.Build(*profile, page, s.accounts.QuotaInfos(), s.feeds(), time.Now())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"view": view})
}

func clientKey(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Api-Key"); v != "" {
		return v
	}
	return r.Header.Get("X-Goog-Api-Key")
}
