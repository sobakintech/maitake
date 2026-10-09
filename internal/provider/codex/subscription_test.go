package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/sobakintech/maitake/internal/provider"
)

func fakeIDToken(claims map[string]any) string {
	raw, _ := json.Marshal(claims)
	return "x." + base64.RawURLEncoding.EncodeToString(raw) + ".y"
}

func TestSubscriptionFromIDToken(t *testing.T) {
	tok := fakeIDToken(map[string]any{"https://api.openai.com/auth": map[string]any{
		"chatgpt_plan_type":                 "plus",
		"chatgpt_subscription_active_start": "2026-09-24T11:12:27+00:00",
		"chatgpt_subscription_active_until": "2099-10-24T11:12:27+00:00",
	}})
	s, err := (&Provider{}).Subscription(context.Background(), provider.Credentials{IDToken: tok})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "active" || s.Until == nil || s.Until.Year() != 2099 || s.Since == nil || s.Since.Day() != 24 {
		t.Fatalf("unexpected subscription: %+v", s)
	}
}
