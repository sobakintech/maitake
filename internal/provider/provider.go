// Package provider holds what Claude and Codex accounts have in common: credentials,
// identity, and the operations the account manager needs from each provider.
package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/quota"
)

type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastRefresh  time.Time `json:"last_refresh"`

	// Codex: ChatGPT workspace/account id sent as ChatGPT-Account-Id.
	AccountID string `json:"account_id,omitempty"`
	// Claude: profile identifiers and a stable device id used in request metadata.
	AccountUUID string `json:"account_uuid,omitempty"`
	OrgUUID     string `json:"organization_uuid,omitempty"`
	DeviceID    string `json:"device_id,omitempty"`

	// WebSession is a claude.ai session cookie, kept in its own column and attached only when
	// reading subscription details.
	WebSession string `json:"-"`
}

func ParseCredentials(raw string) (Credentials, error) {
	var c Credentials
	err := json.Unmarshal([]byte(raw), &c)
	return c, err
}

func (c Credentials) Encode() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// Identity describes the subscription behind a set of credentials.
type Identity struct {
	Email      string
	ExternalID string // stable id used to dedupe re-logins
	Plan       string
	Label      string
}

type Provider interface {
	ID() string
	// Refresh exchanges the refresh token for new credentials.
	Refresh(ctx context.Context, c Credentials) (Credentials, Identity, error)
	// RefreshLead is how long before expiry credentials should be refreshed in the background.
	RefreshLead() time.Duration
	// FetchQuota polls the provider's usage endpoint.
	FetchQuota(ctx context.Context, c Credentials) (*quota.Snapshot, error)
}

// ErrInvalidGrant means the refresh token is dead and the account needs a new login.
var ErrInvalidGrant = errors.New("refresh token rejected; log in again")

// HTTPError is a non-2xx upstream response.
type HTTPError struct {
	Status int
	Body   string
	Header http.Header
}

func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("upstream status %d: %s", e.Status, strings.TrimSpace(body))
}

// ReadError drains a failed response into an *HTTPError.
func ReadError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return &HTTPError{Status: resp.StatusCode, Body: string(body), Header: resp.Header}
}

// DoJSON sends req and decodes a 2xx JSON response into dst.
func DoJSON(client *http.Client, req *http.Request, dst any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ReadError(resp)
	}
	defer resp.Body.Close()
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// PKCE returns a verifier and its S256 challenge.
func PKCE() (verifier, challenge string) {
	b := make([]byte, 96)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func RandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func UUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// JWTClaims decodes a JWT payload without verifying it (tokens come straight from the issuer over TLS).
func JWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	return claims, json.Unmarshal(raw, &claims)
}

// ParseCallback extracts code and state from a pasted OAuth callback URL, a bare query
// string, or a raw "code#state" value.
func ParseCallback(input string) (code, state string) {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "code=") {
		if i := strings.IndexAny(input, "?#"); i >= 0 {
			input = input[i+1:]
		}
		for _, part := range strings.FieldsFunc(input, func(r rune) bool { return r == '?' || r == '#' }) {
			q, err := url.ParseQuery(part)
			if err != nil {
				continue
			}
			if v := q.Get("code"); v != "" {
				code = v
			}
			if v := q.Get("state"); v != "" {
				state = v
			}
		}
	} else {
		code = input
	}
	if c, s, ok := strings.Cut(code, "#"); ok {
		code = c
		if s != "" {
			state = s
		}
	}
	return code, state
}
