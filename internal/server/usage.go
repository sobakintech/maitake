package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/store"
)

var usageRanges = map[string]time.Duration{
	"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour,
}

func queryID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return id
}

type namedBucket struct {
	store.UsageBucket
	Name string `json:"name"`
}

// usage returns everything the usage dashboard needs for one time range in a single call.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	span, ok := usageRanges[q.Get("range")]
	if !ok {
		span = usageRanges["24h"]
	}
	until := time.Now()
	f := store.UsageFilter{Since: until.Add(-span), Until: until.Add(time.Second),
		UserID: queryID(r, "userId"), KeyID: queryID(r, "keyId"), AccountID: queryID(r, "accountId")}
	tz, _ := strconv.Atoi(q.Get("tzOffset")) // minutes east of UTC
	seriesBy := "day"
	if span <= 48*time.Hour {
		seriesBy = "hour"
	}
	ctx := r.Context()

	totals, err := s.store.UsageTotals(ctx, f)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	series, err := s.store.UsageBreakdown(ctx, f, seriesBy, tz)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	resp := map[string]any{"range": q.Get("range"), "since": f.Since, "until": until, "totals": totals,
		"series": series, "seriesBy": seriesBy}

	names := s.usageNames(r)
	for _, dim := range []string{"model", "provider", "user", "key", "account"} {
		buckets, err := s.store.UsageBreakdown(ctx, f, dim, tz)
		if err != nil {
			writeStoreError(w, r, err)
			return
		}
		out := make([]namedBucket, 0, len(buckets))
		for _, b := range buckets {
			name := b.Key
			if m, ok := names[dim]; ok {
				if n, ok := m[b.Key]; ok {
					name = n
				} else if b.Key == "0" || b.Key == "k0" {
					name = "—"
				} else if id, ok := strings.CutPrefix(b.Key, "k"); ok {
					name = "deleted key #" + id
				} else {
					name = "deleted #" + b.Key
				}
			}
			out = append(out, namedBucket{UsageBucket: b, Name: name})
		}
		resp["by"+capitalize(dim)] = out
	}
	resp["names"] = names
	writeJSON(w, http.StatusOK, resp)
}

// usageNames labels the ids that show up in usage and audit records. Requests made with admin
// keys have no user, so in the user dimension each such key stands on its own as "k<id>".
func (s *Server) usageNames(r *http.Request) map[string]map[string]string {
	ctx := r.Context()
	names := map[string]map[string]string{"user": {}, "key": {}, "account": {}}
	if users, err := s.store.ListUsers(ctx); err == nil {
		for _, u := range users {
			names["user"][strconv.FormatInt(u.ID, 10)] = u.Name
		}
	}
	if keys, err := s.store.ListAllKeys(ctx); err == nil {
		for _, k := range keys {
			label := k.Prefix + "…"
			if k.Name != "" {
				label = k.Name
			}
			id := strconv.FormatInt(k.ID, 10)
			names["key"][id] = label + " (" + k.OwnerName + ")"
			if k.OwnerType == "admin" {
				names["user"]["k"+id] = label + " (" + k.OwnerName + ", admin)"
			}
		}
	}
	for _, a := range s.accounts.List() {
		names["account"][strconv.FormatInt(a.ID, 10)] = a.Label
	}
	return names
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
