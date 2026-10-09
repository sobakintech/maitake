package routing

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/models"
)

// Payload rule modes.
const (
	PayloadDefault  = "default"  // set the field only when the request doesn't have it
	PayloadOverride = "override" // always set the field
	PayloadRemove   = "remove"   // delete the field
)

// PayloadRule edits the final upstream request body, after all translation.
type PayloadRule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Match is a model glob (bare or lab-prefixed); empty matches every model.
	Match    string          `json:"match"`
	Provider string          `json:"provider"` // "" any, "claude", "codex"
	Formats  []string        `json:"formats"`  // client API formats; empty = all
	Mode     string          `json:"mode"`
	Path     string          `json:"path"` // dot path, e.g. "service_tier" or "reasoning.effort"
	Value    json.RawMessage `json:"value"`
	Note     string          `json:"note"`
}

func normalizePayloadRules(rules []PayloadRule) []PayloadRule {
	out := []PayloadRule{}
	for _, r := range rules {
		r.Path = strings.TrimSpace(r.Path)
		switch r.Mode {
		case PayloadDefault, PayloadOverride, PayloadRemove:
		default:
			r.Mode = PayloadOverride
		}
		if r.Mode != PayloadRemove && (len(r.Value) == 0 || !json.Valid(r.Value)) {
			r.Value = json.RawMessage("null")
		}
		if r.Formats == nil {
			r.Formats = []string{}
		}
		out = append(out, r)
	}
	return out
}

// ApplyPayloadRules runs every matching rule over an upstream body and reports whether it changed.
func (s Settings) ApplyPayloadRules(body []byte, m models.Model, format string) ([]byte, bool) {
	changed := false
	for _, r := range s.PayloadRules {
		if !r.Enabled || r.Path == "" {
			continue
		}
		if r.Provider != "" && r.Provider != m.Provider {
			continue
		}
		if len(r.Formats) > 0 && !slices.Contains(r.Formats, format) {
			continue
		}
		if strings.TrimSpace(r.Match) != "" && !models.Match(r.Match, m) {
			continue
		}
		var next []byte
		var err error
		switch r.Mode {
		case PayloadRemove:
			if !gjson.GetBytes(body, r.Path).Exists() {
				continue
			}
			next, err = sjson.DeleteBytes(body, r.Path)
		case PayloadDefault:
			if gjson.GetBytes(body, r.Path).Exists() {
				continue
			}
			next, err = sjson.SetRawBytes(body, r.Path, r.Value)
		default:
			next, err = sjson.SetRawBytes(body, r.Path, r.Value)
		}
		if err == nil {
			body, changed = next, true
		}
	}
	return body, changed
}
