package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/store"
)

// codexCatalog caches ChatGPT's Codex model templates per client version.
type codexCatalog struct {
	mu      sync.Mutex
	entries map[string]catalogEntry
	last    []byte // most recent successful fetch, used when a refresh fails
}

type catalogEntry struct {
	data []byte
	at   time.Time
}

const catalogTTL = time.Hour

func (h *Handler) codexTemplates(ctx context.Context, version string) map[string]map[string]any {
	h.catalog.mu.Lock()
	e, ok := h.catalog.entries[version]
	last := h.catalog.last
	h.catalog.mu.Unlock()
	data := e.data
	if !ok || time.Since(e.at) > catalogTTL {
		if fresh, err := h.fetchCodexCatalog(ctx, version); err == nil {
			data = fresh
			h.catalog.mu.Lock()
			if h.catalog.entries == nil {
				h.catalog.entries = map[string]catalogEntry{}
			}
			h.catalog.entries[version] = catalogEntry{fresh, time.Now()}
			h.catalog.last = fresh
			h.catalog.mu.Unlock()
		} else if data == nil {
			data = last
		}
	}
	out := map[string]map[string]any{}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(data, &payload) == nil {
		for _, m := range payload.Models {
			if slug, _ := m["slug"].(string); slug != "" {
				out[slug] = m
			}
		}
	}
	return out
}

func (h *Handler) fetchCodexCatalog(ctx context.Context, version string) ([]byte, error) {
	lease, err := h.accounts.Pick(ctx, "codex", nil, accounts.PickOptions{})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := h.codex.ClientModels(ctx, lease.Creds, version)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(data, "models").IsArray() {
		return nil, errors.New("codex models: unexpected response")
	}
	return data, nil
}

var reasoningDescriptions = map[string]string{
	"none": "No reasoning", "minimal": "Fastest responses with minimal reasoning",
	"low": "Fast responses with lighter reasoning", "medium": "Balances speed and reasoning depth for everyday tasks",
	"high": "Greater reasoning depth for complex problems", "xhigh": "Extra high reasoning depth for complex problems",
	"max": "Maximum reasoning depth for the hardest problems",
}

// fallbackTemplate is used when ChatGPT's catalog can't be fetched (e.g. no Codex account).
func fallbackTemplate() map[string]any {
	return map[string]any{
		"description": "", "default_reasoning_level": "medium", "default_reasoning_summary": "auto",
		"shell_type": "shell_command", "visibility": "list", "supported_in_api": true,
		"input_modalities": []string{"text", "image"}, "truncation_policy": map[string]any{"mode": "tokens", "limit": 10000},
		"supports_parallel_tool_calls": true, "apply_patch_tool_type": "freeform", "experimental_supported_tools": []string{},
		"supports_reasoning_summaries": true, "supports_reasoning_summary_parameter": true, "support_verbosity": false,
		"web_search_tool_type": "text", "supports_search_tool": true, "prefer_websockets": true,
		"base_instructions": "You are Codex, a coding agent. You and the user share one workspace.",
	}
}

func cloneTemplate(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func reasoningLevels(efforts []string) []map[string]string {
	if len(efforts) == 0 {
		efforts = []string{"low", "medium", "high"}
	}
	out := make([]map[string]string, 0, len(efforts))
	for _, e := range efforts {
		out = append(out, map[string]string{"effort": e, "description": reasoningDescriptions[e]})
	}
	return out
}

// codexClientModels answers /v1/models?client_version=… in the Codex CLI's own format, so the
// model picker in Codex lists everything the key can use, Claude models included.
func (h *Handler) codexClientModels(w http.ResponseWriter, r *http.Request, owner *store.KeyOwner) {
	templates := h.codexTemplates(r.Context(), r.URL.Query().Get("client_version"))
	base := templates["gpt-5.5"]
	if base == nil {
		for _, t := range templates {
			base = t
			break
		}
	}
	if base == nil {
		base = fallbackTemplate()
	}

	available := map[string]bool{}
	for _, a := range h.accounts.List() {
		if !a.Disabled {
			available[a.Provider] = true
		}
	}
	rs := h.routing()
	out := []map[string]any{}
	priority := 1000
	for _, m := range rs.Catalog() {
		if !available[m.Provider] || rs.Models[m.Canonical].Disabled || !models.Allowed(owner.Key.AllowedModels, m) {
			continue
		}
		var entry map[string]any
		if t, ok := templates[m.ID]; ok {
			entry = cloneTemplate(t)
		} else {
			entry = cloneTemplate(base)
			priority++
			entry["priority"] = priority
			entry["description"] = m.Name + " via maitake"
			entry["supported_reasoning_levels"] = reasoningLevels(m.Efforts)
			entry["default_reasoning_level"] = "medium"
			for _, k := range []string{"upgrade", "availability_nux", "model_messages", "comp_hash", "service_tiers",
				"additional_speed_tiers", "default_service_tier"} {
				delete(entry, k)
			}
			if m.ContextLength > 0 {
				entry["context_window"], entry["max_context_window"] = m.ContextLength, m.ContextLength
			}
			if m.Provider == "claude" {
				entry["base_instructions"] = strings.ReplaceAll(stringOr(entry["base_instructions"]), "based on GPT-5", "based on Claude")
			}
		}
		entry["slug"] = m.ID
		entry["display_name"] = m.Name
		entry["visibility"] = "list"
		out = append(out, entry)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(marshalJSON(map[string]any{"models": out}))
}

func stringOr(v any) string {
	s, _ := v.(string)
	return s
}

// serveSearch forwards Codex CLI's standalone web search endpoint to a Codex account.
func (h *Handler) serveSearch(w http.ResponseWriter, r *http.Request) {
	f := formatResponses
	if h.authenticate(w, r, f) == nil {
		return
	}
	body, ok := readBody(w, r, f)
	if !ok {
		return
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) == nil {
		delete(payload, "prompt_cache_key")
		delete(payload, "prompt_cache_retention")
		body, _ = json.Marshal(payload)
	}
	s := h.settings()
	s.Normalize()
	ctx := r.Context()
	c := &call{ctx: ctx, provider: "codex", baseModel: gjson.GetBytes(body, "model").String()}
	tried := map[int64]bool{}
	var last *apiError
	for attempt := 0; attempt < s.MaxRetries; attempt++ {
		lease, err := h.accounts.Pick(ctx, "codex", tried, accounts.PickOptions{})
		if err != nil {
			if last == nil {
				last = newError(http.StatusServiceUnavailable, err.Error())
			}
			break
		}
		tried[lease.ID] = true
		resp, err := h.codex.SendSearch(ctx, lease.Creds, body, r.Header)
		if err == nil && resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			if creds, rerr := h.accounts.Credentials(ctx, lease.ID, true); rerr == nil {
				resp, err = h.codex.SendSearch(ctx, creds, body, r.Header)
			} else {
				err = rerr
			}
		}
		if err != nil {
			last = newError(http.StatusBadGateway, err.Error())
			continue
		}
		if resp.StatusCode < 400 {
			defer resp.Body.Close()
			if ct := resp.Header.Get("Content-Type"); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
		fl := failure{status: resp.StatusCode, body: string(data), headers: resp.Header}
		d := h.decide(c, fl, s)
		h.applyCooldown(c, lease, fl, d, upstreamMessage(fl.body))
		last = clientError("codex", fl)
		if !d.retry {
			break
		}
	}
	if last == nil {
		last = newError(http.StatusServiceUnavailable, "no available codex accounts")
	}
	writeError(w, f, last)
}
