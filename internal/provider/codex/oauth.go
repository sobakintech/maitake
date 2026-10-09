// Package codex implements ChatGPT/Codex subscription accounts.
package codex

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
	authorizeURL      = "https://auth.openai.com/oauth/authorize"
	tokenURL          = "https://auth.openai.com/oauth/token"
	clientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	RedirectURI       = "http://localhost:1455/auth/callback"
	deviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL    = "https://auth.openai.com/api/accounts/deviceauth/token"
	DeviceVerifyURL   = "https://auth.openai.com/codex/device"
	deviceRedirectURI = "https://auth.openai.com/deviceauth/callback"
)

type Provider struct {
	client *http.Client
	// BackendBase is overridable for tests.
	BackendBase string
}

func New(client *http.Client) *Provider {
	return &Provider{client: client, BackendBase: "https://chatgpt.com/backend-api"}
}

func (p *Provider) ID() string { return "codex" }

func (p *Provider) RefreshLead() time.Duration { return 24 * time.Hour }

func (p *Provider) AuthURL(state, challenge string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI)
	q.Set("scope", "openid email profile offline_access")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("prompt", "login")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	return authorizeURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (p *Provider) postForm(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var tr tokenResponse
	if err := provider.DoJSON(p.client, req, &tr); err != nil {
		var he *provider.HTTPError
		if errors.As(err, &he) && (strings.Contains(he.Body, "invalid_grant") || strings.Contains(he.Body, "refresh_token_reused")) {
			return nil, provider.ErrInvalidGrant
		}
		return nil, err
	}
	if tr.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}
	return &tr, nil
}

func (p *Provider) Exchange(ctx context.Context, code, verifier string) (provider.Credentials, provider.Identity, error) {
	return p.exchange(ctx, code, verifier, RedirectURI)
}

func (p *Provider) exchange(ctx context.Context, code, verifier, redirect string) (provider.Credentials, provider.Identity, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("code_verifier", verifier)
	tr, err := p.postForm(ctx, form)
	if err != nil {
		return provider.Credentials{}, provider.Identity{}, err
	}
	return finish(provider.Credentials{}, tr)
}

func (p *Provider) Refresh(ctx context.Context, c provider.Credentials) (provider.Credentials, provider.Identity, error) {
	if c.RefreshToken == "" {
		return c, provider.Identity{}, provider.ErrInvalidGrant
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.RefreshToken)
	form.Set("scope", "openid profile email")
	tr, err := p.postForm(ctx, form)
	if err != nil {
		return c, provider.Identity{}, err
	}
	return finish(c, tr)
}

func finish(c provider.Credentials, tr *tokenResponse) (provider.Credentials, provider.Identity, error) {
	now := time.Now().UTC()
	c.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		c.RefreshToken = tr.RefreshToken
	}
	if tr.IDToken != "" {
		c.IDToken = tr.IDToken
	}
	c.ExpiresAt = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	if tr.ExpiresIn <= 0 {
		c.ExpiresAt = jwtExpiry(c.AccessToken, now.Add(time.Hour))
	}
	c.LastRefresh = now
	id := identityFromIDToken(c.IDToken)
	if id.ExternalID != "" {
		c.AccountID = id.ExternalID
	}
	return c, id, nil
}

func jwtExpiry(token string, fallback time.Time) time.Time {
	claims, err := provider.JWTClaims(token)
	if err != nil {
		return fallback
	}
	if exp, ok := claims["exp"].(float64); ok {
		return time.Unix(int64(exp), 0).UTC()
	}
	return fallback
}

func identityFromIDToken(idToken string) provider.Identity {
	claims, err := provider.JWTClaims(idToken)
	if err != nil {
		return provider.Identity{}
	}
	id := provider.Identity{}
	id.Email, _ = claims["email"].(string)
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		id.ExternalID, _ = auth["chatgpt_account_id"].(string)
		id.Plan, _ = auth["chatgpt_plan_type"].(string)
	}
	id.Plan = planName(id.Plan)
	id.Label = id.Email
	return id
}

func planName(p string) string {
	if p == "" {
		return ""
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

// DeviceCode is an in-progress device login.
type DeviceCode struct {
	DeviceAuthID string
	UserCode     string
	Interval     time.Duration
}

func (p *Provider) StartDevice(ctx context.Context) (*DeviceCode, error) {
	body, _ := json.Marshal(map[string]string{"client_id": clientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var resp struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := provider.DoJSON(p.client, req, &resp); err != nil {
		return nil, err
	}
	dc := &DeviceCode{DeviceAuthID: resp.DeviceAuthID, UserCode: resp.UserCode, Interval: 5 * time.Second}
	if dc.UserCode == "" {
		dc.UserCode = resp.UserCodeAlt
	}
	var secs float64
	if json.Unmarshal(resp.Interval, &secs) == nil && secs > 0 {
		dc.Interval = time.Duration(secs * float64(time.Second))
	} else {
		var s string
		if json.Unmarshal(resp.Interval, &s) == nil {
			if d, err := time.ParseDuration(s + "s"); err == nil && d > 0 {
				dc.Interval = d
			}
		}
	}
	if dc.DeviceAuthID == "" || dc.UserCode == "" {
		return nil, errors.New("device code response missing fields")
	}
	return dc, nil
}

// ErrPending means the user hasn't approved the device code yet.
var ErrPending = errors.New("authorization pending")

// PollDevice checks once whether the device code was approved and, if so, completes the login.
func (p *Provider) PollDevice(ctx context.Context, dc *DeviceCode) (provider.Credentials, provider.Identity, error) {
	body, _ := json.Marshal(map[string]string{"device_auth_id": dc.DeviceAuthID, "user_code": dc.UserCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, bytes.NewReader(body))
	if err != nil {
		return provider.Credentials{}, provider.Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var resp struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := provider.DoJSON(p.client, req, &resp); err != nil {
		var he *provider.HTTPError
		if errors.As(err, &he) && (he.Status == http.StatusForbidden || he.Status == http.StatusNotFound) {
			return provider.Credentials{}, provider.Identity{}, ErrPending
		}
		return provider.Credentials{}, provider.Identity{}, err
	}
	if resp.AuthorizationCode == "" {
		return provider.Credentials{}, provider.Identity{}, ErrPending
	}
	return p.exchange(ctx, resp.AuthorizationCode, resp.CodeVerifier, deviceRedirectURI)
}

// ImportCredentials accepts maitake's format, CLIProxyAPI auth files, and Codex CLI's ~/.codex/auth.json.
func ImportCredentials(raw []byte) (provider.Credentials, provider.Identity, error) {
	var probe struct {
		Tokens *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
		Expired      string `json:"expired"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return provider.Credentials{}, provider.Identity{}, err
	}
	c := provider.Credentials{IDToken: probe.IDToken, AccessToken: probe.AccessToken,
		RefreshToken: probe.RefreshToken, AccountID: probe.AccountID}
	if t := probe.Tokens; t != nil {
		c.IDToken, c.AccessToken, c.RefreshToken, c.AccountID = t.IDToken, t.AccessToken, t.RefreshToken, t.AccountID
	}
	for _, v := range []string{probe.Expired, probe.ExpiresAt} {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.ExpiresAt = t
			break
		}
	}
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = jwtExpiry(c.AccessToken, time.Now())
	}
	if c.AccessToken == "" && c.RefreshToken == "" {
		return c, provider.Identity{}, errors.New("no Codex tokens found in JSON")
	}
	id := identityFromIDToken(c.IDToken)
	if c.AccountID == "" {
		c.AccountID = id.ExternalID
	}
	if id.ExternalID == "" {
		id.ExternalID = c.AccountID
	}
	return c, id, nil
}
