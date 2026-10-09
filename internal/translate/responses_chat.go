package translate

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// LooksLikeResponses reports a Responses API body sent to the Chat Completions endpoint.
func LooksLikeResponses(body []byte) bool {
	return !gjson.GetBytes(body, "messages").Exists() &&
		(gjson.GetBytes(body, "input").Exists() || gjson.GetBytes(body, "instructions").Exists())
}

// ResponsesRequestToChat converts a Responses API request into a Chat Completions request.
func ResponsesRequestToChat(body []byte) []byte {
	req := gjson.ParseBytes(body)
	var msgs arr
	if s := req.Get("instructions").String(); s != "" {
		msgs = append(msgs, obj{"role": "system", "content": s})
	}
	appendCall := func(call obj) {
		if n := len(msgs); n > 0 {
			if last := msgs[n-1].(obj); last["role"] == "assistant" {
				calls, _ := last["tool_calls"].(arr)
				last["tool_calls"] = append(calls, call)
				return
			}
		}
		msgs = append(msgs, obj{"role": "assistant", "content": nil, "tool_calls": arr{call}})
	}

	in := req.Get("input")
	if in.Type == gjson.String {
		msgs = append(msgs, obj{"role": "user", "content": in.String()})
	}
	for _, it := range in.Array() {
		switch it.Get("type").String() {
		case "message", "":
			role := it.Get("role").String()
			if role == "developer" {
				role = "system"
			}
			content := it.Get("content")
			if content.Type == gjson.String {
				msgs = append(msgs, obj{"role": role, "content": content.String()})
				continue
			}
			var parts arr
			for _, p := range content.Array() {
				switch p.Get("type").String() {
				case "input_text", "output_text", "text":
					parts = append(parts, obj{"type": "text", "text": p.Get("text").String()})
				case "input_image":
					url := p.Get("image_url").String()
					if url == "" {
						url = p.Get("image_url.url").String()
					}
					parts = append(parts, obj{"type": "image_url", "image_url": obj{"url": url}})
				case "input_file":
					parts = append(parts, obj{"type": "file", "file": obj{"file_data": p.Get("file_data").String(),
						"filename": p.Get("filename").String()}})
				}
			}
			msgs = append(msgs, obj{"role": role, "content": parts})
		case "function_call":
			appendCall(obj{"id": it.Get("call_id").String(), "type": "function",
				"function": obj{"name": it.Get("name").String(), "arguments": it.Get("arguments").String()}})
		case "function_call_output":
			out := it.Get("output")
			var content any = out.String()
			if out.IsArray() {
				content = json.RawMessage(out.Raw)
			}
			msgs = append(msgs, obj{"role": "tool", "tool_call_id": it.Get("call_id").String(), "content": content})
		}
	}

	out := obj{"model": req.Get("model").String(), "messages": msgs}
	for _, k := range []string{"stream", "temperature", "top_p", "parallel_tool_calls", "service_tier", "metadata", "user"} {
		if v := req.Get(k); v.Exists() {
			out[k] = json.RawMessage(v.Raw)
		}
	}
	if v := req.Get("max_output_tokens"); v.Exists() {
		out["max_tokens"] = v.Int()
	}
	if e := req.Get("reasoning.effort").String(); e != "" {
		out["reasoning_effort"] = e
	}
	var tools arr
	for _, t := range req.Get("tools").Array() {
		if t.Get("type").String() != "function" {
			tools = append(tools, json.RawMessage(t.Raw))
			continue
		}
		fn := obj{"name": t.Get("name").String()}
		if d := t.Get("description").String(); d != "" {
			fn["description"] = d
		}
		if p := t.Get("parameters"); p.Exists() {
			fn["parameters"] = json.RawMessage(p.Raw)
		}
		if t.Get("strict").Bool() {
			fn["strict"] = true
		}
		tools = append(tools, obj{"type": "function", "function": fn})
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if tc := req.Get("tool_choice"); tc.Type == gjson.String {
		out["tool_choice"] = tc.String()
	} else if tc.Get("type").String() == "function" {
		out["tool_choice"] = obj{"type": "function", "function": obj{"name": tc.Get("name").String()}}
	}
	switch f := req.Get("text.format"); f.Get("type").String() {
	case "json_schema":
		js := obj{"name": f.Get("name").String(), "strict": f.Get("strict").Bool()}
		if s := f.Get("schema"); s.Exists() {
			js["schema"] = json.RawMessage(s.Raw)
		}
		out["response_format"] = obj{"type": "json_schema", "json_schema": js}
	case "json_object":
		out["response_format"] = obj{"type": "json_object"}
	}
	return marshal(out)
}
