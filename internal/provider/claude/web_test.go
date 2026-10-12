package claude

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebRenewal(t *testing.T) {
	var reply func(w http.ResponseWriter)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sessionKey"); err != nil || c.Value != "old" {
			t.Errorf("sent cookie %v, want sessionKey=old", r.Header.Get("Cookie"))
		}
		reply(w)
	}))
	defer srv.Close()
	base := WebBase
	WebBase = srv.URL
	defer func() { WebBase = base }()
	p := New(srv.Client(), false)
	ctx := context.Background()

	reply = func(w http.ResponseWriter) {
		http.SetCookie(w, &http.Cookie{Name: "sessionKey", Value: "new", MaxAge: 86400})
		w.Write([]byte(`{"next_charge_date":"2026-11-05"}`))
	}
	until, renewed, err := p.webRenewal(ctx, "sessionKey=old; other=1", "org")
	if err != nil || until == nil || renewed != "new" {
		t.Fatalf("got %v %q %v, want a date and the renewed key", until, renewed, err)
	}

	reply = func(w http.ResponseWriter) {
		http.SetCookie(w, &http.Cookie{Name: "sessionKey", Value: "old"})
		w.Write([]byte(`{"next_charge_date":"2026-11-05"}`))
	}
	if _, renewed, _ := p.webRenewal(ctx, "old", "org"); renewed != "" {
		t.Fatalf("an unchanged key isn't a renewal, got %q", renewed)
	}

	reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }
	if _, _, err := p.webRenewal(ctx, "old", "org"); !errors.Is(err, ErrWebSessionExpired) {
		t.Fatalf("got %v, want ErrWebSessionExpired", err)
	}
}
