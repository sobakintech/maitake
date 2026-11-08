package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/provider"
)

// ClientModels fetches the model catalog the Codex CLI shows, as ChatGPT serves it for a
// client version: per-model instructions, reasoning levels and tool settings.
func (p *Provider) ClientModels(ctx context.Context, c provider.Credentials, clientVersion string) ([]byte, error) {
	u := p.BackendBase + "/codex/models?client_version=" + url.QueryEscape(clientVersion)
	req, err := p.newRequest(ctx, http.MethodGet, u, c, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex models: %d", resp.StatusCode)
	}
	return body, nil
}

// listClientVersion is deliberately high: ChatGPT leaves out models whose minimal_client_version
// is above the one asked about, even though requests for them work from any version.
const listClientVersion = "99.0.0"

// ListModels reports the models this account can use. Anything not "list" visibility counts as a preview.
func (p *Provider) ListModels(ctx context.Context, c provider.Credentials) ([]models.Listed, error) {
	data, err := p.ClientModels(ctx, c, listClientVersion)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("codex models: %w", err)
	}
	out := make([]models.Listed, 0, len(payload.Models))
	for _, m := range payload.Models {
		if m.Slug != "" {
			out = append(out, models.Listed{ID: m.Slug, Preview: m.Visibility != "" && m.Visibility != "list"})
		}
	}
	return out, nil
}

// SendSearch forwards a Codex CLI /alpha/search request.
func (p *Provider) SendSearch(ctx context.Context, c provider.Credentials, body []byte, clientHeaders http.Header) (*http.Response, error) {
	req, err := p.newRequest(ctx, http.MethodPost, p.BackendBase+"/codex/alpha/search", c, body)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Version", "Session_id", "X-Client-Request-Id"} {
		if v := clientHeaders.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	return p.client.Do(req)
}
