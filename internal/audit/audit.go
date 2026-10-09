// Package audit records full request detail (headers and bodies) for later review, off the
// request path. It is off unless enabled globally or for a key.
package audit

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sobakintech/maitake/internal/models"
	"github.com/sobakintech/maitake/internal/store"
)

type Settings struct {
	// Enabled records every key that doesn't set its own mode.
	Enabled bool `json:"enabled"`
	// Record is "all", "errors" or "success".
	Record string `json:"record"`
	// SamplePercent records only this share of matching requests.
	SamplePercent int `json:"samplePercent"`
	// Models limits recording to these glob patterns; empty means every model.
	Models []string `json:"models"`

	Request          bool `json:"request"`
	UpstreamRequest  bool `json:"upstreamRequest"`
	UpstreamResponse bool `json:"upstreamResponse"`
	Response         bool `json:"response"`
	Headers          bool `json:"headers"`
	// RedactHeaders are hidden on top of the credentials that always are.
	RedactHeaders []string `json:"redactHeaders"`
	// MaxBodyKB truncates each body; 0 keeps it whole.
	MaxBodyKB int `json:"maxBodyKB"`

	RetentionDays int `json:"retentionDays"`
	MaxEntries    int `json:"maxEntries"`
	MaxStorageMB  int `json:"maxStorageMB"`
}

func DefaultSettings() Settings {
	return Settings{
		Record:        "all",
		SamplePercent: 100,
		Request:       true,
		Response:      true,
		Headers:       true,
		RedactHeaders: []string{},
		Models:        []string{},
		MaxBodyKB:     1024,
		RetentionDays: 7,
		MaxStorageMB:  1024,
	}
}

func (s *Settings) Normalize() {
	switch s.Record {
	case "all", "errors", "success":
	default:
		s.Record = "all"
	}
	s.SamplePercent = min(max(s.SamplePercent, 1), 100)
	s.MaxBodyKB = max(s.MaxBodyKB, 0)
	s.RetentionDays = max(s.RetentionDays, 0)
	s.MaxEntries = max(s.MaxEntries, 0)
	s.MaxStorageMB = max(s.MaxStorageMB, 0)
	s.Models = clean(s.Models)
	s.RedactHeaders = clean(s.RedactHeaders)
}

func clean(items []string) []string {
	out := []string{}
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" && !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	return out
}

// Headers that carry credentials are never stored.
var alwaysRedacted = []string{"Authorization", "Proxy-Authorization", "X-Api-Key", "X-Goog-Api-Key", "Cookie",
	"Set-Cookie"}

var redactedParams = []string{"key", "auth_token"}

type Attempt struct {
	AccountID  int64  `json:"accountId"`
	Status     int    `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type Body struct {
	Text string `json:"text"`
	// Size is the full length; Text holds only the first MaxBodyKB when Truncated.
	Size      int64 `json:"size"`
	Truncated bool  `json:"truncated"`
}

// Payload is the detail stored compressed with each entry.
type Payload struct {
	Method                  string              `json:"method,omitempty"`
	Query                   string              `json:"query,omitempty"`
	RequestHeaders          map[string][]string `json:"requestHeaders,omitempty"`
	ResponseHeaders         map[string][]string `json:"responseHeaders,omitempty"`
	UpstreamStatus          int                 `json:"upstreamStatus,omitempty"`
	UpstreamResponseHeaders map[string][]string `json:"upstreamResponseHeaders,omitempty"`
	Request                 *Body               `json:"request,omitempty"`
	UpstreamRequest         *Body               `json:"upstreamRequest,omitempty"`
	UpstreamResponse        *Body               `json:"upstreamResponse,omitempty"`
	Response                *Body               `json:"response,omitempty"`
}

// buffer keeps the first limit bytes written to it and counts the rest.
type buffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	total int64
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	room := len(p)
	if b.limit > 0 {
		room = min(room, b.limit-b.buf.Len())
	}
	if room > 0 {
		b.buf.Write(p[:room])
	}
	return len(p), nil
}

func (b *buffer) body() *Body {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return &Body{Text: b.buf.String(), Size: b.total, Truncated: b.total > int64(b.buf.Len())}
}

// Capture collects one request's detail while it runs. All methods are safe on a nil Capture,
// which is what requests that aren't recorded get.
type Capture struct {
	cfg        Settings
	limit      int
	method     string
	query      string
	reqHeaders http.Header
	userAgent  string

	mu               sync.Mutex
	request          *buffer
	upstreamRequest  *buffer
	upstreamResponse *buffer
	upstreamHeaders  http.Header
	upstreamStatus   int
	response         *buffer
	responseHeaders  http.Header
	attempts         []Attempt
}

func (c *Capture) newBuffer() *buffer { return &buffer{limit: c.limit} }

// Request stores the client's request body.
func (c *Capture) Request(body []byte) {
	if c == nil || !c.cfg.Request {
		return
	}
	b := c.newBuffer()
	b.Write(body)
	c.mu.Lock()
	c.request = b
	c.mu.Unlock()
}

// UpstreamRequest stores the body sent to the provider; the last attempt's wins.
func (c *Capture) UpstreamRequest(body []byte) {
	if c == nil || !c.cfg.UpstreamRequest {
		return
	}
	b := c.newBuffer()
	b.Write(body)
	c.mu.Lock()
	c.upstreamRequest = b
	c.mu.Unlock()
}

// Upstream tees the provider's response body as it is read; the last attempt's wins.
func (c *Capture) Upstream(resp *http.Response) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upstreamStatus = resp.StatusCode
	if c.cfg.Headers {
		c.upstreamHeaders = resp.Header.Clone()
	}
	if c.cfg.UpstreamResponse {
		b := c.newBuffer()
		c.upstreamResponse = b
		resp.Body = teeBody{resp.Body, b}
	}
}

type teeBody struct {
	io.ReadCloser
	b *buffer
}

func (t teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 {
		t.b.Write(p[:n])
	}
	return n, err
}

// Attempt notes one try against an upstream account.
func (c *Capture) Attempt(accountID int64, status int, msg string, started time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.attempts = append(c.attempts, Attempt{AccountID: accountID, Status: status, Error: msg,
		DurationMS: time.Since(started).Milliseconds()})
	c.mu.Unlock()
}

// Writer wraps w so what the client receives is recorded too.
func (c *Capture) Writer(w http.ResponseWriter) http.ResponseWriter {
	if c == nil {
		return w
	}
	c.responseHeaders = w.Header()
	if !c.cfg.Response {
		return w
	}
	c.response = c.newBuffer()
	return &captureWriter{ResponseWriter: w, b: c.response}
}

type captureWriter struct {
	http.ResponseWriter
	b *buffer
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.b.Write(p)
	return w.ResponseWriter.Write(p)
}

func (w *captureWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (c *Capture) redact(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for name, values := range h {
		hidden := slices.ContainsFunc(alwaysRedacted, func(n string) bool { return strings.EqualFold(n, name) }) ||
			slices.ContainsFunc(c.cfg.RedactHeaders, func(n string) bool { return strings.EqualFold(n, name) })
		if hidden {
			out[name] = []string{"[redacted]"}
		} else {
			out[name] = slices.Clone(values)
		}
	}
	return out
}

func (c *Capture) payload() Payload {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := Payload{Method: c.method, Query: c.query, UpstreamStatus: c.upstreamStatus,
		Request: c.request.body(), UpstreamRequest: c.upstreamRequest.body(),
		UpstreamResponse: c.upstreamResponse.body(), Response: c.response.body()}
	if c.cfg.Headers {
		p.RequestHeaders = c.redact(c.reqHeaders)
		p.ResponseHeaders = c.redact(c.responseHeaders)
		p.UpstreamResponseHeaders = c.redact(c.upstreamHeaders)
	}
	return p
}

type queued struct {
	entry   store.AuditEntry
	payload Payload
}

// Recorder decides which requests to capture and writes finished ones in batches.
type Recorder struct {
	store    *store.Store
	settings func() Settings
	ch       chan queued
	done     chan struct{}
}

func NewRecorder(st *store.Store, settings func() Settings) *Recorder {
	return &Recorder{store: st, settings: settings, ch: make(chan queued, 256), done: make(chan struct{})}
}

// Begin returns a Capture when this request should be recorded, or nil. keyMode is the key's
// own setting: "on" or "off" override the global switch.
func (r *Recorder) Begin(req *http.Request, keyMode string) *Capture {
	if r == nil || keyMode == "off" {
		return nil
	}
	cfg := r.settings()
	cfg.Normalize()
	if keyMode != "on" && !cfg.Enabled {
		return nil
	}
	if cfg.SamplePercent < 100 && rand.IntN(100) >= cfg.SamplePercent {
		return nil
	}
	c := &Capture{cfg: cfg, limit: cfg.MaxBodyKB << 10, method: req.Method, userAgent: req.UserAgent()}
	if cfg.Headers {
		c.reqHeaders = req.Header.Clone()
	}
	if req.URL.RawQuery != "" {
		c.query = redactQuery(req.URL)
	}
	return c
}

func redactQuery(u *url.URL) string {
	q := u.Query()
	for _, p := range redactedParams {
		if q.Has(p) {
			q.Set(p, "[redacted]")
		}
	}
	return q.Encode()
}

func (c *Capture) wanted(ev store.UsageEvent) bool {
	switch {
	case c.cfg.Record == "errors" && ev.Status < 400, c.cfg.Record == "success" && ev.Status >= 400:
		return false
	}
	if len(c.cfg.Models) == 0 {
		return true
	}
	m := models.Model{ID: ev.Model, Canonical: ev.RequestedModel}
	for _, p := range c.cfg.Models {
		if models.Match(p, m) {
			return true
		}
	}
	return false
}

// Finish queues a finished request; ev carries its outcome.
func (r *Recorder) Finish(c *Capture, ev store.UsageEvent, clientIP string) {
	if r == nil || c == nil || !c.wanted(ev) {
		return
	}
	c.mu.Lock()
	attempts, _ := json.Marshal(c.attempts)
	c.mu.Unlock()
	q := queued{
		entry:   store.AuditEntry{UsageEvent: ev, ClientIP: clientIP, UserAgent: c.userAgent, Attempts: attempts},
		payload: c.payload(),
	}
	select {
	case r.ch <- q:
	default:
		slog.Warn("audit queue full; dropping entry")
	}
}

func compress(p Payload) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	json.NewEncoder(zw).Encode(p)
	zw.Close()
	return buf.Bytes()
}

// Decode unpacks a stored payload.
func Decode(raw []byte) (Payload, error) {
	var p Payload
	if len(raw) == 0 {
		return p, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return p, err
	}
	defer zr.Close()
	return p, json.NewDecoder(zr).Decode(&p)
}

// Run writes queued entries until ctx ends, then drains what's left.
func (r *Recorder) Run(ctx context.Context) {
	defer close(r.done)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var batch []store.AuditEntry
	flush := func() {
		if len(batch) == 0 {
			return
		}
		fctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := r.store.InsertAudit(fctx, batch); err != nil {
			slog.Error("write audit log", "err", err, "entries", len(batch))
		}
		cancel()
		batch = batch[:0]
	}
	add := func(q queued) {
		q.entry.Payload = compress(q.payload)
		batch = append(batch, q.entry)
	}
	for {
		select {
		case q := <-r.ch:
			add(q)
			if len(batch) >= 50 {
				flush()
			}
		case <-tick.C:
			flush()
		case <-ctx.Done():
			for {
				select {
				case q := <-r.ch:
					add(q)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Wait blocks until Run has drained the queue after shutdown.
func (r *Recorder) Wait() { <-r.done }

// Prune applies the retention limits.
func (r *Recorder) Prune(ctx context.Context) error {
	cfg := r.settings()
	cfg.Normalize()
	var before time.Time
	if cfg.RetentionDays > 0 {
		before = time.Now().AddDate(0, 0, -cfg.RetentionDays)
	}
	return r.store.PruneAudit(ctx, before, cfg.MaxEntries, int64(cfg.MaxStorageMB)<<20)
}
