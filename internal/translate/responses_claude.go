package translate

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// ClaudeReasoningPrefix marks a Responses reasoning item that carries a Claude thinking block,
// so it can be replayed to Claude (and kept away from Codex).
const ClaudeReasoningPrefix = "mtk-claude:"

type claudeReasoning struct {
	Thinking  string `json:"t,omitempty"`
	Signature string `json:"s,omitempty"`
	Redacted  string `json:"r,omitempty"`
}

func encodeClaudeReasoning(r claudeReasoning) string {
	b, _ := json.Marshal(r)
	return ClaudeReasoningPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func decodeClaudeReasoning(enc string) (claudeReasoning, bool) {
	var r claudeReasoning
	raw, ok := strings.CutPrefix(enc, ClaudeReasoningPrefix)
	if !ok {
		return r, false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, r.Signature != "" || r.Redacted != ""
}

// IsClaudeReasoning reports whether encrypted reasoning came from Claude through maitake.
func IsClaudeReasoning(enc string) bool { return strings.HasPrefix(enc, ClaudeReasoningPrefix) }

// ReasoningSignature returns the provider signature inside an encrypted reasoning value.
func ReasoningSignature(enc string) string {
	if r, ok := decodeClaudeReasoning(enc); ok {
		if r.Signature != "" {
			return r.Signature
		}
		return r.Redacted
	}
	return strings.TrimPrefix(enc, CodexSignaturePrefix)
}

// ResponsesToClaude converts a Responses API request into a Claude Messages request.
func ResponsesToClaude(body []byte, model, effort string, names *ToolNames) ([]byte, error) {
	req := gjson.ParseBytes(body)
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
	if s := strings.TrimSpace(req.Get("instructions").String()); s != "" {
		system = append(system, obj{"type": "text", "text": req.Get("instructions").String()})
	}

	var tools arr
	var addTool func(t gjson.Result)
	addTool = func(t gjson.Result) {
		name := t.Get("name").String()
		switch typ := t.Get("type").String(); {
		case typ == "function":
			tool := obj{"name": names.Short(name), "input_schema": json.RawMessage(claudeToolSchema(t.Get("parameters")))}
			if d := t.Get("description").String(); d != "" {
				tool["description"] = d
			}
			tools = append(tools, tool)
		case typ == "custom":
			// Freeform tools (like Codex's apply_patch) take one string argument.
			names.MarkCustom(name)
			desc := t.Get("description").String()
			if def := t.Get("format.definition").String(); def != "" {
				desc += "\n\nThe input must follow this " + t.Get("format.syntax").String() + " grammar:\n" + def
			}
			tools = append(tools, obj{"name": names.Short(name), "description": desc,
				"input_schema": obj{"type": "object", "properties": obj{"input": obj{"type": "string"}}, "required": arr{"input"}}})
		case strings.HasPrefix(typ, "web_search"):
			ws := obj{"type": "web_search_20250305", "name": "web_search"}
			if d := t.Get("filters.allowed_domains"); d.IsArray() && len(d.Array()) > 0 {
				ws["allowed_domains"] = json.RawMessage(d.Raw)
			}
			if loc := t.Get("user_location"); loc.IsObject() {
				ws["user_location"] = json.RawMessage(loc.Raw)
			}
			tools = append(tools, ws)
		case typ == "namespace":
			t.Get("tools").ForEach(func(_, sub gjson.Result) bool {
				addTool(sub)
				return true
			})
		}
	}
	req.Get("tools").ForEach(func(_, t gjson.Result) bool {
		addTool(t)
		return true
	})

	in := req.Get("input")
	if in.Type == gjson.String {
		push("user", arr{obj{"type": "text", "text": in.String()}})
	}
	for _, it := range in.Array() {
		typ := it.Get("type").String()
		if typ == "" && it.Get("role").Exists() {
			typ = "message"
		}
		switch typ {
		case "message":
			role := it.Get("role").String()
			blocks := responsesContentToClaude(it.Get("content"))
			switch role {
			case "system", "developer":
				if len(msgs) == 0 {
					system = append(system, blocks...)
				} else {
					push("user", blocks)
				}
			case "assistant":
				push("assistant", blocks)
			default:
				push("user", blocks)
			}
		case "reasoning":
			if r, ok := decodeClaudeReasoning(it.Get("encrypted_content").String()); ok {
				if r.Redacted != "" {
					push("assistant", arr{obj{"type": "redacted_thinking", "data": r.Redacted}})
				} else {
					push("assistant", arr{obj{"type": "thinking", "thinking": r.Thinking, "signature": r.Signature}})
				}
			}
		case "function_call", "custom_tool_call":
			var input any = obj{}
			if typ == "custom_tool_call" {
				input = obj{"input": it.Get("input").String()}
			} else if args := it.Get("arguments").String(); args != "" {
				var parsed any
				if json.Unmarshal([]byte(args), &parsed) == nil {
					if _, isObj := parsed.(map[string]any); isObj {
						input = parsed
					}
				}
			}
			push("assistant", arr{obj{"type": "tool_use", "id": sanitizeToolID(it.Get("call_id").String()),
				"name": names.Short(it.Get("name").String()), "input": input}})
		case "function_call_output", "custom_tool_call_output":
			result := obj{"type": "tool_result", "tool_use_id": sanitizeToolID(it.Get("call_id").String())}
			if out := it.Get("output"); out.IsArray() {
				result["content"] = responsesContentToClaude(out)
			} else {
				result["content"] = out.String()
			}
			push("user", arr{result})
		}
	}
	if len(msgs) == 0 || msgs[0]["role"] != "user" {
		msgs = append([]obj{{"role": "user", "content": arr{obj{"type": "text", "text": "."}}}}, msgs...)
	}

	out := obj{"model": model, "messages": msgs}
	if len(system) > 0 {
		out["system"] = system
	}
	if n := req.Get("max_output_tokens").Int(); n > 0 {
		out["max_tokens"] = n
	}
	if len(tools) > 0 {
		out["tools"] = tools
		var tc obj
		switch choice := req.Get("tool_choice"); {
		case choice.Type == gjson.String:
			switch choice.String() {
			case "required":
				tc = obj{"type": "any"}
			case "none":
				tc = obj{"type": "none"}
			}
		case choice.Get("name").String() != "":
			tc = obj{"type": "tool", "name": names.Short(choice.Get("name").String())}
		}
		if p := req.Get("parallel_tool_calls"); p.Exists() && !p.Bool() {
			if tc == nil {
				tc = obj{"type": "auto"}
			}
			if tc["type"] != "none" {
				tc["disable_parallel_tool_use"] = true
			}
		}
		if tc != nil {
			out["tool_choice"] = tc
		}
	}
	if effort == "" {
		effort = strings.ToLower(req.Get("reasoning.effort").String())
	}
	if thinking, oe, ok := claudeThinking(model, effort); ok {
		if thinking["type"] != "disabled" {
			display := "omitted"
			if s := req.Get("reasoning.summary").String(); s != "" && s != "none" {
				display = "summarized"
			}
			thinking["display"] = display
		}
		out["thinking"] = thinking
		if oe != "" {
			out["output_config"] = obj{"effort": oe}
		}
	}
	if f := req.Get("text.format"); f.Get("type").String() == "json_schema" && f.Get("schema").IsObject() {
		cfg, _ := out["output_config"].(obj)
		if cfg == nil {
			cfg = obj{}
		}
		cfg["format"] = obj{"type": "json_schema", "schema": json.RawMessage(f.Get("schema").Raw)}
		out["output_config"] = cfg
	}
	if tier := req.Get("service_tier").String(); tier == "priority" || tier == "fast" {
		out["speed"] = "fast"
	}
	return FitClaudeTokens(marshal(out), model), nil
}

func responsesContentToClaude(content gjson.Result) arr {
	if content.Type == gjson.String {
		if s := content.String(); s != "" {
			return arr{obj{"type": "text", "text": s}}
		}
		return nil
	}
	var out arr
	for _, p := range content.Array() {
		switch p.Get("type").String() {
		case "input_text", "output_text", "text":
			if t := p.Get("text").String(); t != "" {
				out = append(out, obj{"type": "text", "text": t})
			}
		case "input_image":
			url := p.Get("image_url").String()
			if url == "" {
				url = p.Get("image_url.url").String()
			}
			if img := imageBlock(url); img != nil {
				out = append(out, img)
			}
		case "input_file":
			if data := p.Get("file_data").String(); data != "" {
				mediaType, b64 := splitDataURL(data, "application/pdf")
				out = append(out, obj{"type": "document", "source": obj{"type": "base64", "media_type": mediaType, "data": b64}})
			}
		}
	}
	return out
}

// ClaudeToResponsesStream turns Claude Messages SSE into Responses API events.
type ClaudeToResponsesStream struct {
	model    string
	names    *ToolNames
	id       string
	created  int64
	seq      int
	next     int // next output_index
	blocks   map[int64]*responseBlock
	output   []any
	searches map[string]string // server_tool_use id -> query
	usage    Usage
	stop     string
	started  bool
	finished bool
	final    []byte
}

type responseBlock struct {
	kind    string // text, thinking, redacted, function, custom, search, search_result
	itemID  string
	outIdx  int
	text    strings.Builder
	sig     string
	callID  string
	name    string
	data    string
	toolID  string
	results gjson.Result
}

func NewClaudeToResponsesStream(model string, names *ToolNames) *ClaudeToResponsesStream {
	return &ClaudeToResponsesStream{model: model, names: names, created: time.Now().Unix(), id: "resp_" + randomID(),
		blocks: map[int64]*responseBlock{}, searches: map[string]string{}}
}

func (c *ClaudeToResponsesStream) emit(typ string, fields obj) Event {
	fields["type"] = typ
	fields["sequence_number"] = c.seq
	c.seq++
	return ev(typ, fields)
}

func (c *ClaudeToResponsesStream) response(status string) obj {
	r := obj{"id": c.id, "object": "response", "created_at": c.created, "status": status, "model": c.model,
		"output": c.output}
	if status != "in_progress" {
		r["usage"] = responsesUsage(c.usage)
	}
	return r
}

func responsesUsage(u Usage) obj {
	input := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
	return obj{"input_tokens": input, "input_tokens_details": obj{"cached_tokens": u.CacheReadTokens},
		"output_tokens": u.OutputTokens, "output_tokens_details": obj{"reasoning_tokens": u.ReasoningTokens},
		"total_tokens": input + u.OutputTokens}
}

func (c *ClaudeToResponsesStream) start() []Event {
	c.started = true
	if c.output == nil {
		c.output = []any{}
	}
	return []Event{
		c.emit("response.created", obj{"response": c.response("in_progress")}),
		c.emit("response.in_progress", obj{"response": c.response("in_progress")}),
	}
}

func (c *ClaudeToResponsesStream) add(b *responseBlock, item obj) Event {
	b.outIdx = c.next
	c.next++
	return c.emit("response.output_item.added", obj{"output_index": b.outIdx, "item": item})
}

func (c *ClaudeToResponsesStream) done(b *responseBlock, item obj) Event {
	c.output = append(c.output, item)
	return c.emit("response.output_item.done", obj{"output_index": b.outIdx, "item": item})
}

func (c *ClaudeToResponsesStream) Convert(e Event) []Event {
	d := gjson.ParseBytes(e.Data)
	var out []Event
	typ := d.Get("type").String()
	if !c.started && typ != "error" {
		if id := d.Get("message.id").String(); id != "" {
			c.id = "resp_" + strings.TrimPrefix(id, "msg_")
		}
		readClaudeUsage(d.Get("message.usage"), &c.usage)
		out = append(out, c.start()...)
		if typ == "message_start" {
			return out
		}
	}
	idx := d.Get("index").Int()
	switch typ {
	case "content_block_start":
		cb := d.Get("content_block")
		b := &responseBlock{}
		c.blocks[idx] = b
		switch cb.Get("type").String() {
		case "text":
			b.kind, b.itemID = "text", "msg_"+randomID()
			out = append(out, c.add(b, obj{"id": b.itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": arr{}}),
				c.emit("response.content_part.added", obj{"item_id": b.itemID, "output_index": b.outIdx, "content_index": 0,
					"part": obj{"type": "output_text", "text": "", "annotations": arr{}}}))
		case "thinking":
			b.kind, b.itemID = "thinking", "rs_"+randomID()
			out = append(out, c.add(b, obj{"id": b.itemID, "type": "reasoning", "summary": arr{}}),
				c.emit("response.reasoning_summary_part.added", obj{"item_id": b.itemID, "output_index": b.outIdx,
					"summary_index": 0, "part": obj{"type": "summary_text", "text": ""}}))
		case "redacted_thinking":
			b.kind, b.itemID, b.data = "redacted", "rs_"+randomID(), cb.Get("data").String()
		case "tool_use":
			b.callID, b.name = cb.Get("id").String(), c.names.Original(cb.Get("name").String())
			if c.names.IsCustom(b.name) {
				b.kind, b.itemID = "custom", "ctc_"+randomID()
				out = append(out, c.add(b, obj{"id": b.itemID, "type": "custom_tool_call", "status": "in_progress",
					"call_id": b.callID, "name": b.name, "input": ""}))
			} else {
				b.kind, b.itemID = "function", "fc_"+randomID()
				out = append(out, c.add(b, obj{"id": b.itemID, "type": "function_call", "status": "in_progress",
					"call_id": b.callID, "name": b.name, "arguments": ""}))
			}
		case "server_tool_use":
			b.kind, b.toolID = "search", cb.Get("id").String()
		case "web_search_tool_result":
			b.kind, b.toolID, b.results = "search_result", cb.Get("tool_use_id").String(), cb.Get("content")
		}
	case "content_block_delta":
		b := c.blocks[idx]
		if b == nil {
			break
		}
		delta := d.Get("delta")
		switch delta.Get("type").String() {
		case "text_delta":
			t := delta.Get("text").String()
			b.text.WriteString(t)
			out = append(out, c.emit("response.output_text.delta", obj{"item_id": b.itemID, "output_index": b.outIdx,
				"content_index": 0, "delta": t}))
		case "thinking_delta":
			t := delta.Get("thinking").String()
			b.text.WriteString(t)
			out = append(out, c.emit("response.reasoning_summary_text.delta", obj{"item_id": b.itemID,
				"output_index": b.outIdx, "summary_index": 0, "delta": t}))
		case "signature_delta":
			b.sig += delta.Get("signature").String()
		case "input_json_delta":
			t := delta.Get("partial_json").String()
			b.text.WriteString(t)
			if b.kind == "function" {
				out = append(out, c.emit("response.function_call_arguments.delta", obj{"item_id": b.itemID,
					"output_index": b.outIdx, "delta": t}))
			}
		}
	case "content_block_stop":
		b := c.blocks[idx]
		delete(c.blocks, idx)
		if b != nil {
			out = append(out, c.finishBlock(b)...)
		}
	case "message_delta":
		readClaudeUsage(d.Get("usage"), &c.usage)
		if s := d.Get("delta.stop_reason").String(); s != "" {
			c.stop = s
		}
	case "message_stop":
		c.finished = true
		status, final := "completed", "response.completed"
		resp := c.response(status)
		switch c.stop {
		case "max_tokens", "model_context_window_exceeded":
			status, final = "incomplete", "response.incomplete"
			resp = c.response(status)
			resp["incomplete_details"] = obj{"reason": "max_output_tokens"}
		case "refusal":
			status, final = "incomplete", "response.incomplete"
			resp = c.response(status)
			resp["incomplete_details"] = obj{"reason": "content_filter"}
		}
		c.final = marshal(resp)
		out = append(out, c.emit(final, obj{"response": resp}))
	case "error":
		c.finished = true
		resp := c.response("failed")
		resp["error"] = obj{"code": d.Get("error.type").String(), "message": d.Get("error.message").String()}
		c.final = marshal(resp)
		out = append(out, c.emit("response.failed", obj{"response": resp}))
	}
	return out
}

func (c *ClaudeToResponsesStream) finishBlock(b *responseBlock) []Event {
	switch b.kind {
	case "text":
		text := b.text.String()
		part := obj{"type": "output_text", "text": text, "annotations": arr{}}
		return []Event{
			c.emit("response.output_text.done", obj{"item_id": b.itemID, "output_index": b.outIdx, "content_index": 0, "text": text}),
			c.emit("response.content_part.done", obj{"item_id": b.itemID, "output_index": b.outIdx, "content_index": 0, "part": part}),
			c.done(b, obj{"id": b.itemID, "type": "message", "status": "completed", "role": "assistant", "content": arr{part}}),
		}
	case "thinking":
		text := b.text.String()
		summary := arr{}
		if text != "" {
			summary = arr{obj{"type": "summary_text", "text": text}}
		}
		return []Event{
			c.emit("response.reasoning_summary_text.done", obj{"item_id": b.itemID, "output_index": b.outIdx, "summary_index": 0, "text": text}),
			c.emit("response.reasoning_summary_part.done", obj{"item_id": b.itemID, "output_index": b.outIdx, "summary_index": 0,
				"part": obj{"type": "summary_text", "text": text}}),
			c.done(b, obj{"id": b.itemID, "type": "reasoning", "summary": summary,
				"encrypted_content": encodeClaudeReasoning(claudeReasoning{Thinking: text, Signature: b.sig})}),
		}
	case "redacted":
		item := obj{"id": b.itemID, "type": "reasoning", "summary": arr{},
			"encrypted_content": encodeClaudeReasoning(claudeReasoning{Redacted: b.data})}
		return []Event{c.add(b, item), c.done(b, item)}
	case "function":
		args := b.text.String()
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		return []Event{
			c.emit("response.function_call_arguments.done", obj{"item_id": b.itemID, "output_index": b.outIdx, "arguments": args}),
			c.done(b, obj{"id": b.itemID, "type": "function_call", "status": "completed", "call_id": b.callID,
				"name": b.name, "arguments": args}),
		}
	case "custom":
		input := gjson.Get(b.text.String(), "input").String()
		return []Event{c.done(b, obj{"id": b.itemID, "type": "custom_tool_call", "status": "completed",
			"call_id": b.callID, "name": b.name, "input": input})}
	case "search":
		c.searches[b.toolID] = gjson.Get(b.text.String(), "query").String()
	case "search_result":
		sources := arr{}
		b.results.ForEach(func(_, r gjson.Result) bool {
			if url := r.Get("url").String(); url != "" {
				sources = append(sources, obj{"type": "url", "url": url})
			}
			return true
		})
		item := obj{"id": "ws_" + randomID(), "type": "web_search_call", "status": "completed",
			"action": obj{"type": "search", "query": c.searches[b.toolID], "sources": sources}}
		return []Event{c.add(b, item), c.done(b, item)}
	}
	return nil
}

func (c *ClaudeToResponsesStream) Finish() []Event {
	if c.finished {
		return nil
	}
	return []Event{ev("error", obj{"type": "error", "code": "stream_truncated",
		"message": "upstream stream ended before the response finished", "sequence_number": c.seq})}
}

// ClaudeMessageToResponse converts a complete Claude message into a Responses object.
func ClaudeMessageToResponse(msg []byte, model string, names *ToolNames) []byte {
	m := gjson.ParseBytes(msg)
	c := NewClaudeToResponsesStream(model, names)
	feed := func(v obj) { c.Convert(Event{Data: marshal(v)}) }
	usage := any(obj{})
	if u := m.Get("usage"); u.IsObject() {
		usage = json.RawMessage(u.Raw)
	}
	feed(obj{"type": "message_start", "message": obj{"id": m.Get("id").String(), "usage": usage}})
	for i, b := range m.Get("content").Array() {
		start := obj{}
		_ = json.Unmarshal([]byte(b.Raw), &start)
		var deltas []obj
		switch b.Get("type").String() {
		case "text":
			start = obj{"type": "text", "text": ""}
			deltas = append(deltas, obj{"type": "text_delta", "text": b.Get("text").String()})
		case "thinking":
			start = obj{"type": "thinking", "thinking": ""}
			deltas = append(deltas, obj{"type": "thinking_delta", "thinking": b.Get("thinking").String()},
				obj{"type": "signature_delta", "signature": b.Get("signature").String()})
		case "tool_use", "server_tool_use":
			start["input"] = obj{}
			if in := b.Get("input"); in.IsObject() {
				deltas = append(deltas, obj{"type": "input_json_delta", "partial_json": in.Raw})
			}
		}
		feed(obj{"type": "content_block_start", "index": i, "content_block": start})
		for _, dl := range deltas {
			feed(obj{"type": "content_block_delta", "index": i, "delta": dl})
		}
		feed(obj{"type": "content_block_stop", "index": i})
	}
	feed(obj{"type": "message_delta", "delta": obj{"stop_reason": m.Get("stop_reason").String()}, "usage": usage})
	feed(obj{"type": "message_stop"})
	return c.final
}
