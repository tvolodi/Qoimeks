package api

import (
	"sync"
	"time"
)

// rateLimiter allows n events per rolling window. now is injectable for tests.
type rateLimiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	now    func() time.Time
	events []time.Time
}

// allow records an event and reports whether it is within the limit; when it is not,
// retryAfter says how long until the oldest event leaves the window.
func (l *rateLimiter) allow() (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cut := 0
	for cut < len(l.events) && !l.events[cut].After(now.Add(-l.window)) {
		cut++
	}
	l.events = l.events[cut:]
	if len(l.events) >= l.n {
		return false, l.events[0].Add(l.window).Sub(now)
	}
	l.events = append(l.events, now)
	return true, 0
}
