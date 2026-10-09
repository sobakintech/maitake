package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type format int

const (
	formatClaude format = iota
	formatChat
	formatResponses
)

func errorType(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusPaymentRequired:
		return "billing_error"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "timeout_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case 529, http.StatusServiceUnavailable:
		return "overloaded_error"
	}
	return "api_error"
}

// apiError is an error to return to the client in its own API format.
type apiError struct {
	status     int
	message    string
	retryAfter time.Duration
	typ        string // overrides the type derived from status
	code       string
}

func (e *apiError) Error() string { return e.message }

func newError(status int, msg string) *apiError { return &apiError{status: status, message: msg} }

func writeError(w http.ResponseWriter, f format, e *apiError) {
	w.Header().Set("Content-Type", "application/json")
	if e.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(e.retryAfter.Seconds()+0.999)))
	}
	w.WriteHeader(e.status)
	typ := e.typ
	if typ == "" {
		typ = errorType(e.status)
	}
	var code any
	if e.code != "" {
		code = e.code
	}
	var body any
	if f == formatClaude {
		body = map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": e.message}}
	} else {
		body = map[string]any{"error": map[string]any{"message": e.message, "type": typ, "code": code}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

// upstreamMessage pulls a human-readable message out of an upstream error body.
func upstreamMessage(body string) string {
	for _, path := range []string{"error.message", "message", "detail", "error.detail", "error"} {
		if v := gjson.Get(body, path); v.Type == gjson.String && v.String() != "" {
			return v.String()
		}
	}
	body = strings.TrimSpace(body)
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	if body == "" {
		return "upstream error"
	}
	return body
}

// clientError builds the error returned to the client for an upstream failure, keeping the
// upstream's error type and recognising Codex's well-known error kinds.
func clientError(provider string, f failure) *apiError {
	e := newError(f.status, upstreamMessage(f.body))
	if t := gjson.Get(f.body, "error.type").String(); t != "" && t != "error" {
		e.typ = t
	}
	if provider == "codex" {
		e.code, e.typ = codexErrorKind(f.status, f.body, e.typ)
		if isCapacityError(f.body) {
			e.status, e.typ = http.StatusTooManyRequests, "rate_limit_error"
		}
	}
	return e
}

func isCapacityError(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "at capacity") || strings.Contains(b, "model_at_capacity")
}

func codexErrorKind(status int, body, typ string) (string, string) {
	lower := strings.ToLower(body)
	msg := strings.ToLower(upstreamMessage(body))
	code := strings.ToLower(gjson.Get(body, "error.code").String())
	invalid := typ == "" || typ == "invalid_request_error"
	switch {
	case status == http.StatusRequestEntityTooLarge || code == "context_length_exceeded" || code == "context_too_large" ||
		invalid && (strings.Contains(msg, "context length") || strings.Contains(msg, "context_length") ||
			strings.Contains(msg, "maximum context") || strings.Contains(msg, "too many tokens")):
		return "context_too_large", "invalid_request_error"
	case strings.Contains(lower, "invalid_encrypted_content") || strings.Contains(lower, "signature") && strings.Contains(lower, "thinking"):
		return "thinking_signature_invalid", "invalid_request_error"
	case code == "previous_response_not_found" || strings.Contains(lower, "previous_response_not_found"):
		return "previous_response_not_found", "invalid_request_error"
	}
	return code, typ
}
