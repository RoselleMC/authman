package limbgo

import (
	"net"
	"testing"
	"time"
)

func TestRateLimiterLimitsPerRemoteHost(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{Requests: 2, Window: time.Second})
	now := time.Unix(10, 0)
	limiter.now = func() time.Time { return now }

	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 25565}
	if !limiter.Allow(remote) {
		t.Fatalf("first request denied")
	}
	if !limiter.Allow(remote) {
		t.Fatalf("second request denied")
	}
	if limiter.Allow(remote) {
		t.Fatalf("third request allowed")
	}

	if !limiter.Allow(&net.TCPAddr{IP: net.ParseIP("192.0.2.11"), Port: 25565}) {
		t.Fatalf("different host denied")
	}

	now = now.Add(time.Second)
	if !limiter.Allow(remote) {
		t.Fatalf("request after window denied")
	}
}
