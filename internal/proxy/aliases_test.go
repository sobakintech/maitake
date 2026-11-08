package proxy

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

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
		if gjson.Get(body, "model").String() != "sol" {
			t.Errorf("client should see the name it asked for: %s", body)
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
