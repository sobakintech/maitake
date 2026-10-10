package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
	"github.com/sobakintech/maitake/internal/quota"
)

// Banked resets are Claude Code's "cedar_ember" program. /api/oauth/usage?cedar_ember=1 lists the
// grants, but only to a claude-cli user agent of 2.1.280 or newer.
const resetProgram = "cedar_ember"

type resetGrant struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	ResetsLeft       int      `json:"resets_left"`
	StartsAt         *string  `json:"starts_at"`
	EndsAt           *string  `json:"ends_at"`
	Clears           []string `json:"clears"`
	Paused           bool     `json:"paused"`
	UseRequiresLimit bool     `json:"use_requires_limit"`
}

type cedarEmber struct {
	Eligible         bool         `json:"eligible"`
	IneligibleReason *string      `json:"ineligible_reason"`
	Grants           []resetGrant `json:"grants"`
	NextGrantID      *string      `json:"next_grant_id"`
	CooldownUntil    *string      `json:"cooldown_until"`
}

var clearsLabels = map[string]string{"five_hour": "5-hour", "seven_day": "Weekly"}

func timeOf(s *string) *time.Time {
	if s == nil {
		return nil
	}
	return parseResetTime(*s)
}

// bankedFromUsage reads the cedar_ember block. It returns nil when the block is missing or this
// client isn't eligible to see it, so no reading isn't shown as zero resets.
func bankedFromUsage(raw json.RawMessage, now time.Time) *quota.Banked {
	var ce cedarEmber
	if len(raw) == 0 || json.Unmarshal(raw, &ce) != nil || !ce.Eligible {
		return nil
	}
	b := &quota.Banked{}
	if t := timeOf(ce.CooldownUntil); t != nil && t.After(now) {
		b.CooldownUntil = t
	}
	next := ""
	if ce.NextGrantID != nil {
		next = *ce.NextGrantID
	}
	for _, g := range ce.Grants {
		ends := timeOf(g.EndsAt)
		if g.ID == "" || g.ResetsLeft <= 0 || g.Paused || (ends != nil && !ends.After(now)) {
			continue
		}
		c := quota.BankedCredit{ID: g.ID, Type: resetProgram, Title: g.Label, GrantedAt: timeOf(g.StartsAt),
			ExpiresAt: ends, Count: g.ResetsLeft, RequiresLimit: g.UseRequiresLimit}
		for _, w := range g.Clears {
			if l, ok := clearsLabels[w]; ok {
				c.Clears = append(c.Clears, l)
			} else {
				c.Clears = append(c.Clears, titleCase(w))
			}
		}
		b.Available += g.ResetsLeft
		b.Credits = append(b.Credits, c)
	}
	// The grant Claude would spend next goes first, then soonest expiry.
	sort.SliceStable(b.Credits, func(i, j int) bool {
		ci, cj := b.Credits[i], b.Credits[j]
		if (ci.ID == next) != (cj.ID == next) {
			return ci.ID == next
		}
		if ci.ExpiresAt == nil || cj.ExpiresAt == nil {
			return cj.ExpiresAt == nil && ci.ExpiresAt != nil
		}
		return ci.ExpiresAt.Before(*cj.ExpiresAt)
	})
	return b
}

var claimCodes = map[string]string{
	"reset":        "reset",
	"already_used": "already_redeemed",
	"not_limited":  "nothing_to_reset",
	"ineligible":   "no_credit",
	"cooldown":     "cooldown",
	"unavailable":  "unconfirmed",
}

// Redeem claims one banked reset. With no creditID it claims the grant Claude names as next.
func (p *Provider) Redeem(ctx context.Context, c provider.Credentials, requestID, creditID string) (*provider.RedeemResult, error) {
	if c.OrgUUID == "" {
		return nil, errors.New("organization unknown; log in to this account again")
	}
	if creditID == "" {
		raw, err := p.usage(ctx, c)
		if err != nil {
			return nil, err
		}
		var ce cedarEmber
		_ = json.Unmarshal(raw["cedar_ember"], &ce)
		if !ce.Eligible || ce.NextGrantID == nil || *ce.NextGrantID == "" {
			return &provider.RedeemResult{Code: "no_credit"}, nil
		}
		creditID = *ce.NextGrantID
	}
	body, _ := json.Marshal(map[string]string{"program": resetProgram, "grant_id": creditID, "request_id": requestID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.APIBase+"/api/organizations/"+url.PathEscape(c.OrgUUID)+"/reset_rate_limits", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setOAuthHeaders(req, c)
	req.Header.Set("Content-Type", "application/json")
	var raw struct {
		Result string `json:"result"`
	}
	if err := provider.DoJSON(p.http(), req, &raw); err != nil {
		return nil, err
	}
	code, ok := claimCodes[raw.Result]
	if !ok {
		code = "unknown"
		if raw.Result != "" {
			code = raw.Result
		}
	}
	return &provider.RedeemResult{Code: code}, nil
}
