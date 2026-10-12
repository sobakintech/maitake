package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sobakintech/maitake/internal/provider"
)

func TestResolveModel(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"data":[
			{"id":"claude-haiku-4-5-20251001","line":"haiku","created_at":"2025-10-01T00:00:00Z","lifecycle":"active"},
			{"id":"claude-haiku-5-5","line":"haiku","created_at":"2026-10-07T18:00:00Z","lifecycle":"active"},
			{"id":"claude-haiku-6","line":"haiku","created_at":"2027-01-01T00:00:00Z","lifecycle":"preview"},
			{"id":"claude-sonnet-5-5","line":"sonnet","created_at":"2026-09-28T00:00:00Z","lifecycle":"active"}]}`))
	}))
	defer srv.Close()
	p := New(srv.Client(), false)
	p.APIBase = srv.URL
	ctx, c := context.Background(), provider.Credentials{AccessToken: "t"}
	for in, want := range map[string]string{"haiku": "claude-haiku-5-5", "Sonnet": "claude-sonnet-5-5",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5-20251001", "opus": "claude-opus-5-5"} {
		if got := p.ResolveModel(ctx, c, in); got != want {
			t.Errorf("ResolveModel(%q) = %q, want %q", in, got, want)
		}
	}
	if calls != 1 {
		t.Errorf("model list fetched %d times, want 1", calls)
	}
}
