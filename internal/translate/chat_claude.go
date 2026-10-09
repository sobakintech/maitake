package translate

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// ChatToClaude converts an OpenAI Chat Completions request into a Claude Messages request.
// Tool names are made Claude-safe through names, so responses can map them back.
func ChatToClaude(body []byte, model, effort string, names *ToolNames) ([]byte, error) {
	req := gjson.ParseBytes(body)
	out := obj{"model": model}

	maxTokens := req.Get("max_tokens").Int()
	if maxTokens <= 0 {
		maxTokens = req.Get("max_completion_tokens").Int()
	}
	if maxTokens <= 0 {
		maxTokens = defaultClaudeMaxTokens
	}

	var system arr
	var msgs []obj
	push := func(role string, blocks arr) {
		if len(blocks) == 0 {
			return
		}
		if n := len(msgs); n > 0 && msgs[n-1]["role"] == role {
			msgs[n-1]["content"] = append(msgs[n-1]["content"].(arr), blocks...)
			return
		}
		msgs = append(msgs, obj{"role": role, "content": blocks})
	}

	for _, m := range req.Get("messages").Array() {
		role := m.Get("role").String()
		switch role {
		case "system", "developer":
			system = append(system, withMessageCache(chatTextBlocks(m.Get("content")), m)...)
		case "user":
			push("user", withMessageCache(chatUserBlocks(m.Get("content")), m))
		case "assistant":
			// reasoning_content is dropped: Claude rejects thinking blocks without a valid signature.
			var blocks arr
			for _, b := range chatTextBlocks(m.Get("content")) {
				blocks = append(blocks, b)
			}
			for i, tc := range m.Get("tool_calls").Array() {
				id := tc.Get("id").String()
				if id == "" {
					id = fmt.Sprintf("toolu_%d_%d", len(msgs), i)
				}
				var input any = obj{}
				if args := tc.Get("function.arguments").String(); args != "" {
					var parsed any
					if json.Unmarshal([]byte(args), &parsed) == nil {
						if _, isObj := parsed.(map[string]any); isObj {
							input = parsed
						}
					}
				}
				blocks = append(blocks, obj{"type": "tool_use", "id": sanitizeToolID(id),
					"name": names.Short(tc.Get("function.name").String()), "input": input})
			}
			push("assistant", withMessageCache(blocks, m))
		case "tool", "function":
			content := m.Get("content")
			result := obj{"type": "tool_result", "tool_use_id": sanitizeToolID(m.Get("tool_call_id").String())}
			if content.IsArray() {
				// Anthropic rejects cache_control inside tool_result content; hoist it onto the result.
				blocks := chatUserBlocks(content)
				for _, b := range blocks {
					if cc, ok := b.(obj)["cache_control"]; ok {
						result["cache_control"] = cc
						delete(b.(obj), "cache_control")
					}
				}
				result["content"] = blocks
			} else {
				result["content"] = content.String()
			}
			if cc := cacheControl(m); cc != nil {
				result["cache_control"] = cc
			}
			push("user", arr{result})
		}
	}
	if len(msgs) == 0 || msgs[0]["role"] != "user" {
		msgs = append([]obj{{"role": "user", "content": arr{obj{"type": "text", "text": "."}}}}, msgs...)
	}

	if rf := req.Get("response_format"); rf.Exists() {
		switch rf.Get("type").String() {
		case "json_schema":
			system = append(system, obj{"type": "text", "text": "Respond only with a single JSON value that conforms to this JSON Schema, with no surrounding text or code fences:\n" + rf.Get("json_schema.schema").Raw})
		case "json_object":
			system = append(system, obj{"type": "text", "text": "Respond only with a single valid JSON object, with no surrounding text or code fences."})
		}
	}

	out["messages"] = msgs
	if len(system) > 0 {
		out["system"] = system
	}
	out["max_tokens"] = maxTokens

	allowed, allowedMode := allowedTools(req.Get("tool_choice"))
	if tools := req.Get("tools"); tools.IsArray() && len(tools.Array()) > 0 {
		var ct arr
		for _, t := range tools.Array() {
			fn := t.Get("function")
			if t.Get("type").String() != "function" || !fn.Exists() {
				continue
			}
			name := fn.Get("name").String()
			if allowed != nil && !allowed[name] {
				continue
			}
			tool := obj{"name": names.Short(name), "input_schema": json.RawMessage(claudeToolSchema(fn.Get("parameters")))}
			if d := fn.Get("description").String(); d != "" {
				tool["description"] = d
			}
			if fn.Get("strict").Bool() {
				tool["strict"] = true
			}
			if cc := cacheControl(t); cc != nil {
				tool["cache_control"] = cc
			} else if cc := cacheControl(fn); cc != nil {
				tool["cache_control"] = cc
			}
			ct = append(ct, tool)
		}
		if len(ct) > 0 {
			out["tools"] = ct
		}
	}
	tc := chatToolChoice(req, names)
	if allowed != nil {
		tc = obj{"type": "auto"}
		if allowedMode == "required" {
			tc = obj{"type": "any"}
		}
	}
	if tc != nil && out["tools"] != nil {
		out["tool_choice"] = tc
	}

	if stops := req.Get("stop"); stops.Exists() {
		var seqs []string
		if stops.IsArray() {
			for _, s := range stops.Array() {
				seqs = append(seqs, s.String())
			}
		} else if s := stops.String(); s != "" {
			seqs = append(seqs, s)
		}
		if len(seqs) > 0 {
			out["stop_sequences"] = seqs
		}
	}

	if effort == "" {
		effort = strings.ToLower(req.Get("reasoning_effort").String())
	}
	if thinking, oe, ok := claudeThinking(model, effort); ok {
		out["thinking"] = thinking
		if oe != "" {
			out["output_config"] = obj{"effort": oe}
		}
	}
	return FitClaudeTokens(marshal(out), model), nil
}

func chatToolChoice(req gjson.Result, names *ToolNames) obj {
	tc := req.Get("tool_choice")
	var out obj
	switch {
	case tc.Type == gjson.String:
		switch tc.String() {
		case "none":
			out = obj{"type": "none"}
		case "auto":
			out = obj{"type": "auto"}
		case "required":
			out = obj{"type": "any"}
		}
	case tc.IsObject():
		if name := tc.Get("function.name").String(); name != "" {
			out = obj{"type": "tool", "name": names.Short(name)}
		}
	}
	if req.Get("parallel_tool_calls").Exists() && !req.Get("parallel_tool_calls").Bool() {
		if out == nil {
			out = obj{"type": "auto"}
		}
		if out["type"] != "none" {
			out["disable_parallel_tool_use"] = true
		}
	}
	return out
}

// allowedTools reads an "allowed_tools" tool_choice: the permitted names and the mode, or nil.
func allowedTools(tc gjson.Result) (map[string]bool, string) {
	if tc.Get("type").String() != "allowed_tools" {
		return nil, ""
	}
	list := tc.Get("allowed_tools.tools").Array()
	if len(list) == 0 {
		list = tc.Get("tools").Array()
	}
	names := map[string]bool{}
	for _, t := range list {
		name := t.Get("function.name").String()
		if name == "" {
			name = t.Get("name").String()
		}
		if name != "" {
			names[name] = true
		}
	}
	mode := strings.ToLower(tc.Get("allowed_tools.mode").String())
	if mode == "" {
		mode = strings.ToLower(tc.Get("mode").String())
	}
	return names, mode
}

// cacheControl returns src's cache_control when it's one Claude accepts.
func cacheControl(src gjson.Result) json.RawMessage {
	cc := src.Get("cache_control")
	if !cc.IsObject() || cc.Get("type").String() != "ephemeral" {
		return nil
	}
	return json.RawMessage(cc.Raw)
}

// withMessageCache applies a message-level cache_control to its last block, unless that block
// already has one.
func withMessageCache(blocks arr, m gjson.Result) arr {
	cc := cacheControl(m)
	if cc == nil || len(blocks) == 0 {
		return blocks
	}
	if last, ok := blocks[len(blocks)-1].(obj); ok && last["cache_control"] == nil {
		last["cache_control"] = cc
	}
	return blocks
}

// claudeToolSchema makes a tool schema acceptable to Claude: an object without root-level
// unions. Union branches that are objects contribute their properties.
func claudeToolSchema(params gjson.Result) []byte {
	empty := []byte(`{"type":"object","properties":{}}`)
	var root map[string]json.RawMessage
	if !params.IsObject() || json.Unmarshal([]byte(params.Raw), &root) != nil {
		return empty
	}
	_, hasUnion := root["anyOf"]
	if _, ok := root["oneOf"]; ok {
		hasUnion = true
	}
	if _, ok := root["allOf"]; ok {
		hasUnion = true
	}
	if t, ok := root["type"]; ok && string(t) == `"object"` && !hasUnion {
		return []byte(params.Raw)
	}
	props := map[string]json.RawMessage{}
	_ = json.Unmarshal(root["properties"], &props)
	var required []string
	_ = json.Unmarshal(root["required"], &required)
	for _, union := range []string{"anyOf", "oneOf", "allOf"} {
		raw, ok := root[union]
		if !ok {
			continue
		}
		delete(root, union)
		var branches []map[string]json.RawMessage
		if json.Unmarshal(raw, &branches) != nil {
			continue
		}
		for _, b := range branches {
			if t, ok := b["type"]; ok && string(t) != `"object"` {
				continue
			}
			var bp map[string]json.RawMessage
			if json.Unmarshal(b["properties"], &bp) == nil {
				for k, v := range bp {
					if _, exists := props[k]; !exists {
						props[k] = v
					}
				}
			}
			if union == "allOf" {
				var br []string
				if json.Unmarshal(b["required"], &br) == nil {
					for _, r := range br {
						if !slices.Contains(required, r) {
							required = append(required, r)
						}
					}
				}
			}
		}
	}
	root["type"] = json.RawMessage(`"object"`)
	root["properties"], _ = json.Marshal(props)
	if len(required) > 0 {
		root["required"], _ = json.Marshal(required)
	}
	out, err := json.Marshal(root)
	if err != nil {
		return empty
	}
	return out
}

func sanitizeToolID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "toolu_unknown"
	}
	return b.String()
}

func chatTextBlocks(content gjson.Result) arr {
	var out arr
	if content.Type == gjson.String {
		if s := content.String(); s != "" {
			out = append(out, obj{"type": "text", "text": s})
		}
		return out
	}
	for _, p := range content.Array() {
		if t := p.Get("type").String(); (t == "text" || t == "input_text" || t == "output_text") && p.Get("text").String() != "" {
			out = append(out, withCache(obj{"type": "text", "text": p.Get("text").String()}, p))
		}
	}
	return out
}

func withCache(b obj, part gjson.Result) obj {
	if cc := cacheControl(part); cc != nil {
		b["cache_control"] = cc
	}
	return b
}

func chatUserBlocks(content gjson.Result) arr {
	if content.Type == gjson.String {
		return chatTextBlocks(content)
	}
	var out arr
	for _, p := range content.Array() {
		switch p.Get("type").String() {
		case "text", "input_text":
			if t := p.Get("text").String(); t != "" {
				out = append(out, withCache(obj{"type": "text", "text": t}, p))
			}
		case "image_url":
			url := p.Get("image_url.url").String()
			if url == "" {
				url = p.Get("image_url").String()
			}
			if img := imageBlock(url); img != nil {
				out = append(out, withCache(img, p))
			}
		case "file":
			if data := p.Get("file.file_data").String(); data != "" {
				mediaType, b64 := splitDataURL(data, "application/pdf")
				out = append(out, withCache(obj{"type": "document", "source": obj{"type": "base64", "media_type": mediaType, "data": b64}}, p))
			}
		}
	}
	return out
}

func imageBlock(url string) obj {
	if strings.HasPrefix(url, "data:") {
		mediaType, b64 := splitDataURL(url, "image/png")
		return obj{"type": "image", "source": obj{"type": "base64", "media_type": mediaType, "data": b64}}
	}
	if url != "" {
		return obj{"type": "image", "source": obj{"type": "url", "url": url}}
	}
	return nil
}

func splitDataURL(s, fallbackType string) (mediaType, data string) {
	if !strings.HasPrefix(s, "data:") {
		return fallbackType, s
	}
	meta, data, _ := strings.Cut(strings.TrimPrefix(s, "data:"), ",")
	mediaType, _, _ = strings.Cut(meta, ";")
	if mediaType == "" {
		mediaType = fallbackType
	}
	return mediaType, data
}

// ClaudeToChatStream turns Claude Messages SSE into Chat Completions chunks.
type ClaudeToChatStream struct {
	model     string
	opts      ChatOptions
	names     *ToolNames
	id        string
	created   int64
	tools     map[int64]*pendingTool
	toolIndex int
	usage     Usage
	finished  bool
	failed    bool
}

type pendingTool struct {
	id, name string
	args     strings.Builder
}

func NewClaudeToChatStream(model string, opts ChatOptions, names *ToolNames) *ClaudeToChatStream {
	return &ClaudeToChatStream{model: model, opts: opts, names: names, created: time.Now().Unix(),
		id: "chatcmpl-" + randomID(), tools: map[int64]*pendingTool{}}
}

func (c *ClaudeToChatStream) chunk(delta obj, finish any) Event {
	return ev("", obj{"id": c.id, "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": arr{obj{"index": 0, "delta": delta, "finish_reason": finish}}})
}

func (c *ClaudeToChatStream) Convert(e Event) []Event {
	d := gjson.ParseBytes(e.Data)
	switch d.Get("type").String() {
	case "message_start":
		if id := d.Get("message.id").String(); id != "" {
			c.id = "chatcmpl-" + strings.TrimPrefix(id, "msg_")
		}
		readClaudeUsage(d.Get("message.usage"), &c.usage)
		return []Event{c.chunk(obj{"role": "assistant", "content": ""}, nil)}
	case "content_block_start":
		if cb := d.Get("content_block"); cb.Get("type").String() == "tool_use" {
			c.tools[d.Get("index").Int()] = &pendingTool{id: cb.Get("id").String(), name: c.names.Original(cb.Get("name").String())}
		}
	case "content_block_delta":
		delta := d.Get("delta")
		switch delta.Get("type").String() {
		case "text_delta":
			return []Event{c.chunk(obj{"content": delta.Get("text").String()}, nil)}
		case "thinking_delta":
			if d := c.opts.reasoningDelta(delta.Get("thinking").String()); d != nil {
				return []Event{c.chunk(d, nil)}
			}
		case "input_json_delta":
			if t := c.tools[d.Get("index").Int()]; t != nil {
				t.args.WriteString(delta.Get("partial_json").String())
			}
		}
	case "content_block_stop":
		idx := d.Get("index").Int()
		if t := c.tools[idx]; t != nil {
			delete(c.tools, idx)
			args := t.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			call := obj{"index": c.toolIndex, "id": t.id, "type": "function", "function": obj{"name": t.name, "arguments": args}}
			c.toolIndex++
			return []Event{c.chunk(obj{"tool_calls": arr{call}}, nil)}
		}
	case "message_delta":
		readClaudeUsage(d.Get("usage"), &c.usage)
		if sr := d.Get("delta.stop_reason"); sr.Exists() && sr.String() != "" {
			c.finished = true
			return []Event{c.chunk(obj{}, chatFinishReason(sr.String()))}
		}
	case "message_stop":
		var out []Event
		if c.opts.IncludeUsage {
			out = append(out, ev("", obj{"id": c.id, "object": "chat.completion.chunk", "created": c.created,
				"model": c.model, "choices": arr{}, "usage": chatUsage(c.usage)}))
		}
		return out
	case "error":
		c.failed = true
		return []Event{ev("", obj{"error": obj{"message": d.Get("error.message").String(), "type": d.Get("error.type").String()}})}
	}
	return nil
}

// Finish ends the stream; one that stopped before a finish reason is reported as an error.
func (c *ClaudeToChatStream) Finish() []Event {
	var out []Event
	if !c.finished && !c.failed {
		out = append(out, ev("", obj{"error": obj{"message": "upstream stream ended before the response finished",
			"type": "api_error", "code": "stream_truncated"}}))
	}
	return append(out, Event{Data: []byte("[DONE]")})
}

// Complete reports whether the stream reached a finish reason.
func (c *ClaudeToChatStream) Complete() bool { return c.finished }

func chatFinishReason(stop string) string {
	switch stop {
	case "tool_use":
		return "tool_calls"
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "refusal", "sensitive":
		return "content_filter"
	}
	return "stop"
}

func readClaudeUsage(u gjson.Result, dst *Usage) {
	if !u.Exists() {
		return
	}
	if v := u.Get("input_tokens"); v.Exists() {
		dst.InputTokens = v.Int()
	}
	if v := u.Get("output_tokens"); v.Exists() {
		dst.OutputTokens = v.Int()
	}
	if v := u.Get("cache_read_input_tokens"); v.Exists() {
		dst.CacheReadTokens = v.Int()
	}
	if v := u.Get("cache_creation_input_tokens"); v.Exists() {
		dst.CacheWriteTokens = v.Int()
	}
	if v := u.Get("output_tokens_details.thinking_tokens"); v.Exists() {
		dst.ReasoningTokens = v.Int()
	}
}

// chatUsage reports prompt tokens including cached ones, as OpenAI does.
func chatUsage(u Usage) obj {
	prompt := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
	return obj{
		"prompt_tokens":             prompt,
		"completion_tokens":         u.OutputTokens,
		"total_tokens":              prompt + u.OutputTokens,
		"prompt_tokens_details":     obj{"cached_tokens": u.CacheReadTokens, "cache_write_tokens": u.CacheWriteTokens},
		"completion_tokens_details": obj{"reasoning_tokens": u.ReasoningTokens},
	}
}

// ClaudeMessageToChat converts a complete Claude message into a chat.completion object.
func ClaudeMessageToChat(msg []byte, model string, opts ChatOptions, names *ToolNames) []byte {
	m := gjson.ParseBytes(msg)
	var text, reasoning strings.Builder
	var calls arr
	for _, b := range m.Get("content").Array() {
		switch b.Get("type").String() {
		case "text":
			text.WriteString(b.Get("text").String())
		case "thinking":
			reasoning.WriteString(b.Get("thinking").String())
		case "tool_use":
			calls = append(calls, obj{"id": b.Get("id").String(), "type": "function",
				"function": obj{"name": names.Original(b.Get("name").String()), "arguments": b.Get("input").Raw}})
		}
	}
	message := obj{"role": "assistant", "content": text.String()}
	if reasoning.Len() > 0 {
		for k, v := range opts.reasoningDelta(reasoning.String()) {
			message[k] = v
		}
	}
	if len(calls) > 0 {
		message["tool_calls"] = calls
	}
	var u Usage
	readClaudeUsage(m.Get("usage"), &u)
	return marshal(obj{
		"id":      "chatcmpl-" + strings.TrimPrefix(m.Get("id").String(), "msg_"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": arr{obj{"index": 0, "message": message, "finish_reason": chatFinishReason(m.Get("stop_reason").String())}},
		"usage":   chatUsage(u),
	})
}
