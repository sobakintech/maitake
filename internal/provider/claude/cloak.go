package claude

import (
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// CloakSettings tune how closely requests imitate Claude Code. Editable in the dashboard.
type CloakSettings struct {
	// ToolAliases renames client tools to Claude Code-style MCP names upstream and back.
	ToolAliases bool `json:"toolAliases"`
	// CacheBreakpoints places prompt-cache markers like Claude Code does.
	CacheBreakpoints bool `json:"cacheBreakpoints"`
	// LongCacheTTL upgrades cache markers to the 1-hour TTL.
	LongCacheTTL bool `json:"longCacheTTL"`
	// SensitiveWords get a zero-width space inserted after their first letter in prompts.
	SensitiveWords []string `json:"sensitiveWords"`
	// TLSFingerprint makes TLS handshakes to Anthropic look like Claude Code's (Node).
	TLSFingerprint bool `json:"tlsFingerprint"`
}

func DefaultCloak() CloakSettings {
	return CloakSettings{ToolAliases: true, CacheBreakpoints: true, LongCacheTTL: true, TLSFingerprint: true,
		SensitiveWords: []string{}}
}

func (c *CloakSettings) Normalize() {
	words := c.SensitiveWords[:0]
	for _, w := range c.SensitiveWords {
		if w = strings.TrimSpace(w); utf8.RuneCountInString(w) >= 2 && !strings.ContainsRune(w, zwsp) {
			words = append(words, w)
		}
	}
	if words == nil {
		words = []string{}
	}
	c.SensitiveWords = words
}

// ---- Tool aliases ----

//go:embed bip39_words.txt
var bip39Raw string

var bip39 = strings.Fields(bip39Raw)

var mcpName = regexp.MustCompile(`^mcp__[A-Za-z0-9_-]+__[A-Za-z0-9_-]+$`)

// Server tools are Anthropic-hosted and keep their names.
var serverToolPrefixes = []string{"advisor_", "agent_toolset_", "bash_", "code_execution_", "computer_", "memory_",
	"text_editor_", "tool_search_tool_", "web_fetch_", "web_search_"}

func isServerTool(toolType string) bool {
	for _, p := range serverToolPrefixes {
		if strings.HasPrefix(toolType, p) {
			return true
		}
	}
	return false
}

// ToolAliases maps client tool names to Claude Code-style MCP names for one request.
type ToolAliases struct {
	secret   []byte
	forward  map[string]string
	original map[string]string
	reserved map[string]bool
}

func NewToolAliases(secret string) *ToolAliases {
	return &ToolAliases{secret: []byte(secret), forward: map[string]string{}, original: map[string]string{},
		reserved: map[string]bool{}}
}

func (a *ToolAliases) digest(purpose, original string) []byte {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte("maitake-mcp-alias-v1\x00" + purpose + "\x00" + original))
	return mac.Sum(nil)
}

func semantic(name string, max int) string {
	var b strings.Builder
	lastInvalid := false
	for _, r := range name {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastInvalid = false
		} else if b.Len() > 0 && !lastInvalid {
			b.WriteByte('_')
			lastInvalid = true
		}
	}
	s := b.String()
	if max < 1 {
		max = 1
	}
	if len(s) > max {
		s = s[:max]
	}
	s = strings.Trim(s, "_-")
	if s == "" {
		return "tool"
	}
	return s
}

// Alias returns the upstream name for a client tool name; MCP-shaped names pass through.
func (a *ToolAliases) Alias(name string) string {
	if a == nil || name == "" || (len(name) <= 64 && mcpName.MatchString(name)) {
		return name
	}
	if v, ok := a.forward[name]; ok {
		return v
	}
	d := a.digest("server", "")
	w1 := bip39[int(binary.BigEndian.Uint16(d[0:2]))%len(bip39)]
	w2 := bip39[int(binary.BigEndian.Uint16(d[2:4]))%len(bip39)]
	td := a.digest("tool", name)
	base := int(binary.BigEndian.Uint16(td[0:2]))
	alias := ""
	for attempt := 0; attempt < len(bip39); attempt++ {
		word := bip39[(base+attempt)%len(bip39)]
		prefix := "mcp__" + w1 + "_" + w2 + "__" + word + "_"
		alias = prefix + semantic(name, 64-len(prefix))
		if !a.reserved[alias] {
			break
		}
	}
	a.forward[name], a.original[alias] = alias, name
	a.reserved[alias] = true
	return alias
}

// Original maps an upstream name back; unknown names are returned unchanged.
func (a *ToolAliases) Original(alias string) string {
	if a == nil {
		return alias
	}
	if o, ok := a.original[alias]; ok {
		return o
	}
	return alias
}

func (a *ToolAliases) Empty() bool { return a == nil || len(a.forward) == 0 }

// ---- Sensitive words ----

const zwsp = '​'

func wordPattern(words []string) *regexp.Regexp {
	if len(words) == 0 {
		return nil
	}
	sorted := slices.Clone(words)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	parts := make([]string, len(sorted))
	for i, w := range sorted {
		parts[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile("(?i)" + strings.Join(parts, "|"))
}

// obfuscate inserts a zero-width space after the first letter of each sensitive word.
func obfuscate(re *regexp.Regexp, text string) string {
	if re == nil {
		return text
	}
	return re.ReplaceAllStringFunc(text, func(m string) string {
		r, size := utf8.DecodeRuneInString(m)
		if size == len(m) || strings.ContainsRune(m, zwsp) {
			return m
		}
		return string(r) + string(zwsp) + m[size:]
	})
}

// ---- Cache breakpoints ----

const maxCacheBreakpoints = 4

type marker = map[string]any

func hasMarker(b any) bool {
	m, ok := b.(map[string]any)
	return ok && m["cache_control"] != nil
}

// placeCacheBreakpoints marks the last eligible message like Claude Code (system is already
// marked by the cloak), keeps at most 4 markers, and applies the TTL policy.
func placeCacheBreakpoints(o *object, msgs []message, addMessageMarker, longTTL bool) []message {
	if addMessageMarker {
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			role := m["role"]
			if role != "user" && role != "assistant" {
				continue
			}
			blocks := contentBlocks(m)
			if len(blocks) == 0 {
				continue
			}
			lastType, _ := blocks[len(blocks)-1].(map[string]any)["type"].(string)
			if role == "assistant" && (lastType == "thinking" || lastType == "redacted_thinking") {
				continue
			}
			if !slices.ContainsFunc(blocks, hasMarker) {
				if b, ok := blocks[len(blocks)-1].(map[string]any); ok {
					b["cache_control"] = marker{"type": "ephemeral"}
					m["content"] = blocks
				}
			}
			break
		}
	}

	// Collect markers in order: tools, system, messages.
	type ref struct {
		obj  map[string]any
		kind string
	}
	var refs []ref
	var tools []any
	if t := o.get("tools"); t.IsArray() {
		_ = decodeJSON(t.Raw, &tools)
	}
	for _, t := range tools {
		if hasMarker(t) {
			refs = append(refs, ref{t.(map[string]any), "tools"})
		}
	}
	var system []any
	_ = decodeJSON(o.get("system").Raw, &system)
	for _, s := range system {
		if hasMarker(s) {
			refs = append(refs, ref{s.(map[string]any), "system"})
		}
	}
	for _, m := range msgs {
		for _, b := range contentBlocks(m) {
			if hasMarker(b) {
				refs = append(refs, ref{b.(map[string]any), "messages"})
			}
		}
	}

	// Over the limit: drop earliest system, then tools, then message markers (keeping the last of each).
	excess := len(refs) - maxCacheBreakpoints
	for _, kind := range []string{"system", "tools", "messages"} {
		var idx []int
		for i, r := range refs {
			if r.kind == kind && r.obj["cache_control"] != nil {
				idx = append(idx, i)
			}
		}
		for k := 0; k < len(idx)-1 && excess > 0; k++ {
			delete(refs[idx[k]].obj, "cache_control")
			excess--
		}
	}

	// TTL: upgrade unset TTLs to 1h when enabled; a 1h marker may not follow a shorter one.
	seenShort := false
	for _, r := range refs {
		cc, ok := r.obj["cache_control"].(map[string]any)
		if !ok {
			continue
		}
		if longTTL && cc["ttl"] == nil {
			cc["ttl"] = "1h"
		}
		if cc["ttl"] != "1h" {
			seenShort = true
		} else if seenShort {
			delete(cc, "ttl")
		}
	}
	if len(tools) > 0 {
		o.set("tools", tools)
	}
	if len(system) > 0 {
		o.set("system", system)
	}
	return msgs
}
