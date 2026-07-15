package limbgo

import (
	"net"
	"sync"
	"time"
)

const (
	DefaultStatusRateLimitRequests = 60
	DefaultStatusRateLimitWindow   = time.Second
)

// RateLimitConfig configures a small per-address fixed-window limiter.
type RateLimitConfig struct {
	Requests int
	Window   time.Duration
}

// RateLimiter limits repeated requests from the same remote address.
type RateLimiter struct {
	mu       sync.Mutex
	requests int
	window   time.Duration
	entries  map[string]rateLimitEntry
	now      func() time.Time
}

type rateLimitEntry struct {
	count int
	reset time.Time
}

// NewRateLimiter creates a per-IP request limiter.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	requests := cfg.Requests
	if requests <= 0 {
		requests = DefaultStatusRateLimitRequests
	}
	window := cfg.Window
	if window <= 0 {
		window = DefaultStatusRateLimitWindow
	}
	return &RateLimiter{
		requests: requests,
		window:   window,
		entries:  make(map[string]rateLimitEntry),
		now:      time.Now,
	}
}

// Allow reports whether a request from remote should be served.
func (l *RateLimiter) Allow(remote net.Addr) bool {
	if l == nil {
		return true
	}
	key := rateLimitKey(remote)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.entries[key]
	if entry.reset.IsZero() || !now.Before(entry.reset) {
		entry = rateLimitEntry{reset: now.Add(l.window)}
	}
	if entry.count >= l.requests {
		l.entries[key] = entry
		return false
	}
	entry.count++
	l.entries[key] = entry

	if len(l.entries) > l.requests*32 {
		l.cleanup(now)
	}
	return true
}

func (l *RateLimiter) cleanup(now time.Time) {
	for key, entry := range l.entries {
		if !now.Before(entry.reset) {
			delete(l.entries, key)
		}
	}
}

func rateLimitKey(remote net.Addr) string {
	if remote == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err == nil {
		return host
	}
	return remote.String()
}
