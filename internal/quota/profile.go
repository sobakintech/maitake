package quota

import (
	"encoding/json"
	"math"
	"slices"
)

// PageSettings controls the public quota page as a whole.
type PageSettings struct {
	Enabled bool `json:"enabled"`
	// Access is "key" (only keys with a view see anything) or "public" (visitors without a key
	// see PublicProfileID; keys still see their own view).
	Access string `json:"access"`
	Title  string `json:"title"`
	Notice string `json:"notice"`
	// PublicProfileID is the profile shown without a key in public mode.
	PublicProfileID int64 `json:"publicProfileId"`
	// DefaultProfileID is the pre-0.2 setting; in public mode it seeds PublicProfileID.
	DefaultProfileID int64 `json:"defaultProfileId,omitempty"`
	// RefreshSeconds is how often an open page reloads itself; 0 turns it off.
	RefreshSeconds int `json:"refreshSeconds"`
}

func DefaultPageSettings() PageSettings {
	return PageSettings{Enabled: true, Access: "key", Title: "Quota", RefreshSeconds: 60}
}

func (p *PageSettings) Normalize() {
	if p.Access != "public" {
		p.Access = "key"
	}
	if p.DefaultProfileID > 0 {
		if p.PublicProfileID == 0 && p.Access == "public" {
			p.PublicProfileID = p.DefaultProfileID
		}
		p.DefaultProfileID = 0
	}
	if len(p.Title) > 100 {
		p.Title = p.Title[:100]
	}
	if len(p.Notice) > 2000 {
		p.Notice = p.Notice[:2000]
	}
	if p.RefreshSeconds < 0 {
		p.RefreshSeconds = 0
	} else if p.RefreshSeconds > 0 {
		p.RefreshSeconds = min(max(p.RefreshSeconds, 10), 3600)
	}
}

// Profile decides what a viewer of the quota page gets to see. Filtering happens server-side.
type Profile struct {
	Providers []string `json:"providers"` // provider IDs shown; empty shows none

	// Mode: "accounts" lists each account, "pooled" shows one combined entry per provider, "both" shows both.
	Mode string `json:"mode"`
	// PoolAggregate: "average", "min", "max" (of remaining capacity) or "total" (sum of remaining, in accounts).
	PoolAggregate string `json:"poolAggregate"`

	// AccountNaming: "label", "alias" (falls back to anonymous), "anonymous" ("Account 1") or "hidden".
	AccountNaming  string  `json:"accountNaming"`
	HiddenAccounts []int64 `json:"hiddenAccounts"`

	Windows struct {
		Session     bool `json:"session"`
		Weekly      bool `json:"weekly"`
		ModelWeekly bool `json:"modelWeekly"`
		Other       bool `json:"other"`
	} `json:"windows"`
	Extra bool `json:"extra"`

	ShowPlan   bool `json:"showPlan"`
	ShowStatus bool `json:"showStatus"`
	// ShowAmounts shows money allowances (like Claude credit allowances) in dollars, per account.
	ShowAmounts bool `json:"showAmounts"`

	// NumberFormat: "remaining", "used", "bar" (rounded to 10%) or "level" (plenty/ok/low/out only).
	NumberFormat string `json:"numberFormat"`
	// ResetFormat: "both" (date and countdown), "exact", "relative" or "hidden".
	ResetFormat   string `json:"resetFormat"`
	ShowUpdatedAt bool   `json:"showUpdatedAt"`
	// Pace: "warnings" (only when a window is projected to finish near or past its limit),
	// "always" or "hidden".
	Pace string `json:"pace"`

	// Levels are the percentages left at or below which a limit counts as low or only OK.
	Levels struct {
		Low float64 `json:"low"`
		OK  float64 `json:"ok"`
	} `json:"levels"`

	// Banked: "hidden", "count", "count_expiry" or "full" (every reset with its details).
	// BankedMode: "accounts" or "pooled".
	Banked     string `json:"banked"`
	BankedMode string `json:"bankedMode"`

	// Layout is how entries are first shown: "cards" or "rows". AllowLayoutSwitch lets viewers
	// switch, remembered per browser.
	Layout            string `json:"layout"`
	AllowLayoutSwitch bool   `json:"allowLayoutSwitch"`
	// Width: "narrow", "wide" or "full".
	Width string `json:"width"`
	// AccountSort: "default" (the pool's order), "name", "most_left" or "least_left".
	AccountSort string `json:"accountSort"`
	// FeedsPosition puts the reset feeds "bottom" or "top".
	FeedsPosition    string `json:"feedsPosition"`
	ShowAccountCount bool   `json:"showAccountCount"`
	// AllowFlip lets viewers click a number to switch between left and used.
	AllowFlip bool `json:"allowFlip"`

	CodexResets  ResetsDisplay `json:"codexResets"`
	ClaudeResets ResetsDisplay `json:"claudeResets"`
}

// ResetsDisplay picks which parts of a reset feed a profile shows. The Claude feed has no
// schedule or forecast.
type ResetsDisplay struct {
	Latest    bool `json:"latest"`
	Scheduled bool `json:"scheduled"`
	Forecast  bool `json:"forecast"`
	Stats     bool `json:"stats"`
}

func DefaultProfile() Profile {
	p := Profile{
		Providers:     []string{"claude", "codex"},
		Mode:          "pooled",
		PoolAggregate: "average",
		AccountNaming: "anonymous",
		NumberFormat:  "remaining",
		ResetFormat:   "both",
		ShowUpdatedAt: true,
		Pace:          "warnings",
		Banked:        "count",
		BankedMode:    "pooled",

		Layout:            "cards",
		AllowLayoutSwitch: true,
		Width:             "narrow",
		AccountSort:       "default",
		FeedsPosition:     "bottom",
		ShowAccountCount:  true,
		AllowFlip:         true,
	}
	p.Levels.Low, p.Levels.OK = 20, 50
	p.Windows.Session = true
	p.Windows.Weekly = true
	p.Windows.ModelWeekly = true
	p.CodexResets.Scheduled = true
	return p
}

// ParseProfile decodes a stored profile on top of defaults, then normalizes enum fields.
func ParseProfile(raw json.RawMessage) Profile {
	p := DefaultProfile()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	p.Normalize()
	return p
}

func oneOf(v string, allowed ...string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return allowed[0]
}

func (p *Profile) Normalize() {
	p.Mode = oneOf(p.Mode, "pooled", "accounts", "both")
	p.PoolAggregate = oneOf(p.PoolAggregate, "average", "min", "max", "total")
	p.AccountNaming = oneOf(p.AccountNaming, "anonymous", "label", "alias", "hidden")
	p.NumberFormat = oneOf(p.NumberFormat, "remaining", "used", "bar", "level")
	p.ResetFormat = oneOf(p.ResetFormat, "both", "relative", "exact", "hidden")
	p.Pace = oneOf(p.Pace, "warnings", "always", "hidden")
	p.Banked = oneOf(p.Banked, "count", "hidden", "count_expiry", "full")
	p.BankedMode = oneOf(p.BankedMode, "pooled", "accounts")
	p.Layout = oneOf(p.Layout, "cards", "rows")
	p.Width = oneOf(p.Width, "narrow", "wide", "full")
	p.AccountSort = oneOf(p.AccountSort, "default", "name", "most_left", "least_left")
	p.FeedsPosition = oneOf(p.FeedsPosition, "bottom", "top")
	p.Levels.Low = math.Min(100, math.Max(0, p.Levels.Low))
	p.Levels.OK = math.Min(100, math.Max(p.Levels.Low, p.Levels.OK))
	if p.Providers == nil {
		p.Providers = []string{}
	}
	if p.HiddenAccounts == nil {
		p.HiddenAccounts = []int64{}
	}
}

func (p *Profile) showsKind(kind string) bool {
	switch kind {
	case KindSession:
		return p.Windows.Session
	case KindWeekly:
		return p.Windows.Weekly
	case KindModelWeekly:
		return p.Windows.ModelWeekly
	default:
		return p.Windows.Other
	}
}
