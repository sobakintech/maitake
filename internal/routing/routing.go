// Package routing turns the model name a client asked for into the model and accounts that
// serve it: model mapping rules, the catalog with admin overrides, and access checks.
package routing

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sobakintech/maitake/internal/models"
)

// Account selection strategies within a provider's highest available priority tier.
const (
	StrategyRoundRobin    = "round_robin"    // rotate, least recently used first
	StrategyFill          = "fill"           // always the first account until it's limited
	StrategyMostRemaining = "most_remaining" // the account with the most quota left
	StrategyRandom        = "random"
)

var strategies = []string{StrategyRoundRobin, StrategyFill, StrategyMostRemaining, StrategyRandom}

// Client API formats, used to scope mapping rules.
const (
	FormatAnthropic  = "anthropic"
	FormatOpenAI     = "openai"
	FormatResponses  = "responses"
	FormatOpenRouter = "openrouter"
)

type Settings struct {
	// Strategy is the default account selection; ProviderStrategy overrides it per provider.
	Strategy         string            `json:"strategy"`
	ProviderStrategy map[string]string `json:"providerStrategy"`
	// Affinity keeps related requests on the same account for StickyMinutes after their last
	// use, which keeps upstream prompt caches warm and thinking signatures valid:
	// "session" (per conversation), "key" (per API key) or "off". The binding is dropped if the
	// account becomes unavailable.
	Affinity string `json:"affinity"`
	// Sticky is the pre-affinity setting, kept so older saved settings still load.
	Sticky        bool `json:"sticky,omitempty"`
	StickyMinutes int  `json:"stickyMinutes"`

	// ModelList picks names in /v1/models: "bare" (claude-opus-5), "canonical"
	// (anthropic/claude-opus-5) or "both". OpenRouter-style paths always list canonical names.
	ModelList string `json:"modelList"`
	// UnknownModels decides whether names missing from the catalog are passed through to the
	// provider their family suggests ("claude-…" → Claude).
	UnknownModels bool `json:"unknownModels"`
	// ReasoningField is where Chat Completions responses put reasoning text:
	// "reasoning_content", "reasoning" (OpenRouter style) or "both".
	ReasoningField string `json:"reasoningField"`

	Models   map[string]ModelOverride `json:"models"` // keyed by canonical name
	Custom   []CustomModel            `json:"custom"`
	Mappings []Rule                   `json:"mappings"`
	// PayloadRules edit upstream request bodies after translation.
	PayloadRules []PayloadRule `json:"payloadRules"`
	Images       ImageSettings `json:"images"`
}

// ImageSettings control /v1/images, served by Codex's image_generation tool.
type ImageSettings struct {
	Enabled bool `json:"enabled"`
	// Model is the Responses model that runs the tool; requests are routed and checked
	// against key allow-lists as this model.
	Model string `json:"model"`
	// ToolModel is the image model used when the request doesn't name a gpt-image one.
	ToolModel string `json:"toolModel"`
	// AutoTool adds the image_generation tool to Responses API requests sent to Codex.
	AutoTool bool `json:"autoTool"`
}

type ModelOverride struct {
	Disabled bool   `json:"disabled"`
	Name     string `json:"name"`
	// Accounts limits which accounts may serve the model; empty means any.
	Accounts []int64 `json:"accounts"`
	Strategy string  `json:"strategy"`
	// Effort applies when the request doesn't set one.
	Effort string `json:"effort"`
}

type CustomModel struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength"`
}

// Rule rewrites a requested model name before resolution.
type Rule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Match is a case-insensitive glob over the requested name, with or without a lab prefix.
	Match  string `json:"match"`
	Target string `json:"target"`
	// Effort overrides the reasoning effort; empty keeps the requested one.
	Effort string `json:"effort"`
	// Formats limits the rule to some client API formats; empty means all.
	Formats []string `json:"formats"`
	Note    string   `json:"note"`
}

// KeyRules are a key's own mapping rules, checked before the global ones.
type KeyRules struct {
	Rules        []Rule `json:"rules"`
	IgnoreGlobal bool   `json:"ignoreGlobal"`
}

func DefaultSettings() Settings {
	return Settings{Strategy: StrategyRoundRobin, Affinity: "session", StickyMinutes: 30, ModelList: "bare",
		UnknownModels: true, ReasoningField: "both", Models: map[string]ModelOverride{},
		ProviderStrategy: map[string]string{}, Custom: []CustomModel{}, Mappings: []Rule{}, PayloadRules: []PayloadRule{},
		Images: ImageSettings{Enabled: true, Model: "gpt-5.4-mini", ToolModel: "gpt-image-2"}}
}

func validStrategy(s string) bool { return slices.Contains(strategies, s) }

func (s *Settings) Normalize() {
	if !validStrategy(s.Strategy) {
		s.Strategy = StrategyRoundRobin
	}
	if s.ProviderStrategy == nil {
		s.ProviderStrategy = map[string]string{}
	}
	for p, v := range s.ProviderStrategy {
		if !validStrategy(v) {
			delete(s.ProviderStrategy, p)
		}
	}
	switch s.Affinity {
	case "session", "key", "off":
	default:
		s.Affinity = "session"
		if s.Sticky {
			s.Affinity = "key"
		}
	}
	s.Sticky = false
	if s.StickyMinutes < 1 {
		s.StickyMinutes = 30
	}
	s.PayloadRules = normalizePayloadRules(s.PayloadRules)
	if s.Images.Model = strings.TrimSpace(s.Images.Model); s.Images.Model == "" {
		s.Images.Model = "gpt-5.4-mini"
	}
	if s.Images.ToolModel = strings.TrimSpace(s.Images.ToolModel); s.Images.ToolModel == "" {
		s.Images.ToolModel = "gpt-image-2"
	}
	if s.ModelList != "canonical" && s.ModelList != "both" {
		s.ModelList = "bare"
	}
	if s.ReasoningField != "reasoning" && s.ReasoningField != "reasoning_content" {
		s.ReasoningField = "both"
	}
	if s.Models == nil {
		s.Models = map[string]ModelOverride{}
	}
	for k, o := range s.Models {
		if o.Strategy != "" && !validStrategy(o.Strategy) {
			o.Strategy = ""
			s.Models[k] = o
		}
	}
	if s.Custom == nil {
		s.Custom = []CustomModel{}
	}
	if s.Mappings == nil {
		s.Mappings = []Rule{}
	}
}

// Catalog is every model: built-ins, then custom ones, with display-name overrides applied.
func (s Settings) Catalog() []models.Model {
	out := models.Builtin()
	seen := map[string]bool{}
	for _, m := range out {
		seen[m.Canonical] = true
	}
	for _, c := range s.Custom {
		lab := models.LabForProvider[c.Provider]
		id := strings.TrimSpace(c.ID)
		if id == "" || lab == "" || seen[lab+"/"+id] {
			continue
		}
		name := c.Name
		if name == "" {
			name = models.DisplayName(id)
		}
		out = append(out, models.Model{ID: id, Canonical: lab + "/" + id, Lab: lab, Provider: c.Provider,
			Name: name, ContextLength: c.ContextLength, Custom: true})
		seen[lab+"/"+id] = true
	}
	for i := range out {
		if o, ok := s.Models[out[i].Canonical]; ok && o.Name != "" {
			out[i].Name = o.Name
		}
	}
	return out
}

func (s Settings) lookup(name string) (models.Model, bool) {
	name = strings.ToLower(name)
	for _, m := range s.Catalog() {
		if strings.ToLower(m.ID) == name || strings.ToLower(m.Canonical) == name {
			return m, true
		}
	}
	return models.Model{}, false
}

var (
	ErrUnknownModel  = errors.New("unknown model")
	ErrModelDisabled = errors.New("model is disabled")
	ErrNotAllowed    = errors.New("model not allowed for this key")
)

// Request is what the resolver needs to know about an incoming call.
type Request struct {
	Model    string
	Format   string
	KeyRules KeyRules
	Allowed  []string
	// Providers optionally restricts which providers may serve it (OpenRouter's provider.only).
	Providers []string
}

type Route struct {
	Requested string       `json:"requested"`
	Mapped    string       `json:"mapped,omitempty"` // set when a rule rewrote the name
	Rule      string       `json:"rule,omitempty"`   // "key" or "global"
	Model     models.Model `json:"model"`
	Effort    string       `json:"effort,omitempty"`
	// DefaultEffort applies only when the request itself doesn't ask for a reasoning level.
	DefaultEffort string  `json:"defaultEffort,omitempty"`
	Accounts      []int64 `json:"accounts,omitempty"`
	Strategy      string  `json:"strategy"`
	Catalog       bool    `json:"inCatalog"`
}

// knownPrefixes are accepted in front of a model: labs (OpenRouter style) and providers (pins).
func stripPrefix(name string) (bare, prefix string) {
	if i := strings.Index(name, "/"); i > 0 {
		p := strings.ToLower(name[:i])
		for _, l := range models.Labs {
			if p == l.ID {
				return name[i+1:], p
			}
		}
		if _, ok := models.LabForProvider[p]; ok {
			return name[i+1:], p
		}
	}
	return name, ""
}

// canonicalOf gives the lab-prefixed form of a name, using the catalog or the model family.
func (s Settings) canonicalOf(name string) (bare, canonical string) {
	bare, prefix := stripPrefix(name)
	if m, ok := s.lookup(bare); ok {
		return bare, m.Canonical
	}
	if lab := models.LabForProvider[prefix]; lab != "" {
		return bare, lab + "/" + bare
	}
	if prefix != "" {
		return bare, prefix + "/" + bare
	}
	if lab := models.LabForProvider[models.GuessProvider(bare)]; lab != "" {
		return bare, lab + "/" + bare
	}
	return bare, bare
}

func (s Settings) ruleMatches(r Rule, name, format string) bool {
	if !r.Enabled || strings.TrimSpace(r.Match) == "" || strings.TrimSpace(r.Target) == "" {
		return false
	}
	if len(r.Formats) > 0 && !slices.Contains(r.Formats, format) {
		return false
	}
	bare, canonical := s.canonicalOf(name)
	return models.Match(r.Match, models.Model{ID: bare, Canonical: canonical})
}

// applyRules returns the first matching rule from the key's rules, then the global ones.
func (s Settings) applyRules(name, format string, key KeyRules) (*Rule, string) {
	for i := range key.Rules {
		if s.ruleMatches(key.Rules[i], name, format) {
			return &key.Rules[i], "key"
		}
	}
	if !key.IgnoreGlobal {
		for i := range s.Mappings {
			if s.ruleMatches(s.Mappings[i], name, format) {
				return &s.Mappings[i], "global"
			}
		}
	}
	return nil, ""
}

// Resolve maps a requested model to the model, provider and accounts that should serve it.
func (s Settings) Resolve(req Request) (*Route, error) {
	requested := strings.TrimSpace(req.Model)
	name, effort := models.SplitEffort(requested)
	route := &Route{Requested: requested}

	if rule, source := s.applyRules(name, req.Format, req.KeyRules); rule != nil {
		target, targetEffort := models.SplitEffort(rule.Target)
		route.Mapped, route.Rule = rule.Target, source
		name = target
		if targetEffort != "" {
			effort = targetEffort
		}
		if rule.Effort != "" {
			effort = rule.Effort
		}
	}

	bare, prefix := stripPrefix(name)
	m, ok := s.lookup(bare)
	if !ok && prefix != "" {
		m, ok = s.lookup(name)
	}
	if ok {
		route.Catalog = true
	} else {
		provider := models.GuessProvider(bare)
		if p, isProvider := models.LabForProvider[prefix]; isProvider && p != "" {
			provider = prefix
		} else if prov := models.ProviderForLab[prefix]; prov != "" {
			provider = prov
		}
		if provider == "" || !s.UnknownModels {
			return route, fmt.Errorf("%w %q", ErrUnknownModel, name)
		}
		lab := models.LabForProvider[provider]
		m = models.Model{ID: bare, Canonical: lab + "/" + bare, Lab: lab, Provider: provider, Name: models.DisplayName(bare)}
	}
	// A provider prefix ("codex/gpt-5.5") must agree with the model's provider.
	if _, isProvider := models.LabForProvider[prefix]; isProvider && prefix != m.Provider {
		return route, fmt.Errorf("%w %q: %s doesn't serve it", ErrUnknownModel, name, prefix)
	}
	route.Model = m

	o := s.Models[m.Canonical]
	if o.Disabled {
		return route, fmt.Errorf("%w: %s", ErrModelDisabled, m.ID)
	}
	if !models.Allowed(req.Allowed, m) {
		return route, fmt.Errorf("%w: %s", ErrNotAllowed, m.ID)
	}
	if len(req.Providers) > 0 && !providerAllowed(req.Providers, m) {
		return route, fmt.Errorf("%w %q: excluded by the request's provider preferences", ErrUnknownModel, m.ID)
	}
	route.Effort = effort
	route.DefaultEffort = o.Effort
	route.Accounts = o.Accounts
	route.Strategy = s.StrategyFor(m)
	return route, nil
}

// StrategyFor returns the account strategy for a model: model override, provider override, default.
func (s Settings) StrategyFor(m models.Model) string {
	if o := s.Models[m.Canonical]; o.Strategy != "" {
		return o.Strategy
	}
	if v := s.ProviderStrategy[m.Provider]; v != "" {
		return v
	}
	return s.Strategy
}

// providerAllowed matches OpenRouter-style provider names ("anthropic", "openai", or ours).
func providerAllowed(names []string, m models.Model) bool {
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == m.Provider || n == m.Lab {
			return true
		}
	}
	return false
}
