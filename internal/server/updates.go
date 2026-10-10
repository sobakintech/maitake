package server

import "net/http"

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"updates": s.updates.Snapshot()})
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"updates": s.updates.Check(r.Context())})
}
