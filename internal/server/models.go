package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/store"
)

type catalogEntry struct {
	models.Model
	Disabled bool                  `json:"disabled"`
	Override routing.ModelOverride `json:"override"`
	Strategy string                `json:"strategy"`
	// Accounts is how many enabled accounts can serve the model right now.
	Accounts int `json:"accounts"`
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	rs := s.settings.Get().Routing
	enabled := map[string][]int64{}
	for _, a := range s.accounts.List() {
		if !a.Disabled {
			enabled[a.Provider] = append(enabled[a.Provider], a.ID)
		}
	}
	out := []catalogEntry{}
	for _, m := range rs.Catalog() {
		o := rs.Models[m.Canonical]
		n := 0
		for _, id := range enabled[m.Provider] {
			if len(o.Accounts) == 0 || slices.Contains(o.Accounts, id) {
				n++
			}
		}
		out = append(out, catalogEntry{Model: m, Disabled: o.Disabled, Override: o, Strategy: rs.StrategyFor(m), Accounts: n})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out, "labs": models.Labs})
}

// resolveModel previews how a request would be routed, optionally as a specific key.
func (s *Server) resolveModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string            `json:"model"`
		Format string            `json:"format"`
		KeyID  int64             `json:"keyId"`
		Rules  *routing.KeyRules `json:"rules"`
		// Allowed previews an unsaved allow-list.
		Allowed *[]string `json:"allowed"`
		// Routing previews unsaved routing settings instead of the stored ones.
		Routing *routing.Settings `json:"routing"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	rs := s.settings.Get().Routing
	if body.Routing != nil {
		rs = *body.Routing
		rs.Normalize()
	}
	req := routing.Request{Model: body.Model, Format: body.Format}
	if body.KeyID > 0 {
		key, err := s.store.GetKey(r.Context(), body.KeyID)
		if err != nil {
			writeStoreError(w, r, err)
			return
		}
		req.Allowed = key.AllowedModels
		_ = json.Unmarshal(key.ModelRules, &req.KeyRules)
	}
	if body.Rules != nil {
		req.KeyRules = *body.Rules
	}
	if body.Allowed != nil {
		req.Allowed = *body.Allowed
	}
	route, err := rs.Resolve(req)
	resp := map[string]any{"route": route}
	if err != nil {
		resp["error"] = err.Error()
		resp["notAllowed"] = errors.Is(err, routing.ErrNotAllowed)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	keys, err := s.store.ListAllKeys(r.Context())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	for _, k := range keys {
		if k.ID == id {
			writeJSON(w, http.StatusOK, map[string]any{"key": k})
			return
		}
	}
	writeStoreError(w, r, store.ErrNotFound)
}
