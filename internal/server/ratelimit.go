package server

import (
	"sync"
	"time"
)

// failureLimiter blocks a key after too many failures within a fixed window.
type failureLimiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	entries map[string]*failureEntry
}

type failureEntry struct {
	count int
	start time.Time
}

func newFailureLimiter(max int, window time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, entries: map[string]*failureEntry{}}
}

// blocked reports whether key is locked out and for how long.
func (l *failureLimiter) blocked(key string) (bool, time.Duration) {
	if l.max <= 0 {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return false, 0
	}
	remaining := l.window - time.Since(e.start)
	if remaining <= 0 {
		delete(l.entries, key)
		return false, 0
	}
	return e.count >= l.max, remaining
}

func (l *failureLimiter) fail(key string) {
	if l.max <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.start) >= l.window {
		e = &failureEntry{start: now}
		l.entries[key] = e
	}
	e.count++
	if len(l.entries) > 10000 {
		for k, v := range l.entries {
			if now.Sub(v.start) >= l.window {
				delete(l.entries, k)
			}
		}
	}
}

func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
