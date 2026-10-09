package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/provider"
)

// Claude Code client identity mimicked on upstream requests (CLIProxyAPI pins).
const (
	cliVersion   = "2.1.280"
	sdkVersion   = "0.112.1"
	userAgent    = "claude-cli/" + cliVersion + " (external, cli)"
	identityLine = "You are Claude Code, Anthropic's official CLI for Claude."
	midConvBeta  = "mid-conversation-system-2026-04-07"
	effortBeta   = "effort-2025-11-24"
	redactBeta   = "redact-thinking-2026-02-12"
	fastModeBeta = "fast-mode-2026-02-01"
)

var baseBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"interleaved-thinking-2025-05-14",
	redactBeta,
	"thinking-token-count-2026-05-13",
	"context-management-2025-06-27",
	"prompt-caching-scope-2026-01-05",
}

var countTokensBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"interleaved-thinking-2025-05-14",
	"context-management-2025-06-27",
	"token-counting-2024-11-01",
}

// Models before the mid-conversation system beta get the caller's system prompt as a reminder block.
var legacyModel = regexp.MustCompile(`^claude-(3|(opus|sonnet|haiku)-4)`)

type Options struct {
	SessionID   string
	ClientBetas []string
	// Native is true when the client itself spoke the Claude Messages API (e.g. Claude Code).
	Native bool
	Cloak  CloakSettings
	// Aliases receives the tool renames when Cloak.ToolAliases is on, so responses can be mapped back.
	Aliases *ToolAliases
}

const extendedCacheBeta = "extended-cache-ttl-2025-04-11"

func decodeJSON(raw string, dst any) error {
	if raw == "" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

// aliasTools renames client tools (definitions, tool_choice and history) to MCP-style names.
func aliasTools(o *object, msgs []message, a *ToolAliases) {
	var tools []any
	if t := o.get("tools"); t.IsArray() && decodeJSON(t.Raw, &tools) == nil {
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := tm["type"].(string)
			if typ != "" && typ != "custom" && isServerTool(typ) {
				continue
			}
			if typ == "custom" {
				delete(tm, "type")
			}
			if name, _ := tm["name"].(string); name != "" {
				tm["name"] = a.Alias(name)
			}
		}
		o.set("tools", tools)
	}
	if o.get("tool_choice.type").String() == "tool" {
		var tc map[string]any
		if decodeJSON(o.get("tool_choice").Raw, &tc) == nil {
			if name, _ := tc["name"].(string); name != "" {
				tc["name"] = a.Alias(name)
				o.set("tool_choice", tc)
			}
		}
	}
	for _, m := range msgs {
		blocks, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
				if name, _ := bm["name"].(string); name != "" {
					bm["name"] = a.Alias(name)
				}
			}
		}
	}
}

// obfuscateMessages applies sensitive-word obfuscation to message text.
func obfuscateMessages(msgs []message, re *regexp.Regexp) {
	for _, m := range msgs {
		switch c := m["content"].(type) {
		case string:
			m["content"] = obfuscate(re, c)
		case []any:
			for _, b := range c {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "text" {
					if t, ok := bm["text"].(string); ok {
						bm["text"] = obfuscate(re, t)
					}
				}
			}
		}
	}
}

// field is one top-level JSON member, kept in order.
type field struct {
	key string
	val json.RawMessage
}

type object []field

func parseObject(body []byte) (object, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid JSON body")
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	var o object
	root.ForEach(func(k, v gjson.Result) bool {
		o = append(o, field{k.String(), json.RawMessage(v.Raw)})
		return true
	})
	return o, nil
}

func (o object) get(key string) gjson.Result {
	for _, f := range o {
		if f.key == key {
			return gjson.ParseBytes(f.val)
		}
	}
	return gjson.Result{}
}

func (o *object) set(key string, v any) {
	raw, ok := v.(json.RawMessage)
	if !ok {
		raw, _ = json.Marshal(v)
	}
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].val = raw
			return
		}
	}
	*o = append(*o, field{key, raw})
}

func (o *object) del(key string) {
	*o = slices.DeleteFunc(*o, func(f field) bool { return f.key == key })
}

// Claude Code's top-level key order; unknown keys follow in their original order.
var keyOrder = []string{"model", "messages", "system", "tools", "tool_choice", "metadata", "max_tokens",
	"thinking", "context_management", "output_config", "stream"}

func (o object) marshal() []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	write := func(f field) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(f.key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(f.val)
	}
	for _, k := range keyOrder {
		for _, f := range o {
			if f.key == k {
				write(f)
			}
		}
	}
	for _, f := range o {
		if !slices.Contains(keyOrder, f.key) {
			write(f)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

type block = map[string]any
type message = map[string]any

func decodeMessages(raw gjson.Result) ([]message, error) {
	if !raw.Exists() {
		return nil, fmt.Errorf("messages is required")
	}
	dec := json.NewDecoder(strings.NewReader(raw.Raw))
	dec.UseNumber()
	var msgs []message
	return msgs, dec.Decode(&msgs)
}

// contentBlocks normalizes string content into a block list.
func contentBlocks(m message) []any {
	switch c := m["content"].(type) {
	case string:
		return []any{block{"type": "text", "text": c}}
	case []any:
		return c
	}
	return []any{}
}

func callerSystem(sys gjson.Result) string {
	var parts []string
	keep := func(t string) {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" || trimmed == identityLine || strings.HasPrefix(trimmed, "x-anthropic-billing-header:") {
			return
		}
		parts = append(parts, t)
	}
	if sys.Type == gjson.String {
		keep(sys.String())
	} else if sys.IsArray() {
		for _, b := range sys.Array() {
			if b.Get("type").String() == "text" {
				keep(b.Get("text").String())
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func reminder(text string) block {
	return block{"type": "text", "text": "<system-reminder>\n" + text + "\n</system-reminder>"}
}

func dateReminder(now time.Time) block {
	return block{"type": "text", "text": "<system-reminder>\nAs you answer the user's questions, you can use the following context:\n# currentDate\nToday's date is " +
		now.Format("2006-01-02") + ".\n\n      IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.\n</system-reminder>\n\n"}
}

func firstUserText(blocks []any) string {
	for _, b := range blocks {
		bm, ok := b.(block)
		if !ok || bm["type"] != "text" {
			continue
		}
		t, _ := bm["text"].(string)
		if !strings.HasPrefix(strings.TrimSpace(t), "<system-reminder>") {
			return t
		}
	}
	return ""
}

func countCacheControl(raw string) int { return strings.Count(raw, `"cache_control"`) }

func betaList(o object, extra ...string) []string {
	var out []string
	for _, b := range extra {
		for _, part := range strings.Split(b, ",") {
			if part = strings.TrimSpace(part); part != "" && !slices.Contains(out, part) {
				out = append(out, part)
			}
		}
	}
	if betas := o.get("betas"); betas.IsArray() {
		for _, b := range betas.Array() {
			if s := b.String(); s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// PrepareMessages rewrites a Claude Messages request so an OAuth subscription token accepts it:
// Claude Code's system prompt and billing header, metadata, betas, and a signed body.
func PrepareMessages(body []byte, c provider.Credentials, opts Options) ([]byte, []string, error) {
	o, err := parseObject(body)
	if err != nil {
		return nil, nil, err
	}
	model := o.get("model").String()
	legacy := legacyModel.MatchString(model)
	msgs, err := decodeMessages(o.get("messages"))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid messages: %w", err)
	}
	sysText := callerSystem(o.get("system"))
	o.del("system")
	if re := wordPattern(opts.Cloak.SensitiveWords); re != nil {
		sysText = obfuscate(re, sysText)
		obfuscateMessages(msgs, re)
	}
	if opts.Cloak.ToolAliases && opts.Aliases != nil {
		aliasTools(&o, msgs, opts.Aliases)
	}

	// Thinking blocks produced by Codex (see translate.CodexSignaturePrefix) would fail Claude's signature check.
	for _, m := range msgs {
		if blocks, ok := m["content"].([]any); ok {
			m["content"] = slices.DeleteFunc(blocks, func(b any) bool {
				bm, ok := b.(block)
				sig, _ := bm["signature"].(string)
				return ok && bm["type"] == "thinking" && strings.HasPrefix(sig, "mtk-codex:")
			})
		}
	}

	fu := slices.IndexFunc(msgs, func(m message) bool { return m["role"] == "user" })
	if fu < 0 {
		msgs = append([]message{{"role": "user", "content": []any{block{"type": "text", "text": "."}}}}, msgs...)
		fu = 0
	}
	firstBlocks := contentBlocks(msgs[fu])
	fingerprintText := firstUserText(firstBlocks)

	var prefix []any
	if sysText != "" && legacy {
		prefix = append(prefix, reminder(sysText))
	}
	if !strings.Contains(fmt.Sprint(firstBlocks), "# currentDate") {
		prefix = append(prefix, dateReminder(time.Now()))
	}
	msgs[fu]["content"] = append(prefix, firstBlocks...)
	if sysText != "" && !legacy {
		sysMsg := message{"role": "system", "content": []any{block{"type": "text", "text": sysText}}}
		msgs = slices.Insert(msgs, fu+1, sysMsg)
	}

	o.set("system", []block{
		{"type": "text", "text": billingHeader(cliVersion, fingerprintText)},
		{"type": "text", "text": identityLine, "cache_control": block{"type": "ephemeral"}},
	})
	if opts.Cloak.CacheBreakpoints {
		msgsJSON, _ := json.Marshal(msgs)
		// Claude Code places its own message markers; only add one for other clients.
		addMarker := !(opts.Native && countCacheControl(string(msgsJSON)) > 0)
		msgs = placeCacheBreakpoints(&o, msgs, addMarker, opts.Cloak.LongCacheTTL)
	}
	o.set("messages", msgs)

	userID, _ := json.Marshal(map[string]string{
		"device_id":    c.DeviceID,
		"account_uuid": c.AccountUUID,
		"session_id":   opts.SessionID,
	})
	o.set("metadata", map[string]string{"user_id": string(userID)})

	thinkingType := o.get("thinking.type").String()
	thinking := thinkingType == "enabled" || thinkingType == "adaptive"
	if tc := o.get("tool_choice.type").String(); tc == "any" || tc == "tool" {
		o.del("thinking")
		if oc := o.get("output_config"); oc.Exists() {
			var cfg map[string]any
			if json.Unmarshal([]byte(oc.Raw), &cfg) == nil {
				delete(cfg, "effort")
				o.set("output_config", cfg)
			}
		}
		thinking = false
	}
	if thinking {
		o.del("top_k")
		if !o.get("context_management").Exists() {
			o.set("context_management", json.RawMessage(`{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]}`))
		}
	}
	// Sampling: Anthropic wants temperature 1, top_p >= 0.95 and no top_k with thinking, and
	// rejects temperature together with top_p otherwise.
	switch {
	case !opts.Native:
		o.del("temperature")
		o.del("top_p")
	case thinking:
		if t := o.get("temperature"); t.Exists() && t.Float() != 1 {
			o.del("temperature")
		}
		if p := o.get("top_p"); p.Exists() && p.Float() < 0.95 {
			o.del("top_p")
		}
	case o.get("temperature").Exists() && o.get("top_p").Exists():
		o.del("top_p")
	}
	if !o.get("max_tokens").Exists() {
		o.set("max_tokens", 32000)
	}
	o.set("stream", true)

	betas := slices.Clone(baseBetas)
	if o.get("thinking.display").Exists() {
		betas = slices.DeleteFunc(betas, func(b string) bool { return b == redactBeta })
	}
	if !legacy {
		betas = append(betas, midConvBeta)
	}
	if o.get("output_config.effort").Exists() {
		betas = append(betas, effortBeta)
	}
	if strings.EqualFold(o.get("speed").String(), "fast") {
		betas = append(betas, fastModeBeta)
	}
	if strings.Contains(o.get("system").Raw+o.get("messages").Raw+o.get("tools").Raw, `"ttl":"1h"`) {
		betas = append(betas, extendedCacheBeta)
	}
	for _, b := range betaList(o, opts.ClientBetas...) {
		if !slices.Contains(betas, b) {
			betas = append(betas, b)
		}
	}
	o.del("betas")

	signed, err := signBody(dropEmptyDomains(o.marshal()))
	return signed, betas, err
}

// dropEmptyDomains removes empty allowed/blocked domain lists from web search tools, which
// some clients send and Anthropic rejects as ambiguous.
func dropEmptyDomains(body []byte) []byte {
	gjson.GetBytes(body, "tools").ForEach(func(i, t gjson.Result) bool {
		if !strings.HasPrefix(t.Get("type").String(), "web_search_") {
			return true
		}
		for _, f := range []string{"allowed_domains", "blocked_domains"} {
			if v := t.Get(f); v.IsArray() && len(v.Array()) == 0 {
				body, _ = sjson.DeleteBytes(body, fmt.Sprintf("tools.%d.%s", i.Int(), f))
			}
		}
		return true
	})
	return body
}

// PrepareCountTokens shapes a count_tokens request: no billing block, system moved into messages.
func PrepareCountTokens(body []byte, opts Options) ([]byte, []string, error) {
	o, err := parseObject(body)
	if err != nil {
		return nil, nil, err
	}
	msgs, err := decodeMessages(o.get("messages"))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid messages: %w", err)
	}
	if sysText := callerSystem(o.get("system")); sysText != "" {
		if fu := slices.IndexFunc(msgs, func(m message) bool { return m["role"] == "user" }); fu >= 0 {
			msgs[fu]["content"] = append([]any{reminder(sysText)}, contentBlocks(msgs[fu])...)
		}
	}
	o.set("messages", msgs)
	for _, k := range []string{"system", "metadata", "context_management", "diagnostics", "stream", "max_tokens"} {
		o.del(k)
	}
	betas := slices.Clone(countTokensBetas)
	for _, b := range betaList(o, opts.ClientBetas...) {
		if !slices.Contains(betas, b) {
			betas = append(betas, b)
		}
	}
	o.del("betas")
	return o.marshal(), betas, nil
}

// Headers Claude Code sends for subagents and remote sessions, passed through when the client set them.
var forwardedHeaders = []string{"X-Claude-Code-Agent-Id", "X-Claude-Code-Parent-Agent-Id", "X-Claude-Remote-Container-Id",
	"X-Claude-Remote-Session-Id", "X-Client-App", "X-Anthropic-Additional-Protection"}

// Request hints only the real Claude Code client may set.
var nativeHeaders = []string{"X-Claude-Code-Request-Class", "X-Claude-Code-Agent-Type",
	"X-Claude-Code-Prev-Tool-Durations", "X-Claude-Code-Compaction", "X-Claude-Code-Context-Compacted", "X-Client-Request-Id"}

// IsClaudeCode reports whether a request came from the Claude Code CLI itself.
func IsClaudeCode(h http.Header) bool {
	return strings.HasPrefix(h.Get("User-Agent"), "claude-cli/")
}

// Send posts a prepared body upstream with Claude Code's headers. client holds the caller's
// headers; Claude Code's own identity headers are passed through from it.
func (p *Provider) Send(ctx context.Context, c provider.Credentials, body []byte, betas []string, sessionID string, countTokens bool, client http.Header) (*http.Response, error) {
	target := p.APIBase + "/v1/messages?beta=true"
	if countTokens {
		target = p.APIBase + "/v1/messages/count_tokens?beta=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	h := req.Header
	h.Set("Authorization", "Bearer "+c.AccessToken)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	h["anthropic-version"] = []string{"2023-06-01"}
	h["anthropic-beta"] = []string{strings.Join(betas, ",")}
	h["anthropic-dangerous-direct-browser-access"] = []string{"true"}
	h["x-app"] = []string{"cli"}
	h.Set("User-Agent", userAgent)
	h.Set("X-Stainless-Retry-Count", "0")
	h.Set("X-Stainless-Runtime", "node")
	h.Set("X-Stainless-Lang", "js")
	h.Set("X-Stainless-Package-Version", sdkVersion)
	h.Set("X-Stainless-Runtime-Version", "v26.3.0")
	h["X-Stainless-OS"] = []string{"MacOS"}
	h.Set("X-Stainless-Arch", "arm64")
	if !countTokens {
		h.Set("X-Stainless-Timeout", "600")
	}
	h.Set("X-Claude-Code-Session-Id", sessionID)
	h.Set("x-client-request-id", provider.UUID())
	h.Set("Connection", "keep-alive")
	for _, name := range forwardedHeaders {
		if v := client.Get(name); v != "" {
			h.Set(name, v)
		}
	}
	if IsClaudeCode(client) {
		for _, name := range nativeHeaders {
			if v := client.Get(name); v != "" {
				h.Set(name, v)
			}
		}
	}

	resp, err := p.http().Do(req)
	if err != nil {
		return nil, err
	}
	if err := provider.Decompress(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}
