package translate

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// CodexSignaturePrefix marks thinking signatures that carry Codex encrypted reasoning, so a
// Claude client can round-trip them back to Codex (and so they are dropped before reaching Claude).
const CodexSignaturePrefix = "mtk-codex:"

// ClaudeToResponses converts a Claude Messages request into a Responses request for Codex.
func ClaudeToResponses(body []byte, model, effort string, names *ToolNames) ([]byte, error) {
	req := gjson.ParseBytes(body)
	var input arr

	var sysParts arr
	addSys := func(t string) {
		if strings.HasPrefix(strings.TrimSpace(t), "x-anthropic-billing-header:") || strings.TrimSpace(t) == "" {
			return
		}
		sysParts = append(sysParts, obj{"type": "input_text", "text": t})
	}
	if s := req.Get("system"); s.Type == gjson.String {
		addSys(s.String())
	} else {
		for _, b := range s.Array() {
			addSys(b.Get("text").String())
		}
	}
	if len(sysParts) > 0 {
		input = append(input, obj{"type": "message", "role": "developer", "content": sysParts})
	}

	for _, m := range req.Get("messages").Array() {
		role := m.Get("role").String()
		content := m.Get("content")
		if content.Type == gjson.String {
			textType := "input_text"
			if role == "assistant" {
				textType = "output_text"
			}
			if s := content.String(); s != "" {
				r := role
				if r == "system" {
					r = "developer"
				}
				input = append(input, obj{"type": "message", "role": r, "content": arr{obj{"type": textType, "text": s}}})
			}
			continue
		}
		var parts arr
		flush := func() {
			if len(parts) == 0 {
				return
			}
			r := role
			if r == "system" {
				r = "developer"
			}
			input = append(input, obj{"type": "message", "role": r, "content": parts})
			parts = nil
		}
		for _, b := range content.Array() {
			switch b.Get("type").String() {
			case "text":
				t := b.Get("text").String()
				if t == "" {
					continue
				}
				if role == "assistant" {
					parts = append(parts, obj{"type": "output_text", "text": t})
				} else {
					parts = append(parts, obj{"type": "input_text", "text": t})
				}
			case "image":
				if url := claudeImageURL(b.Get("source")); url != "" {
					parts = append(parts, obj{"type": "input_image", "image_url": url})
				}
			case "document":
				src := b.Get("source")
				if src.Get("type").String() == "base64" {
					parts = append(parts, obj{"type": "input_file", "filename": "document.pdf",
						"file_data": "data:" + src.Get("media_type").String() + ";base64," + src.Get("data").String()})
				}
			case "thinking":
				if sig := b.Get("signature").String(); strings.HasPrefix(sig, CodexSignaturePrefix) {
					flush()
					input = append(input, obj{"type": "reasoning", "summary": arr{},
						"encrypted_content": strings.TrimPrefix(sig, CodexSignaturePrefix)})
				}
			case "tool_use":
				flush()
				args := b.Get("input").Raw
				if args == "" {
					args = "{}"
				}
				input = append(input, obj{"type": "function_call", "call_id": shortCallID(b.Get("id").String()),
					"name": names.Short(b.Get("name").String()), "arguments": args})
			case "tool_result":
				flush()
				input = append(input, obj{"type": "function_call_output",
					"call_id": shortCallID(b.Get("tool_use_id").String()), "output": toolResultOutput(b.Get("content"))})
			}
		}
		flush()
	}

	out := obj{"model": model, "instructions": "", "input": input, "stream": true, "store": false,
		"include": arr{"reasoning.encrypted_content"}}

	if effort == "" {
		switch req.Get("thinking.type").String() {
		case "enabled":
			effort = effortFromBudget(req.Get("thinking.budget_tokens").Int())
		case "adaptive", "auto":
			effort = codexEffort(req.Get("output_config.effort").String())
			if effort == "" {
				effort = "high"
			}
		case "disabled":
			effort = "low"
		}
	}
	if effort == "" {
		effort = "medium"
	}
	reasoning := obj{"effort": effort, "summary": "auto"}
	if req.Get("thinking.display").String() == "omitted" {
		delete(reasoning, "summary")
	}
	out["reasoning"] = reasoning

	if tools := req.Get("tools").Array(); len(tools) > 0 {
		var rt arr
		for _, t := range tools {
			typ := t.Get("type").String()
			if strings.HasPrefix(typ, "web_search") {
				ws := obj{"type": "web_search"}
				if d := t.Get("allowed_domains"); d.IsArray() && len(d.Array()) > 0 {
					ws["filters"] = obj{"allowed_domains": json.RawMessage(d.Raw)}
				}
				if loc := t.Get("user_location"); loc.IsObject() {
					ws["user_location"] = json.RawMessage(loc.Raw)
				}
				rt = append(rt, ws)
				out["include"] = arr{"reasoning.encrypted_content", "web_search_call.action.sources"}
				continue
			}
			if typ != "" && typ != "custom" {
				continue
			}
			tool := obj{"type": "function", "name": names.Short(t.Get("name").String()), "strict": false,
				"parameters": normalizeSchema(t.Get("input_schema"))}
			if d := t.Get("description").String(); d != "" {
				tool["description"] = d
			}
			rt = append(rt, tool)
		}
		if len(rt) > 0 {
			out["tools"] = rt
			out["parallel_tool_calls"] = !req.Get("tool_choice.disable_parallel_tool_use").Bool()
			switch tc := req.Get("tool_choice"); tc.Get("type").String() {
			case "auto":
				out["tool_choice"] = "auto"
			case "any":
				out["tool_choice"] = "required"
			case "none":
				out["tool_choice"] = "none"
			case "tool":
				out["tool_choice"] = obj{"type": "function", "name": names.Short(tc.Get("name").String())}
			}
		}
	}
	if f := req.Get("output_config.format"); f.Get("type").String() == "json_schema" {
		out["text"] = obj{"format": obj{"type": "json_schema", "name": "output", "strict": false,
			"schema": json.RawMessage(f.Get("schema").Raw)}}
	}
	if req.Get("speed").String() == "fast" {
		out["service_tier"] = "priority"
	}
	return marshal(out), nil
}

func claudeImageURL(src gjson.Result) string {
	switch src.Get("type").String() {
	case "base64":
		return "data:" + src.Get("media_type").String() + ";base64," + src.Get("data").String()
	case "url":
		return src.Get("url").String()
	}
	return ""
}

func toolResultOutput(content gjson.Result) any {
	if content.Type == gjson.String || !content.Exists() {
		return content.String()
	}
	var parts arr
	allText := true
	var text strings.Builder
	for _, p := range content.Array() {
		switch p.Get("type").String() {
		case "text":
			parts = append(parts, obj{"type": "input_text", "text": p.Get("text").String()})
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(p.Get("text").String())
		case "image":
			allText = false
			if url := claudeImageURL(p.Get("source")); url != "" {
				parts = append(parts, obj{"type": "input_image", "image_url": url})
			}
		}
	}
	if allText {
		return text.String()
	}
	return parts
}

// normalizeSchema strips keys the Responses API rejects from a JSON schema.
func normalizeSchema(s gjson.Result) any {
	if !s.IsObject() {
		return obj{"type": "object", "properties": obj{}}
	}
	var m map[string]any
	if json.Unmarshal([]byte(s.Raw), &m) != nil {
		return obj{"type": "object", "properties": obj{}}
	}
	delete(m, "$schema")
	delete(m, "$id")
	if m["type"] == "object" && m["properties"] == nil {
		m["properties"] = obj{}
	}
	return m
}

// ResponsesToClaudeStream turns Responses SSE into Claude Messages SSE.
type ResponsesToClaudeStream struct {
	model    string
	names    *ToolNames
	started  bool
	index    int
	open     map[int64]*openBlock // output_index -> block
	sawTool  bool
	usage    Usage
	finished bool
}

type openBlock struct {
	index    int
	kind     string // thinking, text, tool_use
	started  bool
	argsSeen bool
	parts    int
}

func NewResponsesToClaudeStream(model string, names *ToolNames) *ResponsesToClaudeStream {
	return &ResponsesToClaudeStream{model: model, names: names, open: map[int64]*openBlock{}}
}

func (c *ResponsesToClaudeStream) messageStart(id string) Event {
	c.started = true
	if id == "" {
		id = "msg_" + randomID()
	}
	return ev("message_start", obj{"type": "message_start", "message": obj{"id": id, "type": "message",
		"role": "assistant", "model": c.model, "content": arr{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": obj{"input_tokens": 0, "output_tokens": 0}}})
}

func (c *ResponsesToClaudeStream) startBlock(b *openBlock, contentBlock obj) []Event {
	if b.started {
		return nil
	}
	b.started = true
	b.index = c.index
	c.index++
	return []Event{ev("content_block_start", obj{"type": "content_block_start", "index": b.index, "content_block": contentBlock})}
}

func (c *ResponsesToClaudeStream) delta(b *openBlock, delta obj) Event {
	return ev("content_block_delta", obj{"type": "content_block_delta", "index": b.index, "delta": delta})
}

func (c *ResponsesToClaudeStream) stop(b *openBlock) Event {
	return ev("content_block_stop", obj{"type": "content_block_stop", "index": b.index})
}

func (c *ResponsesToClaudeStream) Convert(e Event) []Event {
	d := gjson.ParseBytes(e.Data)
	var out []Event
	if !c.started {
		out = append(out, c.messageStart("msg_"+strings.TrimPrefix(d.Get("response.id").String(), "resp_")))
	}
	outIdx := d.Get("output_index").Int()
	switch t := d.Get("type").String(); t {
	case "response.output_item.added":
		item := d.Get("item")
		switch item.Get("type").String() {
		case "reasoning":
			c.open[outIdx] = &openBlock{kind: "thinking"}
		case "message":
			c.open[outIdx] = &openBlock{kind: "text"}
		case "function_call":
			b := &openBlock{kind: "tool_use"}
			c.open[outIdx] = b
			c.sawTool = true
			out = append(out, c.startBlock(b, obj{"type": "tool_use", "id": item.Get("call_id").String(),
				"name": c.names.Original(item.Get("name").String()), "input": obj{}})...)
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if b := c.open[outIdx]; b != nil {
			out = append(out, c.startBlock(b, obj{"type": "thinking", "thinking": "", "signature": ""})...)
			out = append(out, c.delta(b, obj{"type": "thinking_delta", "thinking": d.Get("delta").String()}))
		}
	case "response.reasoning_summary_part.added":
		if b := c.open[outIdx]; b != nil {
			b.parts++
			if b.parts > 1 && b.started {
				out = append(out, c.delta(b, obj{"type": "thinking_delta", "thinking": "\n\n"}))
			}
		}
	case "response.output_text.delta":
		if b := c.open[outIdx]; b != nil {
			out = append(out, c.startBlock(b, obj{"type": "text", "text": ""})...)
			out = append(out, c.delta(b, obj{"type": "text_delta", "text": d.Get("delta").String()}))
		}
	case "response.function_call_arguments.delta":
		if b := c.open[outIdx]; b != nil {
			b.argsSeen = true
			out = append(out, c.delta(b, obj{"type": "input_json_delta", "partial_json": d.Get("delta").String()}))
		}
	case "response.output_item.done":
		if item := d.Get("item"); item.Get("type").String() == "web_search_call" {
			out = append(out, c.webSearch(item)...)
			break
		}
		b := c.open[outIdx]
		if b == nil {
			break
		}
		delete(c.open, outIdx)
		item := d.Get("item")
		switch b.kind {
		case "thinking":
			enc := item.Get("encrypted_content").String()
			if !b.started && enc == "" {
				return out
			}
			out = append(out, c.startBlock(b, obj{"type": "thinking", "thinking": "", "signature": ""})...)
			if enc != "" {
				out = append(out, c.delta(b, obj{"type": "signature_delta", "signature": CodexSignaturePrefix + enc}))
			}
		case "tool_use":
			if !b.argsSeen {
				out = append(out, c.delta(b, obj{"type": "input_json_delta", "partial_json": item.Get("arguments").String()}))
			}
		}
		if b.started {
			out = append(out, c.stop(b))
		}
	case "response.completed", "response.incomplete":
		readResponsesUsage(d.Get("response.usage"), &c.usage)
		stop := "end_turn"
		if c.sawTool {
			stop = "tool_use"
		} else if t == "response.incomplete" {
			switch d.Get("response.incomplete_details.reason").String() {
			case "max_output_tokens":
				stop = "max_tokens"
			case "content_filter":
				stop = "refusal"
			}
		}
		c.finished = true
		out = append(out,
			ev("message_delta", obj{"type": "message_delta", "delta": obj{"stop_reason": stop, "stop_sequence": nil},
				"usage": claudeUsage(c.usage)}),
			ev("message_stop", obj{"type": "message_stop"}))
	case "response.failed":
		c.finished = true
		out = append(out, claudeError("api_error", d.Get("response.error.message").String()))
	case "error":
		c.finished = true
		msg := d.Get("message").String()
		if msg == "" {
			msg = d.Get("error.message").String()
		}
		out = append(out, claudeError("api_error", msg))
	}
	return out
}

// webSearch emits a finished Codex web search as Claude's server_tool_use and result blocks.
func (c *ResponsesToClaudeStream) webSearch(item gjson.Result) []Event {
	var out []Event
	for _, block := range webSearchBlocks(item) {
		b := &openBlock{}
		start := block
		var delta obj
		if block["type"] == "server_tool_use" {
			start = obj{"type": "server_tool_use", "id": block["id"], "name": "web_search", "input": obj{}}
			delta = obj{"type": "input_json_delta", "partial_json": string(marshal(block["input"]))}
		}
		out = append(out, c.startBlock(b, start)...)
		if delta != nil {
			out = append(out, c.delta(b, delta))
		}
		out = append(out, c.stop(b))
	}
	return out
}

// webSearchBlocks turns a web_search_call item into Claude server_tool_use and
// web_search_tool_result blocks.
func webSearchBlocks(item gjson.Result) []obj {
	id := "srvtoolu_" + strings.TrimPrefix(item.Get("id").String(), "ws_")
	query := item.Get("action.query").String()
	if query == "" {
		query = item.Get("action.queries.0").String()
	}
	results := arr{}
	item.Get("action.sources").ForEach(func(_, src gjson.Result) bool {
		if url := src.Get("url").String(); url != "" {
			results = append(results, obj{"type": "web_search_result", "url": url, "title": src.Get("title").String(),
				"encrypted_content": "", "page_age": nil})
		}
		return true
	})
	return []obj{
		{"type": "server_tool_use", "id": id, "name": "web_search", "input": obj{"query": query}},
		{"type": "web_search_tool_result", "tool_use_id": id, "content": results},
	}
}

func (c *ResponsesToClaudeStream) Finish() []Event {
	if c.finished {
		return nil
	}
	return []Event{claudeError("api_error", "upstream stream ended before completion")}
}

func claudeError(typ, msg string) Event {
	return ev("error", obj{"type": "error", "error": obj{"type": typ, "message": msg}})
}

func claudeUsage(u Usage) obj {
	out := obj{"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens,
		"cache_read_input_tokens": u.CacheReadTokens, "cache_creation_input_tokens": u.CacheWriteTokens}
	return out
}

// ResponseToClaudeMessage converts a final Responses object into a Claude message.
func ResponseToClaudeMessage(resp []byte, model string, names *ToolNames) []byte {
	r := gjson.ParseBytes(resp)
	content := arr{}
	sawTool := false
	for _, item := range r.Get("output").Array() {
		switch item.Get("type").String() {
		case "reasoning":
			var text []string
			for _, s := range item.Get("summary").Array() {
				text = append(text, s.Get("text").String())
			}
			enc := item.Get("encrypted_content").String()
			if len(text) == 0 && enc == "" {
				continue
			}
			sig := ""
			if enc != "" {
				sig = CodexSignaturePrefix + enc
			}
			content = append(content, obj{"type": "thinking", "thinking": strings.Join(text, "\n\n"), "signature": sig})
		case "message":
			for _, p := range item.Get("content").Array() {
				if p.Get("type").String() == "output_text" {
					content = append(content, obj{"type": "text", "text": p.Get("text").String()})
				}
			}
		case "web_search_call":
			for _, b := range webSearchBlocks(item) {
				content = append(content, b)
			}
		case "function_call":
			sawTool = true
			var input any = obj{}
			_ = json.Unmarshal([]byte(item.Get("arguments").String()), &input)
			content = append(content, obj{"type": "tool_use", "id": item.Get("call_id").String(),
				"name": names.Original(item.Get("name").String()), "input": input})
		}
	}
	stop := "end_turn"
	if sawTool {
		stop = "tool_use"
	} else if r.Get("status").String() == "incomplete" && r.Get("incomplete_details.reason").String() == "max_output_tokens" {
		stop = "max_tokens"
	}
	var u Usage
	readResponsesUsage(r.Get("usage"), &u)
	return marshal(obj{"id": "msg_" + strings.TrimPrefix(r.Get("id").String(), "resp_"), "type": "message",
		"role": "assistant", "model": model, "content": content, "stop_reason": stop, "stop_sequence": nil,
		"usage": claudeUsage(u)})
}
