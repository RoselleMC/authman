package mojang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/internal/yggdrasil"
	"golang.org/x/net/proxy"
)

const DefaultSessionServerURL = "https://sessionserver.mojang.com"

type SessionVerifier struct {
	mu       sync.Mutex
	Pool     *Pool
	BaseURL  string
	Timeout  time.Duration
	Cache    *ProfileCache
	Now      func() time.Time
	ClientFn func(Route) (*http.Client, error)
}

func (v *SessionVerifier) HasJoined(ctx context.Context, request yggdrasil.HasJoinedRequest) (yggdrasil.Profile, error) {
	if v.Pool == nil || len(v.Pool.Routes) == 0 {
		return yggdrasil.Profile{}, yggdrasil.ErrProfileNotFound
	}
	key := CacheKey(request)
	base := strings.TrimRight(v.BaseURL, "/")
	if base == "" {
		base = DefaultSessionServerURL
	}
	timeout := v.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	cache := v.Cache
	if cache == nil {
		cache = NewProfileCache(30*time.Second, 5*time.Minute)
	}
	now := v.now()
	var profile yggdrasil.Profile
	var profileNotFound bool
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := v.Pool.Execute(ctx, routeTransportFunc(func(ctx context.Context, route Route) error {
		routeProfile, routeErr := v.fetch(ctx, route, base, timeout, request)
		if routeErr != nil {
			if errors.Is(routeErr, yggdrasil.ErrProfileNotFound) {
				profileNotFound = true
				return nil
			}
			return routeErr
		}
		profile = routeProfile
		cache.Put(key, routeProfile, now)
		return nil
	}))
	if err == nil {
		if profileNotFound {
			return yggdrasil.Profile{}, yggdrasil.ErrProfileNotFound
		}
		return profile, nil
	}
	if cached, ok := cache.Get(key, now); ok {
		return cached, nil
	}
	if errors.Is(err, ErrAllRoutesFailed) {
		return yggdrasil.Profile{}, fmt.Errorf("%w: %v", ErrAllRoutesFailed, err)
	}
	return yggdrasil.Profile{}, err
}

func (v *SessionVerifier) RoutesSnapshot() []Route {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Pool == nil {
		return nil
	}
	routes := make([]Route, len(v.Pool.Routes))
	copy(routes, v.Pool.Routes)
	return routes
}

func (v *SessionVerifier) EventsSnapshot() []Event {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Pool == nil {
		return nil
	}
	return v.Pool.EventsSnapshot()
}

func (v *SessionVerifier) SetRoutes(routes []Route, failureCooldown time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Pool == nil {
		v.Pool = &Pool{}
	}
	events := v.Pool.EventsSnapshot()
	nextEventID := v.Pool.nextEventID
	v.Pool.Routes = append([]Route(nil), routes...)
	v.Pool.FailureCooldown = failureCooldown
	v.Pool.events = events
	v.Pool.nextEventID = nextEventID
}

func (v *SessionVerifier) CacheSnapshot() map[string]int {
	if v.Cache == nil {
		return map[string]int{"fresh": 0, "stale": 0, "expired": 0}
	}
	return v.Cache.Snapshot(v.now())
}

type routeTransportFunc func(context.Context, Route) error

func (f routeTransportFunc) Do(ctx context.Context, route Route) error {
	return f(ctx, route)
}

func (v *SessionVerifier) fetch(ctx context.Context, route Route, baseURL string, timeout time.Duration, request yggdrasil.HasJoinedRequest) (yggdrasil.Profile, error) {
	client, err := v.client(route)
	if err != nil {
		return yggdrasil.Profile{}, err
	}
	client.Timeout = timeout
	endpoint, err := url.Parse(baseURL + "/session/minecraft/hasJoined")
	if err != nil {
		return yggdrasil.Profile{}, err
	}
	query := endpoint.Query()
	query.Set("username", request.Username)
	query.Set("serverId", request.ServerID)
	if request.IP != "" {
		query.Set("ip", request.IP)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return yggdrasil.Profile{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return yggdrasil.Profile{}, fmt.Errorf("%w: %v", ErrRouteUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return yggdrasil.Profile{}, yggdrasil.ErrProfileNotFound
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return yggdrasil.Profile{}, rateLimitFromResponse(resp)
	}
	if routeErr := ErrorFromStatus(resp.StatusCode); routeErr != nil {
		return yggdrasil.Profile{}, routeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return yggdrasil.Profile{}, yggdrasil.ErrProfileNotFound
	}
	var profile yggdrasil.Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return yggdrasil.Profile{}, fmt.Errorf("%w: malformed mojang profile", ErrRouteUnavailable)
	}
	if profile.ID == "" || profile.Name == "" {
		return yggdrasil.Profile{}, fmt.Errorf("%w: incomplete mojang profile", ErrRouteUnavailable)
	}
	return profile, nil
}

func (v *SessionVerifier) client(route Route) (*http.Client, error) {
	if v.ClientFn != nil {
		return v.ClientFn(route)
	}
	transport := &http.Transport{}
	switch route.Kind {
	case RouteDirect, "":
	case RouteHTTP:
		proxyURL, err := url.Parse(route.URL)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid http proxy", ErrRouteUnavailable)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	case RouteSOCKS5:
		address := route.URL
		var auth *proxy.Auth
		if parsed, err := url.Parse(route.URL); err == nil && parsed.Scheme != "" {
			address = parsed.Host
			if parsed.User != nil {
				auth = &proxy.Auth{User: parsed.User.Username()}
				auth.Password, _ = parsed.User.Password()
			}
		}
		dialer, err := proxy.SOCKS5("tcp", address, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid socks5 proxy", ErrRouteUnavailable)
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported route kind %s", ErrRouteUnavailable, route.Kind)
	}
	return &http.Client{Transport: transport}, nil
}

func (v *SessionVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func rateLimitFromResponse(resp *http.Response) error {
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now)
	if retryAfter <= 0 {
		return ErrRateLimited
	}
	return RateLimitError{RetryAfter: retryAfter}
}

func parseRetryAfter(raw string, now func() time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	duration := when.Sub(now().UTC())
	if duration < 0 {
		return 0
	}
	return duration
}

type ProfileCache struct {
	mu       sync.RWMutex
	freshTTL time.Duration
	staleTTL time.Duration
	entries  map[string]cacheEntry
}

type cacheEntry struct {
	Profile   yggdrasil.Profile
	StoredAt  time.Time
	ExpiresAt time.Time
	StaleAt   time.Time
}

func NewProfileCache(freshTTL time.Duration, staleTTL time.Duration) *ProfileCache {
	if freshTTL <= 0 {
		freshTTL = 30 * time.Second
	}
	if staleTTL < freshTTL {
		staleTTL = freshTTL
	}
	return &ProfileCache{
		freshTTL: freshTTL,
		staleTTL: staleTTL,
		entries:  make(map[string]cacheEntry),
	}
}

func CacheKey(request yggdrasil.HasJoinedRequest) string {
	return request.Username + "\x00" + request.ServerID + "\x00" + request.IP
}

func (c *ProfileCache) Put(key string, profile yggdrasil.Profile, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		Profile:   profile,
		StoredAt:  now.UTC(),
		ExpiresAt: now.UTC().Add(c.staleTTL),
		StaleAt:   now.UTC().Add(c.freshTTL),
	}
}

func (c *ProfileCache) Get(key string, now time.Time) (yggdrasil.Profile, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || now.UTC().After(entry.ExpiresAt) {
		return yggdrasil.Profile{}, false
	}
	return entry.Profile, true
}

func (c *ProfileCache) Snapshot(now time.Time) map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot := map[string]int{"fresh": 0, "stale": 0, "expired": 0}
	for _, entry := range c.entries {
		switch {
		case now.UTC().After(entry.ExpiresAt):
			snapshot["expired"]++
		case now.UTC().After(entry.StaleAt):
			snapshot["stale"]++
		default:
			snapshot["fresh"]++
		}
	}
	return snapshot
}
