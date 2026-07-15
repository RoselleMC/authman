package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/sjzar/ips/format"
	"github.com/sjzar/ips/format/geo"
	"github.com/sjzar/ips/format/mmdb"
	ipsmodel "github.com/sjzar/ips/pkg/model"
)

var (
	ipGeoNameCatalogOnce sync.Once
	ipGeoNameCatalogs    map[string][]map[string]string
)

type ipGeoDatabaseReader struct {
	source store.IPGeoSource
	reader format.Reader
}

type ipGeoCandidate struct {
	countryCode string
	countryEN   string
	countryZH   string
	regionEN    string
	regionZH    string
	cityEN      string
	cityZH      string
	isp         string
	asn         string
}

func openIPGeoDatabaseReader(source store.IPGeoSource, filename string) (database ipGeoDatabaseReader, err error) {
	var reader format.Reader
	defer func() {
		if recovered := recover(); recovered != nil {
			if reader != nil {
				_ = reader.Close()
			}
			database = ipGeoDatabaseReader{}
			err = fmt.Errorf("database reader initialization failed: %v", recovered)
		}
	}()
	databaseFormat := strings.ToLower(strings.TrimSpace(source.Format))
	if databaseFormat == "" {
		databaseFormat = ipGeoFormatForFilename(filename)
	}
	reader, err = format.NewReader(databaseFormat, filename)
	if err != nil {
		return ipGeoDatabaseReader{}, err
	}
	if reader == nil || reader.Meta() == nil {
		if reader != nil {
			_ = reader.Close()
		}
		return ipGeoDatabaseReader{}, fmt.Errorf("database reader returned no metadata")
	}
	if strings.EqualFold(reader.Meta().Format, "mmdb") {
		if err := reader.SetOption(mmdb.ReaderOption{UseFullField: true}); err != nil {
			_ = reader.Close()
			return ipGeoDatabaseReader{}, err
		}
	}
	return ipGeoDatabaseReader{source: source, reader: reader}, nil
}

func (r *ipGeoResolver) lookupLocal(ip net.IP) ipGeoLookupResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.readers) == 0 {
		return ipGeoLookupResult{}
	}

	type resolvedCandidate struct {
		source    store.IPGeoSource
		candidate ipGeoCandidate
		order     int
	}
	resolved := make([]resolvedCandidate, 0, len(r.readers))
	evidence := make([]ipGeoLookupEvidence, 0, len(r.readers))
	for index, database := range r.readers {
		item := ipGeoLookupEvidence{
			SourceID:   database.source.ID,
			SourceName: database.source.Name,
			DataFamily: sourceDataFamily(database.source),
			Weight:     database.source.Weight,
		}
		info, err := safeIPGeoFind(database.reader, ip)
		if err != nil {
			item.Status = "no_match"
			item.Error = err.Error()
			evidence = append(evidence, item)
			continue
		}
		candidate := candidateFromIPInfo(database.reader.Meta().Format, info)
		if candidate.empty() {
			item.Status = "no_match"
			evidence = append(evidence, item)
			continue
		}
		item.Status = "hit"
		item.Geo = candidate.geo(ip.String())
		evidence = append(evidence, item)
		resolved = append(resolved, resolvedCandidate{source: database.source, candidate: candidate, order: index})
	}

	if len(resolved) == 0 {
		return ipGeoLookupResult{Evidence: evidence}
	}
	vote := func(value func(ipGeoCandidate) string) string {
		type familyValue struct {
			value  string
			weight int
			order  int
		}
		families := map[string]familyValue{}
		for _, item := range resolved {
			text := strings.TrimSpace(value(item.candidate))
			if text == "" {
				continue
			}
			family := sourceDataFamily(item.source)
			weight := item.source.Weight
			if weight <= 0 {
				weight = 1
			}
			current, ok := families[family]
			if !ok || weight > current.weight || (weight == current.weight && item.order < current.order) {
				families[family] = familyValue{value: text, weight: weight, order: item.order}
			}
		}
		type totalValue struct {
			value     string
			total     int
			maxWeight int
			order     int
		}
		totals := map[string]totalValue{}
		for _, item := range families {
			key := strings.ToLower(strings.TrimSpace(item.value))
			current, ok := totals[key]
			if !ok {
				current = totalValue{value: item.value, order: item.order}
			}
			current.total += item.weight
			if item.weight > current.maxWeight || (item.weight == current.maxWeight && item.order < current.order) {
				current.value = item.value
				current.maxWeight = item.weight
				current.order = item.order
			}
			totals[key] = current
		}
		var winner totalValue
		found := false
		for _, item := range totals {
			if !found || item.total > winner.total ||
				(item.total == winner.total && item.maxWeight > winner.maxWeight) ||
				(item.total == winner.total && item.maxWeight == winner.maxWeight && item.order < winner.order) {
				winner = item
				found = true
			}
		}
		return winner.value
	}

	candidate := ipGeoCandidate{
		countryCode: strings.ToUpper(vote(func(value ipGeoCandidate) string { return value.countryCode })),
		countryEN:   vote(func(value ipGeoCandidate) string { return value.countryEN }),
		countryZH:   vote(func(value ipGeoCandidate) string { return value.countryZH }),
		regionEN:    vote(func(value ipGeoCandidate) string { return value.regionEN }),
		regionZH:    vote(func(value ipGeoCandidate) string { return value.regionZH }),
		cityEN:      vote(func(value ipGeoCandidate) string { return value.cityEN }),
		cityZH:      vote(func(value ipGeoCandidate) string { return value.cityZH }),
		isp:         vote(func(value ipGeoCandidate) string { return value.isp }),
		asn:         vote(func(value ipGeoCandidate) string { return value.asn }),
	}
	if !candidate.hasLocation() {
		return ipGeoLookupResult{Evidence: evidence}
	}
	return ipGeoLookupResult{Geo: candidate.geo(ip.String()), Provider: "local_database", Evidence: evidence}
}

func safeIPGeoFind(reader format.Reader, ip net.IP) (info *ipsmodel.IPInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			info = nil
			err = fmt.Errorf("database lookup failed: %v", recovered)
		}
	}()
	return reader.Find(ip)
}

func sourceDataFamily(source store.IPGeoSource) string {
	if family := strings.TrimSpace(source.DataFamily); family != "" {
		return strings.ToLower(family)
	}
	return "source:" + strings.ToLower(strings.TrimSpace(source.ID))
}

func candidateFromIPInfo(databaseFormat string, info *ipsmodel.IPInfo) ipGeoCandidate {
	if info == nil {
		return ipGeoCandidate{}
	}
	if strings.EqualFold(databaseFormat, "mmdb") {
		return candidateFromMMDB(info)
	}
	country := infoValue(info, ipsmodel.Country)
	region := infoValue(info, ipsmodel.Province)
	city := infoValue(info, ipsmodel.City)
	isp := infoValue(info, ipsmodel.ISP)
	asn := normalizeASN(infoValue(info, ipsmodel.ASN))
	countryNames := localizeIPGeoName(ipsmodel.Country, country)
	regionNames := localizeIPGeoName(ipsmodel.Province, region)
	cityNames := localizeIPGeoName(ipsmodel.City, city)
	return ipGeoCandidate{
		countryCode: firstNonEmpty(firstInfoValue(info, "country_code", "countryCode", "iso_code"), countryNames.code),
		countryEN:   countryNames.en,
		countryZH:   countryNames.zh,
		regionEN:    regionNames.en,
		regionZH:    regionNames.zh,
		cityEN:      cityNames.en,
		cityZH:      cityNames.zh,
		isp:         isp,
		asn:         asn,
	}
}

func candidateFromMMDB(info *ipsmodel.IPInfo) ipGeoCandidate {
	country := mmdbNamedRecord(info.Data, "country", ipsmodel.Country)
	if country.empty() {
		country = mmdbNamedRecord(info.Data, "registered_country", ipsmodel.Country)
	}
	region := mmdbSubdivision(info.Data["subdivisions"])
	city := mmdbNamedRecord(info.Data, "city", ipsmodel.City)
	countryFallback := localizeIPGeoName(ipsmodel.Country, firstNonEmpty(
		mmdbScalar(info.Data, "country_name"),
		mmdbScalar(info.Data, "country"),
	))
	regionFallback := localizeIPGeoName(ipsmodel.Province, firstNonEmpty(
		mmdbScalar(info.Data, "region_name"),
		mmdbScalar(info.Data, "region"),
		mmdbScalar(info.Data, "province"),
	))
	cityFallback := localizeIPGeoName(ipsmodel.City, firstNonEmpty(
		mmdbScalar(info.Data, "city_name"),
		mmdbScalar(info.Data, "city"),
	))
	countryCode := firstNonEmpty(
		country.code,
		mmdbScalar(info.Data, "country_code"),
		mmdbScalar(info.Data, "country_iso_code"),
		mmdbScalar(info.Data, "iso_code"),
		countryFallback.code,
	)
	countryEN := firstNonEmpty(country.en, countryFallback.en)
	countryZH := firstNonEmpty(country.zh, countryFallback.zh, countryEN)
	regionEN := firstNonEmpty(region.en, regionFallback.en)
	regionZH := firstNonEmpty(region.zh, regionFallback.zh, regionEN)
	cityEN := firstNonEmpty(city.en, cityFallback.en)
	cityZH := firstNonEmpty(city.zh, cityFallback.zh, cityEN)
	asn := normalizeASN(firstNonEmpty(
		mmdbNestedScalar(info.Data, "traits", "autonomous_system_number"),
		mmdbScalar(info.Data, "autonomous_system_number"),
		mmdbScalar(info.Data, "traits_autonomous_system_number"),
		mmdbScalar(info.Data, "asn"),
	))
	organization := firstNonEmpty(
		mmdbNestedScalar(info.Data, "traits", "autonomous_system_organization"),
		mmdbScalar(info.Data, "autonomous_system_organization"),
		mmdbScalar(info.Data, "traits_autonomous_system_organization"),
		mmdbScalar(info.Data, "as_name"),
		mmdbScalar(info.Data, "organization"),
	)
	isp := firstNonEmpty(
		mmdbNestedScalar(info.Data, "traits", "isp"),
		mmdbScalar(info.Data, "isp"),
		mmdbScalar(info.Data, "traits_isp"),
		organization,
	)
	return ipGeoCandidate{
		countryCode: strings.ToUpper(countryCode),
		countryEN:   countryEN,
		countryZH:   countryZH,
		regionEN:    regionEN,
		regionZH:    regionZH,
		cityEN:      cityEN,
		cityZH:      cityZH,
		isp:         isp,
		asn:         asn,
	}
}

type mmdbName struct {
	code string
	en   string
	zh   string
}

func (n mmdbName) empty() bool {
	return n.code == "" && n.en == "" && n.zh == ""
}

type localizedIPGeoName struct {
	code string
	en   string
	zh   string
}

func initIPGeoNameCatalogs() {
	ipGeoNameCatalogs = make(map[string][]map[string]string, 3)
	for _, field := range []string{ipsmodel.Country, ipsmodel.Province, ipsmodel.City} {
		catalogs := make([]map[string]string, 0, len(geo.SupportedLanguages))
		for _, language := range geo.SupportedLanguages {
			catalogs = append(catalogs, geo.GetNameInfos(field, language))
		}
		ipGeoNameCatalogs[field] = catalogs
	}
	// Prime sjzar/ips' GeoName ID index before concurrent MMDB lookups begin.
	_, _ = geo.GetInfoByID(0)
}

func findIPGeoName(field string, name string) (*geo.Info, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	ipGeoNameCatalogOnce.Do(initIPGeoNameCatalogs)
	for _, catalog := range ipGeoNameCatalogs[field] {
		if raw, ok := catalog[name]; ok {
			return geo.ParseGeoInfo(raw)
		}
	}
	return nil, false
}

func localizedIPGeoInfo(field string, info *geo.Info, fallback string) localizedIPGeoName {
	fallback = strings.TrimSpace(fallback)
	if info == nil {
		return localizedIPGeoName{en: fallback, zh: fallback}
	}
	result := localizedIPGeoName{
		code: strings.TrimSpace(info.IsoCode),
		en:   strings.TrimSpace(info.Names[geo.LangEnglish]),
		zh:   strings.TrimSpace(info.Names[geo.LangChinese]),
	}
	if result.en == "" || result.zh == "" {
		for _, language := range geo.SupportedLanguages {
			name := strings.TrimSpace(info.Names[language])
			translated, ok := findIPGeoName(field, name)
			if !ok {
				continue
			}
			if result.en == "" {
				result.en = strings.TrimSpace(translated.Name(geo.LangEnglish))
			}
			if result.zh == "" {
				result.zh = strings.TrimSpace(translated.Name(geo.LangChinese))
			}
			if result.code == "" {
				result.code = strings.TrimSpace(translated.IsoCode)
			}
			if result.en != "" && result.zh != "" {
				break
			}
		}
	}
	result.en = firstNonEmpty(result.en, info.Name(geo.LangEnglish), fallback)
	result.zh = firstNonEmpty(result.zh, info.Name(geo.LangChinese), fallback)
	return result
}

func localizeIPGeoName(field string, name string) localizedIPGeoName {
	name = strings.TrimSpace(name)
	info, ok := findIPGeoName(field, name)
	if !ok {
		return localizedIPGeoName{en: name, zh: name}
	}
	return localizedIPGeoInfo(field, info, name)
}

func parseMMDBGeoInfo(raw string) (*geo.Info, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var record map[string]any
	if err := decoder.Decode(&record); err != nil {
		return nil, false
	}
	if number, ok := record["geoname_id"].(json.Number); ok {
		if value, err := strconv.Atoi(number.String()); err == nil {
			record["geoname_id"] = value
		}
	}
	ipGeoNameCatalogOnce.Do(initIPGeoNameCatalogs)
	return geo.ParseInfoFromMMDB(record, false)
}

func mmdbNamedRecord(data map[string]string, key string, field string) mmdbName {
	raw := strings.TrimSpace(data[key])
	if raw == "" {
		return mmdbName{}
	}
	info, ok := parseMMDBGeoInfo(raw)
	if !ok {
		localized := localizeIPGeoName(field, scalarString(raw))
		return mmdbName{code: localized.code, en: localized.en, zh: localized.zh}
	}
	localized := localizedIPGeoInfo(field, info, scalarString(raw))
	return mmdbName{code: localized.code, en: localized.en, zh: localized.zh}
}

func mmdbSubdivision(raw string) mmdbName {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mmdbName{}
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var records []map[string]any
	if decoder.Decode(&records) == nil && len(records) > 0 {
		if number, ok := records[0]["geoname_id"].(json.Number); ok {
			if value, err := strconv.Atoi(number.String()); err == nil {
				records[0]["geoname_id"] = value
			}
		}
		ipGeoNameCatalogOnce.Do(initIPGeoNameCatalogs)
		if info, ok := geo.ParseInfoFromMMDB(records[0], false); ok {
			localized := localizedIPGeoInfo(ipsmodel.Province, info, "")
			return mmdbName{code: localized.code, en: localized.en, zh: localized.zh}
		}
	}
	localized := localizeIPGeoName(ipsmodel.Province, scalarString(raw))
	return mmdbName{code: localized.code, en: localized.en, zh: localized.zh}
}

func mmdbScalar(data map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := scalarString(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func mmdbNestedScalar(data map[string]string, recordKey string, keys ...string) string {
	raw := strings.TrimSpace(data[recordKey])
	if raw == "" {
		return ""
	}
	var record map[string]any
	if json.Unmarshal([]byte(raw), &record) != nil {
		return ""
	}
	for _, key := range keys {
		if value := anyString(record[key]); value != "" {
			return value
		}
	}
	return ""
}

func scalarString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" || raw == "[]" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return anyString(value)
	}
	return raw
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

func infoValue(info *ipsmodel.IPInfo, key string) string {
	value, _ := info.GetData(key)
	return strings.TrimSpace(value)
}

func firstInfoValue(info *ipsmodel.IPInfo, keys ...string) string {
	for _, key := range keys {
		if value := infoValue(info, key); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func normalizeASN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, "AS") {
		return "AS" + strings.TrimSpace(value[2:])
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return "AS" + value
	}
	return value
}

func (candidate ipGeoCandidate) empty() bool {
	return candidate.countryCode == "" && candidate.countryEN == "" && candidate.countryZH == "" &&
		candidate.regionEN == "" && candidate.regionZH == "" && candidate.cityEN == "" &&
		candidate.cityZH == "" && candidate.isp == "" && candidate.asn == ""
}

func (candidate ipGeoCandidate) hasLocation() bool {
	return candidate.countryCode != "" || candidate.countryEN != "" || candidate.countryZH != "" ||
		candidate.regionEN != "" || candidate.regionZH != "" || candidate.cityEN != "" || candidate.cityZH != ""
}

func (candidate ipGeoCandidate) geo(ip string) *identity.IPGeo {
	locales := map[string]identity.IPGeoLocale{}
	if candidate.countryEN != "" || candidate.regionEN != "" || candidate.cityEN != "" {
		locales["en"] = identity.IPGeoLocale{Country: candidate.countryEN, Region: candidate.regionEN, City: candidate.cityEN}
	}
	if candidate.countryZH != "" || candidate.regionZH != "" || candidate.cityZH != "" {
		locales["zh"] = identity.IPGeoLocale{Country: candidate.countryZH, Region: candidate.regionZH, City: candidate.cityZH}
	}
	return &identity.IPGeo{IP: ip, CountryCode: strings.ToUpper(candidate.countryCode), ISP: candidate.isp, ASN: candidate.asn, Locales: locales}
}

func (s *Server) reloadIPGeoSources(ctx context.Context) {
	sources := s.store.ListIPGeoSources(ctx)
	readers := make([]ipGeoDatabaseReader, 0, len(sources))
	for _, source := range sources {
		hasUsableStatus := source.Status == store.IPGeoSourceReady || source.Status == store.IPGeoSourceUpdating
		if !source.Enabled || !hasUsableStatus || strings.TrimSpace(source.StorageFilename) == "" {
			continue
		}
		filename := filepath.Join(s.cfg.IPGeoDataDir, filepath.Base(source.StorageFilename))
		reader, err := openIPGeoDatabaseReader(source, filename)
		if err != nil {
			s.logger.Warn("failed to open IP geolocation database", "source_id", source.ID, "file", filename, "error", err)
			continue
		}
		readers = append(readers, reader)
	}
	s.ipGeo.replaceReaders(readers)
}

func validateIPGeoDatabase(source store.IPGeoSource, filename string) (store.IPGeoSource, error) {
	reader, err := openIPGeoDatabaseReader(source, filename)
	if err != nil {
		return source, err
	}
	defer reader.reader.Close()
	meta := reader.reader.Meta()
	source.Format = strings.ToLower(meta.Format)
	source.Fields = append([]string(nil), meta.Fields...)
	sort.Strings(source.Fields)
	source.SupportsIPv4 = meta.IsIPv4Support()
	source.SupportsIPv6 = meta.IsIPv6Support()
	if !source.SupportsIPv4 && !source.SupportsIPv6 {
		return source, fmt.Errorf("database does not report IPv4 or IPv6 support")
	}
	if stat, err := os.Stat(filename); err == nil {
		source.SizeBytes = stat.Size()
	}
	return source, nil
}
