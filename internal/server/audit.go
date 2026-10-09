package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/sobakintech/maitake/internal/audit"
	"github.com/sobakintech/maitake/internal/store"
)

func auditFilter(r *http.Request) store.AuditFilter {
	q := r.URL.Query()
	f := store.AuditFilter{UserID: queryID(r, "userId"), KeyID: queryID(r, "keyId"), AccountID: queryID(r, "accountId"),
		Model: q.Get("model"), Status: q.Get("status"), Search: q.Get("q")}
	if span, ok := usageRanges[q.Get("range")]; ok {
		f.Since = time.Now().Add(-span)
	}
	return f
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	f.Before = queryID(r, "before")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	ctx := r.Context()
	entries, err := s.store.ListAudit(ctx, f, limit)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	stats, err := s.store.AuditStats(ctx)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	var next int64
	if len(entries) == limit {
		next = entries[len(entries)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "next": next, "stats": stats,
		"names": s.usageNames(r)})
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	entry, err := s.store.GetAudit(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	payload, err := audit.Decode(entry.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored detail is unreadable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry, "payload": payload, "names": s.usageNames(r)})
}

func (s *Server) deleteAudit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAudit(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearAudit deletes every entry matching the same filters the list takes.
func (s *Server) clearAudit(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ClearAudit(r.Context(), auditFilter(r))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
