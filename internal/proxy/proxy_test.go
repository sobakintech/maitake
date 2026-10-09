package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
	"github.com/sobakintech/maitake/internal/usage"
)

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		name := gjson.Get(e, "type").String()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, e)
	}
}

type fakeUpstream struct {
	mu          sync.Mutex
	claudeBody  []byte
	claudeHdr   http.Header
	codexBody   []byte
	codexBodies [][]byte
	compactBody []byte
	claudeCalls map[string]int
	claudeSeen  [][]byte
}

func (f *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.URL.Path == "/v1/messages/count_tokens":
		w.Write([]byte(`{"input_tokens":42}`))
	case r.URL.Path == "/codex/models":
		w.Write([]byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":13,"base_instructions":"You are Codex, based on GPT-5.","upgrade":{"model":"x"}}]}`))
	case r.URL.Path == "/codex/alpha/search":
		w.Write([]byte(`{"results":[],"echo":` + string(body) + `}`))
	case strings.HasPrefix(r.URL.Path, "/v1/messages"):
		f.mu.Lock()
		f.claudeCalls[token]++
		f.claudeSeen = append(f.claudeSeen, body)
		f.mu.Unlock()
		switch {
		case strings.Contains(string(body), "TOO_LONG"):
			w.WriteHeader(400)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 300000 tokens > 200000 maximum"}}`))
			return
		case strings.Contains(string(body), `"signature":"foreign-sig"`):
			w.WriteHeader(400)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid ` + "`signature`" + ` in ` + "`thinking`" + ` block"}}`))
			return
		case token == "tok-oi":
			w.Header().Set("anthropic-ratelimit-unified-5h-status", "allowed")
			w.Header().Set("anthropic-ratelimit-unified-7d-status", "allowed")
			w.Header().Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")
			w.Header().Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(429)
			w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"model limit"}}`))
			return
		case token == "tok-trunc":
			sse(w,
				`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`)
			return
		case token == "tok-overload":
			sse(w,
				`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
				`{"type":"ping"}`,
				`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
			return
		}
		if gjson.GetBytes(body, "tools.0.name").Exists() {
			f.mu.Lock()
			f.claudeBody, f.claudeHdr = body, r.Header.Clone()
			f.mu.Unlock()
			name := gjson.GetBytes(body, "tools.0.name").String()
			sse(w,
				`{"type":"message_start","message":{"id":"msg_t","type":"message","role":"assistant","content":[],"usage":{"input_tokens":3,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"`+name+`","input":{}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`)
			return
		}
		if token == "tok-limited" {
			w.Header().Set("anthropic-ratelimit-unified-5h-status", "rejected")
			w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
			w.Header().Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(429)
			w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"limit reached"}}`))
			return
		}
		f.mu.Lock()
		f.claudeBody, f.claudeHdr = body, r.Header.Clone()
		f.mu.Unlock()
		w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
		w.Header().Set("anthropic-ratelimit-unified-5h-status", "allowed")
		sse(w,
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`)
	case r.URL.Path == "/codex/responses/compact":
		f.mu.Lock()
		f.compactBody = body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_c","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"xyz"}],"usage":{"input_tokens":50,"output_tokens":5}}`))
	case r.URL.Path == "/codex/responses":
		f.mu.Lock()
		f.codexBody = body
		f.codexBodies = append(f.codexBodies, body)
		f.mu.Unlock()
		if gjson.GetBytes(body, "tool_choice.type").String() == "image_generation" {
			sse(w,
				`{"type":"response.created","response":{"id":"resp_img"}}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"aW1n","revised_prompt":"a cat, painted","output_format":"png","size":"1024x1024","quality":"high"}}`,
				`{"type":"response.completed","response":{"id":"resp_img","status":"completed","output":[],"tool_usage":{"image_gen":{"input_tokens":10,"output_tokens":4000}},"usage":{"input_tokens":30,"output_tokens":9}}}`)
			return
		}
		w.Header().Set("X-Codex-Primary-Used-Percent", "40")
		w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
		if gjson.GetBytes(body, "tools").Exists() {
			sse(w,
				`{"type":"response.created","response":{"id":"resp_2"}}`,
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"city\":"}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"\"Paris\"}"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}`,
				`{"type":"response.completed","response":{"id":"resp_2","status":"completed","usage":{"input_tokens":20,"output_tokens":7}}}`)
			return
		}
		sse(w,
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"item_id":"m1","delta":"Hi"}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"Hi"}]}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":4}}}}`)
	default:
		w.WriteHeader(404)
	}
}

type env struct {
	t        *testing.T
	base     string
	key      string
	store    *store.Store
	mgr      *accounts.Manager
	upstream *fakeUpstream
	accts    map[string]int64
	stopRec  func()
}

type acctSpec struct {
	provider, token string
	priority        int
}

type setupOpts struct {
	allowed  []string
	accounts []acctSpec
	routing  func(*routing.Settings)
	pool     func(*accounts.Settings)
	cloak    *claude.CloakSettings
}

func setup(t *testing.T, allowed []string) *env {
	return setupWith(t, setupOpts{allowed: allowed})
}

func setupWith(t *testing.T, o setupOpts) *env {
	allowed := o.allowed
	if o.accounts == nil {
		o.accounts = []acctSpec{{"claude", "tok-limited", 10}, {"claude", "tok-ok", 0}, {"codex", "tok-codex", 0}}
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	up := &fakeUpstream{claudeCalls: map[string]int{}}
	upSrv := httptest.NewServer(up)
	t.Cleanup(upSrv.Close)
	cl := claude.New(upSrv.Client(), false)
	cl.APIBase = upSrv.URL
	cx := codex.New(upSrv.Client())
	cx.BackendBase = upSrv.URL

	accts := map[string]int64{}
	for _, a := range o.accounts {
		creds := provider.Credentials{AccessToken: a.token, RefreshToken: "r", ExpiresAt: time.Now().Add(24 * time.Hour),
			DeviceID: strings.Repeat("ab", 32), AccountUUID: "uuid-" + a.token, AccountID: "acct"}
		acct, err := st.UpsertAccount(ctx, &store.Account{Provider: a.provider, Label: a.token, ExternalID: a.token,
			Credentials: creds.Encode()})
		if err != nil {
			t.Fatal(err)
		}
		p := a.priority
		if _, err := st.UpdateAccount(ctx, acct.ID, store.AccountPatch{Priority: &p}); err != nil {
			t.Fatal(err)
		}
		accts[a.token] = acct.ID
	}
	pool := func() accounts.Settings {
		s := accounts.DefaultSettings()
		if o.pool != nil {
			o.pool(&s)
		}
		return s
	}
	if o.cloak != nil {
		c := *o.cloak
		cl.Cloak = func() claude.CloakSettings { return c }
	}
	mgr := accounts.New(st, cx, pool, cl, cx)
	if err := mgr.Load(ctx); err != nil {
		t.Fatal(err)
	}

	user, _ := st.CreateUser(ctx, "alice", "")
	key, prefix, hash := secret.NewAPIKey()
	k, err := st.CreateKey(ctx, user.ID, "test", prefix, hash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if allowed != nil {
		st.UpdateKey(ctx, k.ID, store.KeyPatch{AllowedModels: &allowed})
	}

	rec := usage.NewRecorder(st)
	recCtx, cancel := context.WithCancel(context.Background())
	go rec.Run(recCtx)

	r := chi.NewRouter()
	rs := func() routing.Settings {
		s := routing.DefaultSettings()
		s.Mappings = []routing.Rule{{Enabled: true, Match: "claude-haiku-*", Target: "gpt-5.5(low)"}}
		if o.routing != nil {
			o.routing(&s)
		}
		s.Normalize()
		return s
	}
	h := New(st, mgr, cl, cx, rec, pool, rs)
	h.Mount(r)
	r.Route("/api", h.MountOpenRouter)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &env{t: t, base: srv.URL, key: key, store: st, mgr: mgr, upstream: up, accts: accts,
		stopRec: func() { cancel(); rec.Wait() }}
}

func (e *env) post(path, body string) (int, string, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestClaudeNativeStreamWithFailover(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/messages", `{"model":"claude-opus-5","max_tokens":100,"stream":true,
		"system":"Be terse.","messages":[{"role":"user","content":"hi there friend, how are you doing today?"}]}`)
	if code != 200 || !strings.Contains(body, `"text":"Hello"`) || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("unexpected response %d: %s", code, body)
	}
	if e.upstream.claudeCalls["tok-limited"] != 1 || e.upstream.claudeCalls["tok-ok"] != 1 {
		t.Fatalf("expected failover from limited account, calls: %v", e.upstream.claudeCalls)
	}
	limited, _ := e.mgr.Get(e.accts["tok-limited"])
	if limited.Status != accounts.StatusCooldown || limited.CooldownUntil == nil || time.Until(*limited.CooldownUntil) < 50*time.Minute {
		t.Fatalf("limited account not cooled down: %+v", limited.Account)
	}

	up := gjson.ParseBytes(e.upstream.claudeBody)
	billing := up.Get("system.0.text").String()
	if !strings.HasPrefix(billing, "x-anthropic-billing-header: cc_version=2.1.280.") || strings.Contains(billing, "cch=00000") {
		t.Errorf("billing header not signed: %q", billing)
	}
	if up.Get("system.1.text").String() != "You are Claude Code, Anthropic's official CLI for Claude." {
		t.Errorf("identity line missing: %s", up.Get("system").Raw)
	}
	if !strings.Contains(up.Get("metadata.user_id").String(), `"device_id"`) {
		t.Errorf("metadata.user_id not set: %s", up.Get("metadata").Raw)
	}
	if up.Get("messages.1.role").String() != "system" || !strings.Contains(up.Get("messages.1.content.0.text").String(), "Be terse.") {
		t.Errorf("caller system prompt not relocated: %s", up.Get("messages").Raw)
	}
	if !strings.Contains(up.Get("messages.0.content.0.text").String(), "# currentDate") {
		t.Errorf("date reminder missing: %s", up.Get("messages.0").Raw)
	}
	if !strings.Contains(e.upstream.claudeHdr.Get("anthropic-beta"), "oauth-2025-04-20") {
		t.Errorf("betas missing: %s", e.upstream.claudeHdr.Get("anthropic-beta"))
	}
	ok, _ := e.mgr.Get(e.accts["tok-ok"])
	if ok.Quota == nil || ok.Quota.Window("5h") == nil || ok.Quota.Window("5h").UsedPercent != 25 {
		t.Errorf("quota not observed from headers: %+v", ok.Quota)
	}
}

func TestChatToClaudeNonStream(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/chat/completions", `{"model":"claude-opus-5","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	r := gjson.Parse(body)
	if r.Get("choices.0.message.content").String() != "Hello" || r.Get("choices.0.finish_reason").String() != "stop" {
		t.Fatalf("bad completion: %s", body)
	}
	if r.Get("usage.completion_tokens").Int() != 5 || r.Get("usage.prompt_tokens").Int() != 10 {
		t.Fatalf("bad usage: %s", r.Get("usage").Raw)
	}
	if gjson.GetBytes(e.upstream.claudeBody, "temperature").Exists() {
		t.Error("temperature should be stripped for translated requests")
	}
}

func TestChatToCodexStream(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/chat/completions", `{"model":"gpt-5.5(high)","stream":true,"stream_options":{"include_usage":true},
		"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if !strings.Contains(body, `"content":"Hi"`) || !strings.Contains(body, `"finish_reason":"stop"`) || !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("bad stream: %s", body)
	}
	if !strings.Contains(body, `"prompt_tokens":12`) {
		t.Errorf("usage chunk missing: %s", body)
	}
	up := gjson.ParseBytes(e.upstream.codexBody)
	if up.Get("model").String() != "gpt-5.5" || up.Get("reasoning.effort").String() != "high" ||
		!up.Get("stream").Bool() || up.Get("store").Bool() || up.Get("input.0.role").String() != "developer" {
		t.Fatalf("bad codex body: %s", up.Raw)
	}
}

func TestClaudeClientOnCodexToolCall(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/messages", `{"model":"gpt-5.5","max_tokens":100,"stream":true,
		"tools":[{"name":"get_weather","description":"w","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
		"messages":[{"role":"user","content":"weather in paris?"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	for _, want := range []string{`"type":"tool_use"`, `"name":"get_weather"`, `"partial_json":"{\"city\":"`, `"stop_reason":"tool_use"`, "event: message_stop"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in stream: %s", want, body)
		}
	}
}

func TestModelAllowList(t *testing.T) {
	e := setup(t, []string{"claude-*"})
	code, body, _ := e.post("/v1/chat/completions", `{"model":"gpt-5.5","messages":[{"role":"user","content":"x"}]}`)
	if code != 403 || gjson.Get(body, "error.type").String() != "permission_error" {
		t.Fatalf("expected 403, got %d: %s", code, body)
	}
}

func TestUsageRecorded(t *testing.T) {
	e := setup(t, nil)
	e.post("/v1/chat/completions", `{"model":"claude-opus-5","messages":[{"role":"user","content":"hello"}]}`)
	e.post("/v1/chat/completions", `{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}]}`)
	e.stopRec()
	totals, err := e.store.UsageTotals(context.Background(), store.UsageFilter{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Requests != 2 || totals.OutputTokens != 8 || totals.CacheReadTokens != 4 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
}

func (e *env) get(path string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestOpenRouterStyle(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/api/v1/chat/completions", `{"model":"anthropic/claude-opus-5","reasoning":{"effort":"high"},
		"usage":{"include":true},"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || gjson.Get(body, "choices.0.message.content").String() != "Hello" {
		t.Fatalf("openrouter chat: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.claudeBody)
	if up.Get("model").String() != "claude-opus-5" || up.Get("output_config.effort").String() != "high" {
		t.Fatalf("reasoning object not applied upstream: %s", up.Raw)
	}

	code, body, _ = e.post("/v1/chat/completions", `{"model":"gpt-5.5","provider":{"only":["anthropic"]},
		"messages":[{"role":"user","content":"hi"}]}`)
	if code != 404 {
		t.Fatalf("provider.only should exclude codex: %d %s", code, body)
	}

	code, body = e.get("/api/v1/models")
	if code != 200 || !strings.Contains(body, `"id":"anthropic/claude-opus-5"`) || !strings.Contains(body, `"context_length"`) {
		t.Fatalf("openrouter model list: %d %s", code, body)
	}
	if _, body = e.get("/v1/models"); !strings.Contains(body, `"id":"claude-opus-5"`) {
		t.Fatalf("default model list should use bare ids: %s", body)
	}
}

func TestModelMapping(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/messages", `{"model":"claude-haiku-4-5-20251001","max_tokens":50,
		"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("mapped request: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.codexBody)
	if up.Get("model").String() != "gpt-5.5" || up.Get("reasoning.effort").String() != "low" {
		t.Fatalf("mapping not applied: %s", up.Raw)
	}
	if gjson.Get(body, "model").String() != "claude-haiku-4-5-20251001" {
		t.Errorf("client should see the model it asked for: %s", body)
	}
	e.stopRec()
	recent, _ := e.store.RecentUsage(context.Background(), store.UsageFilter{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Minute)}, 5)
	if len(recent) != 1 || recent[0].Model != "gpt-5.5" || recent[0].RequestedModel != "claude-haiku-4-5-20251001" {
		t.Fatalf("usage should record requested and served model: %+v", recent)
	}
}

func (e *env) postWith(path, body string, headers map[string]string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.key)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

const hello = `{"model":"claude-opus-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}]}`

func TestInStreamErrorFailsOver(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-overload", 10}, {"claude", "tok-ok", 0}}})
	code, body := e.postWith("/v1/messages", hello, nil)
	if code != 200 || !strings.Contains(body, `"text":"Hello"`) || strings.Contains(body, "overloaded") {
		t.Fatalf("expected failover past the in-stream overload: %d %s", code, body)
	}
	if e.upstream.claudeCalls["tok-overload"] != 1 || e.upstream.claudeCalls["tok-ok"] != 1 {
		t.Fatalf("calls: %v", e.upstream.claudeCalls)
	}
}

func TestSignatureErrorRetriesWithoutThinking(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-ok", 0}}})
	code, body := e.postWith("/v1/messages", `{"model":"claude-opus-5","max_tokens":50,"stream":true,
		"messages":[{"role":"user","content":"hi"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"foreign-sig"},{"type":"text","text":"yo"}]},
		{"role":"user","content":"again"}]}`, nil)
	if code != 200 || !strings.Contains(body, `"text":"Hello"`) {
		t.Fatalf("expected success after stripping thinking: %d %s", code, body)
	}
	if n := e.upstream.claudeCalls["tok-ok"]; n != 2 {
		t.Fatalf("expected one retry on the same account, got %d calls", n)
	}
	last := e.upstream.claudeSeen[len(e.upstream.claudeSeen)-1]
	if strings.Contains(string(last), "foreign-sig") {
		t.Fatal("retry still carried the rejected thinking block")
	}
}

func TestErrorRuleStopsWithoutRetry(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-a", 1}, {"claude", "tok-b", 0}}})
	code, body := e.postWith("/v1/messages", `{"model":"claude-opus-5","max_tokens":50,"messages":[{"role":"user","content":"TOO_LONG"}]}`, nil)
	if code != 400 || !strings.Contains(body, "prompt is too long") {
		t.Fatalf("expected the 400 to pass through: %d %s", code, body)
	}
	if e.upstream.claudeCalls["tok-a"]+e.upstream.claudeCalls["tok-b"] != 1 {
		t.Fatalf("client errors must not be retried: %v", e.upstream.claudeCalls)
	}
	a, _ := e.mgr.Get(e.accts["tok-a"])
	if a.Status != accounts.StatusOK {
		t.Fatalf("account should not be benched for a client error: %s", a.Status)
	}
}

func TestModelScopedCooldown(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-oi", 10}, {"claude", "tok-ok", 0}}})
	e.postWith("/v1/messages", hello, nil)
	oi, _ := e.mgr.Get(e.accts["tok-oi"])
	if oi.Status != accounts.StatusOK || oi.ModelCooldowns["claude-opus-5"].IsZero() {
		t.Fatalf("expected only claude-opus-5 to cool down: status=%s cooldowns=%v", oi.Status, oi.ModelCooldowns)
	}
	before := e.upstream.claudeCalls["tok-oi"]
	e.postWith("/v1/messages", strings.Replace(strings.Replace(hello, "claude-opus-5", "claude-sonnet-5", 1), `"hi"`, `"different"`, 1), nil)
	if e.upstream.claudeCalls["tok-oi"] != before+1 {
		t.Fatalf("other models should still use the account: %v", e.upstream.claudeCalls)
	}
}

func TestSessionAffinity(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-a", 0}, {"claude", "tok-b", 0}}})
	session := map[string]string{"X-Claude-Code-Session-Id": "11111111-2222-3333-4444-555555555555"}
	for i := 0; i < 4; i++ {
		e.postWith("/v1/messages", hello, session)
	}
	a, b := e.upstream.claudeCalls["tok-a"], e.upstream.claudeCalls["tok-b"]
	if !(a == 4 && b == 0 || a == 0 && b == 4) {
		t.Fatalf("one conversation should stay on one account: a=%d b=%d", a, b)
	}
	e.postWith("/v1/messages", hello, map[string]string{"X-Claude-Code-Session-Id": "other"})
	e.postWith("/v1/messages", hello, map[string]string{"X-Claude-Code-Session-Id": "third"})
	if e.upstream.claudeCalls["tok-a"] == 0 || e.upstream.claudeCalls["tok-b"] == 0 {
		t.Fatalf("different conversations should still spread: %v", e.upstream.claudeCalls)
	}
}

func TestPayloadRules(t *testing.T) {
	e := setupWith(t, setupOpts{routing: func(s *routing.Settings) {
		s.PayloadRules = []routing.PayloadRule{
			{Enabled: true, Provider: "codex", Mode: routing.PayloadOverride, Path: "service_tier", Value: []byte(`"priority"`)},
			{Enabled: true, Match: "gpt-*", Mode: routing.PayloadRemove, Path: "parallel_tool_calls"},
		}
	}})
	e.post("/v1/chat/completions", `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}]}`)
	up := gjson.ParseBytes(e.upstream.codexBody)
	if up.Get("service_tier").String() != "priority" || up.Get("parallel_tool_calls").Exists() {
		t.Fatalf("payload rules not applied: %s", up.Raw)
	}
}

func TestToolAliasRoundTrip(t *testing.T) {
	cloak := claude.DefaultCloak()
	cloak.TLSFingerprint = false
	cloak.SensitiveWords = []string{"maitake"}
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-ok", 0}}, cloak: &cloak})
	code, body := e.postWith("/v1/messages", `{"model":"claude-opus-5","max_tokens":50,"stream":true,
		"tools":[{"name":"get_weather","description":"w","input_schema":{"type":"object"}}],
		"messages":[{"role":"user","content":"ask maitake about the weather"}]}`, nil)
	if code != 200 || !strings.Contains(body, `"name":"get_weather"`) {
		t.Fatalf("client should see its own tool name: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.claudeBody)
	if name := up.Get("tools.0.name").String(); !strings.HasPrefix(name, "mcp__") || !strings.HasSuffix(name, "get_weather") {
		t.Fatalf("upstream tool should be aliased: %q", name)
	}
	if strings.Contains(up.Raw, "ask maitake") {
		t.Fatal("sensitive word not obfuscated")
	}
	if !strings.Contains(e.upstream.claudeHdr.Get("anthropic-beta"), "extended-cache-ttl") || !strings.Contains(up.Raw, `"ttl":"1h"`) {
		t.Fatal("expected 1h cache markers with the extended TTL beta")
	}
}

func TestCompact(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/responses/compact", `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"hi"}]}`)
	if code != 200 || gjson.Get(body, "output.0.type").String() != "compaction" {
		t.Fatalf("compact: %d %s", code, body)
	}
	if gjson.GetBytes(e.upstream.compactBody, "stream").Exists() {
		t.Fatalf("compact must not stream upstream: %s", e.upstream.compactBody)
	}
	code, body, _ = e.post("/backend-api/codex/responses/compact", `{"model":"gpt-5.5","stream":true,"input":[]}`)
	if code != 400 || !strings.Contains(body, "Streaming not supported") {
		t.Fatalf("stream:true should be rejected: %d %s", code, body)
	}
	code, body, _ = e.post("/v1/responses/compact", `{"model":"claude-opus-5","input":[]}`)
	if code != 400 {
		t.Fatalf("claude models can't compact: %d %s", code, body)
	}
}

func TestCodexNativePathAndQueryKey(t *testing.T) {
	e := setup(t, nil)
	req, _ := http.NewRequest("POST", e.base+"/backend-api/codex/responses?key="+e.key,
		strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":"hi"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "response.completed") {
		t.Fatalf("native path: %d %s", resp.StatusCode, b)
	}
}

func TestImages(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/images/generations", `{"model":"gpt-image-2","prompt":"a cat","size":"1024x1024","quality":"high","output_compression":80}`)
	if code != 200 || gjson.Get(body, "data.0.b64_json").String() != "aW1n" || gjson.Get(body, "data.0.revised_prompt").String() == "" ||
		gjson.Get(body, "usage.output_tokens").Int() != 4000 {
		t.Fatalf("images: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.codexBody)
	tool := up.Get("tools.0")
	if up.Get("model").String() != "gpt-5.4-mini" || tool.Get("action").String() != "generate" || tool.Get("model").String() != "gpt-image-2" ||
		tool.Get("size").String() != "1024x1024" || tool.Get("output_compression").Int() != 80 {
		t.Fatalf("bad upstream image request: %s", up.Raw)
	}

	var form strings.Builder
	mw := multipart.NewWriter(&form)
	mw.WriteField("prompt", "make it blue")
	mw.WriteField("response_format", "url")
	fw, _ := mw.CreateFormFile("image[]", "a.png")
	fw.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	mw.Close()
	req, _ := http.NewRequest("POST", e.base+"/v1/images/edits", strings.NewReader(form.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(gjson.GetBytes(b, "data.0.url").String(), "data:image/png;base64,") {
		t.Fatalf("edits: %d %s", resp.StatusCode, b)
	}
	up = gjson.ParseBytes(e.upstream.codexBody)
	if up.Get("tools.0.action").String() != "edit" || !strings.HasPrefix(up.Get("input.0.content.1.image_url").String(), "data:image/png;base64,") {
		t.Fatalf("bad edit request: %s", up.Raw)
	}
}

func TestAutoImageTool(t *testing.T) {
	e := setupWith(t, setupOpts{routing: func(s *routing.Settings) { s.Images.AutoTool = true }})
	e.post("/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"hi","tools":[{"type":"function","name":"f","parameters":{}}]}`)
	if gjson.GetBytes(e.upstream.codexBody, `tools.#(type=="image_generation")`).Exists() == false {
		t.Fatalf("image tool not added: %s", e.upstream.codexBody)
	}
	e.post("/v1/chat/completions", `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}]}`)
	if gjson.GetBytes(e.upstream.codexBody, `tools.#(type=="image_generation")`).Exists() {
		t.Fatal("image tool must only be added for Responses clients")
	}
}

func TestResponsesWebsocket(t *testing.T) {
	e := setup(t, nil)
	url := "ws" + strings.TrimPrefix(e.base, "http") + "/v1/responses"
	hdr := http.Header{"Authorization": {"Bearer " + e.key}}
	conn, _, err := websocket.DefaultDialer.Dial(url, hdr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	read := func() []string {
		var types []string
		for {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read: %v (got %v)", err, types)
			}
			typ := gjson.GetBytes(msg, "type").String()
			types = append(types, typ)
			if typ == "response.completed" || typ == "error" {
				return types
			}
		}
	}
	send := func(v string) {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}

	send(`{"type":"response.create","model":"gpt-5.5","generate":false,"instructions":"be nice","input":[{"type":"message","role":"user","content":"hi"}]}`)
	if got := read(); len(got) != 2 || got[0] != "response.created" {
		t.Fatalf("prewarm: %v", got)
	}
	if len(e.upstream.codexBodies) != 0 {
		t.Fatal("prewarm must not reach upstream")
	}
	send(`{"type":"response.create","input":[{"type":"message","role":"user","content":"second"}]}`)
	if got := read(); got[len(got)-1] != "response.completed" {
		t.Fatalf("turn 1: %v", got)
	}
	first := gjson.ParseBytes(e.upstream.codexBodies[0])
	if first.Get("model").String() != "gpt-5.5" || first.Get("instructions").String() != "be nice" || len(first.Get("input").Array()) != 2 {
		t.Fatalf("turn 1 should merge the prewarm input and inherit fields: %s", first.Raw)
	}
	send(`{"type":"response.create","previous_response_id":"resp_1","input":[{"type":"message","role":"user","content":"third"}]}`)
	read()
	second := gjson.ParseBytes(e.upstream.codexBodies[1])
	if n := len(second.Get("input").Array()); n != 4 || second.Get("input.2.role").String() != "assistant" {
		t.Fatalf("turn 2 should carry history + last output + new input: %s", second.Raw)
	}
	send(`{"type":"response.create","previous_response_id":"resp_nope","input":[]}`)
	if got := read(); got[len(got)-1] != "error" {
		t.Fatalf("unknown previous_response_id should error: %v", got)
	}
}

func TestChatToClaudeToolNamesAndThinking(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-ok", 0}}})
	code, body := e.postWith("/v1/chat/completions", `{"model":"claude-opus-5","stream":true,"reasoning_effort":"xhigh",
		"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],
		"tools":[{"type":"function","function":{"name":"mcp.files.read","parameters":{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}}]}}}]}`, nil)
	if code != 200 || !strings.Contains(body, `"name":"mcp.files.read"`) {
		t.Fatalf("client should get its dotted tool name back: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.claudeBody)
	if up.Get("tools.0.name").String() != "mcp_files_read" || up.Get("tools.0.input_schema.type").String() != "object" ||
		!up.Get("tools.0.input_schema.properties.a").Exists() {
		t.Fatalf("tool not made Claude-safe: %s", up.Get("tools").Raw)
	}
	if up.Get("thinking.display").String() != "summarized" || up.Get("output_config.effort").String() != "xhigh" {
		t.Fatalf("expected summarized thinking at xhigh: %s %s", up.Get("thinking").Raw, up.Get("output_config").Raw)
	}
	if !strings.Contains(up.Get("messages").Raw, `"cache_control"`) {
		t.Fatal("cache_control should pass through")
	}
}

func TestTruncatedStreamIsReported(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-trunc", 0}}})
	code, body := e.postWith("/v1/chat/completions", `{"model":"claude-opus-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if code != 200 || !strings.Contains(body, "stream_truncated") {
		t.Fatalf("expected a truncation error chunk: %d %s", code, body)
	}
	code, body = e.postWith("/v1/messages", `{"model":"claude-opus-5","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if code != 502 {
		t.Fatalf("non-streamed truncated answer should fail: %d %s", code, body)
	}
}

func TestTokenAndEffortLimits(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-ok", 0}, {"codex", "tok-codex", 0}}})
	e.postWith("/v1/chat/completions", `{"model":"claude-3-5-haiku-20241022","stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if n := gjson.GetBytes(e.upstream.claudeSeen[len(e.upstream.claudeSeen)-1], "max_tokens").Int(); n != 8192 {
		t.Fatalf("max_tokens should be capped at the model limit, got %d", n)
	}
	e.postWith("/v1/messages", `{"model":"claude-sonnet-4-5-20250929(max)","max_tokens":1000,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil)
	last := gjson.ParseBytes(e.upstream.claudeSeen[len(e.upstream.claudeSeen)-1])
	if last.Get("max_tokens").Int() != 64000 || last.Get("thinking.budget_tokens").Int() >= 64000 {
		t.Fatalf("budget must fit under the model's output limit: %s %s", last.Get("max_tokens").Raw, last.Get("thinking").Raw)
	}
	e.post("/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"hi","reasoning":{"effort":"max"}}`)
	if eff := gjson.GetBytes(e.upstream.codexBody, "reasoning.effort").String(); eff != "xhigh" {
		t.Fatalf("gpt-5.5 has no max effort, got %q", eff)
	}
}

func TestCodexSanitize(t *testing.T) {
	e := setup(t, nil)
	e.post("/v1/responses", `{"model":"gpt-5.5","stream":true,
		"input":[{"type":"message","id":"abc","role":"user","content":[{"type":"input_text","text":"hi","prompt_cache_breakpoint":true}]}],
		"tool_choice":{"type":"web_search_preview"},
		"tools":[{"type":"function","name":"f","parameters":{"type":"object","properties":{"s":{"type":"string","pattern":"^\\p{L}+$"}}}}]}`)
	up := gjson.ParseBytes(e.upstream.codexBody)
	if up.Get("input.0.id").String() != "msg_abc" || strings.Contains(up.Raw, "prompt_cache_breakpoint") ||
		up.Get("tool_choice.type").String() != "web_search" || up.Get("tools.0.parameters.properties.s.pattern").Exists() {
		t.Fatalf("codex body not sanitized: %s", up.Raw)
	}
}

func TestResponsesBodyOnChatEndpoint(t *testing.T) {
	e := setup(t, nil)
	code, body, _ := e.post("/v1/chat/completions", `{"model":"gpt-5.5","instructions":"be brief","input":"hi"}`)
	if code != 200 || gjson.Get(body, "choices.0.message.content").String() != "Hi" {
		t.Fatalf("responses-shaped chat body: %d %s", code, body)
	}
}

func TestResponsesAPIOnClaude(t *testing.T) {
	e := setupWith(t, setupOpts{accounts: []acctSpec{{"claude", "tok-ok", 0}}})
	sig, _ := json.Marshal(map[string]string{"t": "thinking it over", "s": "sig-123"})
	enc := "mtk-claude:" + base64.RawURLEncoding.EncodeToString(sig)
	code, body := e.postWith("/v1/responses", `{"model":"claude-opus-5","stream":true,"instructions":"be nice",
		"reasoning":{"effort":"high","summary":"auto"},
		"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{}}},
			{"type":"custom","name":"apply_patch","description":"patch files"}],
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]},
			{"type":"reasoning","summary":[],"encrypted_content":"`+enc+`"},
			{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}]}`, nil)
	if code != 200 || !strings.Contains(body, "event: response.completed") ||
		!strings.Contains(body, `"type":"function_call"`) || !strings.Contains(body, `"name":"get_weather"`) {
		t.Fatalf("responses stream from claude: %d %s", code, body)
	}
	up := gjson.ParseBytes(e.upstream.claudeBody)
	asst := up.Get(`messages.#(role=="assistant")`)
	if asst.Get("content.0.type").String() != "thinking" || asst.Get("content.0.signature").String() != "sig-123" ||
		asst.Get("content.1.type").String() != "tool_use" || !strings.Contains(up.Get("messages").Raw, `"tool_result"`) {
		t.Fatalf("history not translated: %s", up.Get("messages").Raw)
	}
	if up.Get("thinking.display").String() != "summarized" || up.Get("tools.1.input_schema.properties.input.type").String() != "string" {
		t.Fatalf("thinking/tools not translated: %s %s", up.Get("thinking").Raw, up.Get("tools").Raw)
	}

	code, body = e.postWith("/v1/responses", `{"model":"claude-opus-5","input":"hi"}`, nil)
	if code != 200 || gjson.Get(body, "object").String() != "response" || gjson.Get(body, "output.0.content.0.text").String() != "Hello" ||
		gjson.Get(body, "usage.output_tokens").Int() == 0 {
		t.Fatalf("non-streamed responses from claude: %d %s", code, body)
	}
}

func TestTokenCounting(t *testing.T) {
	e := setup(t, nil)
	code, body := e.postWith("/v1/messages/count_tokens", `{"model":"gpt-5.5","messages":[{"role":"user","content":"hello there, how are you today?"}]}`, nil)
	if n := gjson.Get(body, "input_tokens").Int(); code != 200 || n < 5 || n > 20 {
		t.Fatalf("tiktoken count: %d %s", code, body)
	}
	code, body = e.postWith("/v1/responses/input_tokens", `{"model":"claude-opus-5","input":"hello"}`, nil)
	if code != 200 || gjson.Get(body, "input_tokens").Int() != 42 {
		t.Fatalf("claude input_tokens: %d %s", code, body)
	}
}

func TestCodexClientCatalogAndSearch(t *testing.T) {
	e := setup(t, nil)
	code, body := e.get("/v1/models?client_version=0.200.0")
	if code != 200 {
		t.Fatalf("catalog: %d %s", code, body)
	}
	gpt := gjson.Get(body, `models.#(slug=="gpt-5.5")`)
	opus := gjson.Get(body, `models.#(slug=="claude-opus-5")`)
	if gpt.Get("priority").Int() != 13 || !opus.Exists() || opus.Get("upgrade").Exists() ||
		!strings.Contains(opus.Get("base_instructions").String(), "based on Claude") ||
		opus.Get("context_window").Int() != 1_000_000 {
		t.Fatalf("bad catalog: %s", body)
	}
	code, body, _ = e.post("/backend-api/codex/alpha/search", `{"id":"s1","query":"go","prompt_cache_key":"x"}`)
	if code != 200 || gjson.Get(body, "echo.query").String() != "go" || gjson.Get(body, "echo.prompt_cache_key").Exists() {
		t.Fatalf("search: %d %s", code, body)
	}
}

func TestUsageTierAndTTFT(t *testing.T) {
	e := setup(t, nil)
	e.post("/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"hi","service_tier":"priority"}`)
	e.stopRec()
	events, err := e.store.RecentUsage(context.Background(), store.UsageFilter{Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Minute)}, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("usage: %v %v", events, err)
	}
	if events[0].ServiceTier != "priority" {
		t.Fatalf("tier not recorded: %+v", events[0])
	}
}
