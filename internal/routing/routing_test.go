package routing

import (
	"errors"
	"testing"
)

func TestResolve(t *testing.T) {
	s := DefaultSettings()
	s.Mappings = []Rule{
		{Enabled: true, Match: "claude-haiku-*", Target: "gpt-5.5(low)", Formats: []string{FormatAnthropic}},
		{Enabled: false, Match: "claude-opus-5", Target: "gpt-5.5"},
	}
	s.Models["openai/gpt-6-sol"] = ModelOverride{Disabled: true}
	s.Models["anthropic/claude-opus-5"] = ModelOverride{Effort: "high", Accounts: []int64{7}, Strategy: StrategyFill}
	s.Normalize()

	cases := []struct {
		name     string
		req      Request
		model    string
		effort   string
		mapped   bool
		strategy string
		err      error
	}{
		{"bare", Request{Model: "claude-sonnet-5"}, "claude-sonnet-5", "", false, StrategyRoundRobin, nil},
		{"canonical", Request{Model: "anthropic/claude-sonnet-5"}, "claude-sonnet-5", "", false, StrategyRoundRobin, nil},
		{"provider pin", Request{Model: "codex/gpt-5.5(high)"}, "gpt-5.5", "high", false, StrategyRoundRobin, nil},
		{"wrong pin", Request{Model: "claude/gpt-5.5"}, "", "", false, "", ErrUnknownModel},
		{"mapping", Request{Model: "claude-haiku-4-5-20251001", Format: FormatAnthropic}, "gpt-5.5", "low", true, StrategyRoundRobin, nil},
		{"mapping format scoped", Request{Model: "claude-haiku-4-5-20251001", Format: FormatOpenAI}, "claude-haiku-4-5-20251001", "", false, StrategyRoundRobin, nil},
		{"key rule wins", Request{Model: "claude-haiku-4-5-20251001", Format: FormatAnthropic,
			KeyRules: KeyRules{Rules: []Rule{{Enabled: true, Match: "anthropic/claude-haiku*", Target: "claude-sonnet-5"}}}},
			"claude-sonnet-5", "", true, StrategyRoundRobin, nil},
		{"ignore global", Request{Model: "claude-haiku-4-5-20251001", Format: FormatAnthropic, KeyRules: KeyRules{IgnoreGlobal: true}},
			"claude-haiku-4-5-20251001", "", false, StrategyRoundRobin, nil},
		{"disabled rule", Request{Model: "claude-opus-5"}, "claude-opus-5", "", false, StrategyFill, nil},
		{"model disabled", Request{Model: "gpt-6-sol"}, "", "", false, "", ErrModelDisabled},
		{"allow list", Request{Model: "gpt-5.5", Allowed: []string{"anthropic/*"}}, "", "", false, "", ErrNotAllowed},
		{"unknown family passthrough", Request{Model: "claude-opus-9"}, "claude-opus-9", "", false, StrategyRoundRobin, nil},
		{"unknown", Request{Model: "llama-4"}, "", "", false, "", ErrUnknownModel},
		{"provider prefs", Request{Model: "gpt-5.5", Providers: []string{"anthropic"}}, "", "", false, "", ErrUnknownModel},
	}
	for _, c := range cases {
		route, err := s.Resolve(c.req)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if route.Model.ID != c.model || route.Effort != c.effort || (route.Mapped != "") != c.mapped || route.Strategy != c.strategy {
			t.Errorf("%s: got model=%s effort=%s mapped=%q strategy=%s", c.name, route.Model.ID, route.Effort, route.Mapped, route.Strategy)
		}
	}

	s.UnknownModels = false
	if _, err := s.Resolve(Request{Model: "claude-opus-9"}); !errors.Is(err, ErrUnknownModel) {
		t.Errorf("unknown models should be rejected when passthrough is off: %v", err)
	}
}
