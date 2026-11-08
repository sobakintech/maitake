package routing

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/models"
)

// Where aliases learn which models exist.
const (
	AliasSourceLive    = "live"    // ask each account, so a line only resolves to what it can use
	AliasSourceCatalog = "catalog" // built-in and custom models only
)

// AliasSettings let a client ask for a model line ("sol") and get its newest model.
type AliasSettings struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
	// RefreshMinutes is how often each account's model list is fetched.
	RefreshMinutes int `json:"refreshMinutes"`
	// Fallback uses the next-newest model when nothing can serve the newest right now.
	Fallback bool `json:"fallback"`
	// Preview counts models upstream lists but doesn't generally offer yet.
	Preview bool `json:"preview"`
	// List shows aliases in /v1/models.
	List  bool    `json:"list"`
	Lines []Alias `json:"lines"`
}

type Alias struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
	// Match is a glob over model names, e.g. "gpt-*-sol".
	Match string `json:"match"`
	// Pin holds the alias at one model instead of the newest.
	Pin string `json:"pin"`
	// Effort applies when the request doesn't set one.
	Effort string `json:"effort"`
}

func DefaultAliases() AliasSettings {
	lines := []Alias{}
	for _, l := range [][2]string{{"opus", "claude-opus-*"}, {"sonnet", "claude-sonnet-*"}, {"haiku", "claude-haiku-*"},
		{"fable", "claude-fable-*"}, {"sol", "gpt-*-sol"}, {"astra", "gpt-*-astra"}, {"terra", "gpt-*-terra"},
		{"luna", "gpt-*-luna"}} {
		lines = append(lines, Alias{ID: l[0], Enabled: true, Name: l[0], Match: l[1]})
	}
	return AliasSettings{Enabled: true, Source: AliasSourceLive, RefreshMinutes: 60, Fallback: true, List: true, Lines: lines}
}

func (a *AliasSettings) normalize() {
	if a.Source != AliasSourceCatalog {
		a.Source = AliasSourceLive
	}
	switch {
	case a.RefreshMinutes <= 0:
		a.RefreshMinutes = 60
	case a.RefreshMinutes < 5:
		a.RefreshMinutes = 5
	}
	if a.Lines == nil {
		a.Lines = DefaultAliases().Lines
	}
	for i := range a.Lines {
		l := &a.Lines[i]
		l.Name = strings.ToLower(strings.TrimSpace(l.Name))
		l.Match = strings.TrimSpace(l.Match)
		l.Pin = strings.TrimSpace(l.Pin)
		if l.ID == "" {
			l.ID = l.Name
		}
	}
}

// RefreshInterval is how often accounts should report their models; 0 when nothing uses them.
func (a AliasSettings) RefreshInterval() time.Duration {
	if !a.Enabled || a.Source != AliasSourceLive {
		return 0
	}
	return time.Duration(a.RefreshMinutes) * time.Minute
}

func (a AliasSettings) find(name string) (Alias, bool) {
	if !a.Enabled {
		return Alias{}, false
	}
	name = strings.ToLower(name)
	for _, l := range a.Lines {
		if l.Enabled && l.Name == name && name != "" && (l.Match != "" || l.Pin != "") {
			return l, true
		}
	}
	return Alias{}, false
}

// Names lists the aliases clients can currently use.
func (a AliasSettings) Names() []string {
	names := []string{}
	for _, l := range a.Lines {
		if _, ok := a.find(l.Name); ok {
			names = append(names, l.Name)
		}
	}
	return names
}

type aliasCandidate struct {
	model     models.Model
	created   time.Time
	accounts  []int64 // nil means any account of the provider
	inCatalog bool
}

// aliasCandidates lists the models an alias could mean, newest first.
func (s Settings) aliasCandidates(a Alias, req Request) []aliasCandidate {
	catalog := s.Catalog()
	var out []aliasCandidate
	if s.Aliases.Source == AliasSourceLive && req.Inventory != nil {
		inv := req.Inventory()
		seen := map[string]bool{}
		for _, sv := range inv.Models {
			accounts := sv.Accounts
			if s.Aliases.Preview {
				accounts = slices.Concat(accounts, sv.PreviewAccounts)
			}
			if len(accounts) == 0 {
				continue
			}
			lab := models.LabForProvider[sv.Provider]
			m := models.Model{ID: sv.ID, Canonical: lab + "/" + sv.ID, Lab: lab, Provider: sv.Provider, Name: models.DisplayName(sv.ID)}
			i := slices.IndexFunc(catalog, func(c models.Model) bool { return c.Canonical == m.Canonical })
			if i >= 0 {
				m = catalog[i]
			} else if !s.UnknownModels {
				continue
			}
			seen[m.Canonical] = true
			out = append(out, aliasCandidate{m, sv.Created, slices.Concat(accounts, inv.Unreported[sv.Provider]), i >= 0})
		}
		for _, m := range catalog {
			if unreported := inv.Unreported[m.Provider]; !seen[m.Canonical] && len(unreported) > 0 {
				out = append(out, aliasCandidate{model: m, accounts: unreported, inCatalog: true})
			}
		}
	} else {
		for _, m := range catalog {
			out = append(out, aliasCandidate{model: m, inCatalog: true})
		}
	}
	out = slices.DeleteFunc(out, func(c aliasCandidate) bool { return !models.Match(a.Match, c.model) })
	slices.SortStableFunc(out, func(x, y aliasCandidate) int {
		if c := models.CompareVersions(y.model.ID, x.model.ID); c != 0 {
			return c
		}
		if !x.created.IsZero() && !y.created.IsZero() {
			if c := y.created.Compare(x.created); c != 0 {
				return c
			}
		}
		return strings.Compare(x.model.ID, y.model.ID)
	})
	return out
}

// aliasAllowed lets a key through for a model it may use directly, or for the alias by name
// ("sol" in an allow-list covers the whole line).
func aliasAllowed(patterns []string, a Alias, m models.Model) bool {
	return models.Allowed(patterns, m) || models.Allowed(patterns, models.Model{ID: a.Name, Canonical: m.Lab + "/" + a.Name})
}

// limitAccounts narrows who can serve a candidate to a model's account override.
func limitAccounts(candidate, override []int64) ([]int64, bool) {
	if candidate == nil {
		return override, true
	}
	if len(override) == 0 {
		return candidate, true
	}
	both := slices.DeleteFunc(slices.Clone(candidate), func(id int64) bool { return !slices.Contains(override, id) })
	return both, len(both) > 0
}

// pickAlias chooses the newest model this request may use and something can serve.
func (s Settings) pickAlias(a Alias, prefix string, req Request) (aliasCandidate, error) {
	err := fmt.Errorf("%w %q: no account can serve it", ErrUnknownModel, a.Name)
	var usable []aliasCandidate
	for _, c := range s.aliasCandidates(a, req) {
		m := c.model
		if prefix != "" && prefix != m.Provider && prefix != m.Lab {
			continue
		}
		o := s.Models[m.Canonical]
		switch {
		case o.Disabled:
			err = fmt.Errorf("%w: %s", ErrModelDisabled, m.ID)
			continue
		case !aliasAllowed(req.Allowed, a, m):
			err = fmt.Errorf("%w: %s", ErrNotAllowed, a.Name)
			continue
		case len(req.Providers) > 0 && !providerAllowed(req.Providers, m):
			err = fmt.Errorf("%w %q: excluded by the request's provider preferences", ErrUnknownModel, a.Name)
			continue
		}
		accounts, ok := limitAccounts(c.accounts, o.Accounts)
		if !ok {
			continue
		}
		c.accounts = accounts
		usable = append(usable, c)
	}
	if len(usable) == 0 {
		return aliasCandidate{}, err
	}
	if s.Aliases.Fallback && req.Usable != nil {
		for _, c := range usable {
			if req.Usable(c.model.Provider, c.model.ID, c.accounts) {
				return c, nil
			}
		}
	}
	return usable[0], nil
}

// ListedAlias is an alias a client can use right now, dressed up as a model.
type ListedAlias struct {
	Model  models.Model // named after the alias
	Target models.Model // what it stands for today
}

// ListedAliases are the aliases to show in a model list, as seen by this request's key.
func (s Settings) ListedAliases(req Request) []ListedAlias {
	if !s.Aliases.List || req.KeyRules.IgnoreAliases {
		return nil
	}
	req = req.withFixedInventory()
	var out []ListedAlias
	for _, name := range s.Aliases.Names() {
		req.Model = name
		route, err := s.Resolve(req)
		if err != nil || route.Alias == "" {
			continue
		}
		m := route.Model
		m.ID, m.Canonical, m.Name, m.Custom = name, m.Lab+"/"+name, models.DisplayName(name)+" (latest)", false
		out = append(out, ListedAlias{Model: m, Target: route.Model})
	}
	return out
}

// AliasCandidate is one model an alias could mean, for the dashboard.
type AliasCandidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// Accounts is how many accounts list it; -1 when that isn't known.
	Accounts int  `json:"accounts"`
	Usable   bool `json:"usable"`
}

// AliasStatus says what an alias means right now, ignoring mapping rules and key limits.
type AliasStatus struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Model      *models.Model    `json:"model,omitempty"`
	Error      string           `json:"error,omitempty"`
	Candidates []AliasCandidate `json:"candidates"`
}

func (s Settings) AliasStatuses(req Request) []AliasStatus {
	req = req.withFixedInventory()
	out := []AliasStatus{}
	for _, l := range s.Aliases.Lines {
		st := AliasStatus{ID: l.ID, Name: l.Name, Candidates: []AliasCandidate{}}
		for _, c := range s.aliasCandidates(l, req) {
			n := len(c.accounts)
			if c.accounts == nil {
				n = -1
			}
			usable := !s.Models[c.model.Canonical].Disabled
			if usable && req.Usable != nil {
				usable = req.Usable(c.model.Provider, c.model.ID, c.accounts)
			}
			st.Candidates = append(st.Candidates, AliasCandidate{c.model.ID, c.model.Name, c.model.Provider, n, usable})
		}
		if _, ok := s.Aliases.find(l.Name); ok {
			route, err := s.Resolve(Request{Model: l.Name, KeyRules: KeyRules{IgnoreGlobal: true}, Inventory: req.Inventory, Usable: req.Usable})
			if err != nil {
				st.Error = err.Error()
			} else if route.Alias != "" {
				st.Model = &route.Model
			} else {
				st.Error = "a model with this name exists, so it wins"
			}
		}
		out = append(out, st)
	}
	return out
}

// withFixedInventory reads the inventory once for a batch of lookups.
func (r Request) withFixedInventory() Request {
	if r.Inventory != nil {
		inv := r.Inventory()
		r.Inventory = func() models.Inventory { return inv }
	}
	return r
}
