package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/provider"
)

// Lines are model families that can stand for their newest model, e.g. "haiku".
var Lines = []string{"haiku", "sonnet", "opus", "fable"}

type listedModel struct {
	ID        string    `json:"id"`
	Line      string    `json:"line"`
	CreatedAt time.Time `json:"created_at"`
	Lifecycle string    `json:"lifecycle"`
}

type modelList struct {
	mu   sync.Mutex
	at   time.Time
	list []listedModel
}

const modelListTTL = 6 * time.Hour

// ResolveModel turns a line name like "haiku" into that line's newest model the account can
// use. Anything else is returned unchanged.
func (p *Provider) ResolveModel(ctx context.Context, c provider.Credentials, model string) string {
	line := strings.ToLower(strings.TrimSpace(model))
	if !isLine(line) {
		return model
	}
	if id := newest(p.listModels(ctx, c), line); id != "" {
		return id
	}
	for _, m := range models.Builtin() {
		if m.Provider == "claude" && strings.HasPrefix(m.ID, "claude-"+line+"-") {
			return m.ID
		}
	}
	return model
}

func isLine(s string) bool {
	for _, l := range Lines {
		if s == l {
			return true
		}
	}
	return false
}

func newest(list []listedModel, line string) string {
	var best listedModel
	for _, m := range list {
		l := m.Line
		if l == "" && strings.HasPrefix(m.ID, "claude-"+line+"-") {
			l = line
		}
		if l != line || (m.Lifecycle != "" && m.Lifecycle != "active") {
			continue
		}
		if best.ID == "" || m.CreatedAt.After(best.CreatedAt) {
			best = m
		}
	}
	return best.ID
}

// listModels returns the account's model list, cached; a failed fetch keeps the last good one.
func (p *Provider) listModels(ctx context.Context, c provider.Credentials) []listedModel {
	p.models.mu.Lock()
	defer p.models.mu.Unlock()
	if p.models.list != nil && time.Since(p.models.at) < modelListTTL {
		return p.models.list
	}
	list, err := p.fetchModels(ctx, c)
	if err != nil {
		return p.models.list
	}
	p.models.list, p.models.at = list, time.Now()
	return list
}

func (p *Provider) fetchModels(ctx context.Context, c provider.Credentials) ([]listedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.APIBase+"/v1/models?limit=1000", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model list: %d", resp.StatusCode)
	}
	var out struct {
		Data []listedModel `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
