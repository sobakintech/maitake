package server

import (
	"log/slog"
	"net/http"

	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
)

// sealKey encrypts a new key for later copying, or returns nil when copies are switched off.
func (s *Server) sealKey(key string) []byte {
	if s.sealer == nil || !s.settings.Get().Keys.StoreCopies {
		return nil
	}
	return s.sealer.Seal(key)
}

func (s *Server) keySecret(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	enc, err := s.store.KeySecret(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	if enc == nil || s.sealer == nil {
		writeError(w, http.StatusNotFound, "no copy of this key is stored; regenerate it to get a copyable key")
		return
	}
	key, err := s.sealer.Open(enc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	admin := r.Context().Value(adminKey).(*store.Admin)
	slog.Info("api key copied", "key", id, "admin", admin.Username)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"secret": key})
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	key, prefix, hash := secret.NewAPIKey()
	rotated, err := s.store.RotateKey(r.Context(), id, prefix, hash, s.sealKey(key))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"key": rotated, "secret": key})
}

func (s *Server) clearKeySecrets(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ClearKeySecrets(r.Context())
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"cleared": n})
}
