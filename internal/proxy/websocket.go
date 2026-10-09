package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// Fields a follow-up turn inherits from the conversation when it doesn't set them.
var inheritedFields = []string{"model", "instructions", "tools", "tool_choice", "reasoning", "text", "include",
	"parallel_tool_calls", "prompt_cache_key", "service_tier"}

// wsSession is one Responses WebSocket connection: the conversation so far, so turns that only
// carry new input (previous_response_id) can be expanded into full requests for the HTTP backend.
type wsSession struct {
	h    *Handler
	r    *http.Request
	conn *websocket.Conn
	wmu  sync.Mutex

	lastRequest []byte // normalized request of the last turn, with its full input
	lastOutput  []byte // output items of the last response (JSON array)
	lastID      string

	mu     sync.Mutex
	cancel context.CancelFunc // cancels the running turn
}

// serveWebsocket speaks the Responses API over WebSocket (Codex CLI's transport): the client
// sends response.create / response.append frames and gets each streamed event as a text frame.
func (h *Handler) serveWebsocket(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		writeError(w, formatResponses, newError(http.StatusMethodNotAllowed, "use POST, or connect with a WebSocket"))
		return
	}
	key := clientKey(r)
	if key == "" {
		writeError(w, formatResponses, newError(http.StatusUnauthorized, "missing API key"))
		return
	}
	if _, err := h.store.AuthenticateKey(r.Context(), secret.Hash(key)); err != nil {
		status, msg := http.StatusInternalServerError, "internal error"
		if errors.Is(err, store.ErrNotFound) {
			status, msg = http.StatusUnauthorized, "invalid API key"
		}
		writeError(w, formatResponses, newError(status, msg))
		return
	}
	header := http.Header{}
	if v := r.Header.Get("X-Codex-Turn-State"); v != "" {
		header.Set("X-Codex-Turn-State", v)
	}
	conn, err := upgrader.Upgrade(w, r, header)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxBody)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s := &wsSession{h: h, r: r.WithContext(ctx), conn: conn}

	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
					cancel()
					return
				}
			}
		}
	}()

	frames := make(chan []byte, 16)
	go func() {
		defer close(frames)
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if kind != websocket.TextMessage && kind != websocket.BinaryMessage {
				continue
			}
			if gjson.GetBytes(payload, "type").String() == "response.interrupt" {
				s.interrupt()
				continue
			}
			select {
			case frames <- payload:
			case <-ctx.Done():
				return
			}
		}
	}()

	for payload := range frames {
		if ctx.Err() != nil {
			return
		}
		s.turn(ctx, payload)
	}
}

func (s *wsSession) send(v []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.conn.WriteMessage(websocket.TextMessage, v)
}

func (s *wsSession) sendError(status int, code, msg string) {
	e := map[string]any{"type": errorType(status), "message": msg}
	if code != "" {
		e["code"] = code
	}
	b, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": e})
	s.send(b)
}

func (s *wsSession) interrupt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *wsSession) turn(ctx context.Context, payload []byte) {
	if !gjson.ValidBytes(payload) {
		s.sendError(http.StatusBadRequest, "", "frame must be a JSON object")
		return
	}
	req, status, code, err := s.normalize(payload)
	if err != nil {
		s.sendError(status, code, err.Error())
		return
	}
	if g := gjson.GetBytes(payload, "generate"); g.Exists() && !g.Bool() {
		s.prewarm(req)
		return
	}

	turnCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		cancel()
	}()

	ww := &wsWriter{s: s, header: http.Header{}}
	s.h.handle(ww, s.r.WithContext(turnCtx), &call{format: formatResponses, body: req}, false)
	ww.finish()

	switch {
	case ww.completed:
		s.remember(req, ww.output(), ww.responseID)
	case turnCtx.Err() != nil && ctx.Err() == nil:
		// Interrupted by the client: acknowledge, and keep what was produced as the turn's output.
		id := ww.responseID
		if id == "" {
			id = "resp_" + randomHex(12)
		}
		ack, _ := json.Marshal(map[string]any{"type": "response.incomplete", "response": map[string]any{
			"id": id, "object": "response", "status": "incomplete",
			"incomplete_details": map[string]string{"reason": "interrupted"}, "output": json.RawMessage(ww.output())}})
		s.send(ack)
		s.remember(req, ww.output(), id)
	}
}

func (s *wsSession) remember(req, output []byte, id string) {
	s.lastRequest, s.lastOutput, s.lastID = req, output, id
}

// normalize turns a frame into a complete Responses request.
func (s *wsSession) normalize(raw []byte) ([]byte, int, string, error) {
	typ := gjson.GetBytes(raw, "type").String()
	if typ != "response.create" && typ != "response.append" {
		return nil, http.StatusBadRequest, "", errors.New("unsupported websocket request type: " + typ)
	}
	input := gjson.GetBytes(raw, "input")
	if input.Exists() && !input.IsArray() {
		return nil, http.StatusBadRequest, "", errors.New("websocket request requires array field: input")
	}
	req := raw
	for _, k := range []string{"type", "generate", "previous_response_id"} {
		req, _ = sjson.DeleteBytes(req, k)
	}
	req, _ = sjson.SetBytes(req, "stream", true)

	if s.lastRequest == nil {
		if typ == "response.append" {
			return nil, http.StatusBadRequest, "", errors.New("websocket request received before response.create")
		}
		if strings.TrimSpace(gjson.GetBytes(req, "model").String()) == "" {
			return nil, http.StatusBadRequest, "", errors.New("missing model in response.create request")
		}
		if !input.Exists() {
			req, _ = sjson.SetRawBytes(req, "input", []byte("[]"))
		}
		return req, 0, "", nil
	}

	for _, k := range inheritedFields {
		if v := gjson.GetBytes(s.lastRequest, k); v.Exists() && !gjson.GetBytes(req, k).Exists() {
			req, _ = sjson.SetRawBytes(req, k, []byte(v.Raw))
		}
	}
	prev := gjson.GetBytes(raw, "previous_response_id").String()
	if prev != "" && prev != s.lastID {
		return nil, http.StatusConflict, "previous_response_not_found",
			errors.New("previous response " + prev + " is not part of this connection")
	}
	if prev == "" && typ == "response.create" && carriesHistory(input) {
		return req, 0, "", nil // the client resent the whole transcript (e.g. after compaction)
	}
	merged := []byte("[]")
	for _, part := range [][]byte{[]byte(gjson.GetBytes(s.lastRequest, "input").Raw), s.lastOutput, []byte(input.Raw)} {
		gjson.ParseBytes(part).ForEach(func(_, it gjson.Result) bool {
			merged, _ = sjson.SetRawBytes(merged, "-1", []byte(it.Raw))
			return true
		})
	}
	req, _ = sjson.SetRawBytes(req, "input", merged)
	return req, 0, "", nil
}

// carriesHistory reports whether input holds model output, i.e. is a full transcript.
func carriesHistory(input gjson.Result) bool {
	found := false
	input.ForEach(func(_, it gjson.Result) bool {
		switch it.Get("type").String() {
		case "function_call", "reasoning", "custom_tool_call", "local_shell_call", "compaction":
			found = true
		case "message":
			found = it.Get("role").String() == "assistant"
		}
		return !found
	})
	return found
}

// prewarm answers generate:false locally; Codex uses it to open the connection early.
func (s *wsSession) prewarm(req []byte) {
	id := "resp_prewarm_" + randomHex(16)
	resp := map[string]any{"id": id, "object": "response", "created_at": time.Now().Unix(),
		"model": gjson.GetBytes(req, "model").String(), "output": []any{}, "status": "in_progress"}
	created, _ := json.Marshal(map[string]any{"type": "response.created", "response": resp})
	resp["status"] = "completed"
	resp["usage"] = map[string]int{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": resp})
	s.send(created)
	s.send(completed)
	s.remember(req, []byte("[]"), id)
}

// wsWriter is the http.ResponseWriter a turn writes to: SSE events become text frames, and an
// error response becomes an error frame.
type wsWriter struct {
	s      *wsSession
	header http.Header
	status int
	buf    bytes.Buffer

	responseID string
	completed  bool
	final      []byte            // output of the terminal response
	items      []json.RawMessage // output_item.done items, for interrupted turns
}

func (w *wsWriter) Header() http.Header { return w.header }

func (w *wsWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *wsWriter) Flush() {}

func (w *wsWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.buf.Write(p)
	if w.status != http.StatusOK {
		return len(p), nil
	}
	for {
		data := w.buf.Bytes()
		i := bytes.Index(data, []byte("\n\n"))
		if i < 0 {
			break
		}
		block := string(data[:i])
		w.buf.Next(i + 2)
		var payload strings.Builder
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "data:"); ok {
				payload.WriteString(strings.TrimPrefix(v, " "))
			}
		}
		if payload.Len() == 0 || payload.String() == "[DONE]" {
			continue
		}
		ev := []byte(payload.String())
		w.observe(ev)
		if err := w.s.send(ev); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *wsWriter) observe(ev []byte) {
	d := gjson.ParseBytes(ev)
	switch d.Get("type").String() {
	case "response.created":
		w.responseID = d.Get("response.id").String()
	case "response.output_item.done":
		w.items = append(w.items, json.RawMessage(d.Get("item").Raw))
	case "response.completed", "response.incomplete":
		w.completed = true
		w.responseID = d.Get("response.id").String()
		if out := d.Get("response.output"); out.IsArray() && len(out.Array()) > 0 {
			w.final = []byte(out.Raw)
		}
	}
}

func (w *wsWriter) output() []byte {
	if w.final != nil {
		return w.final
	}
	b, _ := json.Marshal(w.items)
	if w.items == nil {
		return []byte("[]")
	}
	return b
}

// finish turns a non-200 response into an error frame.
func (w *wsWriter) finish() {
	if w.status == 0 || w.status == http.StatusOK {
		return
	}
	body := w.buf.Bytes()
	msg := upstreamMessage(string(body))
	code := gjson.GetBytes(body, "error.code").String()
	slog.Debug("responses websocket turn failed", "status", w.status, "msg", msg)
	w.s.sendError(w.status, code, msg)
}
