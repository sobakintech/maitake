package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/translate"
)

// failure is an upstream error from either the HTTP status or an error event inside the stream.
type failure struct {
	status  int
	body    string
	headers http.Header
}

// decision is what to do about a failure.
type decision struct {
	retry     bool
	scope     string // accounts.Cooldown*
	until     time.Time
	signature bool // reasoning in the history was rejected; retry the same account without it
}

func isSignatureError(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "signature") && strings.Contains(b, "thinking") ||
		strings.Contains(b, "invalid_encrypted_content") ||
		strings.Contains(b, "encrypted content") && strings.Contains(b, "could not be verified")
}

// defaultCooldown is the built-in bench time for a failure when no rule sets one.
func defaultCooldown(c *call, f failure, now time.Time) time.Time {
	if f.status == http.StatusTooManyRequests {
		if c.provider == "claude" && f.headers != nil {
			if t := claude.CooldownFromHeaders(f.headers, now); t != nil {
				return t.Add(2 * time.Second)
			}
		} else if reset, ok := codex.UsageLimitReset(f.body); ok {
			switch {
			case reset > 0:
				return time.Unix(reset, 0).Add(2 * time.Second)
			case reset < 0:
				return now.Add(time.Duration(-reset) * time.Second)
			}
			return now.Add(5 * time.Minute)
		}
		return now.Add(time.Minute)
	}
	switch f.status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		return now.Add(30 * time.Minute)
	}
	return now.Add(15 * time.Second)
}

func (h *Handler) decide(c *call, f failure, s accounts.Settings) decision {
	now := time.Now()
	if f.status == http.StatusBadRequest && isSignatureError(f.body) {
		return decision{signature: true, retry: true, scope: accounts.CooldownNone}
	}
	var d decision
	scope, seconds := accounts.CooldownAuto, 0
	if rule := s.MatchErrorRule(c.provider, f.status, f.body); rule != nil {
		d.retry = rule.Action == accounts.ActionRetry
		scope, seconds = rule.Cooldown, rule.CooldownSeconds
	} else {
		switch {
		case f.status == http.StatusTooManyRequests, f.status == http.StatusUnauthorized,
			f.status == http.StatusPaymentRequired, f.status == http.StatusForbidden,
			f.status == http.StatusRequestTimeout, f.status >= 500:
			d.retry = true
		}
	}
	if scope == accounts.CooldownAuto {
		switch {
		case f.status == http.StatusTooManyRequests && c.provider == "claude" && f.headers != nil &&
			claude.ModelScopedLimit(f.headers):
			scope = accounts.CooldownModel
		case d.retry:
			scope = accounts.CooldownAccount
		default:
			scope = accounts.CooldownNone
		}
	}
	d.scope = scope
	if seconds > 0 {
		d.until = now.Add(time.Duration(seconds) * time.Second)
	} else {
		d.until = defaultCooldown(c, f, now)
	}
	return d
}

func (h *Handler) applyCooldown(c *call, lease *accounts.Lease, f failure, d decision, msg string) {
	ctx := c.ctx
	switch d.scope {
	case accounts.CooldownModel:
		h.accounts.MarkModelCooldown(lease.ID, c.baseModel, d.until)
	case accounts.CooldownAccount:
		if f.status == http.StatusTooManyRequests {
			h.accounts.MarkCooldown(ctx, lease.ID, d.until, "rate limited: "+msg)
		} else {
			h.accounts.MarkError(ctx, lease.ID, d.until, fmt.Sprintf("%d: %s", f.status, msg))
		}
	}
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request, c *call) (int, translate.Usage, int64, string) {
	ctx := r.Context()
	c.ctx = ctx
	s := h.settings()
	s.Normalize()
	tried := map[int64]bool{}
	var last *apiError
	var lastAccount int64
	var retryLease *accounts.Lease
	stripAll, signatureRetried := false, false

	for attempt := 0; attempt < s.MaxRetries+1; attempt++ {
		lease := retryLease
		retryLease = nil
		if lease == nil {
			if attempt >= s.MaxRetries {
				break
			}
			var err error
			lease, err = h.accounts.Pick(ctx, c.provider, tried, c.pick)
			if err != nil {
				if last != nil {
					break
				}
				var cooling *accounts.ErrAllCooling
				switch {
				case errors.As(err, &cooling):
					last = newError(http.StatusTooManyRequests, "all "+c.provider+" accounts are rate limited for this model; try again later")
					last.retryAfter = time.Until(cooling.Until)
				case errors.Is(err, accounts.ErrNoAccounts):
					last = newError(http.StatusServiceUnavailable, "no available "+c.provider+" accounts")
				default:
					last = newError(http.StatusBadGateway, "could not prepare upstream account: "+err.Error())
				}
				break
			}
		}
		tried[lease.ID] = true
		lastAccount = lease.ID

		src, dropThinking := h.sanitizeHistory(c, lease.ID, stripAll)
		body, betas, err := h.prepare(c, lease, src, dropThinking)
		if err != nil {
			e := newError(http.StatusBadRequest, "invalid request: "+err.Error())
			writeError(w, c.format, e)
			return e.status, translate.Usage{}, lease.ID, e.message
		}

		resp, err := h.send(ctx, c, lease, body, betas)
		if err == nil && resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			if creds, rerr := h.accounts.Credentials(ctx, lease.ID, true); rerr == nil {
				lease.Creds = creds
				if body, betas, err = h.prepare(c, lease, src, dropThinking); err == nil {
					resp, err = h.send(ctx, c, lease, body, betas)
				}
			} else {
				err = rerr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return 499, translate.Usage{}, lease.ID, "client disconnected"
			}
			h.accounts.MarkError(ctx, lease.ID, time.Now().Add(30*time.Second), err.Error())
			last = newError(http.StatusBadGateway, "upstream request failed: "+err.Error())
			continue
		}
		h.observe(ctx, c, lease, resp)

		var f failure
		if resp.StatusCode == http.StatusOK {
			var res relayResult
			if c.kind == kindCompact {
				res = relayJSON(w, resp)
			} else {
				res = h.relay(w, r, c, lease, resp, s)
			}
			if res.failure == nil {
				if res.status < 400 {
					h.accounts.MarkSuccess(ctx, lease.ID)
				}
				return res.status, res.usage, lease.ID, res.msg
			}
			f = *res.failure
		} else {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			resp.Body.Close()
			f = failure{status: resp.StatusCode, body: string(errBody), headers: resp.Header}
		}

		msg := upstreamMessage(f.body)
		d := h.decide(c, f, s)
		if d.signature {
			if !signatureRetried {
				// The history carries reasoning this account can't verify; resend it without.
				signatureRetried, stripAll, retryLease = true, true, lease
				continue
			}
			d = decision{retry: false, scope: accounts.CooldownNone}
		}
		h.applyCooldown(c, lease, f, d, msg)
		if !d.retry {
			e := clientError(c.provider, f)
			writeError(w, c.format, e)
			return e.status, translate.Usage{}, lease.ID, msg
		}
		last = clientError(c.provider, f)
		if f.status == http.StatusUnauthorized {
			last = newError(http.StatusBadGateway, "upstream account unauthorized")
		}
		if f.status == http.StatusTooManyRequests {
			last.retryAfter = time.Until(d.until)
		}
	}
	if last == nil {
		last = newError(http.StatusServiceUnavailable, "no available "+c.provider+" accounts")
	}
	writeError(w, c.format, last)
	return last.status, translate.Usage{}, lastAccount, last.message
}

type accumulator interface {
	Add(translate.Event)
}

type relayResult struct {
	status  int
	usage   translate.Usage
	msg     string
	failure *failure // set when the attempt failed before anything reached the client
}

type nextEvent struct {
	ev  translate.Event
	err error
}

// preamble events carry no output; an error may still follow them inside the same 200 stream.
func isPreamble(e translate.Event) bool {
	switch gjson.GetBytes(e.Data, "type").String() {
	case "message_start", "ping", "response.created", "response.in_progress", "":
		return true
	}
	return false
}

// streamFailure maps an in-stream error event to an HTTP-like failure, or nil if it isn't one.
func streamFailure(e translate.Event) *failure {
	d := gjson.ParseBytes(e.Data)
	t := d.Get("type").String()
	if t == "response.incomplete" && emptyIncomplete(d) {
		return &failure{status: http.StatusBadGateway,
			body: `{"error":{"type":"api_error","message":"upstream ended the response without producing anything"}}`}
	}
	if t != "error" && t != "response.failed" {
		return nil
	}
	kind := strings.ToLower(strings.Join([]string{
		d.Get("error.type").String(), d.Get("error.code").String(), d.Get("code").String(),
		d.Get("response.error.code").String(), d.Get("response.error.type").String(),
	}, " "))
	status := http.StatusBadGateway
	switch {
	case strings.Contains(kind, "overloaded"):
		status = 529
	case strings.Contains(kind, "rate_limit"), strings.Contains(kind, "usage_limit"), strings.Contains(kind, "insufficient_quota"):
		status = http.StatusTooManyRequests
	case strings.Contains(kind, "invalid_request"), strings.Contains(kind, "context_length"), strings.Contains(kind, "invalid_prompt"),
		strings.Contains(kind, "invalid_encrypted_content"):
		status = http.StatusBadRequest
	case strings.Contains(kind, "authentication"):
		status = http.StatusUnauthorized
	case strings.Contains(kind, "permission"):
		status = http.StatusForbidden
	case strings.Contains(kind, "api_error"), strings.Contains(kind, "server_error"):
		status = http.StatusInternalServerError
	}
	return &failure{status: status, body: string(e.Data)}
}

// relay streams or aggregates a 200 upstream response. Opening events are held back for up to
// BootstrapSeconds so an error that arrives inside the stream can still fail over.
func (h *Handler) relay(w http.ResponseWriter, r *http.Request, c *call, lease *accounts.Lease, resp *http.Response, s accounts.Settings) relayResult {
	defer resp.Body.Close()
	ctx := r.Context()
	reader := translate.NewSSEReader(resp.Body)

	events := make(chan nextEvent)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			e, err := reader.Next()
			if err == nil && c.rewrite != nil {
				e = c.rewrite(e)
			}
			select {
			case events <- nextEvent{e, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var claudeAcc *translate.ClaudeAccumulator
	var respAcc *translate.ResponsesAccumulator
	var acc accumulator
	if c.provider == "claude" {
		claudeAcc = translate.NewClaudeAccumulator()
		acc = claudeAcc
	} else {
		respAcc = translate.NewResponsesAccumulator()
		acc = respAcc
	}
	usageOf := func() translate.Usage {
		if claudeAcc != nil {
			c.served = claudeAcc.ServiceTier
			return claudeAcc.Usage
		}
		c.served = respAcc.ServiceTier
		return respAcc.Usage
	}
	recordSignatures := func() {
		var sigs []string
		if claudeAcc != nil {
			sigs = claudeAcc.Signatures()
		} else {
			sigs = respAcc.Signatures()
		}
		for _, sig := range sigs {
			h.sigs.record(sig, lease.ID)
		}
	}

	// Bootstrap: hold events until real output starts, an error shows up, or time runs out.
	var buffered []translate.Event
	ended := false
	bootstrap := time.Duration(s.BootstrapSeconds) * time.Second
	if bootstrap <= 0 {
		bootstrap = time.Millisecond
	}
	timer := time.NewTimer(bootstrap)
	defer timer.Stop()
bootstrapLoop:
	for {
		select {
		case <-ctx.Done():
			return relayResult{status: 499, msg: "client disconnected"}
		case <-timer.C:
			break bootstrapLoop
		case n := <-events:
			if n.err != nil {
				ended = true
				if len(buffered) == 0 || allPreamble(buffered) {
					return relayResult{failure: &failure{status: http.StatusBadGateway,
						body: `{"error":{"message":"upstream closed the stream before sending any output"}}`}}
				}
				break bootstrapLoop
			}
			if f := streamFailure(n.ev); f != nil {
				return relayResult{failure: f}
			}
			buffered = append(buffered, n.ev)
			if !isPreamble(n.ev) {
				c.ttft = time.Since(c.start)
				break bootstrapLoop
			}
		}
	}

	var conv translate.Converter
	switch {
	case c.provider == "claude" && c.format == formatChat:
		conv = translate.NewClaudeToChatStream(c.model, c.chat, c.names)
	case c.provider == "claude" && c.format == formatResponses:
		conv = translate.NewClaudeToResponsesStream(c.model, c.names)
	case c.provider == "codex" && c.format == formatChat:
		conv = translate.NewResponsesToChatStream(c.model, c.chat, c.names)
	case c.provider == "codex" && c.format == formatClaude:
		conv = translate.NewResponsesToClaudeStream(c.model, c.names)
	}

	if !c.stream {
		for _, e := range buffered {
			acc.Add(e)
		}
		for !ended {
			select {
			case <-ctx.Done():
				return relayResult{status: 499, usage: usageOf(), msg: "client disconnected"}
			case n := <-events:
				if n.err != nil {
					ended = true
					continue
				}
				if c.ttft == 0 && !isPreamble(n.ev) {
					c.ttft = time.Since(c.start)
				}
				acc.Add(n.ev)
			}
		}
		var out []byte
		var upstreamErr []byte
		if claudeAcc != nil {
			upstreamErr = claudeAcc.Err
			if upstreamErr == nil && !claudeAcc.Done {
				upstreamErr = []byte(`{"message":"upstream stream ended before the response finished"}`)
			}
			out = claudeAcc.Message()
			switch c.format {
			case formatChat:
				out = translate.ClaudeMessageToChat(out, c.model, c.chat, c.names)
			case formatResponses:
				out = translate.ClaudeMessageToResponse(out, c.model, c.names)
			}
		} else {
			upstreamErr = respAcc.Err
			if !respAcc.Done && upstreamErr == nil {
				upstreamErr = []byte(`{"message":"upstream stream ended before completion"}`)
			}
			out = respAcc.Response()
			if c.respond != nil && upstreamErr == nil {
				recordSignatures()
				return c.respond(w, out, usageOf())
			}
			switch c.format {
			case formatChat:
				out = translate.ResponseToChat(out, c.model, c.names, c.chat)
			case formatClaude:
				out = translate.ResponseToClaudeMessage(out, c.model, c.names)
			}
		}
		if upstreamErr != nil {
			e := newError(http.StatusBadGateway, upstreamMessage(string(upstreamErr)))
			writeError(w, c.format, e)
			return relayResult{status: e.status, usage: usageOf(), msg: e.message}
		}
		recordSignatures()
		if c.format == formatClaude && c.provider == "claude" {
			out, _ = sjson.SetBytes(out, "model", c.model)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(out)
		return relayResult{status: http.StatusOK, usage: usageOf()}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit := func(e translate.Event) bool {
		if c.ttft == 0 && !isPreamble(e) {
			c.ttft = time.Since(c.start)
		}
		acc.Add(e)
		var out []translate.Event
		if conv == nil {
			out = []translate.Event{e}
		} else {
			out = conv.Convert(e)
		}
		for _, o := range out {
			if _, err := w.Write(o.Encode()); err != nil {
				return false
			}
		}
		if len(out) > 0 {
			flush()
		}
		return true
	}
	ok := true
	for _, e := range buffered {
		if ok = emit(e); !ok {
			break
		}
	}

	keepalive := time.Duration(s.KeepaliveSeconds) * time.Second
	var tick <-chan time.Time
	var ticker *time.Ticker
	if keepalive > 0 {
		ticker = time.NewTicker(keepalive)
		defer ticker.Stop()
		tick = ticker.C
	}
	for ok && !ended {
		select {
		case <-ctx.Done():
			ok = false
		case <-tick:
			// SSE comment: ignored by clients, keeps proxies and idle timeouts from closing the stream.
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				ok = false
			}
			flush()
		case n := <-events:
			if n.err != nil {
				ended = true
				continue
			}
			if ticker != nil {
				ticker.Reset(keepalive)
			}
			ok = emit(n.ev)
		}
	}
	if ctx.Err() != nil || !ok {
		return relayResult{status: 499, usage: usageOf(), msg: "client disconnected"}
	}
	complete := (claudeAcc != nil && claudeAcc.Done) || (respAcc != nil && respAcc.Done)
	if conv != nil {
		for _, o := range conv.Finish() {
			w.Write(o.Encode())
		}
		flush()
	} else if !complete && (claudeAcc == nil || claudeAcc.Err == nil) && (respAcc == nil || respAcc.Err == nil) {
		// Passthrough clients would otherwise take a cut-off stream for a finished answer.
		const text = "upstream stream ended before the response finished"
		var e translate.Event
		if c.format == formatClaude {
			e = translate.Event{Name: "error", Data: []byte(`{"type":"error","error":{"type":"api_error","message":"` + text + `"}}`)}
		} else {
			e = translate.Event{Name: "error", Data: []byte(`{"type":"error","code":"stream_truncated","message":"` + text + `"}`)}
		}
		w.Write(e.Encode())
		flush()
	}
	recordSignatures()
	msg := ""
	if claudeAcc != nil && claudeAcc.Err != nil {
		msg = upstreamMessage(string(claudeAcc.Err))
	} else if respAcc != nil && respAcc.Err != nil {
		msg = upstreamMessage(string(respAcc.Err))
	} else if !complete {
		msg = "upstream stream ended before the response finished"
	}
	status := http.StatusOK
	if msg != "" {
		status = http.StatusBadGateway
	}
	return relayResult{status: status, usage: usageOf(), msg: msg}
}

// emptyIncomplete spots Codex silently aborting: incomplete with zero output tokens and no output.
func emptyIncomplete(d gjson.Result) bool {
	out := d.Get("response.output")
	tokens := d.Get("response.usage.output_tokens")
	return (!out.IsArray() || len(out.Array()) == 0) && tokens.Type == gjson.Number && tokens.Raw == "0"
}

func allPreamble(events []translate.Event) bool {
	return !slices.ContainsFunc(events, func(e translate.Event) bool { return !isPreamble(e) })
}

// sigRegistry remembers which account produced each thinking signature / encrypted reasoning,
// so history can be cleaned before it's sent to an account that can't verify it.
type sigRegistry struct {
	mu     sync.Mutex
	owners map[[16]byte]int64
}

func newSigRegistry() *sigRegistry { return &sigRegistry{owners: map[[16]byte]int64{}} }

func sigKey(sig string) [16]byte {
	sum := sha256.Sum256([]byte(translate.ReasoningSignature(sig)))
	var k [16]byte
	copy(k[:], sum[:16])
	return k
}

func (r *sigRegistry) record(sig string, account int64) {
	if sig == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.owners) >= 200_000 {
		r.owners = map[[16]byte]int64{} // crude bound; unknown signatures are simply kept
	}
	r.owners[sigKey(sig)] = account
}

func (r *sigRegistry) owner(sig string) (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.owners[sigKey(sig)]
	return id, ok
}

// sanitizeHistory removes reasoning blocks from the conversation history that the chosen
// account can't verify: ones another account produced, or all of them when stripAll is set.
// It reports whether the last assistant turn lost its reasoning (Claude then needs thinking off).
func (h *Handler) sanitizeHistory(c *call, account int64, stripAll bool) ([]byte, bool) {
	drop := func(sig string) bool {
		if sig == "" {
			return false
		}
		if stripAll {
			return true
		}
		owner, known := h.sigs.owner(sig)
		return known && owner != account
	}
	body := c.body
	switch c.format {
	case formatClaude:
		msgs := gjson.GetBytes(body, "messages").Array()
		lastAssistant := -1
		for i, m := range msgs {
			if m.Get("role").String() == "assistant" {
				lastAssistant = i
			}
		}
		strippedLast := false
		for i := len(msgs) - 1; i >= 0; i-- {
			blocks := msgs[i].Get("content").Array()
			for j := len(blocks) - 1; j >= 0; j-- {
				b := blocks[j]
				var sig string
				switch b.Get("type").String() {
				case "thinking":
					sig = b.Get("signature").String()
				case "redacted_thinking":
					sig = b.Get("data").String()
				default:
					continue
				}
				if drop(sig) {
					body, _ = sjson.DeleteBytes(body, fmt.Sprintf("messages.%d.content.%d", i, j))
					if i == lastAssistant {
						strippedLast = true
					}
				}
			}
		}
		return body, strippedLast
	case formatResponses:
		items := gjson.GetBytes(body, "input").Array()
		lastUser := -1
		for i, it := range items {
			if it.Get("role").String() == "user" {
				lastUser = i
			}
		}
		strippedLast := false
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Get("type").String() != "reasoning" {
				continue
			}
			enc := items[i].Get("encrypted_content").String()
			// Reasoning from the other provider can't be replayed at all.
			foreign := enc != "" && (c.provider == "codex") == translate.IsClaudeReasoning(enc)
			if foreign || drop(enc) {
				body, _ = sjson.DeleteBytes(body, fmt.Sprintf("input.%d", i))
				if i > lastUser {
					strippedLast = true
				}
			}
		}
		return body, strippedLast && c.provider == "claude"
	}
	return body, false
}

// sessionID finds a stable conversation id: client session headers, the request's own cache
// key or Claude Code metadata, or else a fingerprint of the conversation's opening.
func sessionID(h http.Header, body []byte) string {
	if v := strings.TrimSpace(h.Get("X-Claude-Code-Session-Id")); v != "" {
		// Subagents share the session id; each gets its own scope (cache key, affinity).
		if agent := strings.TrimSpace(h.Get("X-Claude-Code-Agent-Id")); agent != "" {
			return "h:" + v + ":agent:" + agent
		}
		return "h:" + v
	}
	for _, name := range []string{"Session_id", "Session-Id", "X-Session-Id",
		"Conversation_id", "Conversation-Id", "Thread-Id", "X-Opencode-Session-Id"} {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return "h:" + v
		}
	}
	if v := gjson.GetBytes(body, "prompt_cache_key").String(); v != "" {
		return "p:" + v
	}
	if uid := gjson.GetBytes(body, "metadata.user_id").String(); uid != "" {
		if sid := gjson.Get(uid, "session_id").String(); sid != "" {
			return "c:" + sid
		}
		if _, after, ok := strings.Cut(uid, "_session_"); ok && after != "" {
			return "c:" + after
		}
	}
	// Fingerprint the system prompt and first message: stable across a conversation's turns.
	first := gjson.GetBytes(body, "messages.0")
	if !first.Exists() {
		first = gjson.GetBytes(body, "input.0")
	}
	if !first.Exists() {
		return ""
	}
	sum := sha256.New()
	sum.Write([]byte(gjson.GetBytes(body, "system").Raw))
	sum.Write([]byte(gjson.GetBytes(body, "instructions").Raw))
	sum.Write([]byte(first.Raw))
	return "f:" + hex.EncodeToString(sum.Sum(nil)[:12])
}

// relayJSON passes a non-streamed Codex response (compaction) through and reads its usage.
func relayJSON(w http.ResponseWriter, resp *http.Response) relayResult {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return relayResult{failure: &failure{status: http.StatusBadGateway,
			body: `{"error":{"message":"reading upstream response: ` + err.Error() + `"}}`}}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
	return relayResult{status: http.StatusOK, usage: translate.ResponsesUsage(gjson.GetBytes(body, "usage"))}
}
