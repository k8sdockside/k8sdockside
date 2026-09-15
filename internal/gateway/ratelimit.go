package gateway

import (
	"sync"
	"time"
)

// limiter counts failures per key -- an address, a username -- and says when
// a key has failed too often lately. It is what stands between a password
// form and someone trying every password.
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// blocked reports whether key has failed limit times within the window.
func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.limit
}

// fail records one failure for key.
func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.recent(key), l.now())
	// Every address that ever failed is a key; drop the stale ones before the
	// map becomes the thing being attacked.
	if len(l.fails) > 10_000 {
		l.pruneLocked()
	}
}

// forget clears key's failures, after it has succeeded.
func (l *limiter) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// prune drops every key with no failure inside the window.
func (l *limiter) prune() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
}

func (l *limiter) pruneLocked() {
	for key := range l.fails {
		l.recent(key)
	}
}

// recent returns key's failures inside the window, forgetting older ones.
func (l *limiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.fails[key][:0]
	for _, at := range l.fails[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = kept
	return kept
}
