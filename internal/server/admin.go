package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	user, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	keys, err := s.store.ListKeys(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "keys": keys})
}

func validName(w http.ResponseWriter, name *string, required bool) bool {
	if name == nil {
		return true
	}
	*name = strings.TrimSpace(*name)
	if (required && *name == "") || len(*name) > 100 {
		writeError(w, http.StatusBadRequest, "name must be 1-100 characters")
		return false
	}
	return true
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if !readJSON(w, r, &body) || !validName(w, &body.Name, true) {
		return
	}
	user, err := s.store.CreateUser(r.Context(), body.Name, strings.TrimSpace(body.Note))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body store.UserPatch
	if !readJSON(w, r, &body) || !validName(w, body.Name, true) {
		return
	}
	user, err := s.store.UpdateUser(r.Context(), id, body)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type newKeyBody struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expiresAt"`
	// UserID picks the owner on POST /admin/keys; omitted means the calling admin.
	UserID *int64 `json:"userId"`
}

func (s *Server) readNewKey(w http.ResponseWriter, r *http.Request) (*newKeyBody, bool) {
	var body newKeyBody
	if !readJSON(w, r, &body) || !validName(w, &body.Name, false) {
		return nil, false
	}
	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "expiry must be in the future")
		return nil, false
	}
	return &body, true
}

// issueKey generates a key and stores it for a user, or for the given admin when userID is 0.
func (s *Server) issueKey(w http.ResponseWriter, r *http.Request, userID, adminID int64, body *newKeyBody) {
	key, prefix, hash := secret.NewAPIKey()
	enc := s.sealKey(key)
	var created *store.APIKey
	var err error
	if userID > 0 {
		created, err = s.store.CreateKey(r.Context(), userID, body.Name, prefix, hash, enc, body.ExpiresAt)
	} else {
		created, err = s.store.CreateAdminKey(r.Context(), adminID, body.Name, prefix, hash, enc, body.ExpiresAt)
	}
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": created, "secret": key})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r)
	if !ok {
		return
	}
	if body, ok := s.readNewKey(w, r); ok {
		s.issueKey(w, r, userID, 0, body)
	}
}

func (s *Server) listAllKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListAllKeys(r.Context())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *Server) createOwnedKey(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readNewKey(w, r)
	if !ok {
		return
	}
	var userID int64
	if body.UserID != nil {
		userID = *body.UserID
	}
	admin := r.Context().Value(adminKey).(*store.Admin)
	s.issueKey(w, r, userID, admin.ID, body)
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body store.KeyPatch
	if !readJSON(w, r, &body) || !validName(w, body.Name, false) {
		return
	}
	if body.ModelRules != nil && string(*body.ModelRules) != "null" {
		var rules routing.KeyRules
		if err := json.Unmarshal(*body.ModelRules, &rules); err != nil {
			writeError(w, http.StatusBadRequest, "invalid model rules: "+err.Error())
			return
		}
		normalized, _ := json.Marshal(rules)
		raw := json.RawMessage(normalized)
		body.ModelRules = &raw
	}
	if body.DisplayConfig != nil && string(*body.DisplayConfig) != "null" {
		cfg, err := normalizedConfig(*body.DisplayConfig)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid quota page view: "+err.Error())
			return
		}
		raw := json.RawMessage(cfg)
		body.DisplayConfig = &raw
	}
	if body.AuditMode != nil && *body.AuditMode != "" && *body.AuditMode != "on" && *body.AuditMode != "off" {
		writeError(w, http.StatusBadRequest, `audit mode must be "", "on" or "off"`)
		return
	}
	if body.ExpiresAt.Value != nil && !body.ExpiresAt.Value.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "expiry must be in the future")
		return
	}
	key, err := s.store.UpdateKey(r.Context(), id, body)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteKey(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
