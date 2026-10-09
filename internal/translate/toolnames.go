package translate

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// ToolNames shortens tool names to the Responses API limit (64 chars, [a-zA-Z0-9_-]) and maps them back.
type ToolNames struct {
	short    map[string]string // original -> short
	original map[string]string // short -> original
	custom   map[string]bool   // freeform (Responses "custom") tools, by original name
}

func NewToolNames() *ToolNames {
	return &ToolNames{short: map[string]string{}, original: map[string]string{}}
}

func (t *ToolNames) Short(name string) string {
	if s, ok := t.short[name]; ok {
		return s
	}
	var b strings.Builder
	for _, r := range name {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if len(s) > 64 {
		if strings.HasPrefix(s, "mcp__") {
			if i := strings.LastIndex(s, "__"); i > 3 {
				s = "mcp__" + s[i+2:]
			}
		}
		if len(s) > 64 {
			s = s[:64]
		}
	}
	base := s
	for n := 1; ; n++ {
		if prev, taken := t.original[s]; !taken || prev == name {
			break
		}
		suffix := "_" + strconv.Itoa(n)
		if len(base)+len(suffix) > 64 {
			s = base[:64-len(suffix)] + suffix
		} else {
			s = base + suffix
		}
	}
	t.short[name] = s
	t.original[s] = name
	return s
}

// MarkCustom records a freeform tool, whose calls go back to the client as custom_tool_call.
func (t *ToolNames) MarkCustom(name string) {
	if t.custom == nil {
		t.custom = map[string]bool{}
	}
	t.custom[name] = true
}

func (t *ToolNames) IsCustom(name string) bool { return t.custom[name] }

func (t *ToolNames) Original(short string) string {
	if o, ok := t.original[short]; ok {
		return o
	}
	return short
}

// shortCallID keeps call ids within the Responses API's 64-char limit, deterministically.
func shortCallID(id string) string {
	if len(id) <= 64 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return id[:47] + "_" + hex.EncodeToString(sum[:8])
}
