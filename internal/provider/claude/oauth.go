// Package claude implements Claude (Anthropic OAuth / Claude Code subscription) accounts.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
)

const (
	authorizeURL = "https://claude.ai/oauth/authorize"
	tokenURL     = "https://platform.claude.com/v1/oauth/token"
	clientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	RedirectURI  = "http://localhost:54545/callback"
	scopes       = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

type Provider struct {
	client *http.Client
	// APIBase is overridable for tests.
	APIBase string
	// Cloak supplies the current disguise settings; nil means defaults off.
	Cloak func() CloakSettings
	// fingerprint is used instead of client when TLS fingerprinting is on. It can't go through
	// an outbound proxy, so it's nil when one is configured.
	fingerprint *http.Client
}

func New(client *http.Client, direct bool) *Provider {
	p := &Provider{client: client, APIBase: "https://api.anthropic.com"}
	if direct {
		p.fingerprint = newFingerprintClient()
	}
	return p
}

// http picks the fingerprinted client when enabled.
func (p *Provider) http() *http.Client {
	if p.fingerprint != nil && p.Cloak != nil && p.Cloak().TLSFingerprint {
		return p.fingerprint
	}
	return p.client
}

func (p *Provider) ID() string { return "claude" }

func (p *Provider) RefreshLead() time.Duration { return 4 * time.Hour }

func (p *Provider) AuthURL(state, challenge string) string {
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI)
	q.Set("scope", scopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Account      struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account"`
	Organization struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	} `json:"organization"`
}

func authHeaders(h http.Header) {
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", "axios/1.15.2")
}

func (p *Provider) tokenRequest(ctx context.Context, body any) (*tokenResponse, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	authHeaders(req.Header)
	var tr tokenResponse
	if err := provider.DoJSON(p.http(), req, &tr); err != nil {
		var he *provider.HTTPError
		if errors.As(err, &he) && strings.Contains(he.Body, "invalid_grant") {
			return nil, provider.ErrInvalidGrant
		}
		return nil, err
	}
	if tr.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}
	return &tr, nil
}

// Exchange completes a login with the code from the callback URL.
func (p *Provider) Exchange(ctx context.Context, code, state, verifier string) (provider.Credentials, provider.Identity, error) {
	// Field order matches Claude Code's request.
	body := struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
		CodeVerifier string `json:"code_verifier"`
		State        string `json:"state"`
	}{"authorization_code", code, RedirectURI, clientID, verifier, state}
	tr, err := p.tokenRequest(ctx, body)
	if err != nil {
		return provider.Credentials{}, provider.Identity{}, err
	}
	creds := provider.Credentials{DeviceID: provider.RandomHex(32)}
	return p.finish(ctx, creds, tr)
}

func (p *Provider) Refresh(ctx context.Context, c provider.Credentials) (provider.Credentials, provider.Identity, error) {
	if c.RefreshToken == "" {
		return c, provider.Identity{}, provider.ErrInvalidGrant
	}
	body := struct {
		ClientID     string `json:"client_id"`
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}{clientID, "refresh_token", c.RefreshToken, scopes}
	tr, err := p.tokenRequest(ctx, body)
	if err != nil {
		return c, provider.Identity{}, err
	}
	return p.finish(ctx, c, tr)
}

func (p *Provider) finish(ctx context.Context, c provider.Credentials, tr *tokenResponse) (provider.Credentials, provider.Identity, error) {
	now := time.Now().UTC()
	c.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		c.RefreshToken = tr.RefreshToken
	}
	c.ExpiresAt = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	c.LastRefresh = now
	if c.DeviceID == "" {
		c.DeviceID = provider.RandomHex(32)
	}
	id := provider.Identity{Email: tr.Account.EmailAddress, ExternalID: tr.Account.UUID}
	if tr.Account.UUID != "" {
		c.AccountUUID = tr.Account.UUID
	}
	if tr.Organization.UUID != "" {
		c.OrgUUID = tr.Organization.UUID
	}

	if prof, err := p.profile(ctx, c.AccessToken); err == nil {
		if prof.Account.UUID != "" {
			c.AccountUUID = prof.Account.UUID
			id.ExternalID = prof.Account.UUID
		}
		if prof.Account.Email != "" {
			id.Email = prof.Account.Email
		}
		if prof.Organization.UUID != "" {
			c.OrgUUID = prof.Organization.UUID
		}
		id.Plan = prof.plan()
	}
	if id.ExternalID == "" {
		id.ExternalID = c.AccountUUID
	}
	id.Label = id.Email
	return c, id, nil
}

type profileResponse struct {
	Account struct {
		UUID         string `json:"uuid"`
		Email        string `json:"email"`
		HasClaudeMax bool   `json:"has_claude_max"`
		HasClaudePro bool   `json:"has_claude_pro"`
	} `json:"account"`
	Organization struct {
		UUID                  string `json:"uuid"`
		Name                  string `json:"name"`
		OrganizationType      string `json:"organization_type"`
		RateLimitTier         string `json:"rate_limit_tier"`
		SubscriptionStatus    string `json:"subscription_status"`
		SubscriptionCreatedAt string `json:"subscription_created_at"`
		BillingType           string `json:"billing_type"`
	} `json:"organization"`
}

func (p profileResponse) plan() string {
	tier := strings.ToLower(p.Organization.RateLimitTier)
	switch {
	case strings.Contains(tier, "max_20x"):
		return "Max 20x"
	case strings.Contains(tier, "max_5x"):
		return "Max 5x"
	case p.Account.HasClaudeMax || strings.Contains(tier, "max"):
		return "Max"
	case p.Account.HasClaudePro || strings.Contains(tier, "pro"):
		return "Pro"
	}
	switch t := strings.ToLower(p.Organization.OrganizationType); {
	case strings.Contains(t, "team"):
		return "Team"
	case strings.Contains(t, "enterprise"):
		return "Enterprise"
	}
	return ""
}

func (p *Provider) profile(ctx context.Context, token string) (*profileResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.APIBase+"/api/oauth/profile", nil)
	if err != nil {
		return nil, err
	}
	authHeaders(req.Header)
	req.Header.Del("Content-Type")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cache-Control", "no-cache")
	var pr profileResponse
	return &pr, provider.DoJSON(p.http(), req, &pr)
}

// ImportCredentials accepts maitake's own format, CLIProxyAPI auth files, and Claude Code's
// ~/.claude/.credentials.json.
func ImportCredentials(raw []byte) (provider.Credentials, error) {
	var probe struct {
		ClaudeAiOauth *struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Expired      string `json:"expired"`
		ExpiresAt    string `json:"expires_at"`
		AccountUUID  string `json:"account_uuid"`
		OrgUUID      string `json:"organization_uuid"`
		DeviceIDs    any    `json:"claude_device_ids"`
		DeviceID     string `json:"device_id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return provider.Credentials{}, err
	}
	c := provider.Credentials{AccountUUID: probe.AccountUUID, OrgUUID: probe.OrgUUID, DeviceID: probe.DeviceID}
	if o := probe.ClaudeAiOauth; o != nil {
		c.AccessToken, c.RefreshToken = o.AccessToken, o.RefreshToken
		if o.ExpiresAt > 0 {
			c.ExpiresAt = time.UnixMilli(o.ExpiresAt).UTC()
		}
	} else {
		c.AccessToken, c.RefreshToken = probe.AccessToken, probe.RefreshToken
		for _, v := range []string{probe.Expired, probe.ExpiresAt} {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				c.ExpiresAt = t
				break
			}
		}
	}
	switch v := probe.DeviceIDs.(type) {
	case string:
		c.DeviceID = v
	case []any:
		if len(v) > 0 {
			c.DeviceID, _ = v[0].(string)
		}
	}
	if c.RefreshToken == "" && c.AccessToken == "" {
		return c, errors.New("no Claude tokens found in JSON")
	}
	if c.DeviceID == "" {
		c.DeviceID = provider.RandomHex(32)
	}
	return c, nil
}
