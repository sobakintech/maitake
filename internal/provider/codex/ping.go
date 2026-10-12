package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/provider"
)

// Ping sends the smallest Responses request the Codex backend accepts. model may carry an
// effort suffix, e.g. "gpt-5.6-luna(low)".
func (p *Provider) Ping(ctx context.Context, c provider.Credentials, model, prompt string) (*provider.PingResult, error) {
	base, effort := models.SplitEffort(model)
	if effort == "" {
		effort = "low"
	}
	body, _ := json.Marshal(map[string]any{"model": base, "input": prompt, "instructions": "Reply with one word."})
	prepared, err := PrepareResponses(body, base, effort, "")
	if err != nil {
		return nil, err
	}
	resp, err := p.Send(ctx, c, prepared, http.Header{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	windows, plan := WindowsFromHeaders(resp.Header, time.Now())
	res := &provider.PingResult{Status: resp.StatusCode, Windows: windows, Plan: plan}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return res, &provider.HTTPError{Status: resp.StatusCode, Body: string(data), Header: resp.Header}
	}
	if s := string(data); !strings.Contains(s, "response.completed") {
		if len(s) > 300 {
			s = s[len(s)-300:]
		}
		return res, fmt.Errorf("response did not complete: %s", strings.TrimSpace(s))
	}
	return res, nil
}
