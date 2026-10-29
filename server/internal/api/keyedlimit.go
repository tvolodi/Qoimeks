package api

import (
	"sync"
	"time"
)

// failureLimits are the limits of a public, guessable endpoint (pairing exchange): attempts and
// failures per key (the client IP), and failures over the whole server. Counters are in
// memory, so a restart resets them.
type failureLimits struct {
	Window       time.Duration
	KeyAttempts  int // all attempts per key within Window
	KeyFailures  int // failed attempts per key within Window
	GlobalFailed int // failed attempts of all keys within Window
}

// keyedLimiter applies failureLimits. now is injectable for tests.
type keyedLimiter struct {
	mu       sync.Mutex
	limits   failureLimits
	now      func() time.Time
	attempts map[string][]time.Time
	failures map[string][]time.Time
	global   []time.Time
}

func newKeyedLimiter(limits failureLimits, now func() time.Time) *keyedLimiter {
	return &keyedLimiter{
		limits:   limits,
		now:      now,
		attempts: map[string][]time.Time{},
		failures: map[string][]time.Time{},
	}
}

// trim drops events that left the window.
func (l *keyedLimiter) trim(events []time.Time, now time.Time) []time.Time {
	cut := 0
	for cut < len(events) && !events[cut].After(now.Add(-l.limits.Window)) {
		cut++
	}
	return events[cut:]
}

// keep stores events under key, forgetting keys with none so idle addresses cost nothing.
func keep(m map[string][]time.Time, key string, events []time.Time) {
	if len(events) == 0 {
		delete(m, key)
		return
	}
	m[key] = events
}

// allow records an attempt by key and reports whether it may proceed. A blocked attempt is not
// recorded (a patient client is not locked out by its own retries); retryAfter is how long
// until the last limit that blocks it lets go.
func (l *keyedLimiter) allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.global = l.trim(l.global, now)
	fails := l.trim(l.failures[key], now)
	keep(l.failures, key, fails)
	atts := l.trim(l.attempts[key], now)
	keep(l.attempts, key, atts)

	blocked := false
	hold := func(events []time.Time, limit int) {
		if limit <= 0 || len(events) < limit {
			return
		}
		blocked = true
		// the limit lets go when the event `limit` places from the end leaves the window
		if w := events[len(events)-limit].Add(l.limits.Window).Sub(now); w > retryAfter {
			retryAfter = w
		}
	}
	hold(l.global, l.limits.GlobalFailed)
	hold(fails, l.limits.KeyFailures)
	hold(atts, l.limits.KeyAttempts)
	if blocked {
		return false, retryAfter
	}
	l.attempts[key] = append(atts, now)
	return true, 0
}

// fail records a failed attempt by key, for the per-key and the global failure limits.
func (l *keyedLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.failures[key] = append(l.trim(l.failures[key], now), now)
	l.global = append(l.trim(l.global, now), now)
}
