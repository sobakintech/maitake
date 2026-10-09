// Package models is the built-in model catalog. A model has a lab (who made it) and a
// provider (which kind of maitake account serves it); with custom sources these can differ.
package models

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

type Lab struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var Labs = []Lab{
	{ID: "anthropic", Name: "Anthropic"},
	{ID: "openai", Name: "OpenAI"},
}

type Model struct {
	// ID is the name the upstream expects, e.g. "claude-opus-5".
	ID string `json:"id"`
	// Canonical is "lab/id", the OpenRouter-style name.
	Canonical     string `json:"canonical"`
	Lab           string `json:"lab"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength"`
	MaxOutput     int    `json:"maxOutput"`
	// Efforts are the reasoning levels the model accepts; empty when unknown or budget-only.
	Efforts []string `json:"efforts,omitempty"`
	Custom  bool     `json:"custom"`
}

type builtin struct {
	id        string
	context   int
	maxOutput int
	efforts   []string
}

// Reasoning levels a model accepts natively. Claude models without any only take a thinking budget.
var (
	claudeAdaptive = []string{"low", "medium", "high", "max"}
	claudeXHigh    = []string{"low", "medium", "high", "xhigh", "max"}
	codexXHigh     = []string{"low", "medium", "high", "xhigh"}
	codexMax       = []string{"low", "medium", "high", "xhigh", "max"}
)

var claudeModels = []builtin{
	{"claude-opus-5-5", 1_000_000, 128_000, claudeXHigh}, {"claude-sonnet-5-5", 1_000_000, 128_000, claudeXHigh},
	{"claude-fable-5-1", 1_000_000, 128_000, claudeXHigh}, {"claude-fable-5", 1_000_000, 128_000, claudeXHigh},
	{"claude-opus-5", 1_000_000, 128_000, claudeXHigh}, {"claude-sonnet-5", 1_000_000, 128_000, claudeXHigh},
	{"claude-opus-4-8", 1_000_000, 128_000, claudeXHigh}, {"claude-opus-4-7", 1_000_000, 128_000, claudeXHigh},
	{"claude-opus-4-6", 1_000_000, 128_000, claudeAdaptive}, {"claude-sonnet-4-6", 200_000, 64_000, claudeAdaptive},
	{"claude-opus-4-5-20251101", 200_000, 64_000, nil}, {"claude-sonnet-4-5-20250929", 200_000, 64_000, nil},
	{"claude-haiku-4-5-20251001", 200_000, 64_000, nil}, {"claude-opus-4-1-20250805", 200_000, 32_000, nil},
	{"claude-opus-4-20250514", 200_000, 32_000, nil}, {"claude-sonnet-4-20250514", 200_000, 64_000, nil},
	{"claude-3-7-sonnet-20250219", 200_000, 64_000, nil}, {"claude-3-5-haiku-20241022", 200_000, 8_192, nil},
}

var codexModels = []builtin{
	{"gpt-6.1-sol", 272_000, 128_000, codexMax}, {"gpt-6-sol", 272_000, 128_000, codexMax},
	{"gpt-6-astra", 272_000, 128_000, codexMax}, {"gpt-6-luna", 272_000, 128_000, codexMax},
	{"gpt-5.6-sol", 921_000, 128_000, codexMax}, {"gpt-5.6-terra", 921_000, 128_000, codexMax},
	{"gpt-5.6-luna", 921_000, 128_000, codexMax}, {"gpt-5.5", 272_000, 128_000, codexXHigh},
	{"codex-auto-review", 272_000, 128_000, codexXHigh},
}

// LabForProvider is the lab whose models a built-in provider serves.
var LabForProvider = map[string]string{"claude": "anthropic", "codex": "openai"}

// ProviderForLab is the reverse of LabForProvider.
var ProviderForLab = map[string]string{"anthropic": "claude", "openai": "codex"}

func Builtin() []Model {
	out := make([]Model, 0, len(claudeModels)+len(codexModels))
	add := func(list []builtin, provider string) {
		lab := LabForProvider[provider]
		for _, b := range list {
			out = append(out, Model{ID: b.id, Canonical: lab + "/" + b.id, Lab: lab, Provider: provider,
				Name: DisplayName(b.id), ContextLength: b.context, MaxOutput: b.maxOutput, Efforts: b.efforts})
		}
	}
	add(claudeModels, "claude")
	add(codexModels, "codex")
	return out
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// Lookup finds a built-in model by ID, ignoring a trailing date stamp on either side.
func Lookup(id string) (Model, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	bare := dateSuffix.ReplaceAllString(id, "")
	for _, m := range Builtin() {
		if m.ID == id || dateSuffix.ReplaceAllString(m.ID, "") == bare {
			return m, true
		}
	}
	return Model{}, false
}

// DisplayName turns an ID like "claude-opus-4-5-20251101" into "Claude Opus 4.5".
func DisplayName(id string) string {
	parts := strings.Split(id, "-")
	var words []string
	var version []string
	flush := func() {
		if len(version) > 0 {
			words = append(words, strings.Join(version, "."))
			version = nil
		}
	}
	for _, p := range parts {
		switch {
		case len(p) == 8 && isDigits(p): // date stamp
			continue
		case isDigits(p) || (len(p) > 0 && unicode.IsDigit(rune(p[0])) && strings.Contains(p, ".")):
			version = append(version, p)
		case p == "gpt":
			flush()
			words = append(words, "GPT")
		default:
			flush()
			words = append(words, strings.ToUpper(p[:1])+p[1:])
		}
	}
	flush()
	name := strings.Join(words, " ")
	// "GPT 5.5" reads better as "GPT-5.5".
	return strings.Replace(name, "GPT ", "GPT-", 1)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// SplitEffort separates an optional "(effort)" suffix: "gpt-5.5(high)" → "gpt-5.5", "high".
func SplitEffort(model string) (string, string) {
	model = strings.TrimSpace(model)
	if i := strings.LastIndex(model, "("); i > 0 && strings.HasSuffix(model, ")") {
		return strings.TrimSpace(model[:i]), strings.ToLower(strings.TrimSpace(model[i+1 : len(model)-1]))
	}
	return model, ""
}

// GuessProvider routes unknown model IDs by family so new upstream models work immediately.
func GuessProvider(id string) string {
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "claude-"):
		return "claude"
	case strings.HasPrefix(lower, "gpt-"), strings.HasPrefix(lower, "codex-"), strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return "codex"
	}
	return ""
}

// Match reports whether a glob pattern matches a model, checked against both its bare and
// canonical names ("claude-*" and "anthropic/*" both match claude-opus-5).
func Match(pattern string, m Model) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	for _, name := range []string{strings.ToLower(m.ID), strings.ToLower(m.Canonical)} {
		if pattern == name {
			return true
		}
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// Allowed reports whether a model passes a key's allow-list; an empty list allows everything.
func Allowed(patterns []string, m Model) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if Match(p, m) {
			return true
		}
	}
	return false
}
