package routing

import (
	"errors"
	"slices"
	"testing"

	"github.com/sobakintech/maitake/internal/models"
)

func setLine(s *Settings, name string, edit func(*Alias)) {
	for i := range s.Aliases.Lines {
		if s.Aliases.Lines[i].Name == name {
			edit(&s.Aliases.Lines[i])
		}
	}
}

func TestAliasFromCatalog(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*Settings)
		req    Request
		model  string
		effort string
		alias  string
		err    error
	}{
		{name: "newest sol", req: Request{Model: "sol"}, model: "gpt-6.1-sol", alias: "sol"},
		{name: "case and effort", req: Request{Model: "SOL(high)"}, model: "gpt-6.1-sol", effort: "high", alias: "sol"},
		{name: "provider pin", req: Request{Model: "codex/sol"}, model: "gpt-6.1-sol", alias: "sol"},
		{name: "lab prefix", req: Request{Model: "openai/sol"}, model: "gpt-6.1-sol", alias: "sol"},
		{name: "wrong provider pin", req: Request{Model: "claude/sol"}, err: ErrUnknownModel},
		{name: "opus", req: Request{Model: "opus"}, model: "claude-opus-5-5", alias: "opus"},
		{name: "fable", req: Request{Model: "fable"}, model: "claude-fable-5-1", alias: "fable"},
		{name: "terra", req: Request{Model: "terra"}, model: "gpt-5.6-terra", alias: "terra"},
		{name: "disabled model skipped", req: Request{Model: "sol"}, model: "gpt-6-sol", alias: "sol",
			edit: func(s *Settings) { s.Models["openai/gpt-6.1-sol"] = ModelOverride{Disabled: true} }},
		{name: "allow list picks newest allowed", req: Request{Model: "sol", Allowed: []string{"gpt-6-sol"}}, model: "gpt-6-sol", alias: "sol"},
		{name: "alias in allow list covers the line", req: Request{Model: "sol", Allowed: []string{"sol"}}, model: "gpt-6.1-sol", alias: "sol"},
		{name: "allow list refuses", req: Request{Model: "sol", Allowed: []string{"anthropic/*"}}, err: ErrNotAllowed},
		{name: "provider prefs", req: Request{Model: "sol", Providers: []string{"anthropic"}}, err: ErrUnknownModel},
		{name: "rule target can be an alias", req: Request{Model: "claude-haiku-4-5-20251001"}, model: "gpt-6-luna", effort: "low", alias: "luna",
			edit: func(s *Settings) { s.Mappings = []Rule{{Enabled: true, Match: "claude-haiku-*", Target: "luna(low)"}} }},
		{name: "rule beats alias", req: Request{Model: "sol"}, model: "gpt-5.5",
			edit: func(s *Settings) { s.Mappings = []Rule{{Enabled: true, Match: "sol", Target: "gpt-5.5"}} }},
		{name: "real model beats alias", req: Request{Model: "sol"}, model: "sol",
			edit: func(s *Settings) { s.Custom = []CustomModel{{ID: "sol", Provider: "codex"}} }},
		{name: "feature off", req: Request{Model: "sol"}, err: ErrUnknownModel,
			edit: func(s *Settings) { s.Aliases.Enabled = false }},
		{name: "line off", req: Request{Model: "sol"}, err: ErrUnknownModel,
			edit: func(s *Settings) { setLine(s, "sol", func(a *Alias) { a.Enabled = false }) }},
		{name: "key turns aliases off", req: Request{Model: "sol", KeyRules: KeyRules{IgnoreAliases: true}}, err: ErrUnknownModel},
		{name: "pin", req: Request{Model: "sol"}, model: "gpt-6-sol", alias: "sol",
			edit: func(s *Settings) { setLine(s, "sol", func(a *Alias) { a.Pin = "gpt-6-sol" }) }},
		{name: "pin honours the key", req: Request{Model: "sol", Allowed: []string{"gpt-5.5"}}, err: ErrNotAllowed,
			edit: func(s *Settings) { setLine(s, "sol", func(a *Alias) { a.Pin = "gpt-6-sol" }) }},
		{name: "pin honours disabling", req: Request{Model: "sol"}, err: ErrModelDisabled,
			edit: func(s *Settings) {
				setLine(s, "sol", func(a *Alias) { a.Pin = "gpt-6-sol" })
				s.Models["openai/gpt-6-sol"] = ModelOverride{Disabled: true}
			}},
		{name: "pin only alias", req: Request{Model: "best"}, model: "gpt-5.5", alias: "best",
			edit: func(s *Settings) {
				s.Aliases.Lines = append(s.Aliases.Lines, Alias{Enabled: true, Name: "best", Pin: "gpt-5.5"})
			}},
	}
	for _, c := range cases {
		s := DefaultSettings()
		if c.edit != nil {
			c.edit(&s)
		}
		s.Normalize()
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
		if route.Model.ID != c.model || route.Effort != c.effort || route.Alias != c.alias {
			t.Errorf("%s: got model=%s effort=%s alias=%q", c.name, route.Model.ID, route.Effort, route.Alias)
		}
	}
}

func TestAliasDefaultEffort(t *testing.T) {
	s := DefaultSettings()
	s.Models["openai/gpt-6.1-sol"] = ModelOverride{Effort: "low"}
	route, _ := s.Resolve(Request{Model: "sol"})
	if route.DefaultEffort != "low" {
		t.Errorf("model override should apply: %q", route.DefaultEffort)
	}
	setLine(&s, "sol", func(a *Alias) { a.Effort = "high" })
	route, _ = s.Resolve(Request{Model: "sol"})
	if route.DefaultEffort != "high" {
		t.Errorf("alias effort should win: %q", route.DefaultEffort)
	}
}

func TestAliasFromAccountLists(t *testing.T) {
	inv := models.Inventory{
		Models: []models.Served{
			{ID: "gpt-6-luna", Provider: "codex", Accounts: []int64{2}},
			{ID: "gpt-6.1-sol", Provider: "codex", Accounts: []int64{1, 2}},
			{ID: "gpt-7-sol", Provider: "codex", Accounts: []int64{1}},
			{ID: "gpt-8-sol", Provider: "codex", PreviewAccounts: []int64{1}},
		},
		Unreported: map[string][]int64{"claude": {5}},
	}
	with := func(i models.Inventory, usable func(string, string, []int64) bool) Request {
		return Request{Inventory: func() models.Inventory { return i }, Usable: usable}
	}
	notSeven := func(_, model string, _ []int64) bool { return model != "gpt-7-sol" }
	partly := inv
	partly.Unreported = map[string][]int64{"codex": {9}}

	cases := []struct {
		name     string
		edit     func(*Settings)
		model    string
		req      Request
		want     string
		accounts []int64
		inCat    bool
		err      error
	}{
		{name: "newest only on the accounts listing it", model: "sol", req: with(inv, nil), want: "gpt-7-sol", accounts: []int64{1}},
		{name: "previews are opt in", model: "sol", req: with(inv, nil), want: "gpt-8-sol", accounts: []int64{1},
			edit: func(s *Settings) { s.Aliases.Preview = true }},
		{name: "falls back when nothing can serve the newest", model: "sol", req: with(inv, notSeven), want: "gpt-6.1-sol", accounts: []int64{1, 2}, inCat: true},
		{name: "fallback off stays on the newest", model: "sol", req: with(inv, notSeven), want: "gpt-7-sol", accounts: []int64{1},
			edit: func(s *Settings) { s.Aliases.Fallback = false }},
		{name: "catalog only setting respected", model: "sol", req: with(inv, nil), want: "gpt-6.1-sol", accounts: []int64{1, 2}, inCat: true,
			edit: func(s *Settings) { s.UnknownModels = false }},
		{name: "model account override narrows", model: "sol", req: with(inv, nil), want: "gpt-6.1-sol", accounts: []int64{2}, inCat: true,
			edit: func(s *Settings) {
				s.Models["openai/gpt-7-sol"] = ModelOverride{Accounts: []int64{2}}
				s.Models["openai/gpt-6.1-sol"] = ModelOverride{Accounts: []int64{2, 3}}
			}},
		{name: "nobody lists the line", model: "astra", req: with(inv, nil), err: ErrUnknownModel},
		{name: "no reports yet uses the catalog", model: "opus", req: with(inv, nil), want: "claude-opus-5-5", accounts: []int64{5}, inCat: true},
		{name: "unreported account may serve listed models", model: "sol", req: with(partly, nil), want: "gpt-7-sol", accounts: []int64{1, 9}},
		{name: "unreported account may serve catalog models", model: "astra", req: with(partly, nil), want: "gpt-6-astra", accounts: []int64{9}, inCat: true},
		{name: "catalog source ignores reports", model: "sol", req: with(inv, nil), want: "gpt-6.1-sol", inCat: true,
			edit: func(s *Settings) { s.Aliases.Source = AliasSourceCatalog }},
	}
	for _, c := range cases {
		s := DefaultSettings()
		if c.edit != nil {
			c.edit(&s)
		}
		s.Normalize()
		c.req.Model = c.model
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
		if route.Model.ID != c.want || !slices.Equal(route.Accounts, c.accounts) || route.Catalog != c.inCat {
			t.Errorf("%s: got model=%s accounts=%v catalog=%v", c.name, route.Model.ID, route.Accounts, route.Catalog)
		}
	}
}

func TestListedAliasesAndStatus(t *testing.T) {
	s := DefaultSettings()
	inv := models.Inventory{Models: []models.Served{{ID: "gpt-7-sol", Provider: "codex", Accounts: []int64{1}}}}
	req := Request{Inventory: func() models.Inventory { return inv }}

	listed := s.ListedAliases(req)
	if len(listed) != 1 || listed[0].Model.ID != "sol" || listed[0].Model.Canonical != "openai/sol" ||
		listed[0].Model.Name != "Sol (latest)" || listed[0].Target.ID != "gpt-7-sol" {
		t.Fatalf("listed = %+v", listed)
	}
	if got := s.ListedAliases(Request{KeyRules: KeyRules{IgnoreAliases: true}, Inventory: req.Inventory}); got != nil {
		t.Errorf("key without aliases shouldn't see them: %+v", got)
	}
	s.Aliases.List = false
	if got := s.ListedAliases(req); got != nil {
		t.Errorf("listing turned off: %+v", got)
	}

	var sol, opus AliasStatus
	for _, st := range s.AliasStatuses(req) {
		switch st.Name {
		case "sol":
			sol = st
		case "opus":
			opus = st
		}
	}
	if sol.Model == nil || sol.Model.ID != "gpt-7-sol" || len(sol.Candidates) != 1 || sol.Candidates[0].Accounts != 1 {
		t.Errorf("sol status = %+v", sol)
	}
	if opus.Model != nil || opus.Error == "" {
		t.Errorf("opus has no accounts: %+v", opus)
	}
}

func TestAliasDefaultsSurviveNormalize(t *testing.T) {
	s := Settings{}
	s.Normalize()
	if s.Aliases.Source != AliasSourceLive || s.Aliases.RefreshMinutes != 60 || len(s.Aliases.Lines) == 0 {
		t.Errorf("missing aliases should get defaults: %+v", s.Aliases)
	}
	s.Aliases.Lines = []Alias{}
	s.Aliases.RefreshMinutes = 1
	s.Normalize()
	if len(s.Aliases.Lines) != 0 || s.Aliases.RefreshMinutes != 5 {
		t.Errorf("deleted aliases must stay deleted and the interval floored: %+v", s.Aliases)
	}
	s.Aliases = DefaultAliases()
	if s.Aliases.RefreshInterval() == 0 {
		t.Error("live aliases need refreshing")
	}
	s.Aliases.Source = AliasSourceCatalog
	if s.Aliases.RefreshInterval() != 0 {
		t.Error("catalog aliases don't")
	}
}
