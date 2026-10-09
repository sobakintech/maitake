package codex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

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
