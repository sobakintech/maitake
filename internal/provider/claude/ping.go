package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sobakintech/maitake/internal/provider"
)

// Ping sends a one-token Messages request, dressed like any other Claude Code request.
func (p *Provider) Ping(ctx context.Context, c provider.Credentials, model, prompt string) (*provider.PingResult, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      p.ResolveModel(ctx, c, model),
		"max_tokens": 1,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
	})
	var cloak CloakSettings
	if p.Cloak != nil {
		cloak = p.Cloak()
	}
	session := provider.UUID()
	prepared, betas, err := PrepareMessages(body, c, Options{SessionID: session, Cloak: cloak})
	if err != nil {
		return nil, err
	}
	resp, err := p.Send(ctx, c, prepared, betas, session, false, http.Header{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	res := &provider.PingResult{Status: resp.StatusCode, Windows: WindowsFromHeaders(resp.Header)}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return res, &provider.HTTPError{Status: resp.StatusCode, Body: string(data), Header: resp.Header}
	}
	if s := string(data); strings.Contains(s, "event: error") {
		return res, fmt.Errorf("stream error: %s", streamError(s))
	}
	return res, nil
}

func streamError(s string) string {
	if i := strings.Index(s, "event: error"); i >= 0 {
		s = s[i:]
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return strings.TrimSpace(s)
}
