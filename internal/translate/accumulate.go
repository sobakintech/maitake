package translate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ClaudeAccumulator rebuilds the final Claude message from its SSE stream and tracks usage.
type ClaudeAccumulator struct {
	message obj
	blocks  map[int64]obj
	partial map[int64]*strings.Builder
	Usage   Usage
	Err     []byte // body of an upstream error event, if any
	Done    bool   // the message reached a stop reason
	// ServiceTier is the tier Anthropic reports serving the request at.
	ServiceTier string
}

func NewClaudeAccumulator() *ClaudeAccumulator {
	return &ClaudeAccumulator{message: obj{}, blocks: map[int64]obj{}, partial: map[int64]*strings.Builder{}}
}

func decodeObj(raw string) obj {
	var o obj
	_ = json.Unmarshal([]byte(raw), &o)
	if o == nil {
		o = obj{}
	}
	return o
}

func (a *ClaudeAccumulator) Add(e Event) {
	d := gjson.ParseBytes(e.Data)
	switch d.Get("type").String() {
	case "message_start":
		a.message = decodeObj(d.Get("message").Raw)
		readClaudeUsage(d.Get("message.usage"), &a.Usage)
		if t := d.Get("message.usage.service_tier").String(); t != "" {
			a.ServiceTier = t
		}
	case "content_block_start":
		a.blocks[d.Get("index").Int()] = decodeObj(d.Get("content_block").Raw)
	case "content_block_delta":
		idx := d.Get("index").Int()
		b := a.blocks[idx]
		if b == nil {
			return
		}
		delta := d.Get("delta")
		switch delta.Get("type").String() {
		case "text_delta":
			b["text"] = asString(b["text"]) + delta.Get("text").String()
		case "thinking_delta":
			b["thinking"] = asString(b["thinking"]) + delta.Get("thinking").String()
		case "signature_delta":
			b["signature"] = asString(b["signature"]) + delta.Get("signature").String()
		case "input_json_delta":
			sb := a.partial[idx]
			if sb == nil {
				sb = &strings.Builder{}
				a.partial[idx] = sb
			}
			sb.WriteString(delta.Get("partial_json").String())
		case "citations_delta":
			cits, _ := b["citations"].([]any)
			b["citations"] = append(cits, decodeObj(delta.Get("citation").Raw))
		}
	case "content_block_stop":
		idx := d.Get("index").Int()
		if sb := a.partial[idx]; sb != nil {
			if b := a.blocks[idx]; b != nil {
				var input any
				if json.Unmarshal([]byte(sb.String()), &input) == nil {
					b["input"] = input
				}
			}
			delete(a.partial, idx)
		}
	case "message_delta":
		for k, v := range decodeObj(d.Get("delta").Raw) {
			a.message[k] = v
		}
		if d.Get("delta.stop_reason").String() != "" {
			a.Done = true
		}
		readClaudeUsage(d.Get("usage"), &a.Usage)
	case "error":
		a.Err = e.Data
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// Message returns the assembled message JSON.
func (a *ClaudeAccumulator) Message() []byte {
	idx := make([]int64, 0, len(a.blocks))
	for i := range a.blocks {
		idx = append(idx, i)
	}
	sort.Slice(idx, func(i, j int) bool { return idx[i] < idx[j] })
	content := arr{}
	for _, i := range idx {
		content = append(content, a.blocks[i])
	}
	a.message["content"] = content
	usage := obj{"input_tokens": a.Usage.InputTokens, "output_tokens": a.Usage.OutputTokens,
		"cache_read_input_tokens": a.Usage.CacheReadTokens, "cache_creation_input_tokens": a.Usage.CacheWriteTokens}
	if u, ok := a.message["usage"].(map[string]any); ok {
		for k, v := range usage {
			u[k] = v
		}
	} else {
		a.message["usage"] = usage
	}
	return marshal(a.message)
}

// ResponsesAccumulator rebuilds the final Responses object and tracks usage.
type ResponsesAccumulator struct {
	response obj
	items    map[int64]json.RawMessage
	Usage    Usage
	Done     bool
	Err      []byte
	// ServiceTier is the tier Codex reports serving the request at.
	ServiceTier string
}

func NewResponsesAccumulator() *ResponsesAccumulator {
	return &ResponsesAccumulator{response: obj{}, items: map[int64]json.RawMessage{}}
}

func (a *ResponsesAccumulator) Add(e Event) {
	d := gjson.ParseBytes(e.Data)
	t := d.Get("type").String()
	switch t {
	case "response.created", "response.in_progress":
		a.response = decodeObj(d.Get("response").Raw)
	case "response.output_item.done":
		a.items[d.Get("output_index").Int()] = json.RawMessage(d.Get("item").Raw)
	case "response.completed", "response.incomplete", "response.failed":
		a.response = decodeObj(d.Get("response").Raw)
		readResponsesUsage(d.Get("response.usage"), &a.Usage)
		a.Done = true
		if t := d.Get("response.service_tier").String(); t != "" {
			a.ServiceTier = t
		}
		if t == "response.failed" {
			a.Err = []byte(d.Get("response.error").Raw)
		}
	case "error":
		a.Err = e.Data
	}
}

// Response returns the final response; output is rebuilt from streamed items when the
// terminal event omits it.
func (a *ResponsesAccumulator) Response() []byte {
	if out, ok := a.response["output"].([]any); !ok || len(out) == 0 {
		idx := make([]int64, 0, len(a.items))
		for i := range a.items {
			idx = append(idx, i)
		}
		sort.Slice(idx, func(i, j int) bool { return idx[i] < idx[j] })
		items := arr{}
		for _, i := range idx {
			items = append(items, a.items[i])
		}
		a.response["output"] = items
	}
	return marshal(a.response)
}

// ResponsesUsage reads a Responses API usage object.
func ResponsesUsage(u gjson.Result) Usage {
	var out Usage
	readResponsesUsage(u, &out)
	return out
}

func readResponsesUsage(u gjson.Result, dst *Usage) {
	if !u.Exists() {
		return
	}
	cached := u.Get("input_tokens_details.cached_tokens").Int()
	dst.InputTokens = u.Get("input_tokens").Int() - cached
	if dst.InputTokens < 0 {
		dst.InputTokens = 0
	}
	dst.CacheReadTokens = cached
	dst.OutputTokens = u.Get("output_tokens").Int()
	dst.ReasoningTokens = u.Get("output_tokens_details.reasoning_tokens").Int()
}

// Signatures lists the thinking signatures in the assembled message.
func (a *ClaudeAccumulator) Signatures() []string {
	var out []string
	for _, b := range a.blocks {
		switch b["type"] {
		case "thinking":
			if s := asString(b["signature"]); s != "" {
				out = append(out, s)
			}
		case "redacted_thinking":
			if s := asString(b["data"]); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Signatures lists the encrypted reasoning in the response's output items.
func (a *ResponsesAccumulator) Signatures() []string {
	var out []string
	for _, raw := range a.items {
		if s := gjson.GetBytes(raw, "encrypted_content").String(); s != "" {
			out = append(out, s)
		}
	}
	if items, ok := a.response["output"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if s := asString(m["encrypted_content"]); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
