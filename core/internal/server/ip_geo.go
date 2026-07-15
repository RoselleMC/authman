package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
)

type ipGeoLookupEvidence struct {
	SourceID   string
	SourceName string
	DataFamily string
	Weight     int
	Status     string
	Geo        *identity.IPGeo
	Error      string
}

type ipGeoLookupResult struct {
	Geo      *identity.IPGeo
	Provider string
	Cached   bool
	Evidence []ipGeoLookupEvidence
}

type ipGeoCacheEntry struct {
	result    ipGeoLookupResult
	expiresAt time.Time
}

type ipGeoResolver struct {
	mu            sync.RWMutex
	entries       map[string]ipGeoCacheEntry
	readers       []ipGeoDatabaseReader
	cacheTTL      time.Duration
	timeout       time.Duration
	now           func() time.Time
	external      func(context.Context, string) ipGeoLookupResult
	externalBatch func(context.Context, []string) map[string]ipGeoLookupResult
}

func newIPGeoResolver() *ipGeoResolver {
	return &ipGeoResolver{
		entries: map[string]ipGeoCacheEntry{},
		now:     time.Now,
	}
}

func (r *ipGeoResolver) lookup(ctx context.Context, ip string) *identity.IPGeo {
	return r.lookupDetailed(ctx, ip).Geo
}

func (r *ipGeoResolver) lookupLocalOnly(ip string) *identity.IPGeo {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	if !publicIP(ip) {
		return localNetworkGeo(ip)
	}

	now := r.nowTime()
	r.mu.RLock()
	entry, cached := r.entries[ip]
	r.mu.RUnlock()
	if cached && now.Before(entry.expiresAt) {
		return cloneGeo(entry.result.Geo)
	}
	result := r.lookupLocal(parsed)
	if result.Geo == nil {
		return nil
	}
	r.cacheResult(ip, result, now)
	return cloneGeo(result.Geo)
}

func (r *ipGeoResolver) lookupBatch(ctx context.Context, ips []string) map[string]*identity.IPGeo {
	resolved := make(map[string]*identity.IPGeo, len(ips))
	unresolved := make([]string, 0, len(ips))
	seen := make(map[string]struct{}, len(ips))
	for _, raw := range ips {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		if geo := r.lookupLocalOnly(ip); geo != nil {
			resolved[ip] = geo
			continue
		}
		if publicIP(ip) {
			unresolved = append(unresolved, ip)
		}
	}

	for start := 0; start < len(unresolved); start += 100 {
		end := min(start+100, len(unresolved))
		batchIPs := unresolved[start:end]
		var batch map[string]ipGeoLookupResult
		switch {
		case r.externalBatch != nil:
			batch = r.externalBatch(ctx, batchIPs)
		case r.external != nil:
			batch = make(map[string]ipGeoLookupResult, len(batchIPs))
			for _, ip := range batchIPs {
				batch[ip] = r.external(ctx, ip)
			}
		default:
			batch = r.fetchExternalBatch(ctx, batchIPs)
		}
		now := r.nowTime()
		for _, ip := range batchIPs {
			result := batch[ip]
			if result.Geo == nil {
				continue
			}
			r.cacheResult(ip, result, now)
			resolved[ip] = cloneGeo(result.Geo)
		}
	}
	return resolved
}

func (r *ipGeoResolver) lookupDetailed(ctx context.Context, ip string) ipGeoLookupResult {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ipGeoLookupResult{}
	}
	if !publicIP(ip) {
		return ipGeoLookupResult{Geo: localNetworkGeo(ip), Provider: "local_network"}
	}

	now := r.nowTime()
	r.mu.RLock()
	entry, cached := r.entries[ip]
	r.mu.RUnlock()
	if cached && now.Before(entry.expiresAt) {
		result := cloneIPGeoLookupResult(entry.result)
		result.Cached = true
		return result
	}

	result := r.resolveFresh(ctx, ip, parsed)
	if result.Geo == nil {
		// Failures deliberately remain uncached so the next request retries.
		return result
	}

	r.cacheResult(ip, result, now)
	return result
}

// refreshDetailed deliberately bypasses the resolver cache. A failed refresh
// leaves the last successful cached value intact.
func (r *ipGeoResolver) refreshDetailed(ctx context.Context, ip string) ipGeoLookupResult {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ipGeoLookupResult{}
	}
	if !publicIP(ip) {
		return ipGeoLookupResult{Geo: localNetworkGeo(ip), Provider: "local_network"}
	}

	result := r.resolveFresh(ctx, ip, parsed)
	if result.Geo != nil {
		r.cacheResult(ip, result, r.nowTime())
	}
	return result
}

func (r *ipGeoResolver) resolveFresh(ctx context.Context, ip string, parsed net.IP) ipGeoLookupResult {
	result := r.lookupLocal(parsed)
	if result.Geo == nil {
		fetchExternal := r.fetchExternal
		if r.external != nil {
			fetchExternal = r.external
		}
		fallback := fetchExternal(ctx, ip)
		fallback.Evidence = append(result.Evidence, fallback.Evidence...)
		result = fallback
	}
	return result
}

func (r *ipGeoResolver) cacheResult(ip string, result ipGeoLookupResult, now time.Time) {
	r.mu.RLock()
	cacheTTL := r.cacheTTL
	r.mu.RUnlock()
	if cacheTTL <= 0 {
		cacheTTL = 24 * time.Hour
	}
	r.mu.Lock()
	r.entries[ip] = ipGeoCacheEntry{result: cloneIPGeoLookupResult(result), expiresAt: now.Add(cacheTTL)}
	r.mu.Unlock()
}

func (r *ipGeoResolver) fetchExternal(ctx context.Context, ip string) ipGeoLookupResult {
	client := r.externalClient()
	type localizedResponse struct {
		locale string
		value  ipAPIResponse
	}
	responses := make(chan localizedResponse, 2)
	for _, locale := range []string{"en", "zh-CN"} {
		go func(locale string) {
			responses <- localizedResponse{locale: locale, value: fetchIPAPILocale(ctx, client, ip, locale)}
		}(locale)
	}
	var en, zh ipAPIResponse
	for range 2 {
		response := <-responses
		if response.locale == "zh-CN" {
			zh = response.value
		} else {
			en = response.value
		}
	}
	return ipAPIResult(ip, en, zh)
}

func (r *ipGeoResolver) fetchExternalBatch(ctx context.Context, ips []string) map[string]ipGeoLookupResult {
	results := make(map[string]ipGeoLookupResult, len(ips))
	if len(ips) == 0 {
		return results
	}
	type localizedBatch struct {
		locale string
		values map[string]ipAPIResponse
		err    error
	}
	responses := make(chan localizedBatch, 2)
	client := r.externalClient()
	for _, locale := range []string{"en", "zh-CN"} {
		go func(locale string) {
			values, err := fetchIPAPIBatchLocale(ctx, client, ips, locale)
			responses <- localizedBatch{locale: locale, values: values, err: err}
		}(locale)
	}
	var enValues, zhValues map[string]ipAPIResponse
	var enErr, zhErr error
	for range 2 {
		response := <-responses
		if response.locale == "zh-CN" {
			zhValues, zhErr = response.values, response.err
		} else {
			enValues, enErr = response.values, response.err
		}
	}
	for _, ip := range ips {
		en := enValues[ip]
		zh := zhValues[ip]
		if en.status == "" && enErr != nil {
			en = ipAPIResponse{status: "fail", message: enErr.Error()}
		}
		if zh.status == "" && zhErr != nil {
			zh = ipAPIResponse{status: "fail", message: zhErr.Error()}
		}
		results[ip] = ipAPIResult(ip, en, zh)
	}
	return results
}

func (r *ipGeoResolver) externalClient() *http.Client {
	r.mu.RLock()
	timeout := r.timeout
	r.mu.RUnlock()
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: transport}
}

func ipAPIResult(ip string, en ipAPIResponse, zh ipAPIResponse) ipGeoLookupResult {
	if en.status != "success" && zh.status != "success" {
		reason := strings.TrimSpace(en.message)
		if reason == "" {
			reason = strings.TrimSpace(zh.message)
		}
		return ipGeoLookupResult{
			Provider: "ip-api.com",
			Evidence: []ipGeoLookupEvidence{{SourceID: "external:ip-api.com", SourceName: "ip-api.com", Status: "error", Error: reason}},
		}
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
	return ipGeoLookupResult{
		Geo:      geo,
		Provider: "ip-api.com",
		Evidence: []ipGeoLookupEvidence{{SourceID: "external:ip-api.com", SourceName: "ip-api.com", Status: "fallback", Geo: cloneGeo(geo)}},
	}
}

func fetchIPAPIBatchLocale(ctx context.Context, client *http.Client, ips []string, lang string) (map[string]ipAPIResponse, error) {
	payload, err := json.Marshal(ips)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "http", Host: "ip-api.com", Path: "/batch"}
	q := u.Query()
	q.Set("lang", lang)
	q.Set("fields", "status,message,query,country,countryCode,regionName,city,isp,as")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Authman-IPGeo/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http_%d", resp.StatusCode)
	}
	var response []ipAPIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&response); err != nil {
		return nil, err
	}
	values := make(map[string]ipAPIResponse, len(response))
	for _, item := range response {
		if ip := strings.TrimSpace(item.query); ip != "" {
			values[ip] = item
		}
	}
	return values, nil
}

func fetchIPAPILocale(ctx context.Context, client *http.Client, ip string, lang string) ipAPIResponse {
	u := url.URL{Scheme: "http", Host: "ip-api.com", Path: "/json/" + ip}
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
		return ipAPIResponse{status: "fail", message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ipAPIResponse{status: "fail", message: fmt.Sprintf("http_%d", resp.StatusCode)}
	}
	var out ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ipAPIResponse{status: "fail", message: err.Error()}
	}
	return out
}

func (r *ipGeoResolver) configure(cacheTTL time.Duration, timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheTTL = cacheTTL
	r.timeout = timeout
}

func (r *ipGeoResolver) replaceReaders(readers []ipGeoDatabaseReader) {
	r.mu.Lock()
	old := r.readers
	r.readers = readers
	r.entries = map[string]ipGeoCacheEntry{}
	for _, reader := range old {
		_ = reader.reader.Close()
	}
	r.mu.Unlock()
}

func (r *ipGeoResolver) clearCache() {
	r.mu.Lock()
	r.entries = map[string]ipGeoCacheEntry{}
	r.mu.Unlock()
}

func (r *ipGeoResolver) nowTime() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
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

var nonPublicIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("3fff::/20"),
}

func publicIP(raw string) bool {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
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

func cloneIPGeoLookupResult(result ipGeoLookupResult) ipGeoLookupResult {
	result.Geo = cloneGeo(result.Geo)
	result.Evidence = append([]ipGeoLookupEvidence(nil), result.Evidence...)
	for index := range result.Evidence {
		result.Evidence[index].Geo = cloneGeo(result.Evidence[index].Geo)
	}
	return result
}
