package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sanitize removes or rewrites request parts the Codex backend rejects.
func sanitize(body []byte) []byte {
	body = stripCacheBreakpoints(body)
	body = normalizeToolChoice(body)
	body = sanitizeToolSchemas(body)
	return sanitizeInputIDs(body)
}

// decode keeps numbers exact so re-encoded schemas don't change.
func decode(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

// stripCacheBreakpoints drops per-item prompt_cache_breakpoint hints (sent by Copilot CLI).
func stripCacheBreakpoints(body []byte) []byte {
	if !bytes.Contains(body, []byte(`"prompt_cache_breakpoint"`)) {
		return body
	}
	var root map[string]any
	if decode(body, &root) != nil {
		return body
	}
	items, _ := root["input"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		delete(m, "prompt_cache_breakpoint")
		for _, key := range []string{"content", "output"} {
			parts, _ := m[key].([]any)
			for _, p := range parts {
				if pm, ok := p.(map[string]any); ok {
					delete(pm, "prompt_cache_breakpoint")
				}
			}
		}
	}
	out, err := sjson.SetBytes(body, "input", items)
	if err != nil {
		return body
	}
	return out
}

func normalizeToolChoice(body []byte) []byte {
	if strings.HasPrefix(gjson.GetBytes(body, "tool_choice.type").String(), "web_search_preview") {
		body, _ = sjson.SetBytes(body, "tool_choice.type", "web_search")
	}
	return body
}

// sanitizeToolSchemas removes regex patterns Codex's validator can't compile and turns large
// unions of constants (common in MCP schemas) into enums, either of which can abort a request.
func sanitizeToolSchemas(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body
	}
	var list []any
	if decode([]byte(tools.Raw), &list) != nil {
		return body
	}
	changed := false
	var visitTools func([]any)
	visitTools = func(ts []any) {
		for _, t := range ts {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if tm["type"] == "namespace" {
				if sub, ok := tm["tools"].([]any); ok {
					visitTools(sub)
				}
				continue
			}
			if params, ok := tm["parameters"].(map[string]any); ok && cleanSchema(params) {
				changed = true
			}
		}
	}
	visitTools(list)
	if !changed {
		return body
	}
	out, err := sjson.SetBytes(body, "tools", list)
	if err != nil {
		return body
	}
	return out
}

var (
	schemaMaps   = []string{"properties", "$defs", "definitions", "patternProperties", "dependentSchemas"}
	schemaValues = []string{"items", "prefixItems", "contains", "additionalProperties", "propertyNames",
		"unevaluatedProperties", "unevaluatedItems", "additionalItems", "anyOf", "oneOf", "allOf", "not", "if", "then", "else"}
)

// badPattern reports regexes with \p{..} / \P{..} escapes or \0, which Codex rejects.
func badPattern(p string) bool {
	for i := 0; i+1 < len(p); i++ {
		if p[i] != '\\' {
			continue
		}
		next := p[i+1]
		if (next == 'p' || next == 'P') && i+2 < len(p) && p[i+2] == '{' || next == '0' {
			return true
		}
		i++
	}
	return false
}

func cleanSchema(v any) bool {
	changed := false
	switch s := v.(type) {
	case map[string]any:
		if p, ok := s["pattern"].(string); ok && badPattern(p) {
			delete(s, "pattern")
			changed = true
		}
		if pp, ok := s["patternProperties"].(map[string]any); ok {
			for k := range pp {
				if badPattern(k) {
					delete(pp, k)
					changed = true
				}
			}
		}
		if constUnionToEnum(s) {
			changed = true
		}
		for _, k := range schemaMaps {
			if m, ok := s[k].(map[string]any); ok {
				for _, sub := range m {
					if cleanSchema(sub) {
						changed = true
					}
				}
			}
		}
		for _, k := range schemaValues {
			if cleanSchema(s[k]) {
				changed = true
			}
		}
	case []any:
		for _, it := range s {
			if cleanSchema(it) {
				changed = true
			}
		}
	}
	return changed
}

// constUnionToEnum rewrites a oneOf/anyOf of 8+ plain {const} branches as an enum.
func constUnionToEnum(s map[string]any) bool {
	_, hasOne := s["oneOf"]
	_, hasAny := s["anyOf"]
	if hasOne == hasAny {
		return false
	}
	key := "oneOf"
	if hasAny {
		key = "anyOf"
	}
	branches, ok := s[key].([]any)
	if !ok || len(branches) < 8 {
		return false
	}
	values := make([]any, 0, len(branches))
	seen := map[string]bool{}
	for _, b := range branches {
		bm, ok := b.(map[string]any)
		if !ok {
			return false
		}
		c, has := bm["const"]
		if !has {
			return false
		}
		for k := range bm {
			if k != "const" && k != "description" && k != "title" {
				return false
			}
		}
		raw, _ := json.Marshal(c)
		if seen[string(raw)] {
			return false
		}
		seen[string(raw)] = true
		values = append(values, c)
	}
	if _, has := s["enum"]; has {
		return false
	}
	s["enum"] = values
	delete(s, key)
	return true
}

const maxItemID = 64

var itemIDPrefixes = map[string]string{"message": "msg", "reasoning": "rs", "function_call": "fc",
	"custom_tool_call": "ctc", "custom_tool_call_output": "ctco"}

// sanitizeInputIDs gives input items the id prefixes Codex expects, shortens ids over 64
// characters, and drops encrypted reasoning whose id is too long to be valid.
func sanitizeInputIDs(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	items := input.Array()
	used := map[string]bool{}
	for _, it := range items {
		if id := it.Get("id"); id.Type == gjson.String {
			used[id.String()] = true
		}
	}
	rebuilt := make([]string, 0, len(items))
	changed := false
	for _, it := range items {
		typ := it.Get("type").String()
		id := it.Get("id")
		if typ == "reasoning" && id.Type == gjson.String && len(id.String()) > maxItemID &&
			it.Get("encrypted_content").String() != "" {
			changed = true
			continue
		}
		raw := it.Raw
		if id.Type == gjson.String && id.String() != "" {
			next := id.String()
			if p := itemIDPrefixes[typ]; p != "" && !strings.HasPrefix(next, p) {
				next = p + "_" + next
			}
			if len(next) > maxItemID {
				sum := sha256.Sum256([]byte(next))
				short := next[:maxItemID-17] + "_" + hex.EncodeToString(sum[:8])
				for n := 0; used[short]; n++ {
					sum = sha256.Sum256([]byte(next + string(rune('a'+n))))
					short = next[:maxItemID-17] + "_" + hex.EncodeToString(sum[:8])
				}
				next = short
			}
			if next != id.String() {
				used[next] = true
				if b, err := sjson.Set(raw, "id", next); err == nil {
					raw = b
					changed = true
				}
			}
		}
		rebuilt = append(rebuilt, raw)
	}
	if !changed {
		return body
	}
	out, err := sjson.SetRawBytes(body, "input", []byte("["+strings.Join(rebuilt, ",")+"]"))
	if err != nil {
		return body
	}
	return out
}
