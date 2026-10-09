package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/quota"
)

func windowKind(minutes int) (string, string) {
	switch {
	case minutes > 0 && minutes <= 24*60:
		h := minutes / 60
		if h < 1 {
			return quota.KindSession, strconv.Itoa(minutes) + "-minute"
		}
		return quota.KindSession, strconv.Itoa(h) + "-hour"
	case minutes >= 6*24*60 && minutes <= 8*24*60:
		return quota.KindWeekly, "Weekly"
	case minutes > 0:
		return quota.KindOther, strconv.Itoa(minutes/(24*60)) + "-day"
	}
	return quota.KindOther, "Limit"
}

// WindowsFromHeaders reads x-codex-{primary,secondary}-* headers from a Codex response.
func WindowsFromHeaders(h http.Header, now time.Time) (windows []quota.Window, plan string) {
	for _, slot := range []string{"primary", "secondary"} {
		used := h.Get("X-Codex-" + slot + "-Used-Percent")
		if used == "" {
			continue
		}
		pct, _ := strconv.ParseFloat(used, 64)
		minutes, _ := strconv.Atoi(h.Get("X-Codex-" + slot + "-Window-Minutes"))
		w := windowFrom(slot, pct, minutes, h.Get("X-Codex-"+slot+"-Reset-At"), h.Get("X-Codex-"+slot+"-Reset-After-Seconds"), now)
		windows = append(windows, w)
	}
	return windows, planName(h.Get("X-Codex-Plan-Type"))
}

func windowFrom(slot string, used float64, minutes int, resetAt, resetAfter string, now time.Time) quota.Window {
	kind, label := windowKind(minutes)
	w := quota.Window{ID: slot, Kind: kind, Label: label, UsedPercent: clamp(used), WindowMinutes: minutes}
	if ts, err := strconv.ParseFloat(resetAt, 64); err == nil && ts > 0 {
		t := time.Unix(int64(ts), 0).UTC()
		w.ResetsAt = &t
	} else if secs, err := strconv.ParseFloat(resetAfter, 64); err == nil && secs >= 0 {
		t := now.Add(time.Duration(secs) * time.Second).UTC()
		w.ResetsAt = &t
	}
	w.Limited = w.UsedPercent >= 100
	return w
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func (p *Provider) newRequest(ctx context.Context, method, url string, c provider.Credentials, body []byte) (*http.Request, error) {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Originator", originator)
	if c.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", c.AccountID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

type usageWindow struct {
	UsedPercent        float64  `json:"used_percent"`
	LimitWindowSeconds int      `json:"limit_window_seconds"`
	ResetAfterSeconds  *float64 `json:"reset_after_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

type rateLimit struct {
	Allowed         *bool        `json:"allowed"`
	LimitReached    bool         `json:"limit_reached"`
	PrimaryWindow   *usageWindow `json:"primary_window"`
	SecondaryWindow *usageWindow `json:"secondary_window"`
}

func (w *usageWindow) toWindow(id string, now time.Time) quota.Window {
	resetAt, resetAfter := "", ""
	if w.ResetAt != nil {
		resetAt = strconv.FormatFloat(*w.ResetAt, 'f', 0, 64)
	}
	if w.ResetAfterSeconds != nil {
		resetAfter = strconv.FormatFloat(*w.ResetAfterSeconds, 'f', 0, 64)
	}
	return windowFrom(id, w.UsedPercent, w.LimitWindowSeconds/60, resetAt, resetAfter, now)
}

// FetchQuota polls /wham/usage (the data behind ChatGPT's Codex usage page) plus banked reset credits.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) (*quota.Snapshot, error) {
	req, err := p.newRequest(ctx, http.MethodGet, p.BackendBase+"/wham/usage", c, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		PlanType             string          `json:"plan_type"`
		RateLimit            *rateLimit      `json:"rate_limit"`
		AdditionalRateLimits json.RawMessage `json:"additional_rate_limits"`
		Credits              *struct {
			HasCredits bool `json:"has_credits"`
			Unlimited  bool `json:"unlimited"`
			Balance    any  `json:"balance"`
		} `json:"credits"`
		ResetCredits *struct {
			AvailableCount int `json:"available_count"`
		} `json:"rate_limit_reset_credits"`
	}
	if err := provider.DoJSON(p.client, req, &raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s := &quota.Snapshot{Plan: planName(raw.PlanType), UpdatedAt: now, FetchedAt: &now, Source: "poll", Windows: []quota.Window{}}
	if rl := raw.RateLimit; rl != nil {
		if rl.PrimaryWindow != nil {
			s.Windows = append(s.Windows, rl.PrimaryWindow.toWindow("primary", now))
		}
		if rl.SecondaryWindow != nil {
			s.Windows = append(s.Windows, rl.SecondaryWindow.toWindow("secondary", now))
		}
	}
	s.Windows = append(s.Windows, additionalWindows(raw.AdditionalRateLimits, now)...)
	if cr := raw.Credits; cr != nil {
		s.Extra = &quota.Extra{Enabled: cr.HasCredits || cr.Unlimited, Unlimited: cr.Unlimited}
		switch b := cr.Balance.(type) {
		case string:
			s.Extra.Balance = b
		case float64:
			s.Extra.Balance = strconv.FormatFloat(b, 'f', -1, 64)
		}
	}
	if raw.ResetCredits != nil {
		s.Banked = &quota.Banked{Available: raw.ResetCredits.AvailableCount}
		if s.Banked.Available > 0 {
			if credits, err := p.ResetCredits(ctx, c); err == nil {
				s.Banked.Credits = credits
			}
		}
	}
	return s, nil
}

// additionalWindows parses per-model limits; the API has used both array and map shapes.
func additionalWindows(raw json.RawMessage, now time.Time) []quota.Window {
	type entry struct {
		LimitName      string     `json:"limit_name"`
		MeteredFeature string     `json:"metered_feature"`
		RateLimit      *rateLimit `json:"rate_limit"`
	}
	var entries []entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		var m map[string]rateLimit
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
		for name, rl := range m {
			rl := rl
			entries = append(entries, entry{LimitName: name, RateLimit: &rl})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].LimitName < entries[j].LimitName })
	}
	var out []quota.Window
	for _, e := range entries {
		if e.RateLimit == nil {
			continue
		}
		name := e.LimitName
		if name == "" {
			name = e.MeteredFeature
		}
		for slot, w := range map[string]*usageWindow{"primary": e.RateLimit.PrimaryWindow, "secondary": e.RateLimit.SecondaryWindow} {
			if w == nil {
				continue
			}
			win := w.toWindow("extra:"+name+":"+slot, now)
			win.Kind = quota.KindModelWeekly
			if win.WindowMinutes > 0 && win.WindowMinutes <= 24*60 {
				win.Kind = quota.KindOther
			}
			_, span := windowKind(win.WindowMinutes)
			win.Label = name + " · " + span
			out = append(out, win)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func parseTime(v any) *time.Time {
	switch t := v.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	case float64:
		if t > 0 {
			parsed := time.Unix(int64(t), 0).UTC()
			return &parsed
		}
	}
	return nil
}

func (p *Provider) ResetCredits(ctx context.Context, c provider.Credentials) ([]quota.BankedCredit, error) {
	req, err := p.newRequest(ctx, http.MethodGet, p.BackendBase+"/wham/rate-limit-reset-credits", c, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Credits []struct {
			ID        string `json:"id"`
			ResetType string `json:"reset_type"`
			Status    string `json:"status"`
			GrantedAt any    `json:"granted_at"`
			ExpiresAt any    `json:"expires_at"`
			Title     string `json:"title"`
		} `json:"credits"`
	}
	if err := provider.DoJSON(p.client, req, &raw); err != nil {
		return nil, err
	}
	var out []quota.BankedCredit
	for _, cr := range raw.Credits {
		if cr.Status != "" && cr.Status != "available" {
			continue
		}
		out = append(out, quota.BankedCredit{ID: cr.ID, Type: cr.ResetType, Title: cr.Title,
			GrantedAt: parseTime(cr.GrantedAt), ExpiresAt: parseTime(cr.ExpiresAt)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExpiresAt == nil || out[j].ExpiresAt == nil {
			return out[j].ExpiresAt == nil && out[i].ExpiresAt != nil
		}
		return out[i].ExpiresAt.Before(*out[j].ExpiresAt)
	})
	return out, nil
}

// RedeemResult is the outcome of spending a banked reset.
type RedeemResult struct {
	Code         string   `json:"code"`
	WindowsReset []string `json:"windowsReset,omitempty"`
}

// Redeem spends one banked reset. requestID makes retries idempotent.
func (p *Provider) Redeem(ctx context.Context, c provider.Credentials, requestID, creditID string) (*RedeemResult, error) {
	payload := map[string]string{"redeem_request_id": requestID}
	if creditID != "" {
		payload["credit_id"] = creditID
	}
	body, _ := json.Marshal(payload)
	req, err := p.newRequest(ctx, http.MethodPost, p.BackendBase+"/wham/rate-limit-reset-credits/consume", c, body)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Code         string          `json:"code"`
		WindowsReset json.RawMessage `json:"windows_reset"`
	}
	if err := provider.DoJSON(p.client, req, &raw); err != nil {
		return nil, err
	}
	res := &RedeemResult{Code: raw.Code}
	var names []string
	if json.Unmarshal(raw.WindowsReset, &names) == nil {
		res.WindowsReset = names
	} else {
		var objs []map[string]any
		if json.Unmarshal(raw.WindowsReset, &objs) == nil {
			for _, o := range objs {
				for _, k := range []string{"name", "window", "id"} {
					if s, ok := o[k].(string); ok {
						res.WindowsReset = append(res.WindowsReset, s)
						break
					}
				}
			}
		}
	}
	if res.Code == "" {
		res.Code = "unknown"
	}
	res.Code = strings.ToLower(res.Code)
	return res, nil
}
