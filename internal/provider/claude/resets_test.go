package claude

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
)

// cedar_ember block as returned to claude-cli 2.1.280+ (Oct 2026), plus a used-up and a paused grant.
const cedarSample = `{"eligible":true,"ineligible_reason":null,"at_limit":false,"exhausted":[],
"grants":[
 {"id":"opus_launch","label":"Claude Opus 5.5 launch","resets_total":1,"resets_left":1,"starts_at":"2026-09-22T16:00:00Z","ends_at":"2026-10-22T23:59:59Z","clears":["five_hour","seven_day"],"paused":false,"usable_now":true,"use_requires_limit":false,"percent_used":{"five_hour":10,"seven_day":40},"blocking":[],"arm":null},
 {"id":"spent","label":"Old","resets_total":1,"resets_left":0,"ends_at":"2026-12-01T00:00:00Z","paused":false},
 {"id":"held","label":"Paused","resets_total":1,"resets_left":1,"ends_at":"2026-12-01T00:00:00Z","paused":true},
 {"id":"later","label":"Later","resets_total":2,"resets_left":2,"ends_at":"2026-12-01T00:00:00Z","clears":["seven_day"],"paused":false,"use_requires_limit":true}
],
"next_grant_id":"later","weekly_resets_at":"2026-10-15T18:00:00Z","cooldown_until":null}`

func TestBankedFromUsage(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	b := bankedFromUsage(json.RawMessage(cedarSample), now)
	if b == nil || b.Available != 3 || len(b.Credits) != 2 {
		t.Fatalf("banked = %+v", b)
	}
	next, first := b.Credits[0], b.Credits[1]
	if next.ID != "later" || next.Count != 2 || !next.RequiresLimit || len(next.Clears) != 1 || next.Clears[0] != "Weekly" {
		t.Errorf("next grant = %+v", next)
	}
	if first.ID != "opus_launch" || first.Title != "Claude Opus 5.5 launch" || first.ExpiresAt == nil ||
		first.GrantedAt == nil || len(first.Clears) != 2 || first.Clears[0] != "5-hour" {
		t.Errorf("launch grant = %+v", first)
	}

	if b := bankedFromUsage(json.RawMessage(`{"eligible":false,"ineligible_reason":"surface"}`), now); b != nil {
		t.Errorf("ineligible block read as %+v", b)
	}
	if b := bankedFromUsage(nil, now); b != nil {
		t.Errorf("missing block read as %+v", b)
	}
	if b := bankedFromUsage(json.RawMessage(`{"eligible":true,"grants":[]}`), now); b == nil || b.Available != 0 {
		t.Errorf("eligible with no grants = %+v, want zero", b)
	}
	if b := bankedFromUsage(json.RawMessage(cedarSample), time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)); b.Available != 0 {
		t.Errorf("expired grants counted: %+v", b)
	}
}

func TestRedeem(t *testing.T) {
	var claimed map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/oauth/usage":
			if r.URL.Query().Get("cedar_ember") != "1" || r.Header.Get("User-Agent") != userAgent {
				t.Errorf("usage request %s ua=%q", r.URL, r.Header.Get("User-Agent"))
			}
			io.WriteString(w, `{"cedar_ember":`+cedarSample+`}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/organizations/org-1/reset_rate_limits":
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("auth = %q", r.Header.Get("Authorization"))
			}
			json.NewDecoder(r.Body).Decode(&claimed)
			io.WriteString(w, `{"result":"not_limited"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := New(srv.Client(), false)
	p.APIBase = srv.URL
	creds := provider.Credentials{AccessToken: "tok", OrgUUID: "org-1"}

	res, err := p.Redeem(t.Context(), creds, "req-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != "nothing_to_reset" {
		t.Errorf("code = %q", res.Code)
	}
	if claimed["program"] != "cedar_ember" || claimed["grant_id"] != "later" || claimed["request_id"] != "req-1" {
		t.Errorf("claim body = %v", claimed)
	}

	if _, err := p.Redeem(t.Context(), provider.Credentials{AccessToken: "tok"}, "req-2", "x"); err == nil {
		t.Error("redeem without an organization should fail")
	}
}
