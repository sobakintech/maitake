package accounts

import (
	"regexp"
	"strings"
)

// Error actions decide what happens to a request after an upstream error.
const (
	ActionStop  = "stop"  // return the error to the client
	ActionRetry = "retry" // try another account
)

// Cooldown scopes decide what gets benched after an error.
const (
	CooldownNone    = "none"
	CooldownAccount = "account"
	CooldownModel   = "model" // only this model on this account
	CooldownAuto    = ""      // built-in judgement (rate-limit headers etc.)
)

// ErrorRule overrides how an upstream error is handled. Rules are checked in order; the first
// match wins, and unmatched errors fall back to the built-in classification.
type ErrorRule struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"` // "" any, "claude", "codex"
	Status   int    `json:"status"`   // 0 any
	// Match is a case-insensitive substring of the error body, or a regular expression when
	// written as /pattern/.
	Match           string `json:"match"`
	Action          string `json:"action"`
	Cooldown        string `json:"cooldown"`
	CooldownSeconds int    `json:"cooldownSeconds"`
	Note            string `json:"note"`
}

// DefaultErrorRules cover errors the client caused (don't punish the account) and capacity
// errors (move on quickly).
func DefaultErrorRules() []ErrorRule {
	return []ErrorRule{
		{ID: "too-long", Enabled: true, Match: "prompt is too long", Action: ActionStop, Cooldown: CooldownNone,
			Note: "The request is too big for the model; another account won't help."},
		{ID: "context-length", Enabled: true, Match: "context_length_exceeded", Action: ActionStop, Cooldown: CooldownNone,
			Note: "Same, Codex wording."},
		{ID: "capacity", Enabled: true, Match: "model is at capacity", Action: ActionRetry, Cooldown: CooldownModel,
			CooldownSeconds: 60, Note: "Only this model is busy on this account."},
		{ID: "fast-mode", Enabled: true, Provider: "claude", Match: "required for fast mode", Action: ActionRetry,
			Cooldown: CooldownNone, Note: "This account has no fast-mode credits; it's fine for everything else."},
		{ID: "overloaded", Enabled: true, Status: 529, Action: ActionRetry, Cooldown: CooldownAccount,
			CooldownSeconds: 30, Note: "Anthropic is overloaded; try elsewhere briefly."},
	}
}

func (r ErrorRule) matches(provider string, status int, body string) bool {
	if !r.Enabled {
		return false
	}
	if r.Provider != "" && r.Provider != provider {
		return false
	}
	if r.Status != 0 && r.Status != status {
		return false
	}
	m := strings.TrimSpace(r.Match)
	if m == "" {
		return r.Status != 0 || r.Provider != ""
	}
	if len(m) > 2 && strings.HasPrefix(m, "/") && strings.HasSuffix(m, "/") {
		re, err := regexp.Compile("(?i)" + m[1:len(m)-1])
		return err == nil && re.MatchString(body)
	}
	return strings.Contains(strings.ToLower(body), strings.ToLower(m))
}

// MatchErrorRule returns the first rule that applies to an error, if any.
func (s Settings) MatchErrorRule(provider string, status int, body string) *ErrorRule {
	for i := range s.ErrorRules {
		if s.ErrorRules[i].matches(provider, status, body) {
			return &s.ErrorRules[i]
		}
	}
	return nil
}

func normalizeRules(rules []ErrorRule) []ErrorRule {
	out := rules[:0]
	for _, r := range rules {
		if r.Action != ActionStop {
			r.Action = ActionRetry
		}
		switch r.Cooldown {
		case CooldownNone, CooldownAccount, CooldownModel, CooldownAuto:
		default:
			r.Cooldown = CooldownAuto
		}
		if r.CooldownSeconds < 0 {
			r.CooldownSeconds = 0
		}
		out = append(out, r)
	}
	return out
}
