// Package translate converts requests and streamed responses between the OpenAI Chat
// Completions, Anthropic Messages and OpenAI Responses formats.
package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Event is one server-sent event. Name may be empty (OpenAI chat chunks).
type Event struct {
	Name string
	Data []byte
}

// SSEReader reads server-sent events from an upstream body.
type SSEReader struct {
	r *bufio.Reader
}

func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next event, or io.EOF when the stream ends.
func (s *SSEReader) Next() (Event, error) {
	var ev Event
	var data bytes.Buffer
	hasData := false
	for {
		line, err := s.r.ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			if hasData {
				ev.Data = data.Bytes()
				return ev, nil
			}
			return ev, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if hasData {
				ev.Data = data.Bytes()
				return ev, nil
			}
			ev.Name = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.Name = value
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		}
	}
}

// Encode renders an event in SSE wire format.
func (e Event) Encode() []byte {
	var b bytes.Buffer
	if e.Name != "" {
		b.WriteString("event: ")
		b.WriteString(e.Name)
		b.WriteByte('\n')
	}
	b.WriteString("data: ")
	b.Write(e.Data)
	b.WriteString("\n\n")
	return b.Bytes()
}

// marshal encodes v without HTML escaping (model output often contains <, > and &).
func marshal(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func ev(name string, v any) Event { return Event{Name: name, Data: marshal(v)} }

// Usage is token accounting normalized across providers.
type Usage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens"`
}

// ChatOptions shape Chat Completions output.
type ChatOptions struct {
	IncludeUsage bool
	// HideReasoning drops reasoning text (OpenRouter's reasoning.exclude).
	HideReasoning bool
	// ReasoningField is where reasoning text goes: "reasoning_content", "reasoning" (OpenRouter)
	// or "both".
	ReasoningField string
}

// reasoningDelta puts reasoning text in the configured field(s), or nothing when hidden.
func (o ChatOptions) reasoningDelta(text string) obj {
	if o.HideReasoning {
		return nil
	}
	switch o.ReasoningField {
	case "reasoning":
		return obj{"reasoning": text}
	case "both":
		return obj{"reasoning_content": text, "reasoning": text}
	}
	return obj{"reasoning_content": text}
}

// Converter rewrites a stream of upstream events into client events.
type Converter interface {
	Convert(e Event) []Event
	Finish() []Event
}

type obj = map[string]any
type arr = []any
