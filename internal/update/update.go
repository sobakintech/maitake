// Package update checks GitHub for newer maitake releases and their changelogs.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releasesURL = "https://api.github.com/repos/sobakintech/maitake/releases?per_page=30"

type Settings struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
}

func DefaultSettings() Settings { return Settings{Enabled: true, IntervalHours: 6} }

func (s *Settings) Normalize() {
	if s.IntervalHours < 1 {
		s.IntervalHours = 1
	}
	if s.IntervalHours > 168 {
		s.IntervalHours = 168
	}
}

type Release struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Sections    []Section `json:"sections"`
}

// Section is one heading of a release's changelog, e.g. "Features" with its entries.
type Section struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

type Snapshot struct {
	Current string   `json:"current"`
	Latest  *Release `json:"latest"`
	// Newer lists releases after the running version, newest first. Empty for dev builds.
	Newer     []Release  `json:"newer"`
	CheckedAt *time.Time `json:"checkedAt"`
	Error     string     `json:"error,omitempty"`
}

func (s Snapshot) UpdateAvailable() bool { return len(s.Newer) > 0 }

type Checker struct {
	client   *http.Client
	current  string
	settings func() Settings

	mu       sync.Mutex
	releases []Release
	snap     Snapshot
	etag     string
	wake     chan struct{}
}

func NewChecker(client *http.Client, current string, settings func() Settings) *Checker {
	return &Checker{client: client, current: current, settings: settings, wake: make(chan struct{}, 1),
		snap: Snapshot{Current: current, Newer: []Release{}}}
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

// Check fetches releases now and returns the result.
func (c *Checker) Check(ctx context.Context) Snapshot {
	c.fetch(ctx)
	return c.Snapshot()
}

// Wake makes the loop re-read its settings, e.g. after checks were switched back on.
func (c *Checker) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Checker) Run(ctx context.Context) {
	for {
		s := c.settings()
		s.Normalize()
		if s.Enabled {
			c.fetch(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-time.After(time.Duration(s.IntervalHours) * time.Hour):
		}
	}
}

func (c *Checker) fetch(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "maitake/"+c.current)
	c.mu.Lock()
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	c.mu.Unlock()

	releases, etag, err := c.do(req)
	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snap.CheckedAt = &now
	if err != nil {
		slog.Warn("update check failed", "err", err)
		c.snap.Error = err.Error()
		return
	}
	c.snap.Error = ""
	if releases != nil {
		c.releases, c.etag = releases, etag
	}
	c.snap.Latest, c.snap.Newer = nil, []Release{}
	if len(c.releases) > 0 {
		c.snap.Latest = &c.releases[0]
	}
	if cur, ok := parse(c.current); ok {
		for _, r := range c.releases {
			if v, _ := parse(r.Version); less(cur, v) {
				c.snap.Newer = append(c.snap.Newer, r)
			}
		}
	}
}

func (c *Checker) do(req *http.Request) ([]Release, string, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}
	var raw []struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	out := []Release{}
	for _, r := range raw {
		if r.Draft || r.Prerelease {
			continue
		}
		if _, ok := parse(r.TagName); !ok {
			continue
		}
		out = append(out, Release{Version: strings.TrimPrefix(r.TagName, "v"), URL: r.HTMLURL,
			PublishedAt: r.PublishedAt, Sections: ParseNotes(r.Body)})
	}
	slices.SortStableFunc(out, func(a, b Release) int {
		va, _ := parse(a.Version)
		vb, _ := parse(b.Version)
		return slices.Compare(vb[:], va[:])
	})
	return out, resp.Header.Get("ETag"), nil
}

type semver [3]int

// parse accepts "1.2.3" or "v1.2.3"; pre-release and dev versions don't parse.
func parse(v string) (semver, bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var out semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		out[i] = n
	}
	return out, true
}

func less(a, b semver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

var (
	commitRef = regexp.MustCompile(`\s*\(\[[0-9a-f]{7,40}\]\([^)]*\)\)`)
	issueRef  = regexp.MustCompile(`,?\s*closes\s+\[#\d+\]\([^)]*\)`)
	mdLink    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// ParseNotes turns a release changelog body into plain-text sections, so the dashboard
// never renders markup from the release.
func ParseNotes(body string) []Section {
	out := []Section{}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "### "):
			out = append(out, Section{Title: strings.TrimSpace(line[4:]), Items: []string{}})
		case strings.HasPrefix(line, "* "), strings.HasPrefix(line, "- "):
			item := commitRef.ReplaceAllString(line[2:], "")
			item = issueRef.ReplaceAllString(item, "")
			item = mdLink.ReplaceAllString(item, "$1")
			item = strings.TrimSpace(strings.ReplaceAll(item, "**", ""))
			if item == "" {
				continue
			}
			if len(out) == 0 {
				out = append(out, Section{Title: "Changes", Items: []string{}})
			}
			out[len(out)-1].Items = append(out[len(out)-1].Items, item)
		}
	}
	kept := out[:0]
	for _, s := range out {
		if len(s.Items) > 0 {
			kept = append(kept, s)
		}
	}
	return kept
}
