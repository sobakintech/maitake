package codex

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/provider"
)

const (
	userAgent  = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.154.0)"
	originator = "codex-tui"
)

// Fields the ChatGPT Codex backend rejects or ignores.
var droppedFields = []string{
	"previous_response_id", "generate", "prompt_cache_retention", "safety_identifier",
	"max_output_tokens", "max_completion_tokens", "temperature", "top_p", "truncation",
	"prompt_cache_options", "user", "context_management", "metadata", "service_tier_options",
}

// PrepareResponses normalizes a Responses API request for the Codex backend.
func PrepareResponses(body []byte, baseModel, effort, cacheKey string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid JSON body")
	}
	var err error
	set := func(path string, v any) {
		if err == nil {
			body, err = sjson.SetBytes(body, path, v)
		}
	}
	del := func(path string) {
		if err == nil {
			body, err = sjson.DeleteBytes(body, path)
		}
	}
	set("model", baseModel)
	set("stream", true)
	set("store", false)
	for _, f := range droppedFields {
		del(f)
	}
	if !gjson.GetBytes(body, "stream_options.reasoning_summary_delivery").Exists() {
		del("stream_options")
	}

	instr := gjson.GetBytes(body, "instructions")
	if !instr.Exists() || instr.Type == gjson.Null {
		set("instructions", "")
	}

	// String input becomes a single user message.
	if in := gjson.GetBytes(body, "input"); in.Type == gjson.String {
		set("input", []map[string]any{{"type": "message", "role": "user",
			"content": []map[string]string{{"type": "input_text", "text": in.String()}}}})
	}
	if err != nil {
		return nil, err
	}

	// system → developer, empty function arguments → "{}".
	gjson.GetBytes(body, "input").ForEach(func(k, item gjson.Result) bool {
		idx := k.String()
		if item.Get("role").String() == "system" {
			set("input."+idx+".role", "developer")
		}
		if item.Get("type").String() == "function_call" && strings.TrimSpace(item.Get("arguments").String()) == "" {
			set("input."+idx+".arguments", "{}")
		}
		return true
	})
	gjson.GetBytes(body, "tools").ForEach(func(k, tool gjson.Result) bool {
		if strings.HasPrefix(tool.Get("type").String(), "web_search_preview") {
			set("tools."+k.String()+".type", "web_search")
		}
		return true
	})

	if effort != "" {
		set("reasoning.effort", effort)
	} else if !gjson.GetBytes(body, "reasoning.effort").Exists() {
		set("reasoning.effort", "medium")
	}
	if !includes(body, "reasoning.encrypted_content") {
		includeList := []string{"reasoning.encrypted_content"}
		gjson.GetBytes(body, "include").ForEach(func(_, v gjson.Result) bool {
			includeList = append(includeList, v.String())
			return true
		})
		set("include", includeList)
	}
	if !gjson.GetBytes(body, "tools").IsArray() || len(gjson.GetBytes(body, "tools").Array()) == 0 {
		del("parallel_tool_calls")
	} else if !gjson.GetBytes(body, "parallel_tool_calls").Exists() {
		set("parallel_tool_calls", true)
	}
	switch strings.ToLower(gjson.GetBytes(body, "service_tier").String()) {
	case "fast", "priority":
		set("service_tier", "priority")
	case "ultrafast", "":
	default:
		del("service_tier")
	}
	if !gjson.GetBytes(body, "prompt_cache_key").Exists() && cacheKey != "" {
		set("prompt_cache_key", cacheKey)
	}
	if err != nil {
		return nil, err
	}
	return sanitize(body), nil
}

func includes(body []byte, v string) bool {
	found := false
	gjson.GetBytes(body, "include").ForEach(func(_, x gjson.Result) bool {
		found = x.String() == v
		return !found
	})
	return found
}

// Send posts a prepared Responses body to the Codex backend; the response is always SSE.
func (p *Provider) Send(ctx context.Context, c provider.Credentials, body []byte, clientHeaders http.Header) (*http.Response, error) {
	return p.post(ctx, c, "/codex/responses", body, clientHeaders, "text/event-stream")
}

// Compact posts to the non-streaming compaction endpoint and returns its JSON response.
func (p *Provider) Compact(ctx context.Context, c provider.Credentials, body []byte, clientHeaders http.Header) (*http.Response, error) {
	return p.post(ctx, c, "/codex/responses/compact", body, clientHeaders, "application/json")
}

func (p *Provider) post(ctx context.Context, c provider.Credentials, path string, body []byte, clientHeaders http.Header, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BackendBase+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer "+c.AccessToken)
	h.Set("Accept", accept)
	h.Set("Connection", "Keep-Alive")
	h.Set("Originator", originator)
	h.Set("User-Agent", userAgent)
	if c.AccountID != "" {
		h.Set("Chatgpt-Account-Id", c.AccountID)
	}
	if key := gjson.GetBytes(body, "prompt_cache_key").String(); key != "" {
		h.Set("Session-Id", key)
	}
	model := gjson.GetBytes(body, "model").String()
	hint := "model=" + model
	if tier := gjson.GetBytes(body, "service_tier").String(); tier != "" {
		hint += ";tier=" + tier
	}
	h.Set("X-Codex-Routing-Hint", hint)
	for _, name := range []string{"Version", "X-Codex-Turn-Metadata", "X-Codex-Turn-State", "X-Client-Request-Id",
		"X-Codex-Window-Id", "Thread-Id", "X-Codex-Beta-Features"} {
		if v := clientHeaders.Get(name); v != "" {
			h.Set(name, v)
		}
	}
	return p.client.Do(req)
}

// AppendImageTool adds the image_generation tool so the model can draw, unless the plan or
// model can't use it or the request already has it.
func AppendImageTool(body []byte, plan string) []byte {
	model := strings.ToLower(gjson.GetBytes(body, "model").String())
	if strings.EqualFold(plan, "free") || strings.HasSuffix(model, "spark") || strings.Contains(model, "responses-lite") {
		return body
	}
	has := false
	gjson.GetBytes(body, "tools").ForEach(func(_, t gjson.Result) bool {
		has = t.Get("type").String() == "image_generation"
		return !has
	})
	if has {
		return body
	}
	if !gjson.GetBytes(body, "tools").IsArray() {
		body, _ = sjson.SetRawBytes(body, "tools", []byte("[]"))
	}
	body, _ = sjson.SetRawBytes(body, "tools.-1", []byte(`{"type":"image_generation","output_format":"png"}`))
	return body
}

// UsageLimitReset extracts the reset time (unix seconds) from a usage_limit_reached error body.
func UsageLimitReset(body string) (resetUnix int64, isUsageLimit bool) {
	t := gjson.Get(body, "error.type").String()
	if t == "" {
		t = gjson.Get(body, "type").String()
	}
	if t != "usage_limit_reached" {
		return 0, false
	}
	if v := gjson.Get(body, "error.resets_at").Int(); v > 0 {
		return v, true
	}
	if v := gjson.Get(body, "error.resets_in_seconds").Int(); v > 0 {
		return -v, true
	}
	return 0, true
}
