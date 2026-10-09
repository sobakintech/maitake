package proxy

import (
	"encoding/json"
	"sync"
	"time"
)

// rpmLimiter enforces per-key requests-per-minute with a fixed one-minute window.
type rpmLimiter struct {
	mu      sync.Mutex
	windows map[int64]*rpmWindow
}

type rpmWindow struct {
	start time.Time
	count int
}

func newRPMLimiter() *rpmLimiter { return &rpmLimiter{windows: map[int64]*rpmWindow{}} }

func (l *rpmLimiter) allow(keyID int64, limit int) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.windows[keyID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &rpmWindow{start: now}
		l.windows[keyID] = w
	}
	if w.count >= limit {
		return false, time.Minute - now.Sub(w.start)
	}
	w.count++
	return true, 0
}

func marshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
