package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a per-key sliding-window counter. It tracks request
// timestamps per key (typically IP) and rejects when count within the
// last `window` exceeds `limit`. Stale keys (no recent activity) are
// garbage-collected on a periodic sweep so the map can't grow without
// bound under a long flood of unique IPs.
type rateLimiter struct {
	mu     sync.Mutex
	visits map[string][]time.Time
	limit  int
	window time.Duration
	lastGC time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		visits: make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

// Allow returns true if the request is within the limit. An empty key
// always allows (avoids penalising unidentifiable callers).
func (r *rateLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if now.Sub(r.lastGC) > r.window {
		r.gcLocked(now)
	}

	cutoff := now.Add(-r.window)
	ts := keepAfter(r.visits[key], cutoff)
	if len(ts) >= r.limit {
		r.visits[key] = ts
		return false
	}
	r.visits[key] = append(ts, now)
	return true
}

func (r *rateLimiter) gcLocked(now time.Time) {
	cutoff := now.Add(-r.window)
	for k, ts := range r.visits {
		kept := keepAfter(ts, cutoff)
		if len(kept) == 0 {
			delete(r.visits, k)
		} else {
			r.visits[k] = kept
		}
	}
	r.lastGC = now
}

func keepAfter(ts []time.Time, cutoff time.Time) []time.Time {
	kept := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

// clientIP extracts the originating client IP for rate-limiting purposes.
// Cloudflare's CF-Connecting-IP is preferred (set by Cloudflare itself,
// not spoofable by the client when arriving via the tunnel). Falls back to
// X-Forwarded-For's leftmost entry, then net.SplitHostPort(RemoteAddr).
func clientIP(r *http.Request) string {
	if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
		return strings.TrimSpace(cf)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
