package proxy

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"

	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/store"
)

const (
	plusList = `{"models":[{"slug":"gpt-7-sol","visibility":"list"},{"slug":"gpt-6-sol","visibility":"list"},{"slug":"gpt-9-sol","visibility":"hide"}]}`
	freeList = `{"models":[{"slug":"gpt-6-luna","visibility":"list"}]}`
	opusList = `{"data":[{"id":"claude-opus-9","created_at":"2027-01-01T00:00:00Z","lifecycle":"active"},
		{"id":"claude-opus-10","created_at":"2027-06-01T00:00:00Z","lifecycle":"preview"}]}`
)

func aliasEnv(t *testing.T, edit func(*routing.Settings)) *env {
	e := setupWith(t, setupOpts{routing: edit, accounts: []acctSpec{
		{"codex", "tok-plus", 0}, {"codex", "tok-free", 0}, {"claude", "tok-ok", 0}}})
	e.upstream.lists = map[string]string{"tok-plus": plusList, "tok-free": freeList, "tok-ok": opusList}
	e.mgr.RefreshModelLists(context.Background(), 0)
	return e
}

func chat(model string) string {
	return `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
}

func TestAliasRoutesOnlyToAccountsListingTheModel(t *testing.T) {
	e := aliasEnv(t, nil)
	for range 4 {
		code, body, _ := e.post("/v1/chat/completions", chat("sol"))
		if code != 200 {
			t.Fatalf("sol: %d %s", code, body)
		}
		if gjson.Get(body, "model").String() != "gpt-7-sol" {
			t.Errorf("client should see the model that answered: %s", body)
		}
	}
	if m := gjson.GetBytes(e.upstream.codexBody, "model").String(); m != "gpt-7-sol" {
		t.Errorf("sol went to %s, the hidden gpt-9-sol must not count either", m)
	}
	if slices.Contains(e.upstream.codexTokens, "tok-free") {
		t.Errorf("free account can't use sol: %v", e.upstream.codexTokens)
	}

	e.upstream.codexTokens = nil
	if code, body, _ := e.post("/v1/chat/completions", chat("luna(low)")); code != 200 {
		t.Fatalf("luna: %d %s", code, body)
	}
	if !slices.Equal(e.upstream.codexTokens, []string{"tok-free"}) || gjson.GetBytes(e.upstream.codexBody, "model").String() != "gpt-6-luna" ||
		gjson.GetBytes(e.upstream.codexBody, "reasoning.effort").String() != "low" {
		t.Errorf("luna: tokens %v, body %s", e.upstream.codexTokens, e.upstream.codexBody)
	}

	if code, body, _ := e.post("/v1/messages", `{"model":"opus","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); code != 200 {
		t.Fatalf("opus: %d %s", code, body)
	}
	if m := gjson.GetBytes(e.upstream.claudeBody, "model").String(); m != "claude-opus-9" {
		t.Errorf("opus went to %s", m)
	}

	if code, body, _ := e.post("/v1/chat/completions", chat("astra")); code != 404 {
		t.Errorf("nobody lists astra: %d %s", code, body)
	}

	e.stopRec()
	recent, _ := e.store.RecentUsage(context.Background(), store.UsageFilter{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Minute)}, 20)
	found := false
	for _, u := range recent {
		found = found || (u.RequestedModel == "sol" && u.Model == "gpt-7-sol")
	}
	if !found {
		t.Errorf("usage should record the alias and the served model: %+v", recent)
	}
}

// reportedModels lists every distinct model name in a JSON body or SSE stream.
func reportedModels(body string) []string {
	var names []string
	for _, m := range regexp.MustCompile(`"model":"([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	return names
}

func TestReportedModelAcrossFormats(t *testing.T) {
	e := aliasEnv(t, func(s *routing.Settings) {
		s.Mappings = []routing.Rule{{Enabled: true, Match: "claude-haiku-*", Target: "gpt-5.5(low)"},
			{Enabled: true, Match: "claude-instant-*", Target: "luna"}}
	})
	endpoints := []struct{ name, path, shape string }{
		{"chat", "/v1/chat/completions", `"messages":[{"role":"user","content":"hi"}]`},
		{"openrouter", "/api/v1/chat/completions", `"messages":[{"role":"user","content":"hi"}]`},
		{"responses", "/v1/responses", `"input":"hi"`},
		{"messages", "/v1/messages", `"max_tokens":10,"messages":[{"role":"user","content":"hi"}]`},
		// Only chat speaks OpenRouter's names on that mount; these two match their /v1 twins.
		{"responses", "/api/v1/responses", `"input":"hi"`},
		{"messages", "/api/v1/messages", `"max_tokens":10,"messages":[{"role":"user","content":"hi"}]`},
	}
	// want maps an endpoint to what it should report; "*" covers the rest.
	cases := []struct {
		requested string
		want      map[string]string
	}{
		// Aliases report the model that answered, on Codex and on Claude, with or without an effort.
		{"sol", map[string]string{"*": "gpt-7-sol", "openrouter": "openai/gpt-7-sol"}},
		{"SOL", map[string]string{"*": "gpt-7-sol", "openrouter": "openai/gpt-7-sol"}},
		{"openai/sol", map[string]string{"*": "gpt-7-sol", "openrouter": "openai/gpt-7-sol"}},
		{"luna(low)", map[string]string{"*": "gpt-6-luna", "openrouter": "openai/gpt-6-luna"}},
		{"opus", map[string]string{"*": "claude-opus-9", "openrouter": "anthropic/claude-opus-9"}},
		{"opus(high)", map[string]string{"*": "claude-opus-9", "openrouter": "anthropic/claude-opus-9"}},
		// Naming the model yourself is unchanged. Responses on Codex has always shown upstream's name.
		{"gpt-6-luna", map[string]string{"*": "gpt-6-luna"}},
		{"claude-opus-9", map[string]string{"*": "claude-opus-9"}},
		{"gpt-6-luna(high)", map[string]string{"*": "gpt-6-luna(high)", "responses": "gpt-6-luna"}},
		{"openai/gpt-6-luna", map[string]string{"*": "openai/gpt-6-luna", "responses": "gpt-6-luna"}},
		// Mapping rules stay hidden, even when the target is an alias.
		{"claude-haiku-4-5-20251001", map[string]string{"*": "claude-haiku-4-5-20251001", "responses": "gpt-5.5"}},
		{"claude-instant-9", map[string]string{"*": "claude-instant-9", "responses": "gpt-6-luna"}},
	}
	for _, c := range cases {
		for _, ep := range endpoints {
			want, ok := c.want[ep.name]
			if !ok {
				want = c.want["*"]
			}
			for _, stream := range []string{"", `"stream":true,`} {
				code, body, _ := e.post(ep.path, `{"model":"`+c.requested+`",`+stream+ep.shape+`}`)
				if code != 200 {
					t.Fatalf("%s on %s: %d %s", c.requested, ep.path, code, body)
				}
				if got := reportedModels(body); !slices.Equal(got, []string{want}) {
					t.Errorf("%s on %s (stream %v): got %v, want %s", c.requested, ep.path, stream != "", got, want)
				}
			}
		}
	}
	// A native Claude stream reports upstream's name for an exact request with an effort.
	if _, body, _ := e.post("/v1/messages", `{"model":"claude-opus-9(high)","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); !slices.Equal(reportedModels(body), []string{"claude-opus-9"}) {
		t.Errorf("native stream: %s", body)
	}
	if _, body, _ := e.post("/v1/messages", `{"model":"claude-opus-9(high)","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); gjson.Get(body, "model").String() != "claude-opus-9(high)" {
		t.Errorf("native non-stream: %s", body)
	}
}

func TestNativeClaudeAliasKeepsUpstreamName(t *testing.T) {
	e := aliasEnv(t, nil)
	e.upstream.claudeServes = "claude-opus-9-20270101"
	for _, stream := range []string{"", `"stream":true,`} {
		_, body, _ := e.post("/v1/messages", `{"model":"opus",`+stream+`"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
		if !slices.Equal(reportedModels(body), []string{"claude-opus-9-20270101"}) {
			t.Errorf("stream %v: %s", stream != "", body)
		}
	}
	_, body, _ := e.post("/v1/messages", `{"model":"claude-opus-9","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	if gjson.Get(body, "model").String() != "claude-opus-9" {
		t.Errorf("naming the model yourself is unchanged: %s", body)
	}
}

func TestAliasReportedOverWebsocket(t *testing.T) {
	e := aliasEnv(t, nil)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(e.base, "http")+"/v1/responses",
		http.Header{"Authorization": {"Bearer " + e.key}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	turn := func(frame string) gjson.Result {
		t.Helper()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if d := gjson.ParseBytes(msg); d.Get("type").String() == "response.completed" {
				return d
			}
		}
	}
	input := `"input":[{"type":"message","role":"user","content":"hi"}]`
	first := turn(`{"type":"response.create","model":"opus",` + input + `}`)
	if m := first.Get("response.model").String(); m != "claude-opus-9" {
		t.Errorf("first turn: %s", first.Raw)
	}
	second := turn(`{"type":"response.create","previous_response_id":"` + first.Get("response.id").String() + `",` + input + `}`)
	if m := second.Get("response.model").String(); m != "claude-opus-9" {
		t.Errorf("second turn: %s", second.Raw)
	}
	// A follow-up turn inherits the alias the client sent, not the name it was told.
	e.stopRec()
	recent, _ := e.store.RecentUsage(context.Background(), store.UsageFilter{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Minute)}, 5)
	if len(recent) != 2 || recent[0].RequestedModel != "opus" || recent[1].RequestedModel != "opus" {
		t.Errorf("both turns should have asked for the alias: %+v", recent)
	}
}

func TestAliasMovesOnFromRefusedModel(t *testing.T) {
	e := aliasEnv(t, nil)
	e.upstream.refuse = "gpt-7-sol"
	code, body, _ := e.post("/v1/chat/completions", chat("sol"))
	if code != 400 || !strings.Contains(body, "not supported") {
		t.Fatalf("first request sees upstream's answer: %d %s", code, body)
	}
	acct, _ := e.mgr.Get(e.accts["tok-plus"])
	if acct.Status != "ok" || acct.ModelCooldowns["gpt-7-sol"].IsZero() {
		t.Fatalf("only the model should be benched: %+v %v", acct.Status, acct.ModelCooldowns)
	}
	if code, body, _ := e.post("/v1/chat/completions", chat("sol")); code != 200 {
		t.Fatalf("second request: %d %s", code, body)
	}
	if m := gjson.GetBytes(e.upstream.codexBody, "model").String(); m != "gpt-6-sol" {
		t.Errorf("should have moved on, got %s", m)
	}
}

func TestDirectModelRefusalIsNotBenched(t *testing.T) {
	e := aliasEnv(t, nil)
	e.upstream.refuse = "gpt-7-sol"
	if code, _, _ := e.post("/v1/chat/completions", chat("gpt-7-sol")); code != 400 {
		t.Fatalf("code = %d", code)
	}
	for _, token := range []string{"tok-plus", "tok-free"} {
		if acct, _ := e.mgr.Get(e.accts[token]); len(acct.ModelCooldowns) != 0 {
			t.Errorf("asking for a model by name shouldn't bench it: %v", acct.ModelCooldowns)
		}
	}
}

func TestAliasFallbackSetting(t *testing.T) {
	soon := time.Now().Add(time.Hour)
	e := aliasEnv(t, nil)
	e.mgr.MarkModelCooldown(e.accts["tok-plus"], "gpt-7-sol", soon)
	if code, body, _ := e.post("/v1/chat/completions", chat("sol")); code != 200 {
		t.Fatalf("fallback on: %d %s", code, body)
	}
	if m := gjson.GetBytes(e.upstream.codexBody, "model").String(); m != "gpt-6-sol" {
		t.Errorf("fallback on went to %s", m)
	}

	e = aliasEnv(t, func(s *routing.Settings) { s.Aliases.Fallback = false })
	e.mgr.MarkModelCooldown(e.accts["tok-plus"], "gpt-7-sol", soon)
	if code, body, _ := e.post("/v1/chat/completions", chat("sol")); code != 429 {
		t.Errorf("fallback off should report the limit: %d %s", code, body)
	}
}

func TestAliasesInModelLists(t *testing.T) {
	e := aliasEnv(t, nil)
	_, body := e.get("/v1/models")
	ids := []string{}
	for _, m := range gjson.Get(body, "data").Array() {
		ids = append(ids, m.Get("id").String())
	}
	for _, want := range []string{"sol", "luna", "opus"} {
		if !slices.Contains(ids, want) {
			t.Errorf("%s missing from %v", want, ids)
		}
	}
	if slices.Contains(ids, "astra") {
		t.Errorf("nobody lists astra: %v", ids)
	}
	if !strings.Contains(body, `"name":"Sol (latest)"`) {
		t.Errorf("alias name missing: %s", body)
	}
	if ids[0] == "sol" || slices.Index(ids, "sol") < slices.Index(ids, "gpt-5.5") {
		t.Errorf("aliases should come after real models: %v", ids)
	}

	_, body = e.get("/api/v1/models")
	if !strings.Contains(body, `"id":"openai/sol"`) {
		t.Errorf("openrouter names need the lab: %s", body)
	}

	_, body = e.get("/v1/models?client_version=0.154.0")
	var sol gjson.Result
	for _, m := range gjson.Get(body, "models").Array() {
		if m.Get("slug").String() == "sol" {
			sol = m
		}
	}
	if sol.Get("description").String() != "Currently GPT-7 Sol" || sol.Get("visibility").String() != "list" || sol.Get("priority").Int() <= 1000 {
		t.Errorf("codex client entry = %s", sol.Raw)
	}

	e = aliasEnv(t, func(s *routing.Settings) { s.Aliases.List = false })
	if _, body = e.get("/v1/models"); strings.Contains(body, `"id":"sol"`) {
		t.Errorf("listing is off: %s", body)
	}
	if code, body, _ := e.post("/v1/chat/completions", chat("sol")); code != 200 {
		t.Errorf("hidden aliases still work: %d %s", code, body)
	}
}
