package provider

import (
	"context"
	"time"
)

// Subscription is what's known about the plan behind an account.
type Subscription struct {
	Status string `json:"status,omitempty"`
	// Since is when the subscription started.
	Since *time.Time `json:"since,omitempty"`
	// Until is the end of the current paid period, when the provider says so.
	Until     *time.Time `json:"until,omitempty"`
	Billing   string     `json:"billing,omitempty"`
	CheckedAt time.Time  `json:"checkedAt"`
	// WebError explains why the renewal date couldn't be read through a web session.
	WebError string `json:"webError,omitempty"`
}

// SubscriptionSource is implemented by providers that can report subscription details.
type SubscriptionSource interface {
	Subscription(ctx context.Context, c Credentials) (*Subscription, error)
}

// ParseTime reads an RFC 3339 timestamp, returning nil when empty or malformed.
func ParseTime(v any) *time.Time {
	s, _ := v.(string)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}
