// Package settings holds runtime settings editable from the dashboard, cached in memory.
package settings

import (
	"context"
	"sync/atomic"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/quota"
	"github.com/sobakintech/maitake/internal/resets"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/store"
)

const key = "settings"

type Settings struct {
	QuotaPage          quota.PageSettings   `json:"quotaPage"`
	Pool               accounts.Settings    `json:"pool"`
	CodexResets        resets.Settings      `json:"codexResets"`
	UsageRetentionDays int                  `json:"usageRetentionDays"`
	Routing            routing.Settings     `json:"routing"`
	Keys               KeySettings          `json:"keys"`
	Claude             claude.CloakSettings `json:"claude"`
}

type KeySettings struct {
	// StoreCopies keeps an encrypted copy of new keys so admins can copy them later.
	StoreCopies bool `json:"storeCopies"`
}

func Defaults() Settings {
	return Settings{
		QuotaPage:          quota.DefaultPageSettings(),
		Pool:               accounts.DefaultSettings(),
		CodexResets:        resets.DefaultSettings(),
		UsageRetentionDays: 90,
		Routing:            routing.DefaultSettings(),
		Keys:               KeySettings{StoreCopies: true},
		Claude:             claude.DefaultCloak(),
	}
}

func (s *Settings) Normalize() {
	s.QuotaPage.Normalize()
	s.Pool.Normalize()
	s.CodexResets.Normalize()
	s.Routing.Normalize()
	s.Claude.Normalize()
	if s.UsageRetentionDays < 0 {
		s.UsageRetentionDays = 0
	}
}

type Store struct {
	db  *store.Store
	cur atomic.Pointer[Settings]
}

func Load(ctx context.Context, db *store.Store) (*Store, error) {
	s := Defaults()
	if err := db.GetSetting(ctx, key, &s); err != nil {
		return nil, err
	}
	s.Normalize()
	st := &Store{db: db}
	st.cur.Store(&s)
	return st, nil
}

func (s *Store) Get() Settings { return *s.cur.Load() }

func (s *Store) Put(ctx context.Context, v Settings) (Settings, error) {
	v.Normalize()
	if err := s.db.PutSetting(ctx, key, v); err != nil {
		return Settings{}, err
	}
	s.cur.Store(&v)
	return v, nil
}
