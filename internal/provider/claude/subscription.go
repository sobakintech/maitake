package claude

import (
	"context"
	"errors"
	"time"

	"github.com/sobakintech/maitake/internal/provider"
)

// Subscription reads status and start date from the OAuth profile. The renewal date needs a
// claude.ai session (c.WebSession); without one Until stays empty.
func (p *Provider) Subscription(ctx context.Context, c provider.Credentials) (*provider.Subscription, error) {
	prof, err := p.profile(ctx, c.AccessToken)
	if err != nil {
		return nil, err
	}
	sub := &provider.Subscription{
		Status:    prof.Organization.SubscriptionStatus,
		Since:     provider.ParseTime(prof.Organization.SubscriptionCreatedAt),
		Billing:   prof.Organization.BillingType,
		CheckedAt: time.Now().UTC(),
	}
	if c.WebSession != "" {
		until, renewed, err := p.webRenewal(ctx, c.WebSession, prof.Organization.UUID)
		sub.RenewedSession = renewed
		if err != nil {
			sub.WebError = err.Error()
			sub.WebExpired = errors.Is(err, ErrWebSessionExpired)
		} else {
			sub.Until = until
		}
	}
	return sub, nil
}
