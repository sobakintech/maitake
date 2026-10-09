package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
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
	if id := body.QuotaPage.DefaultProfileID; id > 0 {
		if _, err := s.store.GetProfile(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "default profile does not exist")
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
	Config    quota.Profile `json:"config"`
	IsDefault bool          `json:"isDefault"`
}

func (s *Server) profileView(p store.DisplayProfile) profileView {
	return profileView{DisplayProfile: p, Config: quota.ParseProfile(p.Config),
		IsDefault: p.ID == s.settings.Get().QuotaPage.DefaultProfileID}
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
	if cur := s.settings.Get(); cur.QuotaPage.DefaultProfileID == id {
		cur.QuotaPage.DefaultProfileID = 0
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
	snap := s.resets.Snapshot()
	view := quota.Build(quota.ParseProfile(body.Config), s.settings.Get().QuotaPage, s.accounts.QuotaInfos(), &snap, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"view": view})
}

func (s *Server) codexResets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"codexResets": s.resets.Snapshot()})
}

func (s *Server) refreshCodexResets(w http.ResponseWriter, r *http.Request) {
	s.resets.Refresh()
	w.WriteHeader(http.StatusAccepted)
}

// quotaMeta tells the public page whether it's enabled and whether it needs a key, without auth.
func (s *Server) quotaMeta(w http.ResponseWriter, r *http.Request) {
	page := s.settings.Get().QuotaPage
	writeJSON(w, http.StatusOK, map[string]any{"enabled": page.Enabled, "access": page.Access, "title": page.Title})
}

// defaultProfile is the view for keys without their own: a chosen profile, else the default view.
func (s *Server) defaultProfile(r *http.Request) quota.Profile {
	page := s.settings.Get().QuotaPage
	if page.DefaultProfileID > 0 {
		if p, err := s.store.GetProfile(r.Context(), page.DefaultProfileID); err == nil {
			return quota.ParseProfile(p.Config)
		}
	}
	return *page.DefaultView
}

// publicQuota serves the quota page, filtered by the viewer's display profile.
func (s *Server) publicQuota(w http.ResponseWriter, r *http.Request) {
	page := s.settings.Get().QuotaPage
	if !page.Enabled {
		writeError(w, http.StatusNotFound, "the quota page is disabled")
		return
	}
	profile := s.defaultProfile(r)
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
		// A key's own view wins, then its profile, then the default.
		if len(owner.Key.DisplayConfig) > 0 {
			profile = quota.ParseProfile(owner.Key.DisplayConfig)
		} else if id := owner.Key.DisplayProfileID; id != nil {
			if p, err := s.store.GetProfile(r.Context(), *id); err == nil {
				profile = quota.ParseProfile(p.Config)
			}
		}
	} else if page.Access != "public" {
		writeError(w, http.StatusUnauthorized, "an API key is required to view this page")
		return
	}
	snap := s.resets.Snapshot()
	view := quota.Build(profile, page, s.accounts.QuotaInfos(), &snap, time.Now())
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
