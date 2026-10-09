package translate

import (
	"regexp"
	"slices"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/models"
)

// Older Claude models only take a thinking budget; newer ones use adaptive thinking with an effort
// level. Used for models missing from the catalog.
var budgetOnlyClaude = regexp.MustCompile(`^claude-(3|(opus|sonnet|haiku)-4-(5|1|2025))|^claude-(opus|sonnet)-4-2025`)

var effortBudgets = map[string]int{
	"minimal": 1024, "low": 1024, "medium": 8192, "high": 24576, "xhigh": 32768, "max": 64000,
}

const defaultClaudeMaxTokens = 32000

func claudeBudgetOnly(model string) bool {
	if m, ok := models.Lookup(model); ok {
		return len(m.Efforts) == 0
	}
	return budgetOnlyClaude.MatchString(model)
}

// claudeAdaptiveEffort picks the closest level the model accepts. Unknown models are assumed
// to take max but not xhigh.
func claudeAdaptiveEffort(model, effort string) string {
	levels := []string{"low", "medium", "high", "max"}
	if m, ok := models.Lookup(model); ok && len(m.Efforts) > 0 {
		levels = m.Efforts
	}
	if effort == "minimal" {
		effort = "low"
	}
	if slices.Contains(levels, effort) {
		return effort
	}
	switch effort {
	case "xhigh":
		if slices.Contains(levels, "max") {
			return "max"
		}
		return "high"
	case "max":
		if slices.Contains(levels, "xhigh") {
			return "xhigh"
		}
		return "high"
	}
	return ""
}

// claudeThinking returns the thinking / output_config settings for an OpenAI-style effort level.
// ok is false when effort is empty or unknown.
func claudeThinking(model, effort string) (thinking obj, outputEffort string, ok bool) {
	switch effort {
	case "":
		return nil, "", false
	case "none":
		return obj{"type": "disabled"}, "", true
	}
	if claudeBudgetOnly(model) {
		if effort == "auto" {
			effort = "medium"
		}
		budget, known := effortBudgets[effort]
		if !known {
			return nil, "", false
		}
		return obj{"type": "enabled", "budget_tokens": budget}, "", true
	}
	if effort == "auto" {
		return obj{"type": "adaptive"}, "", true
	}
	if oe := claudeAdaptiveEffort(model, effort); oe != "" {
		return obj{"type": "adaptive"}, oe, true
	}
	return nil, "", false
}

// EffortFromBudget maps a thinking token budget onto a reasoning effort level.
func EffortFromBudget(budget int64) string { return effortFromBudget(budget) }

// effortFromBudget maps a Claude thinking budget onto a Codex reasoning effort.
func effortFromBudget(budget int64) string {
	switch {
	case budget <= 0:
		return "none"
	case budget <= 512:
		return "minimal"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	case budget <= 24576:
		return "high"
	default:
		return "xhigh"
	}
}

var codexEfforts = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true,
	"xhigh": true, "max": true}

func codexEffort(e string) string {
	if codexEfforts[e] {
		return e
	}
	return ""
}

// CodexEffortFor lowers xhigh / max to the highest level the model accepts.
func CodexEffortFor(model, effort string) string {
	m, ok := models.Lookup(model)
	if !ok || len(m.Efforts) == 0 || slices.Contains(m.Efforts, effort) {
		return effort
	}
	switch effort {
	case "max":
		if slices.Contains(m.Efforts, "xhigh") {
			return "xhigh"
		}
		return "high"
	case "xhigh":
		return "high"
	}
	return effort
}

// ApplyClaudeEffort sets thinking (and output_config.effort) on a Claude request from an effort level,
// e.g. one given as a "claude-opus-5(high)" model suffix.
func ApplyClaudeEffort(body []byte, model, effort string) []byte {
	thinking, outputEffort, ok := claudeThinking(model, effort)
	if !ok {
		return body
	}
	body, _ = sjson.SetBytes(body, "thinking", thinking)
	if outputEffort != "" {
		body, _ = sjson.SetBytes(body, "output_config.effort", outputEffort)
	} else {
		body, _ = sjson.DeleteBytes(body, "output_config.effort")
	}
	return body
}

// FitClaudeTokens keeps max_tokens within the model's output limit (defaulting it when missing)
// and the thinking budget below max_tokens, which Anthropic requires.
func FitClaudeTokens(body []byte, model string) []byte {
	limit := 0
	if m, ok := models.Lookup(model); ok {
		limit = m.MaxOutput
	}
	capped := func(n int64) int64 {
		if limit > 0 && n > int64(limit) {
			return int64(limit)
		}
		return n
	}
	maxTokens := gjson.GetBytes(body, "max_tokens").Int()
	if maxTokens <= 0 {
		maxTokens = defaultClaudeMaxTokens
	}
	maxTokens = capped(maxTokens)

	if gjson.GetBytes(body, "thinking.type").String() == "enabled" {
		budget := gjson.GetBytes(body, "thinking.budget_tokens").Int()
		if budget >= maxTokens {
			maxTokens = capped(budget + 8192)
		}
		if budget >= maxTokens {
			budget = maxTokens - 1
			if budget < 1024 {
				body, _ = sjson.DeleteBytes(body, "thinking")
			} else {
				body, _ = sjson.SetBytes(body, "thinking.budget_tokens", budget)
			}
		}
	}
	body, _ = sjson.SetBytes(body, "max_tokens", maxTokens)
	return body
}
