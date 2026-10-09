package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/tiktoken-go/tokenizer"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/translate"
)

// GPT-5 and later use the o200k encoding.
var o200k = sync.OnceValues(func() (tokenizer.Codec, error) { return tokenizer.Get(tokenizer.O200kBase) })

// countResponsesTokens counts a Responses request's input locally: Codex has no counting endpoint.
func countResponsesTokens(body []byte) (int64, error) {
	enc, err := o200k()
	if err != nil {
		return 0, err
	}
	root := gjson.ParseBytes(body)
	var segs []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	add(root.Get("instructions").String())
	if in := root.Get("input"); in.Type == gjson.String {
		add(in.String())
	}
	root.Get("input").ForEach(func(_, it gjson.Result) bool {
		switch it.Get("type").String() {
		case "function_call":
			add(it.Get("name").String())
			add(it.Get("arguments").String())
		case "custom_tool_call":
			add(it.Get("name").String())
			add(it.Get("input").String())
		case "function_call_output", "custom_tool_call_output":
			if out := it.Get("output"); out.IsArray() {
				out.ForEach(func(_, p gjson.Result) bool {
					add(p.Get("text").String())
					return true
				})
			} else {
				add(out.String())
			}
		default:
			if c := it.Get("content"); c.Type == gjson.String {
				add(c.String())
			} else {
				c.ForEach(func(_, p gjson.Result) bool {
					add(p.Get("text").String())
					return true
				})
			}
			add(it.Get("text").String())
		}
		return true
	})
	root.Get("tools").ForEach(func(_, t gjson.Result) bool {
		add(t.Get("name").String())
		add(t.Get("description").String())
		add(t.Get("parameters").Raw)
		return true
	})
	add(root.Get("text.format.name").String())
	add(root.Get("text.format.schema").Raw)
	n, err := enc.Count(strings.Join(segs, "\n"))
	return int64(n), err
}

// countClaudeTokens asks Claude's count_tokens endpoint, failing over between accounts.
func (h *Handler) countClaudeTokens(ctx context.Context, r *http.Request, route *routing.Route, body []byte) (int64, *apiError) {
	body, _ = sjson.SetBytes(body, "model", route.Model.ID)
	prepared, betas, err := claude.PrepareCountTokens(body, claude.Options{ClientBetas: r.Header.Values("anthropic-beta")})
	if err != nil {
		return 0, newError(http.StatusBadRequest, err.Error())
	}
	s := h.settings()
	s.Normalize()
	c := &call{ctx: ctx, provider: "claude", baseModel: route.Model.ID}
	tried := map[int64]bool{}
	var last *apiError
	for attempt := 0; attempt < s.MaxRetries; attempt++ {
		lease, err := h.accounts.Pick(ctx, "claude", tried, accounts.PickOptions{Strategy: route.Strategy,
			Accounts: route.Accounts, Model: route.Model.ID})
		if err != nil {
			if last == nil {
				last = newError(http.StatusServiceUnavailable, err.Error())
			}
			break
		}
		tried[lease.ID] = true
		resp, err := h.claude.Send(ctx, lease.Creds, prepared, betas, "", true, r.Header)
		if err == nil && resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			if creds, rerr := h.accounts.Credentials(ctx, lease.ID, true); rerr == nil {
				resp, err = h.claude.Send(ctx, creds, prepared, betas, "", true, r.Header)
			} else {
				err = rerr
			}
		}
		if err != nil {
			last = newError(http.StatusBadGateway, err.Error())
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return gjson.GetBytes(data, "input_tokens").Int(), nil
		}
		f := failure{status: resp.StatusCode, body: string(data), headers: resp.Header}
		d := h.decide(c, f, s)
		h.applyCooldown(c, lease, f, d, upstreamMessage(f.body))
		last = clientError("claude", f)
		if !d.retry {
			break
		}
	}
	if last == nil {
		last = newError(http.StatusServiceUnavailable, "no available claude accounts")
	}
	return 0, last
}

// countRequest authenticates and routes a token counting request in format f.
func (h *Handler) countRequest(w http.ResponseWriter, r *http.Request, f format, apiFormat string) (*routing.Route, []byte, bool) {
	owner := h.authenticate(w, r, f)
	if owner == nil {
		return nil, nil, false
	}
	body, ok := readBody(w, r, f)
	if !ok {
		return nil, nil, false
	}
	var keyRules routing.KeyRules
	_ = json.Unmarshal(owner.Key.ModelRules, &keyRules)
	route, err := h.routing().Resolve(routing.Request{Model: gjson.GetBytes(body, "model").String(),
		Format: apiFormat, KeyRules: keyRules, Allowed: owner.Key.AllowedModels})
	switch {
	case errors.Is(err, routing.ErrNotAllowed):
		writeError(w, f, newError(http.StatusForbidden, err.Error()))
		return nil, nil, false
	case err != nil:
		writeError(w, f, newError(http.StatusNotFound, err.Error()))
		return nil, nil, false
	}
	return route, body, true
}

// countTokens serves Anthropic's /v1/messages/count_tokens for both providers.
func (h *Handler) countTokens(w http.ResponseWriter, r *http.Request) {
	route, body, ok := h.countRequest(w, r, formatClaude, routing.FormatAnthropic)
	if !ok {
		return
	}
	var n int64
	if route.Model.Provider == "claude" {
		var e *apiError
		if n, e = h.countClaudeTokens(r.Context(), r, route, body); e != nil {
			writeError(w, formatClaude, e)
			return
		}
	} else {
		converted, err := translate.ClaudeToResponses(body, route.Model.ID, "", translate.NewToolNames())
		if err == nil {
			n, err = countResponsesTokens(converted)
		}
		if err != nil {
			writeError(w, formatClaude, newError(http.StatusBadRequest, err.Error()))
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"input_tokens":%d}`, n)
}

// inputTokens serves OpenAI's /v1/responses/input_tokens for both providers.
func (h *Handler) inputTokens(w http.ResponseWriter, r *http.Request) {
	route, body, ok := h.countRequest(w, r, formatResponses, routing.FormatResponses)
	if !ok {
		return
	}
	var n int64
	var err error
	if route.Model.Provider == "claude" {
		var converted []byte
		if converted, err = translate.ResponsesToClaude(body, route.Model.ID, "", translate.NewToolNames()); err == nil {
			var e *apiError
			if n, e = h.countClaudeTokens(r.Context(), r, route, converted); e != nil {
				writeError(w, formatResponses, e)
				return
			}
		}
	} else {
		n, err = countResponsesTokens(body)
	}
	if err != nil {
		writeError(w, formatResponses, newError(http.StatusBadRequest, err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"object":"response.input_tokens","input_tokens":%d}`, n)
}
