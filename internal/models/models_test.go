package models

import "testing"

func TestDisplayName(t *testing.T) {
	for id, want := range map[string]string{
		"claude-opus-4-5-20251101":   "Claude Opus 4.5",
		"claude-sonnet-5-5":          "Claude Sonnet 5.5",
		"claude-3-7-sonnet-20250219": "Claude 3.7 Sonnet",
		"gpt-5.5":                    "GPT-5.5",
		"gpt-6.1-sol":                "GPT-6.1 Sol",
		"codex-auto-review":          "Codex Auto Review",
	} {
		if got := DisplayName(id); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", id, got, want)
		}
	}
}
