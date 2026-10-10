package resets

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"time"
)

// Claude reads claude-resets.com's dataset of Claude resets announced by @ClaudeDevs and the
// Claude team. It has no schedule or forecast, so only Latest and Stats are filled.
var Claude = Feed{Name: "claude-resets", URL: "https://claude-resets.com/data/resets.json", decode: decodeClaude}

type claudeEvent struct {
	ID          string     `json:"id"`
	Date        time.Time  `json:"date"`
	Kind        string     `json:"kind"`
	ResetType   string     `json:"resetType"`
	UsableUntil *time.Time `json:"usableUntil"`
	Scope       string     `json:"scope"`
	Account     string     `json:"account"`
	Note        string     `json:"note"`
	URL         string     `json:"url"`
}

func decodeClaude(r io.Reader, now time.Time) (*Status, error) {
	var body struct {
		Providers map[string]struct {
			Account string        `json:"account"`
			Events  []claudeEvent `json:"events"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, err
	}
	feed, ok := body.Providers["claude"]
	if !ok {
		return nil, errors.New("no Claude data in the feed")
	}
	// Policy changes are listed too, but only resets count.
	var events []claudeEvent
	for _, e := range feed.Events {
		if e.Kind == "reset" && !e.Date.IsZero() {
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })

	st := &Status{Stats: Stats{Total: len(events)}}
	if len(events) == 0 {
		return st, nil
	}
	last := events[len(events)-1]
	author := last.Account
	if author == "" {
		author = feed.Account
	}
	resetType := last.ResetType
	if resetType == "" {
		resetType = "regular"
	}
	if last.Scope == "all" {
		last.Scope = ""
	}
	st.Latest = &Reset{ID: last.ID, ResetType: resetType, AnnouncedAt: last.Date, Text: last.Note,
		Source: Source{Type: "x_post", Author: author, URL: last.URL}, Scope: last.Scope, UsableUntil: last.UsableUntil}
	at := last.Date
	since := math.Round(now.Sub(at).Hours()/24*10) / 10
	st.Stats.LastResetAt, st.Stats.DaysSinceLast = &at, &since
	if len(events) > 1 {
		avg := math.Round(last.Date.Sub(events[0].Date).Hours()/24/float64(len(events)-1)*10) / 10
		st.Stats.AvgIntervalDays = &avg
	}
	return st, nil
}
