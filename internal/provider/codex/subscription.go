package codex

import (
	"context"
	"errors"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
)

// Subscription reads the plan period from the id_token's ChatGPT claims; no request needed.
func (p *Provider) Subscription(_ context.Context, c provider.Credentials) (*provider.Subscription, error) {
	claims, err := provider.JWTClaims(c.IDToken)
	if err != nil {
		return nil, err
	}
	auth, ok := claims["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return nil, errors.New("id_token has no ChatGPT claims")
	}
	s := &provider.Subscription{
		Since:     provider.ParseTime(auth["chatgpt_subscription_active_start"]),
		Until:     provider.ParseTime(auth["chatgpt_subscription_active_until"]),
		CheckedAt: time.Now().UTC(),
	}
	if checked := provider.ParseTime(auth["chatgpt_subscription_last_checked"]); checked != nil {
		s.CheckedAt = *checked
	}
	switch {
	case s.Until == nil:
		s.Status = "unknown"
	case s.Until.After(time.Now()):
		s.Status = "active"
	default:
		s.Status = "ended"
	}
	return s, nil
}
