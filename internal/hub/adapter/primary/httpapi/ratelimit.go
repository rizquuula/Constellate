package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// loginIPMax is the per-IP login attempt budget within loginWindow.
	loginIPMax = 5
	// loginGlobalMax is the budget across all IPs within loginWindow.
	loginGlobalMax = 15
	// loginWindow is the fixed-window duration for both limiters.
	loginWindow = time.Minute
)

// rateLimiter is a goroutine-safe fixed-window counter keyed by string.
// Each key gets at most max requests in any window of duration window.
// Stale entries are evicted opportunistically: every evictInterval calls
// to allow, a sweep removes keys whose window has fully elapsed.
type rateLimiter struct {
	mu             sync.Mutex
	max            int
	window         time.Duration
	entries        map[string]*rlEntry
	callCount      int
	evictInterval  int
}

type rlEntry struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		max:           max,
		window:        window,
		entries:       make(map[string]*rlEntry),
		evictInterval: 100,
	}
}

// allow returns (true, 0) when the request is permitted, or (false, retryAfter)
// when the caller has exceeded max attempts within window. now is injected so
// tests can control time.
func (l *rateLimiter) allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.callCount++
	if l.callCount%l.evictInterval == 0 {
		l.evict(now)
	}

	e, exists := l.entries[key]
	if !exists || now.After(e.windowEnd) {
		// New or expired window — reset.
		l.entries[key] = &rlEntry{count: 1, windowEnd: now.Add(l.window)}
		return true, 0
	}

	if e.count >= l.max {
		return false, e.windowEnd.Sub(now)
	}

	e.count++
	return true, 0
}

// evict removes entries whose window has fully elapsed. Must be called with l.mu held.
func (l *rateLimiter) evict(now time.Time) {
	for k, e := range l.entries {
		if now.After(e.windowEnd) {
			delete(l.entries, k)
		}
	}
}

// clientIP extracts the real client IP from the request.
//
// XFF trust assumption: the hub runs behind a TLS-terminating Caddy reverse
// proxy in production. Caddy appends the real client IP to X-Forwarded-For, so
// the FIRST hop in XFF is the actual client IP. This is only safe when the
// hub is never directly reachable by the public internet — if that invariant
// breaks, XFF should be ignored or validated against a trusted-proxy list.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take only the first hop (the real client), trim whitespace.
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			xff = xff[:idx]
		}
		ip := strings.TrimSpace(xff)
		if ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
