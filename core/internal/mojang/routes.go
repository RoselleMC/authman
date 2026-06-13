package mojang

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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
	RouteDisabled    RouteState = "disabled"
)

type Route struct {
	ID               string
	Kind             RouteKind
	URL              string
	Weight           int
	Disabled         bool
	State            RouteState
	CooldownUntil    time.Time
	RequestCount     int
	FailureCount     int
	RateLimitCount   int
	LastFailureError string
}

type Event struct {
	ID         string
	ProxyID    string
	EventType  string
	RetryAfter time.Duration
	Error      string
	CreatedAt  time.Time
}

type Transport interface {
	Do(ctx context.Context, route Route) error
}

type Pool struct {
	Routes          []Route
	FailureCooldown time.Duration
	Now             func() time.Time
	cursor          int
	nextEventID     int
	events          []Event
}

func (p *Pool) Execute(ctx context.Context, transport Transport) (Route, error) {
	if p.Now == nil {
		p.Now = time.Now
	}
	now := p.Now().UTC()
	var lastErr error
	order := p.executionOrder()
	tried := make(map[int]bool, len(p.Routes))
	for _, i := range order {
		if tried[i] {
			continue
		}
		tried[i] = true
		route := p.Routes[i]
		if route.Disabled {
			p.Routes[i].State = RouteDisabled
			continue
		}
		if route.CooldownUntil.After(now) {
			continue
		}
		p.Routes[i].RequestCount++
		if err := transport.Do(ctx, route); err != nil {
			lastErr = err
			p.markFailure(i, err, now)
			continue
		}
		if p.Routes[i].State != "" && p.Routes[i].State != RouteHealthy {
			p.recordEvent(i, Event{EventType: "recovered", CreatedAt: now})
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

func (p *Pool) executionOrder() []int {
	if len(p.Routes) == 0 {
		return nil
	}
	weighted := make([]int, 0, len(p.Routes))
	for i, route := range p.Routes {
		weight := route.Weight
		if weight <= 0 {
			weight = 1
		}
		for j := 0; j < weight; j++ {
			weighted = append(weighted, i)
		}
	}
	if len(weighted) == 0 {
		return nil
	}
	start := p.cursor % len(weighted)
	p.cursor = (p.cursor + 1) % len(weighted)
	out := make([]int, 0, len(weighted))
	out = append(out, weighted[start:]...)
	out = append(out, weighted[:start]...)
	return out
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
	event := Event{
		EventType: "network_error",
		Error:     err.Error(),
		CreatedAt: now,
	}
	if errors.Is(err, ErrRateLimited) {
		event.EventType = "rate_limited"
	}
	var rateLimit RateLimitError
	if errors.As(err, &rateLimit) && rateLimit.RetryAfter > cooldown {
		cooldown = rateLimit.RetryAfter
		event.RetryAfter = rateLimit.RetryAfter
	}
	route.CooldownUntil = now.Add(cooldown)
	route.State = RouteCoolingDown
	p.recordEvent(index, event)
}

func (p *Pool) EventsSnapshot() []Event {
	out := make([]Event, len(p.events))
	copy(out, p.events)
	return out
}

func (p *Pool) recordEvent(index int, event Event) {
	if index < 0 || index >= len(p.Routes) {
		return
	}
	p.nextEventID++
	event.ID = "mojang-event-" + strconv.Itoa(p.nextEventID)
	event.ProxyID = p.Routes[index].ID
	if event.CreatedAt.IsZero() {
		if p.Now != nil {
			event.CreatedAt = p.Now().UTC()
		} else {
			event.CreatedAt = time.Now().UTC()
		}
	}
	p.events = append([]Event{event}, p.events...)
	if len(p.events) > 100 {
		p.events = p.events[:100]
	}
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
