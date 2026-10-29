package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	real := map[string]string{"X-Real-IP": "203.0.113.7"}
	tests := []struct {
		name    string
		cidrs   string
		peer    string
		headers map[string]string
		want    string
	}{
		{"default trusts loopback", "", "127.0.0.1:5000", real, "203.0.113.7"},
		{"default trusts ipv6 loopback", "", "[::1]:5000", real, "203.0.113.7"},
		{"default does not trust docker gateway", "", "192.168.208.1:5000", real, "192.168.208.1"},
		{"configured range trusted", "192.168.208.0/24", "192.168.208.1:5000", real, "203.0.113.7"},
		{"configured range replaces loopback", "192.168.208.0/24", "127.0.0.1:5000", real, "127.0.0.1"},
		{"several ranges and a bare address", "10.0.0.0/8, 172.18.0.2", "172.18.0.2:1", real, "203.0.113.7"},
		{"untrusted peer cannot spoof", "", "198.51.100.9:5000", map[string]string{"X-Real-IP": "1.2.3.4"}, "198.51.100.9"},
		{"trusted peer without header", "", "127.0.0.1:5000", nil, "127.0.0.1"},
		{"unparsable header", "", "127.0.0.1:5000", map[string]string{"X-Real-IP": "not-an-ip"}, "127.0.0.1"},
		{"cf header ignored", "", "127.0.0.1:5000", map[string]string{"CF-Connecting-IP": "1.2.3.4"}, "127.0.0.1"},
		{"x-forwarded-for ignored", "", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "127.0.0.1"},
		{"real ip wins over the others", "", "127.0.0.1:5000", map[string]string{"X-Real-IP": "203.0.113.7", "CF-Connecting-IP": "1.2.3.4", "X-Forwarded-For": "5.6.7.8"}, "203.0.113.7"},
		{"ipv4-mapped peer", "", "[::ffff:127.0.0.1]:5000", real, "203.0.113.7"},
		{"ipv6 client", "", "127.0.0.1:5000", map[string]string{"X-Real-IP": "2001:db8::1"}, "2001:db8::1"},
		{"peer without port", "", "127.0.0.1", real, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trusted, err := ParseTrustedProxies(tt.cidrs)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := clientIP(r, trusted); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTrustedProxiesRejectsGarbage(t *testing.T) {
	for _, in := range []string{"nope", "10.0.0.0/33", "127.0.0.1,abc"} {
		if _, err := ParseTrustedProxies(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func testLimits() failureLimits {
	return failureLimits{Window: 10 * time.Minute, KeyAttempts: 30, KeyFailures: 10, GlobalFailed: 300}
}

func testLimiter() (*keyedLimiter, *time.Time) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	return newKeyedLimiter(testLimits(), func() time.Time { return now }), &now
}

func TestKeyedLimiterKeyFailures(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 10; i++ {
		if ok, _ := l.allow("1.1.1.1"); !ok {
			t.Fatalf("attempt %d blocked", i)
		}
		l.fail("1.1.1.1")
		*now = now.Add(10 * time.Second)
	}
	ok, wait := l.allow("1.1.1.1")
	if ok {
		t.Fatal("attempt after 10 failures must be blocked")
	}
	// the oldest failure was 100 s ago
	if want := 10*time.Minute - 100*time.Second; wait != want {
		t.Fatalf("retryAfter = %v, want %v", wait, want)
	}
	if ok, _ := l.allow("2.2.2.2"); !ok {
		t.Fatal("another IP must not be affected")
	}
	*now = now.Add(wait)
	if ok, _ := l.allow("1.1.1.1"); !ok {
		t.Fatal("must be allowed once the oldest failure left the window")
	}
}

func TestKeyedLimiterSuccessDoesNotResetFailures(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < 10; i++ {
		l.allow("a")
		l.fail("a")
	}
	// a success is an allowed attempt with no fail call; it must not clear the failures
	if ok, _ := l.allow("a"); ok {
		t.Fatal("still locked out")
	}
}

func TestKeyedLimiterAttemptsPerKey(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 30; i++ {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("attempt %d blocked", i)
		}
	}
	ok, wait := l.allow("a")
	if ok || wait != 10*time.Minute {
		t.Fatalf("31st attempt: ok=%v wait=%v", ok, wait)
	}
	// blocked attempts are not recorded: the window ends 10 minutes after the first one
	*now = now.Add(10 * time.Minute)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("must be allowed after the window")
	}
}

func TestKeyedLimiterGlobalFailures(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 300; i++ {
		l.fail(fmt.Sprintf("10.0.%d.%d", i/250, i%250))
	}
	ok, wait := l.allow("brand-new-ip")
	if ok || wait != 10*time.Minute {
		t.Fatalf("global cap: ok=%v wait=%v", ok, wait)
	}
	*now = now.Add(10 * time.Minute)
	if ok, _ := l.allow("brand-new-ip"); !ok {
		t.Fatal("global cap must lift after the window")
	}
}

func TestKeyedLimiterForgetsIdleKeys(t *testing.T) {
	l, now := testLimiter()
	l.allow("a")
	l.fail("a")
	*now = now.Add(11 * time.Minute)
	l.allow("a")
	if len(l.failures) != 0 || len(l.attempts) != 1 {
		t.Fatalf("stale keys kept: failures=%d attempts=%d", len(l.failures), len(l.attempts))
	}
}
