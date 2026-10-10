// Package resets polls public trackers of announced global usage-limit resets:
// codex-resets.com for Codex and claude-resets.com for Claude.
package resets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Feed is one tracker: where to fetch it and how to read it into a Status.
type Feed struct {
	Name   string
	URL    string
	decode func(r io.Reader, now time.Time) (*Status, error)
}

var Codex = Feed{Name: "codex-resets", URL: "https://codex-resets.com/api/v1/status", decode: decodeCodex}

type Source struct {
	Type   string `json:"type"`
	Author string `json:"author,omitempty"`
	URL    string `json:"url,omitempty"`
}

type Reset struct {
	ID          string     `json:"id"`
	Status      string     `json:"status,omitempty"`
	ResetType   string     `json:"reset_type"`
	AnnouncedAt time.Time  `json:"announced_at"`
	ScheduledAt *time.Time `json:"scheduled_for,omitempty"`
	Text        string     `json:"text"`
	Source      Source     `json:"source"`
	// Scope is who it applied to, e.g. "Max" (Claude).
	Scope string `json:"scope,omitempty"`
	// UsableUntil is when a banked reset expires (Claude).
	UsableUntil *time.Time `json:"usable_until,omitempty"`
}

type Watch struct {
	Level          string    `json:"level"`
	ChancePercent  *int      `json:"reset_chance_percent"`
	ForecastWindow string    `json:"forecast_window"`
	ObservedAt     time.Time `json:"observed_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Text           string    `json:"text"`
	Source         Source    `json:"source"`
}

type Stats struct {
	Total           int        `json:"total"`
	LastResetAt     *time.Time `json:"last_reset_at"`
	DaysSinceLast   *float64   `json:"days_since_last"`
	AvgIntervalDays *float64   `json:"avg_interval_days"`
}

type Status struct {
	Latest    *Reset `json:"latest_reset"`
	Scheduled *Reset `json:"scheduled_reset"`
	Watch     *Watch `json:"active_watch"`
	Stats     Stats  `json:"stats"`
}

// Snapshot is the last successful fetch plus bookkeeping.
type Snapshot struct {
	Status    *Status    `json:"status"`
	FetchedAt *time.Time `json:"fetchedAt"`
	Error     string     `json:"error,omitempty"`
}

type Settings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

func DefaultSettings() Settings { return Settings{Enabled: true, IntervalMinutes: 10} }

func (s *Settings) Normalize() {
	if s.IntervalMinutes < 2 {
		s.IntervalMinutes = 2
	}
	if s.IntervalMinutes > 1440 {
		s.IntervalMinutes = 1440
	}
}

type Poller struct {
	client   *http.Client
	feed     Feed
	settings func() Settings

	mu   sync.RWMutex
	snap Snapshot
	etag string
	wake chan struct{}
}

func NewPoller(client *http.Client, feed Feed, settings func() Settings) *Poller {
	return &Poller{client: client, feed: feed, settings: settings, wake: make(chan struct{}, 1)}
}

func (p *Poller) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snap
}

// Refresh asks the poll loop to fetch now.
func (p *Poller) Refresh() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Poller) Run(ctx context.Context) {
	for {
		s := p.settings()
		s.Normalize()
		if s.Enabled {
			p.fetch(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		case <-time.After(time.Duration(s.IntervalMinutes) * time.Minute):
		}
	}
}

func (p *Poller) fetch(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.feed.URL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "maitake")
	p.mu.RLock()
	if p.etag != "" && p.snap.Status != nil {
		req.Header.Set("If-None-Match", p.etag)
	}
	p.mu.RUnlock()

	status, etag, err := p.do(req)
	now := time.Now().UTC()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		slog.Warn("reset feed fetch failed", "feed", p.feed.Name, "err", err)
		p.snap.Error = err.Error()
		return
	}
	p.snap.FetchedAt = &now
	p.snap.Error = ""
	if status != nil {
		p.snap.Status = status
		p.etag = etag
	}
}

func (p *Poller) do(req *http.Request) (*Status, string, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}
	status, err := p.feed.decode(resp.Body, time.Now().UTC())
	if err != nil {
		return nil, "", err
	}
	return status, resp.Header.Get("ETag"), nil
}

func decodeCodex(r io.Reader, _ time.Time) (*Status, error) {
	var body struct {
		Data Status `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, err
	}
	return &body.Data, nil
}
