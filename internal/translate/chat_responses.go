package translate

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// ChatToResponses converts a Chat Completions request into a Responses request for Codex.
func ChatToResponses(body []byte, model, effort string, names *ToolNames) ([]byte, error) {
	req := gjson.ParseBytes(body)
	var input arr
	for _, m := range req.Get("messages").Array() {
		role := m.Get("role").String()
		switch role {
		case "system", "developer":
			if parts := responsesParts(m.Get("content"), "input_text"); len(parts) > 0 {
				input = append(input, obj{"type": "message", "role": "developer", "content": parts})
			}
		case "user":
			if parts := responsesParts(m.Get("content"), "input_text"); len(parts) > 0 {
				input = append(input, obj{"type": "message", "role": "user", "content": parts})
			}
		case "assistant":
			if parts := responsesParts(m.Get("content"), "output_text"); len(parts) > 0 {
				input = append(input, obj{"type": "message", "role": "assistant", "content": parts})
			}
			for _, tc := range m.Get("tool_calls").Array() {
				args := tc.Get("function.arguments").String()
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				input = append(input, obj{"type": "function_call", "call_id": shortCallID(tc.Get("id").String()),
					"name": names.Short(tc.Get("function.name").String()), "arguments": args})
			}
		case "tool", "function":
			content := m.Get("content")
			output := any(content.String())
			if content.IsArray() {
				output = responsesParts(content, "input_text")
			}
			input = append(input, obj{"type": "function_call_output",
				"call_id": shortCallID(m.Get("tool_call_id").String()), "output": output})
		}
	}

	out := obj{"model": model, "instructions": "", "input": input, "stream": true, "store": false,
		"include": arr{"reasoning.encrypted_content"}}

	if effort == "" {
		effort = codexEffort(strings.ToLower(req.Get("reasoning_effort").String()))
	}
	if effort == "" {
		effort = "medium"
	}
	out["reasoning"] = obj{"effort": effort, "summary": "auto"}
	if tier := req.Get("service_tier").String(); tier != "" {
		out["service_tier"] = tier
	}

	if tools := req.Get("tools").Array(); len(tools) > 0 {
		var rt arr
		for _, t := range tools {
			if t.Get("type").String() != "function" {
				rt = append(rt, json.RawMessage(t.Raw))
				continue
			}
			fn := t.Get("function")
			tool := obj{"type": "function", "name": names.Short(fn.Get("name").String()), "strict": fn.Get("strict").Bool()}
			if d := fn.Get("description").String(); d != "" {
				tool["description"] = d
			}
			if p := fn.Get("parameters"); p.IsObject() {
				tool["parameters"] = json.RawMessage(p.Raw)
			} else {
				tool["parameters"] = obj{"type": "object", "properties": obj{}}
			}
			rt = append(rt, tool)
		}
		out["tools"] = rt
		out["parallel_tool_calls"] = !req.Get("parallel_tool_calls").Exists() || req.Get("parallel_tool_calls").Bool()
		tc := req.Get("tool_choice")
		if tc.Type == gjson.String {
			out["tool_choice"] = tc.String()
		} else if name := tc.Get("function.name").String(); name != "" {
			out["tool_choice"] = obj{"type": "function", "name": names.Short(name)}
		}
	}

	switch rf := req.Get("response_format"); rf.Get("type").String() {
	case "json_schema":
		js := rf.Get("json_schema")
		format := obj{"type": "json_schema", "name": js.Get("name").String(), "strict": js.Get("strict").Bool()}
		if s := js.Get("schema"); s.Exists() {
			format["schema"] = json.RawMessage(s.Raw)
		}
		out["text"] = obj{"format": format}
	case "json_object":
		out["text"] = obj{"format": obj{"type": "json_object"}}
	}
	if v := req.Get("verbosity").String(); v != "" {
		text, _ := out["text"].(obj)
		if text == nil {
			text = obj{}
		}
		text["verbosity"] = v
		out["text"] = text
	}
	return marshal(out), nil
}

func responsesParts(content gjson.Result, textType string) arr {
	var out arr
	if content.Type == gjson.String {
		if s := content.String(); s != "" {
			out = append(out, obj{"type": textType, "text": s})
		}
		return out
	}
	for _, p := range content.Array() {
		switch p.Get("type").String() {
		case "text", "input_text", "output_text":
			if s := p.Get("text").String(); s != "" {
				out = append(out, obj{"type": textType, "text": s})
			}
		case "image_url":
			url := p.Get("image_url.url").String()
			if url == "" {
				url = p.Get("image_url").String()
			}
			if url != "" {
				out = append(out, obj{"type": "input_image", "image_url": url})
			}
		case "file":
			f := obj{"type": "input_file"}
			if v := p.Get("file.file_data").String(); v != "" {
				f["file_data"] = v
			}
			if v := p.Get("file.file_id").String(); v != "" {
				f["file_id"] = v
			}
			name := p.Get("file.filename").String()
			if name == "" {
				name = "file.pdf"
			}
			f["filename"] = name
			out = append(out, f)
		}
	}
	return out
}

// ResponsesToChatStream turns Responses SSE into Chat Completions chunks.
type ResponsesToChatStream struct {
	model     string
	opts      ChatOptions
	names     *ToolNames
	id        string
	created   int64
	roleSent  bool
	toolIndex map[string]int // item id -> tool index
	argsSeen  map[string]bool
	nextTool  int
	sawTool   bool
	usage     Usage
	finished  bool
	runes     int // text emitted so far, for citation offsets
	partBase  int // runes emitted before the current text part
}

func NewResponsesToChatStream(model string, opts ChatOptions, names *ToolNames) *ResponsesToChatStream {
	return &ResponsesToChatStream{model: model, opts: opts, names: names, created: time.Now().Unix(),
		id: "chatcmpl-" + randomID(), toolIndex: map[string]int{}, argsSeen: map[string]bool{}}
}

func (c *ResponsesToChatStream) chunk(delta obj, finish any) []Event {
	var out []Event
	if !c.roleSent {
		c.roleSent = true
		if _, hasRole := delta["role"]; !hasRole {
			out = append(out, c.raw(obj{"role": "assistant", "content": ""}, nil))
		}
	}
	return append(out, c.raw(delta, finish))
}

func (c *ResponsesToChatStream) raw(delta obj, finish any) Event {
	return ev("", obj{"id": c.id, "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": arr{obj{"index": 0, "delta": delta, "finish_reason": finish}}})
}

func (c *ResponsesToChatStream) Convert(e Event) []Event {
	d := gjson.ParseBytes(e.Data)
	switch t := d.Get("type").String(); t {
	case "response.created":
		if id := d.Get("response.id").String(); id != "" {
			c.id = "chatcmpl-" + strings.TrimPrefix(id, "resp_")
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if delta := c.opts.reasoningDelta(d.Get("delta").String()); delta != nil {
			return c.chunk(delta, nil)
		}
	case "response.reasoning_summary_part.added":
		if d.Get("summary_index").Int() > 0 {
			if delta := c.opts.reasoningDelta("\n\n"); delta != nil {
				return c.chunk(delta, nil)
			}
		}
	case "response.output_text.delta":
		text := d.Get("delta").String()
		c.runes += utf8.RuneCountInString(text)
		return c.chunk(obj{"content": text}, nil)
	case "response.content_part.added":
		c.partBase = c.runes
	case "response.output_text.annotation.added":
		if a := urlCitation(d.Get("annotation"), c.partBase); a != nil {
			return c.chunk(obj{"annotations": arr{a}}, nil)
		}
	case "response.output_item.added":
		item := d.Get("item")
		if item.Get("type").String() == "function_call" {
			idx := c.nextTool
			c.nextTool++
			c.sawTool = true
			c.toolIndex[item.Get("id").String()] = idx
			return c.chunk(obj{"tool_calls": arr{obj{"index": idx, "id": item.Get("call_id").String(), "type": "function",
				"function": obj{"name": c.names.Original(item.Get("name").String()), "arguments": ""}}}}, nil)
		}
	case "response.function_call_arguments.delta":
		itemID := d.Get("item_id").String()
		if idx, ok := c.toolIndex[itemID]; ok {
			c.argsSeen[itemID] = true
			return c.chunk(obj{"tool_calls": arr{obj{"index": idx, "function": obj{"arguments": d.Get("delta").String()}}}}, nil)
		}
	case "response.output_item.done":
		item := d.Get("item")
		itemID := item.Get("id").String()
		if idx, ok := c.toolIndex[itemID]; ok && !c.argsSeen[itemID] {
			c.argsSeen[itemID] = true
			return c.chunk(obj{"tool_calls": arr{obj{"index": idx, "function": obj{"arguments": item.Get("arguments").String()}}}}, nil)
		}
	case "response.completed", "response.incomplete":
		c.finished = true
		readResponsesUsage(d.Get("response.usage"), &c.usage)
		finish := "stop"
		if c.sawTool {
			finish = "tool_calls"
		} else if t == "response.incomplete" {
			switch d.Get("response.incomplete_details.reason").String() {
			case "max_output_tokens":
				finish = "length"
			case "content_filter":
				finish = "content_filter"
			}
		}
		out := c.chunk(obj{}, finish)
		if c.opts.IncludeUsage {
			out = append(out, ev("", obj{"id": c.id, "object": "chat.completion.chunk", "created": c.created,
				"model": c.model, "choices": arr{}, "usage": chatUsage(c.usage)}))
		}
		return out
	case "response.failed":
		c.finished = true
		return []Event{ev("", obj{"error": obj{"message": d.Get("response.error.message").String(),
			"type": d.Get("response.error.code").String()}})}
	case "error":
		c.finished = true
		msg := d.Get("message").String()
		if msg == "" {
			msg = d.Get("error.message").String()
		}
		return []Event{ev("", obj{"error": obj{"message": msg, "type": "upstream_error"}})}
	}
	return nil
}

// Finish ends the stream; one that stopped before completing is reported as an error.
func (c *ResponsesToChatStream) Finish() []Event {
	var out []Event
	if !c.finished {
		out = append(out, ev("", obj{"error": obj{"message": "upstream stream ended before the response finished",
			"type": "api_error", "code": "stream_truncated"}}))
	}
	return append(out, Event{Data: []byte("[DONE]")})
}

// urlCitation converts a Responses url_citation annotation to the Chat Completions shape.
func urlCitation(a gjson.Result, offset int) obj {
	if a.Get("type").String() != "url_citation" {
		return nil
	}
	return obj{"type": "url_citation", "url_citation": obj{"url": a.Get("url").String(), "title": a.Get("title").String(),
		"start_index": a.Get("start_index").Int() + int64(offset), "end_index": a.Get("end_index").Int() + int64(offset)}}
}

// ResponseToChat converts a final Responses object into a chat.completion.
func ResponseToChat(resp []byte, model string, names *ToolNames, opts ChatOptions) []byte {
	r := gjson.ParseBytes(resp)
	var text, reasoning strings.Builder
	var calls, annotations arr
	for _, item := range r.Get("output").Array() {
		switch item.Get("type").String() {
		case "message":
			for _, p := range item.Get("content").Array() {
				if p.Get("type").String() == "output_text" {
					base := utf8.RuneCountInString(text.String())
					p.Get("annotations").ForEach(func(_, a gjson.Result) bool {
						if c := urlCitation(a, base); c != nil {
							annotations = append(annotations, c)
						}
						return true
					})
					text.WriteString(p.Get("text").String())
				}
			}
		case "reasoning":
			for i, s := range item.Get("summary").Array() {
				if i > 0 || reasoning.Len() > 0 {
					reasoning.WriteString("\n\n")
				}
				reasoning.WriteString(s.Get("text").String())
			}
		case "function_call":
			calls = append(calls, obj{"id": item.Get("call_id").String(), "type": "function",
				"function": obj{"name": names.Original(item.Get("name").String()), "arguments": item.Get("arguments").String()}})
		}
	}
	message := obj{"role": "assistant", "content": text.String()}
	if len(annotations) > 0 {
		message["annotations"] = annotations
	}
	if reasoning.Len() > 0 {
		for k, v := range opts.reasoningDelta(reasoning.String()) {
			message[k] = v
		}
	}
	finish := "stop"
	if len(calls) > 0 {
		message["tool_calls"] = calls
		finish = "tool_calls"
	} else if r.Get("status").String() == "incomplete" && r.Get("incomplete_details.reason").String() == "max_output_tokens" {
		finish = "length"
	}
	var u Usage
	readResponsesUsage(r.Get("usage"), &u)
	return marshal(obj{
		"id":      "chatcmpl-" + strings.TrimPrefix(r.Get("id").String(), "resp_"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": arr{obj{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   chatUsage(u),
	})
}
