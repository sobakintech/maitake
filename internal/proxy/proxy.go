// Package proxy serves the client-facing /v1 API and routes requests to upstream accounts.
package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
	"github.com/sobakintech/maitake/internal/translate"
	"github.com/sobakintech/maitake/internal/usage"
)

const maxBody = 64 << 20

type Handler struct {
	store    *store.Store
	accounts *accounts.Manager
	claude   *claude.Provider
	codex    *codex.Provider
	usage    *usage.Recorder
	settings func() accounts.Settings
	routing  func() routing.Settings
	rpm      *rpmLimiter
	sigs     *sigRegistry
	catalog  codexCatalog
}

func New(st *store.Store, mgr *accounts.Manager, cl *claude.Provider, cx *codex.Provider, rec *usage.Recorder,
	settings func() accounts.Settings, routingSettings func() routing.Settings) *Handler {
	return &Handler{store: st, accounts: mgr, claude: cl, codex: cx, usage: rec, settings: settings,
		routing: routingSettings, rpm: newRPMLimiter(), sigs: newSigRegistry()}
}

func (h *Handler) mountAPI(r chi.Router, openRouter bool) {
	r.Get("/v1/models", func(w http.ResponseWriter, r *http.Request) { h.listModels(w, r, openRouter) })
	r.Post("/v1/messages", func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, formatClaude, openRouter) })
	r.Post("/v1/messages/count_tokens", h.countTokens)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, formatChat, openRouter) })
	r.Post("/v1/responses", func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, formatResponses, openRouter) })
	r.Get("/v1/responses", h.serveWebsocket)
	r.Post("/v1/responses/compact", h.serveCompact)
	r.Post("/v1/responses/input_tokens", h.inputTokens)
	r.Post("/v1/alpha/search", h.serveSearch)
	r.Post("/v1/images/generations", h.serveImages)
	r.Post("/v1/images/edits", h.serveImages)
}

// Mount serves the client API at /v1.
func (h *Handler) Mount(r chi.Router) {
	h.mountAPI(r, false)
	// Codex CLI's native paths, for clients pointed at maitake as a ChatGPT backend.
	r.Route("/backend-api/codex", func(r chi.Router) {
		r.Post("/responses", func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, formatResponses, false) })
		r.Get("/responses", h.serveWebsocket)
		r.Post("/responses/compact", h.serveCompact)
		r.Post("/alpha/search", h.serveSearch)
		r.Get("/models", func(w http.ResponseWriter, r *http.Request) { h.listModels(w, r, false) })
	})
}

// MountOpenRouter serves the same API under OpenRouter's base path (/api/v1), so clients that
// only offer an "OpenRouter" option work by swapping the host. Mount it on the /api subrouter.
func (h *Handler) MountOpenRouter(r chi.Router) { h.mountAPI(r, true) }

func clientKey(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	for _, name := range []string{"X-Api-Key", "X-Goog-Api-Key"} {
		if v := r.Header.Get(name); v != "" {
			return v
		}
	}
	q := r.URL.Query()
	if v := q.Get("key"); v != "" {
		return v
	}
	return q.Get("auth_token")
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, f format) *store.KeyOwner {
	key := clientKey(r)
	if key == "" {
		writeError(w, f, newError(http.StatusUnauthorized, "missing API key"))
		return nil
	}
	owner, err := h.store.AuthenticateKey(r.Context(), secret.Hash(key))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, f, newError(http.StatusUnauthorized, "invalid API key"))
		return nil
	}
	if err != nil {
		slog.Error("authenticate key", "err", err)
		writeError(w, f, newError(http.StatusInternalServerError, "internal error"))
		return nil
	}
	if ok, wait := h.rpm.allow(owner.Key.ID, owner.Key.RPMLimit); !ok {
		e := newError(http.StatusTooManyRequests, fmt.Sprintf("rate limit of %d requests per minute exceeded for this key", owner.Key.RPMLimit))
		e.retryAfter = wait
		writeError(w, f, e)
		return nil
	}
	return owner
}

// call describes one client request after parsing.
type call struct {
	ctx          context.Context
	format       format
	owner        *store.KeyOwner
	body         []byte
	model        string // as requested by the client
	baseModel    string
	effort       string
	provider     string
	stream       bool
	route        *routing.Route
	rs           routing.Settings
	clientFormat string
	session      string // conversation id used for affinity and cache keys
	chat         translate.ChatOptions
	pick         accounts.PickOptions
	// rewrite adjusts each upstream event before anything else sees it (tool name aliases).
	rewrite  func(translate.Event) translate.Event
	names    *translate.ToolNames
	headers  http.Header
	endpoint string
	kind     callKind
	start    time.Time
	ttft     time.Duration // until the first upstream output, set by relay
	served   string        // service tier the provider reported
	display  string        // model name to record instead of model (the image model for /v1/images)
	// respond replaces the normal response for a finished non-streamed Codex call (images).
	respond func(w http.ResponseWriter, response []byte, u translate.Usage) relayResult
}

type callKind int

const (
	kindNormal callKind = iota
	kindCompact
	kindImages
)

func clientFormat(f format, openRouter bool) string {
	switch {
	case openRouter && f == formatChat:
		return routing.FormatOpenRouter
	case f == formatClaude:
		return routing.FormatAnthropic
	case f == formatResponses:
		return routing.FormatResponses
	}
	return routing.FormatOpenAI
}

// requestEffort reads a reasoning level the request itself asks for, in any client style.
// OpenRouter's reasoning object is converted here; the others are handled by the translators.
func requestEffort(body []byte) (effort string, explicit, hide bool) {
	r := gjson.GetBytes(body, "reasoning")
	if r.IsObject() {
		hide = r.Get("exclude").Bool()
		switch {
		case r.Get("enabled").Exists() && !r.Get("enabled").Bool():
			return "none", true, hide
		case r.Get("effort").String() != "":
			return strings.ToLower(r.Get("effort").String()), true, hide
		case r.Get("max_tokens").Int() > 0:
			return translate.EffortFromBudget(r.Get("max_tokens").Int()), true, hide
		}
	}
	for _, path := range []string{"reasoning_effort", "thinking", "output_config.effort", "reasoning.effort"} {
		if gjson.GetBytes(body, path).Exists() {
			return "", true, hide
		}
	}
	return "", false, hide
}

// providerPrefs reads OpenRouter's provider.only / provider.ignore into an allow-list.
func providerPrefs(body []byte) []string {
	p := gjson.GetBytes(body, "provider")
	if !p.IsObject() {
		return nil
	}
	var only []string
	for _, v := range p.Get("only").Array() {
		only = append(only, strings.ToLower(v.String()))
	}
	ignore := map[string]bool{}
	for _, v := range p.Get("ignore").Array() {
		ignore[strings.ToLower(v.String())] = true
	}
	if len(only) == 0 && len(ignore) == 0 {
		return nil
	}
	if len(only) == 0 {
		only = []string{"claude", "codex", "anthropic", "openai"}
	}
	out := only[:0]
	for _, v := range only {
		if !ignore[v] {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, f format, openRouter bool) {
	body, ok := readBody(w, r, f)
	if !ok {
		return
	}
	if f == formatChat && translate.LooksLikeResponses(body) {
		body = translate.ResponsesRequestToChat(body)
	}
	h.handle(w, r, &call{format: f, body: body}, openRouter)
}

// requestedTier reads the service tier a client asked for, in either API's terms.
func requestedTier(body []byte) string {
	if t := gjson.GetBytes(body, "service_tier").String(); t != "" {
		return t
	}
	if gjson.GetBytes(body, "speed").String() == "fast" {
		return "fast"
	}
	return ""
}

func readBody(w http.ResponseWriter, r *http.Request, f format) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, f, newError(http.StatusRequestEntityTooLarge, "request body too large or unreadable"))
		return nil, false
	}
	if !gjson.ValidBytes(body) {
		writeError(w, f, newError(http.StatusBadRequest, "request body must be valid JSON"))
		return nil, false
	}
	return body, true
}

// serveCompact handles Codex's history compaction endpoint: a non-streamed Responses call.
func (h *Handler) serveCompact(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, formatResponses)
	if !ok {
		return
	}
	if gjson.GetBytes(body, "stream").Bool() {
		writeError(w, formatResponses, newError(http.StatusBadRequest, "Streaming not supported for compact responses"))
		return
	}
	h.handle(w, r, &call{format: formatResponses, body: body, kind: kindCompact}, false)
}

// handle routes and executes a parsed request; c carries format, body and kind.
func (h *Handler) handle(w http.ResponseWriter, r *http.Request, c *call, openRouter bool) {
	start := time.Now()
	c.start = start
	f, body := c.format, c.body
	owner := c.owner
	if owner == nil {
		if owner = h.authenticate(w, r, f); owner == nil {
			return
		}
	}
	c.owner, c.names, c.headers, c.endpoint = owner, translate.NewToolNames(), r.Header, r.URL.Path
	if c.model == "" {
		c.model = gjson.GetBytes(body, "model").String()
	}
	if c.model == "" {
		writeError(w, f, newError(http.StatusBadRequest, "model is required"))
		return
	}
	if c.kind == kindNormal {
		c.stream = gjson.GetBytes(body, "stream").Bool()
	}

	rs := h.routing()
	var keyRules routing.KeyRules
	_ = json.Unmarshal(owner.Key.ModelRules, &keyRules)
	route, resolveErr := rs.Resolve(routing.Request{Model: c.model, Format: clientFormat(f, openRouter),
		KeyRules: keyRules, Allowed: owner.Key.AllowedModels, Providers: providerPrefs(body)})
	c.route = route
	c.baseModel, c.provider, c.effort = route.Model.ID, route.Model.Provider, route.Effort

	reqEffort, explicit, hide := requestEffort(body)
	if c.effort == "" && reqEffort != "" {
		c.effort = reqEffort
	}
	if c.effort == "" && !explicit {
		c.effort = route.DefaultEffort
	}
	c.chat = translate.ChatOptions{
		IncludeUsage:   gjson.GetBytes(body, "stream_options.include_usage").Bool() || gjson.GetBytes(body, "usage.include").Bool(),
		HideReasoning:  hide,
		ReasoningField: rs.ReasoningField,
	}
	c.rs, c.clientFormat = rs, clientFormat(f, openRouter)
	c.session = sessionID(r.Header, body)
	c.pick = accounts.PickOptions{Strategy: route.Strategy, Accounts: route.Accounts, Model: route.Model.ID}
	switch {
	case rs.Affinity == "session" && c.session != "":
		c.pick.StickyKey = "s:" + strconv.FormatInt(owner.Key.ID, 10) + ":" + c.session
	case rs.Affinity == "session" || rs.Affinity == "key":
		c.pick.StickyKey = "k:" + strconv.FormatInt(owner.Key.ID, 10)
	}
	if c.pick.StickyKey != "" {
		c.pick.StickyFor = time.Duration(rs.StickyMinutes) * time.Minute
	}

	ev := store.UsageEvent{Time: start, UserID: owner.User.ID, KeyID: owner.Key.ID, Provider: c.provider,
		Model: c.baseModel, RequestedModel: c.model, Endpoint: c.endpoint, Stream: c.stream}
	if ev.Model == "" {
		ev.Model = c.model
	}
	if c.display != "" {
		ev.RequestedModel = c.display
	}
	ev.ServiceTier = requestedTier(body)
	finish := func(status int, u translate.Usage, accountID int64, msg string) {
		ev.Status, ev.AccountID, ev.Error = status, accountID, msg
		ev.InputTokens, ev.OutputTokens = u.InputTokens, u.OutputTokens
		ev.CacheReadTokens, ev.CacheWriteTokens, ev.ReasoningTokens = u.CacheReadTokens, u.CacheWriteTokens, u.ReasoningTokens
		ev.DurationMS = time.Since(start).Milliseconds()
		ev.TTFTMS, ev.ServedTier = c.ttft.Milliseconds(), c.served
		h.usage.Record(ev)
	}

	fail := func(e *apiError) {
		writeError(w, f, e)
		finish(e.status, translate.Usage{}, 0, e.message)
	}
	switch {
	case errors.Is(resolveErr, routing.ErrNotAllowed):
		fail(newError(http.StatusForbidden, fmt.Sprintf("this key is not allowed to use model %q", c.model)))
		return
	case resolveErr != nil:
		fail(newError(http.StatusNotFound, fmt.Sprintf("model %q is not available: %v", c.model, resolveErr)))
		return
	}
	if c.kind != kindNormal && c.provider != "codex" {
		fail(newError(http.StatusBadRequest, fmt.Sprintf("model %q is not served by Codex, which this endpoint requires", c.model)))
		return
	}
	status, u, accountID, msg := h.execute(w, r, c)
	finish(status, u, accountID, msg)
}

// prepare builds the upstream body for one attempt from src (the client body with any
// unverifiable reasoning removed). dropThinking turns thinking off for this turn, needed when
// the last assistant turn lost its thinking block.
func (h *Handler) prepare(c *call, lease *accounts.Lease, src []byte, dropThinking bool) ([]byte, []string, error) {
	var body []byte
	var betas []string
	var err error
	switch c.provider {
	case "claude":
		native := c.format == formatClaude
		if native {
			body, _ = sjson.SetBytes(src, "model", c.baseModel)
			if c.effort != "" {
				body = translate.ApplyClaudeEffort(body, c.baseModel, c.effort)
			}
			body = translate.FitClaudeTokens(body, c.baseModel)
		} else if c.format == formatResponses {
			if body, err = translate.ResponsesToClaude(src, c.baseModel, c.effort, c.names); err != nil {
				return nil, nil, err
			}
		} else {
			if body, err = translate.ChatToClaude(src, c.baseModel, c.effort, c.names); err != nil {
				return nil, nil, err
			}
			// Without display, thinking comes back redacted and Chat clients would see no reasoning.
			if t := gjson.GetBytes(body, "thinking.type").String(); (t == "adaptive" || t == "enabled") &&
				!gjson.GetBytes(body, "thinking.display").Exists() {
				display := "summarized"
				if c.chat.HideReasoning {
					display = "omitted"
				}
				body, _ = sjson.SetBytes(body, "thinking.display", display)
			}
		}
		if dropThinking {
			body, _ = sjson.DeleteBytes(body, "thinking")
			body, _ = sjson.DeleteBytes(body, "output_config.effort")
		}
		opts := claude.Options{SessionID: h.claudeSession(c, lease), Native: native, Cloak: h.cloak()}
		if opts.Cloak.ToolAliases {
			aliases := claude.NewToolAliases(fmt.Sprintf("maitake:tools:%d", c.owner.Key.ID))
			opts.Aliases = aliases
			c.rewrite = func(e translate.Event) translate.Event { return restoreToolNames(e, aliases) }
		}
		if native {
			opts.ClientBetas = c.headers.Values("anthropic-beta")
		}
		if body, betas, err = claude.PrepareMessages(body, lease.Creds, opts); err != nil {
			return nil, nil, err
		}
		if next, changed := c.rs.ApplyPayloadRules(body, c.route.Model, c.clientFormat); changed {
			if body, err = claude.Resign(next); err != nil {
				return nil, nil, err
			}
		}
		return body, betas, nil
	default:
		switch c.format {
		case formatChat:
			body, err = translate.ChatToResponses(src, c.baseModel, c.effort, c.names)
		case formatClaude:
			body, err = translate.ClaudeToResponses(src, c.baseModel, c.effort, c.names)
		default:
			body = src
		}
		if err != nil {
			return nil, nil, err
		}
		effort := c.effort
		if c.format != formatResponses {
			effort = ""
		}
		if body, err = codex.PrepareResponses(body, c.baseModel, effort, h.cacheKey(c)); err != nil {
			return nil, nil, err
		}
		if e := gjson.GetBytes(body, "reasoning.effort").String(); e != "" {
			if fit := translate.CodexEffortFor(c.baseModel, e); fit != e {
				body, _ = sjson.SetBytes(body, "reasoning.effort", fit)
			}
		}
		switch {
		case c.kind == kindCompact:
			body, _ = sjson.DeleteBytes(body, "stream")
		case c.kind == kindNormal && c.format == formatResponses && c.rs.Images.Enabled && c.rs.Images.AutoTool:
			body = codex.AppendImageTool(body, lease.Plan)
		}
		body, _ = c.rs.ApplyPayloadRules(body, c.route.Model, c.clientFormat)
		return body, nil, nil
	}
}

func (h *Handler) cloak() claude.CloakSettings {
	if h.claude.Cloak == nil {
		return claude.CloakSettings{}
	}
	return h.claude.Cloak()
}

// restoreToolNames maps aliased tool names in a Claude stream event back to the client's names.
func restoreToolNames(e translate.Event, a *claude.ToolAliases) translate.Event {
	if gjson.GetBytes(e.Data, "content_block.type").String() != "tool_use" {
		return e
	}
	name := gjson.GetBytes(e.Data, "content_block.name").String()
	if orig := a.Original(name); orig != name {
		e.Data, _ = sjson.SetBytes(e.Data, "content_block.name", orig)
	}
	return e
}

func stableUUID(seed string) string {
	h := sha256.Sum256([]byte(seed))
	b := h[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// claudeSession reuses Claude Code's own session id when present; otherwise one per conversation
// (or key) and account.
func (h *Handler) claudeSession(c *call, lease *accounts.Lease) string {
	if v := c.headers.Get("X-Claude-Code-Session-Id"); v != "" {
		return v
	}
	if c.session != "" {
		return stableUUID(fmt.Sprintf("maitake:session:%d:%d:%s", c.owner.Key.ID, lease.ID, c.session))
	}
	return stableUUID(fmt.Sprintf("maitake:session:%d:%d:%s", c.owner.Key.ID, lease.ID, time.Now().UTC().Format("2006-01-02")))
}

// cacheKey is the Codex prompt_cache_key: per conversation when one is known, else per key.
func (h *Handler) cacheKey(c *call) string {
	if c.session != "" {
		return stableUUID(fmt.Sprintf("maitake:codex:prompt-cache:%d:%s", c.owner.Key.ID, c.session))
	}
	return stableUUID(fmt.Sprintf("maitake:codex:prompt-cache:%d", c.owner.Key.ID))
}

func (h *Handler) send(ctx context.Context, c *call, lease *accounts.Lease, body []byte, betas []string) (*http.Response, error) {
	if c.provider == "claude" {
		return h.claude.Send(ctx, lease.Creds, body, betas, h.claudeSession(c, lease), false, c.headers)
	}
	if c.kind == kindCompact {
		return h.codex.Compact(ctx, lease.Creds, body, c.headers)
	}
	return h.codex.Send(ctx, lease.Creds, body, c.headers)
}

func (h *Handler) observe(ctx context.Context, c *call, lease *accounts.Lease, resp *http.Response) {
	if c.provider == "claude" {
		h.accounts.ObserveQuota(ctx, lease.ID, claude.WindowsFromHeaders(resp.Header), "")
		return
	}
	windows, plan := codex.WindowsFromHeaders(resp.Header, time.Now())
	h.accounts.ObserveQuota(ctx, lease.ID, windows, plan)
}

// listModels returns the catalog the key may use. The response is a superset of the OpenAI,
// Anthropic and OpenRouter list formats; Anthropic clients (anthropic-version header) get theirs.
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request, openRouter bool) {
	f := formatChat
	if r.Header.Get("anthropic-version") != "" {
		f = formatClaude
	}
	owner := h.authenticate(w, r, f)
	if owner == nil {
		return
	}
	if _, ok := r.URL.Query()["client_version"]; ok {
		h.codexClientModels(w, r, owner)
		return
	}
	available := map[string]bool{}
	for _, a := range h.accounts.List() {
		if !a.Disabled {
			available[a.Provider] = true
		}
	}
	rs := h.routing()
	naming := rs.ModelList
	if openRouter {
		naming = "canonical"
	}
	type entry struct {
		id string
		m  models.Model
	}
	var list []entry
	for _, m := range rs.Catalog() {
		if !available[m.Provider] || rs.Models[m.Canonical].Disabled || !models.Allowed(owner.Key.AllowedModels, m) {
			continue
		}
		switch naming {
		case "canonical":
			list = append(list, entry{m.Canonical, m})
		case "both":
			list = append(list, entry{m.ID, m}, entry{m.Canonical, m})
		default:
			list = append(list, entry{m.ID, m})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if f == formatClaude {
		data := make([]map[string]any, 0, len(list))
		for _, e := range list {
			data = append(data, map[string]any{"type": "model", "id": e.id, "display_name": e.m.Name,
				"created_at": "2025-01-01T00:00:00Z"})
		}
		resp := map[string]any{"data": data, "has_more": false, "first_id": nil, "last_id": nil}
		if len(list) > 0 {
			resp["first_id"], resp["last_id"] = list[0].id, list[len(list)-1].id
		}
		w.Write(marshalJSON(resp))
		return
	}
	zero := map[string]string{"prompt": "0", "completion": "0", "request": "0", "image": "0"}
	data := make([]map[string]any, 0, len(list))
	for _, e := range list {
		data = append(data, map[string]any{
			"id": e.id, "object": "model", "created": 0, "owned_by": e.m.Lab,
			"name": e.m.Name, "canonical_slug": e.m.Canonical, "context_length": e.m.ContextLength,
			"architecture": map[string]any{"modality": "text+image->text", "input_modalities": []string{"text", "image"},
				"output_modalities": []string{"text"}, "tokenizer": e.m.Lab},
			"pricing":      zero,
			"top_provider": map[string]any{"context_length": e.m.ContextLength, "max_completion_tokens": e.m.MaxOutput, "is_moderated": false},
			"supported_parameters": []string{"tools", "tool_choice", "reasoning", "include_reasoning", "max_tokens",
				"stop", "response_format", "structured_outputs"},
		})
	}
	w.Write(marshalJSON(map[string]any{"object": "list", "data": data}))
}

func ownedBy(p string) string {
	if p == "claude" {
		return "anthropic"
	}
	return "openai"
}
