package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"golang.org/x/net/proxy"
)

type ipGeoCacheEntry struct {
	geo       *identity.IPGeo
	expiresAt time.Time
}

type ipGeoResolver struct {
	mu             sync.Mutex
	entries        map[string]ipGeoCacheEntry
	client         *http.Client
	routeCooldowns map[string]time.Time
	cacheTTL       time.Duration
	timeout        time.Duration
	routes         []mojang.Route
	cursor         int
	now            func() time.Time
}

func newIPGeoResolver() *ipGeoResolver {
	return &ipGeoResolver{
		entries:        map[string]ipGeoCacheEntry{},
		client:         &http.Client{Timeout: 3 * time.Second},
		routeCooldowns: map[string]time.Time{},
		now:            time.Now,
	}
}

func (r *ipGeoResolver) lookup(ctx context.Context, ip string) *identity.IPGeo {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	if !publicIP(ip) {
		return localNetworkGeo(ip)
	}
	now := r.nowTime()
	r.mu.Lock()
	if entry, ok := r.entries[ip]; ok && now.Before(entry.expiresAt) {
		r.mu.Unlock()
		return cloneGeo(entry.geo)
	}
	r.mu.Unlock()

	geo := r.fetch(ctx, ip)
	cacheTTL := r.cacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 24 * time.Hour
	}
	r.mu.Lock()
	r.entries[ip] = ipGeoCacheEntry{geo: cloneGeo(geo), expiresAt: now.Add(cacheTTL)}
	r.mu.Unlock()
	return geo
}

func (r *ipGeoResolver) fetch(ctx context.Context, ip string) *identity.IPGeo {
	now := r.nowTime()
	routes := r.executionRoutes(now)
	if len(routes) == 0 {
		routes = []mojang.Route{{ID: "direct", Kind: mojang.RouteDirect, Weight: 1}}
	}
	var en, zh ipAPIResponse
	for _, route := range routes {
		client, err := r.clientForRoute(route)
		if err != nil {
			r.cooldownRoute(route, 30*time.Second, now)
			continue
		}
		en = r.fetchLocale(ctx, client, ip, "en")
		zh = r.fetchLocale(ctx, client, ip, "zh-CN")
		if en.status == "success" || zh.status == "success" {
			break
		}
		if cooldown := geoCooldownFor(en, zh); cooldown > 0 {
			r.cooldownRoute(route, cooldown, now)
		}
	}
	if en.status != "success" && zh.status != "success" {
		return nil
	}
	base := en
	if base.status != "success" {
		base = zh
	}
	geo := &identity.IPGeo{
		IP:          ip,
		CountryCode: strings.ToUpper(base.countryCode),
		ISP:         base.isp,
		ASN:         base.asn,
		Locales:     map[string]identity.IPGeoLocale{},
	}
	if en.status == "success" {
		geo.Locales["en"] = identity.IPGeoLocale{Country: en.country, Region: en.regionName, City: en.city}
	}
	if zh.status == "success" {
		geo.Locales["zh"] = identity.IPGeoLocale{Country: zh.country, Region: zh.regionName, City: zh.city}
	}
	return geo
}

func (r *ipGeoResolver) fetchLocale(ctx context.Context, client *http.Client, ip string, lang string) ipAPIResponse {
	u := url.URL{
		Scheme: "http",
		Host:   "ip-api.com",
		Path:   "/json/" + ip,
	}
	q := u.Query()
	q.Set("lang", lang)
	q.Set("fields", "status,message,query,country,countryCode,regionName,city,isp,as")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ipAPIResponse{status: "fail", message: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ipAPIResponse{status: "fail", message: err.Error(), routeError: true}
	}
	defer resp.Body.Close()
	retryAfter := geoRetryAfter(resp)
	if resp.StatusCode != http.StatusOK {
		return ipAPIResponse{status: "fail", message: fmt.Sprintf("http_%d", resp.StatusCode), retryAfter: retryAfter}
	}
	var out ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ipAPIResponse{status: "fail", message: err.Error(), routeError: true, retryAfter: retryAfter}
	}
	out.retryAfter = retryAfter
	return out
}

func (r *ipGeoResolver) configure(routes []mojang.Route, cacheTTL time.Duration, timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append([]mojang.Route(nil), routes...)
	r.cacheTTL = cacheTTL
	r.timeout = timeout
}

func (r *ipGeoResolver) executionRoutes(now time.Time) []mojang.Route {
	r.mu.Lock()
	defer r.mu.Unlock()
	routes := make([]mojang.Route, 0, len(r.routes))
	weighted := make([]mojang.Route, 0, len(r.routes))
	for _, route := range r.routes {
		if route.Disabled {
			continue
		}
		key := geoRouteKey(route)
		if until, ok := r.routeCooldowns[key]; ok {
			if until.After(now) {
				continue
			}
			delete(r.routeCooldowns, key)
		}
		weight := route.Weight
		if weight <= 0 {
			weight = 1
		}
		for i := 0; i < weight; i++ {
			weighted = append(weighted, route)
		}
	}
	if len(weighted) == 0 {
		return routes
	}
	start := r.cursor % len(weighted)
	r.cursor = (r.cursor + 1) % len(weighted)
	routes = append(routes, weighted[start:]...)
	routes = append(routes, weighted[:start]...)
	return routes
}

func (r *ipGeoResolver) clientForRoute(route mojang.Route) (*http.Client, error) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	transport := &http.Transport{}
	switch route.Kind {
	case mojang.RouteDirect, "":
	case mojang.RouteHTTP:
		proxyURL, err := url.Parse(route.URL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	case mojang.RouteSOCKS5:
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
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	default:
		return nil, fmt.Errorf("unsupported geo route kind %s", route.Kind)
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func (r *ipGeoResolver) nowTime() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

func (r *ipGeoResolver) cooldownRoute(route mojang.Route, cooldown time.Duration, now time.Time) {
	if cooldown <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasRouteFallbackLocked(route, now) {
		return
	}
	if r.routeCooldowns == nil {
		r.routeCooldowns = map[string]time.Time{}
	}
	r.routeCooldowns[geoRouteKey(route)] = now.Add(cooldown)
}

func (r *ipGeoResolver) hasRouteFallbackLocked(failed mojang.Route, now time.Time) bool {
	failedKey := geoRouteKey(failed)
	for _, route := range r.routes {
		if route.Disabled || geoRouteKey(route) == failedKey {
			continue
		}
		if until, ok := r.routeCooldowns[geoRouteKey(route)]; ok && until.After(now) {
			continue
		}
		return true
	}
	return false
}

func geoRouteKey(route mojang.Route) string {
	if strings.TrimSpace(route.ID) != "" {
		return strings.TrimSpace(route.ID)
	}
	return string(route.Kind) + "|" + strings.TrimSpace(route.URL)
}

func geoCooldownFor(responses ...ipAPIResponse) time.Duration {
	var cooldown time.Duration
	for _, response := range responses {
		if response.retryAfter > cooldown {
			cooldown = response.retryAfter
		}
		if response.routeError && cooldown == 0 {
			cooldown = 30 * time.Second
		}
	}
	return cooldown
}

func geoRetryAfter(resp *http.Response) time.Duration {
	remaining := strings.TrimSpace(resp.Header.Get("X-Rl"))
	if remaining != "0" {
		return 0
	}
	ttlSeconds, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Ttl")))
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	return time.Duration(ttlSeconds) * time.Second
}

type ipAPIResponse struct {
	status      string
	message     string
	query       string
	country     string
	countryCode string
	regionName  string
	city        string
	isp         string
	asn         string
	retryAfter  time.Duration
	routeError  bool
}

func (r *ipAPIResponse) UnmarshalJSON(raw []byte) error {
	var data struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		RegionName  string `json:"regionName"`
		City        string `json:"city"`
		ISP         string `json:"isp"`
		AS          string `json:"as"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	r.status = data.Status
	r.message = data.Message
	r.query = data.Query
	r.country = data.Country
	r.countryCode = data.CountryCode
	r.regionName = data.RegionName
	r.city = data.City
	r.isp = data.ISP
	r.asn = data.AS
	return nil
}

func publicIP(raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified())
}

func localNetworkGeo(ip string) *identity.IPGeo {
	return &identity.IPGeo{
		IP:          ip,
		CountryCode: "UN",
		Locales: map[string]identity.IPGeoLocale{
			"en": {Country: "Local network"},
			"zh": {Country: "局域网"},
		},
	}
}

func cloneGeo(geo *identity.IPGeo) *identity.IPGeo {
	if geo == nil {
		return nil
	}
	out := *geo
	out.Locales = map[string]identity.IPGeoLocale{}
	for key, value := range geo.Locales {
		out.Locales[key] = value
	}
	return &out
}
