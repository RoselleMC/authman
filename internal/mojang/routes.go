package mojang

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var (
	ErrRouteUnavailable = errors.New("mojang route unavailable")
	ErrAllRoutesFailed  = errors.New("all mojang routes failed")
	ErrRateLimited      = errors.New("mojang route rate limited")
)

type RouteKind string

const (
	RouteDirect RouteKind = "direct"
	RouteHTTP   RouteKind = "http"
	RouteSOCKS5 RouteKind = "socks5"
)

type RouteState string

const (
	RouteHealthy     RouteState = "healthy"
	RouteCoolingDown RouteState = "cooling_down"
	RouteFailed      RouteState = "failed"
)

type Route struct {
	ID               string
	Kind             RouteKind
	URL              string
	Weight           int
	State            RouteState
	CooldownUntil    time.Time
	FailureCount     int
	RateLimitCount   int
	LastFailureError string
}

type Transport interface {
	Do(ctx context.Context, route Route) error
}

type Pool struct {
	Routes          []Route
	FailureCooldown time.Duration
	Now             func() time.Time
}

func (p *Pool) Execute(ctx context.Context, transport Transport) (Route, error) {
	if p.Now == nil {
		p.Now = time.Now
	}
	now := p.Now().UTC()
	var lastErr error
	for i := range p.Routes {
		route := p.Routes[i]
		if route.CooldownUntil.After(now) {
			continue
		}
		if err := transport.Do(ctx, route); err != nil {
			lastErr = err
			p.markFailure(i, err, now)
			continue
		}
		p.Routes[i].State = RouteHealthy
		p.Routes[i].FailureCount = 0
		p.Routes[i].LastFailureError = ""
		return p.Routes[i], nil
	}
	if lastErr == nil {
		lastErr = ErrRouteUnavailable
	}
	return Route{}, errors.Join(ErrAllRoutesFailed, lastErr)
}

func (p *Pool) markFailure(index int, err error, now time.Time) {
	route := &p.Routes[index]
	route.FailureCount++
	route.LastFailureError = err.Error()
	route.State = RouteFailed
	cooldown := p.FailureCooldown
	if cooldown == 0 {
		cooldown = 30 * time.Second
	}
	if errors.Is(err, ErrRateLimited) {
		route.RateLimitCount++
		cooldown *= 2
	}
	var rateLimit RateLimitError
	if errors.As(err, &rateLimit) && rateLimit.RetryAfter > cooldown {
		cooldown = rateLimit.RetryAfter
	}
	route.CooldownUntil = now.Add(cooldown)
	route.State = RouteCoolingDown
}

func ErrorFromStatus(status int) error {
	if status == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if status >= 500 {
		return ErrRouteUnavailable
	}
	return nil
}

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string {
	return ErrRateLimited.Error()
}

func (e RateLimitError) Unwrap() error {
	return ErrRateLimited
}
