package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoselleMC/authman/core/internal/config"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	ipsmodel "github.com/sjzar/ips/pkg/model"
)

func TestIPGeoResolverDoesNotCacheExternalFailure(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	var calls atomic.Int32
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		calls.Add(1)
		return ipGeoLookupResult{Provider: "ip-api.com"}
	}

	if geo := resolver.lookup(context.Background(), "8.8.8.8"); geo != nil {
		t.Fatalf("unexpected geo: %#v", geo)
	}
	if geo := resolver.lookup(context.Background(), "8.8.8.8"); geo != nil {
		t.Fatalf("unexpected geo: %#v", geo)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("failed lookup calls = %d, want 2", got)
	}
}

func TestIPGeoLookupAcceptsSyntacticallyValidNonPublicAddress(t *testing.T) {
	ip, valid := normalizeIPGeoLookupIP("192.0.2.44")
	if !valid || ip != "192.0.2.44" {
		t.Fatalf("normalizeIPGeoLookupIP() = %q, %t, want a valid non-public address", ip, valid)
	}

	resolver := newIPGeoResolver()
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		t.Fatal("non-public addresses must not call the external fallback")
		return ipGeoLookupResult{}
	}
	result := resolver.lookupDetailed(context.Background(), ip)
	if result.Provider != "local_network" || result.Geo == nil || result.Geo.CountryCode != "UN" {
		t.Fatalf("unexpected non-public lookup result: %#v", result)
	}
}

func TestIPGeoResolverFallsBackWhenEveryLocalDatabaseMisses(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.replaceReaders([]ipGeoDatabaseReader{{
		source: store.IPGeoSource{ID: "empty", Name: "Empty", DataFamily: "empty", Weight: 1},
		reader: fakeIPGeoReader("", ""),
	}})
	var calls atomic.Int32
	resolver.external = func(_ context.Context, ip string) ipGeoLookupResult {
		calls.Add(1)
		geo := testGeo(ip, "US", "United States")
		return ipGeoLookupResult{
			Geo:      geo,
			Provider: "ip-api.com",
			Evidence: []ipGeoLookupEvidence{{SourceID: "external:ip-api.com", Status: "fallback", Geo: cloneGeo(geo)}},
		}
	}

	result := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	if result.Provider != "ip-api.com" || result.Geo == nil || result.Geo.CountryCode != "US" {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("external fallback calls = %d, want 1", calls.Load())
	}
	if len(result.Evidence) != 2 || result.Evidence[0].Status != "no_match" || result.Evidence[1].Status != "fallback" {
		t.Fatalf("unexpected fallback evidence: %#v", result.Evidence)
	}
}

func TestIPGeoResolverCachesSuccessfulExternalLookup(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	var calls atomic.Int32
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		calls.Add(1)
		return ipGeoLookupResult{Provider: "ip-api.com", Geo: testGeo("8.8.8.8", "US", "United States")}
	}

	first := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	second := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	if first.Geo == nil || second.Geo == nil || !second.Cached {
		t.Fatalf("expected cached successful result, first=%#v second=%#v", first, second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("successful lookup calls = %d, want 1", got)
	}
}

func TestIPGeoResolverRefreshBypassesCacheAndReplacesOnlyOnSuccess(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	var calls atomic.Int32
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		switch calls.Add(1) {
		case 1:
			return ipGeoLookupResult{Provider: "ip-api.com", Geo: testGeo("8.8.8.8", "US", "United States")}
		case 2:
			return ipGeoLookupResult{Provider: "ip-api.com"}
		default:
			return ipGeoLookupResult{Provider: "ip-api.com", Geo: testGeo("8.8.8.8", "CA", "Canada")}
		}
	}

	initial := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	failed := resolver.refreshDetailed(context.Background(), "8.8.8.8")
	retained := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	replaced := resolver.refreshDetailed(context.Background(), "8.8.8.8")
	current := resolver.lookupDetailed(context.Background(), "8.8.8.8")

	if initial.Geo == nil || initial.Geo.CountryCode != "US" {
		t.Fatalf("unexpected initial result: %#v", initial)
	}
	if failed.Geo != nil {
		t.Fatalf("failed refresh unexpectedly returned geo: %#v", failed)
	}
	if retained.Geo == nil || retained.Geo.CountryCode != "US" || !retained.Cached {
		t.Fatalf("failed refresh replaced cached result: %#v", retained)
	}
	if replaced.Geo == nil || replaced.Geo.CountryCode != "CA" {
		t.Fatalf("successful refresh did not return replacement: %#v", replaced)
	}
	if current.Geo == nil || current.Geo.CountryCode != "CA" || !current.Cached {
		t.Fatalf("successful refresh was not cached: %#v", current)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("external calls = %d, want 3", got)
	}
}

func TestIPGeoResolverLocalOnlyDoesNotCallExternalFallback(t *testing.T) {
	resolver := newIPGeoResolver()
	var calls atomic.Int32
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		calls.Add(1)
		return ipGeoLookupResult{Geo: testGeo("8.8.8.8", "US", "United States")}
	}
	if geo := resolver.lookupLocalOnly("8.8.8.8"); geo != nil {
		t.Fatalf("unexpected geo without a local database: %#v", geo)
	}
	if calls.Load() != 0 {
		t.Fatal("critical-path local lookup called the external fallback")
	}
}

func TestIPGeoResolverBatchCachesSuccessAndRetriesFailure(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	var calls atomic.Int32
	requests := make([][]string, 0, 2)
	resolver.externalBatch = func(_ context.Context, ips []string) map[string]ipGeoLookupResult {
		calls.Add(1)
		requests = append(requests, append([]string(nil), ips...))
		return map[string]ipGeoLookupResult{
			"8.8.8.8": {Provider: "ip-api.com", Geo: testGeo("8.8.8.8", "US", "United States")},
		}
	}

	first := resolver.lookupBatch(context.Background(), []string{"8.8.8.8", "1.1.1.1", "8.8.8.8"})
	second := resolver.lookupBatch(context.Background(), []string{"8.8.8.8", "1.1.1.1"})
	if first["8.8.8.8"] == nil || second["8.8.8.8"] == nil {
		t.Fatal("successful batch result was not returned and cached")
	}
	if first["1.1.1.1"] != nil || second["1.1.1.1"] != nil {
		t.Fatal("failed batch result unexpectedly resolved")
	}
	if calls.Load() != 2 {
		t.Fatalf("batch calls = %d, want 2", calls.Load())
	}
	if len(requests) != 2 || len(requests[0]) != 2 || len(requests[1]) != 1 || requests[1][0] != "1.1.1.1" {
		t.Fatalf("unexpected batch requests: %#v", requests)
	}
}

func TestPublicIPRejectsDocumentationAndSharedRanges(t *testing.T) {
	for _, ip := range []string{"192.0.2.1", "198.51.100.10", "203.0.113.77", "100.64.0.1", "2001:db8::1"} {
		if publicIP(ip) {
			t.Fatalf("publicIP(%q) = true, want false", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(ip) {
			t.Fatalf("publicIP(%q) = false, want true", ip)
		}
	}
}

func TestIPGeoFormatInferenceAndArchiveSelection(t *testing.T) {
	if got := ipGeoFormatForFilename("CITY.MMDB"); got != "mmdb" {
		t.Fatalf("uppercase MMDB format = %q, want mmdb", got)
	}
	if archiveIPGeoEntryMatches("README.md", "mmdb") {
		t.Fatal("README was selected as an MMDB archive entry")
	}
	if archiveIPGeoEntryMatches("qqwry.dat", "mmdb") {
		t.Fatal("a mismatched database format was selected from the archive")
	}
	if !archiveIPGeoEntryMatches("nested/Country.MMDB", "mmdb") {
		t.Fatal("uppercase MMDB archive entry was not selected")
	}
}

func TestIPGeoSettingsKeepFallbackProviderAccurate(t *testing.T) {
	settings := normalizeIPGeoSettings(ipGeoSettingsRequest{Provider: "legacy-provider"})
	if settings.Provider != "ip-api.com" {
		t.Fatalf("provider = %q, want ip-api.com", settings.Provider)
	}
}

func TestIPGeoSourceDataUsesEmptyFieldArray(t *testing.T) {
	data := ipGeoSourceData(store.IPGeoSource{})
	fields, ok := data["fields"].([]string)
	if !ok || fields == nil || len(fields) != 0 {
		t.Fatalf("fields = %#v, want a non-nil empty array", data["fields"])
	}
}

func TestIPGeoRetryScheduleIsSoonerThanNormalUpdate(t *testing.T) {
	now := time.Now().UTC()
	source := store.IPGeoSource{Type: store.IPGeoSourceURL, AutoUpdate: true, UpdateIntervalHours: 24}
	initial := nextIPGeoRetry(source, now)
	if initial == nil || !initial.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("initial retry = %v, want five minutes", initial)
	}
	source.StorageFilename = "current.mmdb"
	ready := nextIPGeoRetry(source, now)
	if ready == nil || !ready.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("ready-source retry = %v, want thirty minutes", ready)
	}
}

func TestIPGeoResolverUsesLocalDatabaseBeforeExternalFallback(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	resolver.replaceReaders([]ipGeoDatabaseReader{{
		source: store.IPGeoSource{ID: "local", Name: "Local", DataFamily: "local", Weight: 1},
		reader: fakeIPGeoReader("US", "United States"),
	}})
	var calls atomic.Int32
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		calls.Add(1)
		return ipGeoLookupResult{Geo: testGeo("8.8.8.8", "ZZ", "Fallback")}
	}

	result := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	if result.Provider != "local_database" || result.Geo == nil || result.Geo.CountryCode != "US" {
		t.Fatalf("unexpected local result: %#v", result)
	}
	if calls.Load() != 0 {
		t.Fatalf("external fallback was called despite a local result")
	}
}

func TestIPGeoResolverCollapsesMirrorsInSameDataFamily(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	resolver.replaceReaders([]ipGeoDatabaseReader{
		{source: store.IPGeoSource{ID: "mirror-a", Name: "Mirror A", DataFamily: "same-upstream", Weight: 5}, reader: fakeIPGeoReader("AA", "Alpha")},
		{source: store.IPGeoSource{ID: "mirror-b", Name: "Mirror B", DataFamily: "same-upstream", Weight: 5}, reader: fakeIPGeoReader("AA", "Alpha")},
		{source: store.IPGeoSource{ID: "independent", Name: "Independent", DataFamily: "independent", Weight: 7}, reader: fakeIPGeoReader("BB", "Beta")},
	})
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		t.Fatal("external fallback should not be called")
		return ipGeoLookupResult{}
	}

	result := resolver.lookupDetailed(context.Background(), "8.8.8.8")
	if result.Geo == nil || result.Geo.CountryCode != "BB" {
		t.Fatalf("same-family mirrors inflated the vote: %#v", result.Geo)
	}
}

func TestIPGeoResolverFillsMissingCityFromAnotherLocalizedSource(t *testing.T) {
	resolver := newIPGeoResolver()
	resolver.configure(time.Hour, time.Second)
	resolver.replaceReaders([]ipGeoDatabaseReader{
		{
			source: store.IPGeoSource{ID: "regional", Name: "Regional", DataFamily: "regional", Weight: 5},
			reader: fakeMMDBIPGeoReader(map[string]string{
				"country":      `{"iso_code":"CN","names":{"en":"China","zh-CN":"中国"}}`,
				"subdivisions": `[{"iso_code":"ZJ","names":{"en":"Zhejiang","zh-CN":"浙江"}}]`,
			}),
		},
		{
			source: store.IPGeoSource{ID: "city", Name: "City", DataFamily: "city", Weight: 1},
			reader: fakeMMDBIPGeoReader(map[string]string{
				"country": `{"iso_code":"CN","names":{"en":"China","zh-CN":"中国"}}`,
				"city":    `{"names":{"en":"Hangzhou","zh-CN":"杭州"}}`,
			}),
		},
	})
	resolver.external = func(context.Context, string) ipGeoLookupResult {
		t.Fatal("external fallback should not be called when local sources can combine a location")
		return ipGeoLookupResult{}
	}

	result := resolver.lookupDetailed(context.Background(), "223.5.5.5")
	if result.Geo == nil {
		t.Fatal("combined local result is nil")
	}
	if got := result.Geo.Locales["en"]; got.Country != "China" || got.Region != "Zhejiang" || got.City != "Hangzhou" {
		t.Fatalf("unexpected English location: %#v", got)
	}
	if got := result.Geo.Locales["zh"]; got.Country != "中国" || got.Region != "浙江" || got.City != "杭州" {
		t.Fatalf("unexpected Chinese location: %#v", got)
	}
}

func TestIPGeoPlainReaderUsesSJZARGeoNamesTranslations(t *testing.T) {
	tests := []struct {
		name    string
		country string
		region  string
		city    string
	}{
		{name: "English database", country: "China", region: "Zhejiang", city: "Hangzhou"},
		{name: "Chinese database", country: "中国", region: "浙江", city: "杭州"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := candidateFromIPInfo("plain", &ipsmodel.IPInfo{Data: map[string]string{
				ipsmodel.Country:  test.country,
				ipsmodel.Province: test.region,
				ipsmodel.City:     test.city,
			}})
			if candidate.countryCode != "CN" {
				t.Fatalf("country code = %q, want CN", candidate.countryCode)
			}
			if candidate.countryEN != "China" || candidate.regionEN != "Zhejiang" || candidate.cityEN != "Hangzhou" {
				t.Fatalf("unexpected English translation: %#v", candidate)
			}
			if candidate.countryZH != "中国" || candidate.regionZH != "浙江" || candidate.cityZH != "杭州" {
				t.Fatalf("unexpected Chinese translation: %#v", candidate)
			}
		})
	}
}

func TestIPGeoFlatMMDBFieldsUseSJZARGeoNamesTranslations(t *testing.T) {
	candidate := candidateFromIPInfo("mmdb", &ipsmodel.IPInfo{Data: map[string]string{
		"country_name": `"China"`,
		"region_name":  `"Zhejiang"`,
		"city_name":    `"Hangzhou"`,
	}})
	if candidate.countryCode != "CN" {
		t.Fatalf("country code = %q, want CN", candidate.countryCode)
	}
	if candidate.countryEN != "China" || candidate.regionEN != "Zhejiang" || candidate.cityEN != "Hangzhou" {
		t.Fatalf("unexpected English translation: %#v", candidate)
	}
	if candidate.countryZH != "中国" || candidate.regionZH != "浙江" || candidate.cityZH != "杭州" {
		t.Fatalf("unexpected Chinese translation: %#v", candidate)
	}
}

func TestIPGeoMMDBReaderPreservesLocalizedFieldsAndTraits(t *testing.T) {
	dataDir := t.TempDir()
	filename := filepath.Join(dataDir, ".upload-without-extension")
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoIP2-City",
		IPVersion:    6,
		Languages:    []string{"en", "zh-CN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := mmdbtype.Map{
		"country": mmdbtype.Map{
			"geoname_id": mmdbtype.Uint32(6252001),
			"iso_code":   mmdbtype.String("US"),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("United States"),
			},
		},
		"subdivisions": mmdbtype.Slice{mmdbtype.Map{
			"geoname_id": mmdbtype.Uint32(5332921),
			"iso_code":   mmdbtype.String("CA"),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("California"),
			},
		}},
		"city": mmdbtype.Map{
			"geoname_id": mmdbtype.Uint32(5375480),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("Mountain View"),
			},
		},
		"traits": mmdbtype.Map{
			"autonomous_system_number":       mmdbtype.Uint32(15169),
			"autonomous_system_organization": mmdbtype.String("Google LLC"),
			"isp":                            mmdbtype.String("Google LLC"),
		},
	}
	_, network, err := net.ParseCIDR("8.8.8.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Insert(network, record); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tree.WriteTo(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	server := &Server{cfg: config.Config{IPGeoDataDir: dataDir}}
	installed, err := server.installIPGeoDatabase(store.IPGeoSource{ID: "uploaded"}, filename, "SMOKE.MMDB", "upload")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Format != "mmdb" {
		t.Fatalf("inferred format = %q, want mmdb", installed.Format)
	}
	database, err := openIPGeoDatabaseReader(installed, filepath.Join(dataDir, installed.StorageFilename))
	if err != nil {
		t.Fatal(err)
	}
	defer database.reader.Close()
	info, err := safeIPGeoFind(database.reader, net.ParseIP("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateFromIPInfo(database.reader.Meta().Format, info)
	if candidate.countryCode != "US" || candidate.countryEN != "United States" || candidate.countryZH != "美国" {
		t.Fatalf("unexpected country fields: %#v", candidate)
	}
	if candidate.regionEN != "California" || candidate.regionZH != "加州" || candidate.cityZH != "芒廷维尤" {
		t.Fatalf("unexpected localized location fields: %#v", candidate)
	}
	if candidate.asn != "AS15169" || candidate.isp != "Google LLC" {
		t.Fatalf("unexpected network fields: %#v", candidate)
	}
	missing, err := safeIPGeoFind(database.reader, net.ParseIP("9.9.9.9"))
	if err == nil && !candidateFromIPInfo(database.reader.Meta().Format, missing).empty() {
		t.Fatalf("unexpected candidate for an address outside the database: %#v", missing)
	}
}

type staticIPGeoReader struct {
	meta *ipsmodel.Meta
	info *ipsmodel.IPInfo
}

func fakeIPGeoReader(code string, country string) *staticIPGeoReader {
	return &staticIPGeoReader{
		meta: &ipsmodel.Meta{Format: "plain", IPVersion: ipsmodel.IPv4 | ipsmodel.IPv6, Fields: []string{"country", "country_code"}},
		info: &ipsmodel.IPInfo{Data: map[string]string{"country": country, "country_code": code}, Fields: []string{"country", "country_code"}},
	}
}

func fakeMMDBIPGeoReader(data map[string]string) *staticIPGeoReader {
	fields := make([]string, 0, len(data))
	for key := range data {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	return &staticIPGeoReader{
		meta: &ipsmodel.Meta{Format: "mmdb", IPVersion: ipsmodel.IPv4 | ipsmodel.IPv6, Fields: fields},
		info: &ipsmodel.IPInfo{Data: data, Fields: fields},
	}
}

func (reader *staticIPGeoReader) Meta() *ipsmodel.Meta { return reader.meta }

func (reader *staticIPGeoReader) Find(ip net.IP) (*ipsmodel.IPInfo, error) {
	copy := *reader.info
	copy.IP = append(net.IP(nil), ip...)
	copy.Data = map[string]string{}
	for key, value := range reader.info.Data {
		copy.Data[key] = value
	}
	return &copy, nil
}

func (reader *staticIPGeoReader) SetOption(any) error { return nil }
func (reader *staticIPGeoReader) Close() error        { return nil }

func testGeo(ip string, code string, country string) *identity.IPGeo {
	return &identity.IPGeo{
		IP:          ip,
		CountryCode: code,
		Locales: map[string]identity.IPGeoLocale{
			"en": {Country: country},
		},
	}
}
