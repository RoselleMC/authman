package server

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/google/uuid"
)

const (
	maxIPGeoDatabaseBytes        = 256 * 1024 * 1024
	maxIPGeoExpandedBytes        = 512 * 1024 * 1024
	ipGeoDatabaseDownloadTimeout = 4 * time.Hour
	ipGeoDownloadIdleTimeout     = 90 * time.Second
	ipGeoDownloadProgressWindow  = 2 * time.Minute
	ipGeoDownloadMinProgress     = 128 * 1024
	ipGeoDownloadResumeAttempts  = 12
	ipGeoDownloadConnectAttempts = 2
	ipGeoBackgroundPollInterval  = 15 * time.Minute
	ipGeoRefreshAttempts         = 3
)

type ipGeoSourceRequest struct {
	CatalogID           string `json:"catalog_id"`
	Name                string `json:"name"`
	Type                string `json:"type"`
	Format              string `json:"format"`
	DataFamily          string `json:"data_family"`
	SourceURL           string `json:"source_url"`
	GitHubRepository    string `json:"github_repository"`
	AssetPattern        string `json:"asset_pattern"`
	Enabled             *bool  `json:"enabled"`
	Weight              int    `json:"weight"`
	AutoUpdate          *bool  `json:"auto_update"`
	UpdateIntervalHours int    `json:"update_interval_hours"`
}

type ipGeoLookupRequest struct {
	IP      string `json:"ip"`
	Refresh bool   `json:"refresh"`
}

type ipGeoRefreshJob struct {
	SourceID       string
	Force          bool
	RepeatIfActive bool
}

type ipGeoCatalogEntry struct {
	ID                  string
	Name                string
	Description         string
	Type                store.IPGeoSourceType
	Format              string
	DataFamily          string
	SourceURL           string
	GitHubRepository    string
	AssetPattern        string
	Homepage            string
	License             string
	UpdateIntervalHours int
	Scope               string
}

var ipGeoCatalog = []ipGeoCatalogEntry{
	{
		ID: "p3terx-geolite2-city", Name: "P3TERX GeoLite2 City", Description: "Global city database mirrored from MaxMind GeoLite2.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "maxmind-geolite2", GitHubRepository: "P3TERX/GeoLite.mmdb", AssetPattern: "GeoLite2-City.mmdb",
		Homepage: "https://github.com/P3TERX/GeoLite.mmdb", License: "GeoLite2 / CC BY-SA 4.0", UpdateIntervalHours: 48, Scope: "global_city",
	},
	{
		ID: "p3terx-geolite2-country", Name: "P3TERX GeoLite2 Country", Description: "Global country database mirrored from MaxMind GeoLite2.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "maxmind-geolite2", GitHubRepository: "P3TERX/GeoLite.mmdb", AssetPattern: "GeoLite2-Country.mmdb",
		Homepage: "https://github.com/P3TERX/GeoLite.mmdb", License: "GeoLite2 / CC BY-SA 4.0", UpdateIntervalHours: 48, Scope: "global_country",
	},
	{
		ID: "p3terx-geolite2-asn", Name: "P3TERX GeoLite2 ASN", Description: "Global ASN database mirrored from MaxMind GeoLite2.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "maxmind-geolite2", GitHubRepository: "P3TERX/GeoLite.mmdb", AssetPattern: "GeoLite2-ASN.mmdb",
		Homepage: "https://github.com/P3TERX/GeoLite.mmdb", License: "GeoLite2 / CC BY-SA 4.0", UpdateIntervalHours: 48, Scope: "global_asn",
	},
	{
		ID: "fyralabs-geolite2-city", Name: "FyraLabs GeoLite2 City", Description: "Daily GeoLite2 City mirror; an alternative transport mirror, not an independent vote from P3TERX.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "maxmind-geolite2", GitHubRepository: "FyraLabs/geolite2", AssetPattern: "GeoLite2-City.mmdb",
		Homepage: "https://github.com/FyraLabs/geolite2", License: "GeoLite2 / CC BY-SA 4.0", UpdateIntervalHours: 24, Scope: "global_city",
	},
	{
		ID: "sapics-user-country", Name: "sapics user-country", Description: "Daily end-user country data assembled from public geofeeds; recommended by the upstream project for general use.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "sapics-user-country", GitHubRepository: "sapics/ip-location-db", AssetPattern: "user-country.mmdb",
		Homepage: "https://github.com/sapics/ip-location-db", License: "PDDL 1.0", UpdateIntervalHours: 24, Scope: "global_country",
	},
	{
		ID: "sapics-origin-asn", Name: "sapics origin-asn", Description: "Daily ASN database assembled from public routing data.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "sapics-origin-asn", GitHubRepository: "sapics/ip-location-db", AssetPattern: "origin-asn.mmdb",
		Homepage: "https://github.com/sapics/ip-location-db", License: "PDDL 1.0", UpdateIntervalHours: 24, Scope: "global_asn",
	},
	{
		ID: "sapics-dbip-city-ipv4", Name: "sapics DB-IP Lite City IPv4", Description: "Monthly DB-IP Lite city database for IPv4.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "dbip-lite", GitHubRepository: "sapics/ip-location-db", AssetPattern: "dbip-city-ipv4.mmdb",
		Homepage: "https://github.com/sapics/ip-location-db", License: "DB-IP Lite / CC BY 4.0 (attribution required)", UpdateIntervalHours: 168, Scope: "ipv4_city",
	},
	{
		ID: "sapics-dbip-city-ipv6", Name: "sapics DB-IP Lite City IPv6", Description: "Monthly DB-IP Lite city database for IPv6.",
		Type: store.IPGeoSourceGitHubRelease, Format: "mmdb", DataFamily: "dbip-lite", GitHubRepository: "sapics/ip-location-db", AssetPattern: "dbip-city-ipv6.mmdb",
		Homepage: "https://github.com/sapics/ip-location-db", License: "DB-IP Lite / CC BY 4.0 (attribution required)", UpdateIntervalHours: 168, Scope: "ipv6_city",
	},
	{
		ID: "ip66", Name: "IP66", Description: "Daily global country, continent and ASN MMDB published by Cloud 66.",
		Type: store.IPGeoSourceURL, Format: "mmdb", DataFamily: "ip66", SourceURL: "https://downloads.ip66.dev/db/ip66.mmdb",
		Homepage: "https://ip66.dev/", License: "CC BY 4.0", UpdateIntervalHours: 24, Scope: "global_country_asn",
	},
	{
		ID: "metowolf-qqwry", Name: "metowolf QQWry", Description: "China-focused QQWry database distributed as qqwry.dat; verify upstream data terms before use.",
		Type: store.IPGeoSourceGitHubRelease, Format: "qqwry", DataFamily: "qqwry", GitHubRepository: "metowolf/qqwry.dat", AssetPattern: "qqwry.dat",
		Homepage: "https://github.com/metowolf/qqwry.dat", License: "Upstream data terms", UpdateIntervalHours: 168, Scope: "china_ipv4_city_isp",
	},
	{
		ID: "hackl0us-geoip2-cn", Name: "GeoIP2-CN", Description: "China-mainland-only country database; it returns no result for non-CN addresses.",
		Type: store.IPGeoSourceURL, Format: "mmdb", DataFamily: "geoip2-cn", SourceURL: "https://github.com/Hackl0us/GeoIP2-CN/raw/release/Country.mmdb",
		Homepage: "https://github.com/Hackl0us/GeoIP2-CN", License: "GPL-3.0", UpdateIntervalHours: 72, Scope: "china_country_only",
	},
}

func (s *Server) handleAdminIPGeoCatalog(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(ipGeoCatalog))
	for _, entry := range ipGeoCatalog {
		data = append(data, ipGeoCatalogData(entry))
	}
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func (s *Server) handleAdminIPGeoSources(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	sources := s.store.ListIPGeoSources(r.Context())
	data := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		data = append(data, ipGeoSourceData(source))
	}
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func (s *Server) handleAdminCreateIPGeoSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req ipGeoSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	source, err := buildIPGeoSource(req, nil)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.source_invalid", err.Error()))
		return
	}
	source.ID = uuid.NewString()
	now := time.Now().UTC()
	source.CreatedAt = now
	source.Status = store.IPGeoSourcePending
	source.NextCheckAt = &now
	source, err = s.store.UpsertIPGeoSource(r.Context(), source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.source_save_failed", "failed to save IP database source"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, source.ID, "ip_geo.source.create", map[string]any{"name": source.Name, "type": source.Type})
	s.queueIPGeoRefresh(ipGeoRefreshJob{SourceID: source.ID, Force: true})
	api.WriteJSON(w, http.StatusCreated, ipGeoSourceData(source), nil)
}

func (s *Server) handleAdminUpdateIPGeoSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	existing, err := s.store.GetIPGeoSource(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ip_geo.source_not_found", "IP database source not found"))
		return
	}
	var req ipGeoSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	updated, err := buildIPGeoSource(req, &existing)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.source_invalid", err.Error()))
		return
	}
	changedRemote := updated.Type != existing.Type || updated.SourceURL != existing.SourceURL ||
		updated.GitHubRepository != existing.GitHubRepository || updated.AssetPattern != existing.AssetPattern || updated.Format != existing.Format
	if changedRemote && updated.Type != store.IPGeoSourceUpload {
		now := time.Now().UTC()
		updated.LastError = ""
		updated.NextCheckAt = &now
		if updated.StorageFilename == "" {
			updated.Status = store.IPGeoSourcePending
		} else {
			updated.Status = store.IPGeoSourceReady
		}
	}
	updated, err = s.store.UpsertIPGeoSource(r.Context(), updated)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.source_save_failed", "failed to save IP database source"))
		return
	}
	if changedRemote && updated.Type != store.IPGeoSourceUpload {
		s.reloadIPGeoSources(r.Context())
		s.queueIPGeoRefresh(ipGeoRefreshJob{SourceID: updated.ID, Force: true, RepeatIfActive: true})
	} else {
		s.reloadIPGeoSources(r.Context())
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, updated.ID, "ip_geo.source.update", map[string]any{"name": updated.Name, "enabled": updated.Enabled, "weight": updated.Weight})
	api.WriteJSON(w, http.StatusOK, ipGeoSourceData(updated), nil)
}

func (s *Server) handleAdminDeleteIPGeoSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	source, err := s.store.GetIPGeoSource(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ip_geo.source_not_found", "IP database source not found"))
		return
	}
	if err := s.store.DeleteIPGeoSource(r.Context(), source.ID); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.source_delete_failed", "failed to delete IP database source"))
		return
	}
	if source.StorageFilename != "" {
		_ = os.Remove(filepath.Join(s.cfg.IPGeoDataDir, filepath.Base(source.StorageFilename)))
	}
	s.reloadIPGeoSources(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, source.ID, "ip_geo.source.delete", map[string]any{"name": source.Name})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminRefreshIPGeoSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	source, err := s.store.GetIPGeoSource(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ip_geo.source_not_found", "IP database source not found"))
		return
	}
	if source.Type == store.IPGeoSourceUpload {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.source_not_refreshable", "uploaded databases cannot be refreshed from a remote source"))
		return
	}
	now := time.Now().UTC()
	source.Status = store.IPGeoSourceUpdating
	source.LastError = ""
	source.NextCheckAt = &now
	source, err = s.store.UpsertIPGeoSource(r.Context(), source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.source_save_failed", "failed to queue IP database refresh"))
		return
	}
	if !s.queueIPGeoRefresh(ipGeoRefreshJob{SourceID: source.ID, Force: true}) {
		api.WriteError(w, api.NewError(http.StatusServiceUnavailable, "ip_geo.refresh_queue_full", "IP database refresh queue is full"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, source.ID, "ip_geo.source.refresh", map[string]any{"queued": true})
	api.WriteJSON(w, http.StatusAccepted, ipGeoSourceData(source), nil)
}

func (s *Server) handleAdminUploadIPGeoSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIPGeoDatabaseBytes+2*1024*1024)
	if err := r.ParseMultipartForm(maxIPGeoDatabaseBytes + 1024*1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.upload_invalid", "invalid or oversized database upload"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.file_required", "IP database file is required"))
		return
	}
	defer file.Close()
	if err := os.MkdirAll(s.cfg.IPGeoDataDir, 0o750); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.storage_failed", "failed to prepare IP database storage"))
		return
	}
	temp, err := os.CreateTemp(s.cfg.IPGeoDataDir, ".upload-*")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.storage_failed", "failed to create upload file"))
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, copyErr := io.Copy(temp, io.LimitReader(file, maxIPGeoDatabaseBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil || written == 0 || written > maxIPGeoDatabaseBytes {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.file_invalid", "IP database file is empty, invalid, or too large"))
		return
	}
	enabled := parseFormBool(r.FormValue("enabled"), true)
	weight := parseFormInt(r.FormValue("weight"), 1)
	source := store.IPGeoSource{
		ID:                  uuid.NewString(),
		Name:                strings.TrimSpace(r.FormValue("name")),
		Type:                store.IPGeoSourceUpload,
		Format:              normalizeIPGeoFormat(r.FormValue("format")),
		DataFamily:          strings.TrimSpace(r.FormValue("data_family")),
		Enabled:             enabled,
		Weight:              clampInt(weight, 1, 100),
		AutoUpdate:          false,
		UpdateIntervalHours: 24,
		OriginalFilename:    filepath.Base(header.Filename),
		ContentType:         header.Header.Get("Content-Type"),
		Status:              store.IPGeoSourcePending,
		CreatedAt:           time.Now().UTC(),
	}
	if source.Name == "" {
		source.Name = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	if source.DataFamily == "" {
		source.DataFamily = "upload:" + source.ID
	}
	stored, err := s.installIPGeoDatabase(source, tempPath, header.Filename, "upload")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.database_invalid", err.Error()))
		return
	}
	stored, err = s.store.UpsertIPGeoSource(r.Context(), stored)
	if err != nil {
		_ = os.Remove(filepath.Join(s.cfg.IPGeoDataDir, filepath.Base(stored.StorageFilename)))
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ip_geo.source_save_failed", "failed to save IP database source"))
		return
	}
	s.reloadIPGeoSources(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, stored.ID, "ip_geo.source.upload", map[string]any{"name": stored.Name, "filename": stored.OriginalFilename, "sha256": stored.SHA256})
	api.WriteJSON(w, http.StatusCreated, ipGeoSourceData(stored), nil)
}

func (s *Server) handleAdminIPGeoLookup(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	var req ipGeoLookupRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	ip, valid := normalizeIPGeoLookupIP(req.IP)
	if !valid {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ip_geo.ip_invalid", "a valid IPv4 or IPv6 address is required"))
		return
	}
	var result ipGeoLookupResult
	if req.Refresh {
		result = s.ipGeo.refreshDetailed(r.Context(), ip)
		if result.Geo == nil {
			api.WriteError(w, api.NewError(http.StatusBadGateway, "ip_geo.refresh_failed", "failed to refresh IP geolocation"))
			return
		}
	} else {
		result = s.ipGeo.lookupDetailed(r.Context(), ip)
	}
	data := map[string]any{"ip": ip, "provider": result.Provider, "cached": result.Cached, "geo": ipGeoData(result.Geo)}
	evidence := make([]map[string]any, 0, len(result.Evidence))
	for _, item := range result.Evidence {
		evidence = append(evidence, map[string]any{
			"source_id": item.SourceID, "source_name": item.SourceName, "data_family": item.DataFamily,
			"weight": item.Weight, "status": item.Status, "geo": ipGeoData(item.Geo), "error": emptyStringNil(item.Error),
		})
	}
	data["evidence"] = evidence
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func normalizeIPGeoLookupIP(value string) (string, bool) {
	ip := normalizeClientIPValue(value)
	return ip, net.ParseIP(ip) != nil
}

func buildIPGeoSource(req ipGeoSourceRequest, existing *store.IPGeoSource) (store.IPGeoSource, error) {
	var source store.IPGeoSource
	if existing != nil {
		source = *existing
		source.Fields = append([]string(nil), existing.Fields...)
	}
	if catalogID := strings.TrimSpace(req.CatalogID); catalogID != "" {
		entry, ok := findIPGeoCatalogEntry(catalogID)
		if !ok {
			return source, fmt.Errorf("unknown database catalog entry")
		}
		source.CatalogID = entry.ID
		source.Name = entry.Name
		source.Type = entry.Type
		source.Format = entry.Format
		source.DataFamily = entry.DataFamily
		source.SourceURL = entry.SourceURL
		source.GitHubRepository = entry.GitHubRepository
		source.AssetPattern = entry.AssetPattern
		source.Homepage = entry.Homepage
		source.License = entry.License
		source.AutoUpdate = true
		source.UpdateIntervalHours = entry.UpdateIntervalHours
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		source.Name = name
	}
	if req.Type != "" {
		source.Type = store.IPGeoSourceType(strings.TrimSpace(req.Type))
	}
	if req.Format != "" {
		source.Format = normalizeIPGeoFormat(req.Format)
	}
	if family := strings.TrimSpace(req.DataFamily); family != "" {
		source.DataFamily = family
	}
	if req.SourceURL != "" {
		source.SourceURL = strings.TrimSpace(req.SourceURL)
	}
	if req.GitHubRepository != "" {
		source.GitHubRepository = strings.TrimSpace(req.GitHubRepository)
	}
	if req.AssetPattern != "" {
		source.AssetPattern = strings.TrimSpace(req.AssetPattern)
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	} else if existing == nil {
		source.Enabled = true
	}
	if req.AutoUpdate != nil {
		source.AutoUpdate = *req.AutoUpdate
	} else if existing == nil && source.CatalogID == "" {
		source.AutoUpdate = true
	}
	if req.Weight > 0 {
		source.Weight = req.Weight
	} else if source.Weight <= 0 {
		source.Weight = 1
	}
	if req.UpdateIntervalHours > 0 {
		source.UpdateIntervalHours = req.UpdateIntervalHours
	} else if source.UpdateIntervalHours <= 0 {
		source.UpdateIntervalHours = 24
	}
	source.Weight = clampInt(source.Weight, 1, 100)
	source.UpdateIntervalHours = clampInt(source.UpdateIntervalHours, 1, 8760)
	source.Name = strings.TrimSpace(source.Name)
	if source.Name == "" {
		return source, fmt.Errorf("source name is required")
	}
	switch source.Type {
	case store.IPGeoSourceURL:
		if err := validateHTTPURL(source.SourceURL); err != nil {
			return source, fmt.Errorf("invalid database URL: %w", err)
		}
		source.GitHubRepository = ""
		source.AssetPattern = ""
	case store.IPGeoSourceGitHubRelease:
		if !validGitHubRepository(source.GitHubRepository) {
			return source, fmt.Errorf("GitHub repository must use owner/repository format")
		}
		if source.AssetPattern == "" {
			return source, fmt.Errorf("GitHub release asset pattern is required")
		}
		source.SourceURL = ""
	case store.IPGeoSourceUpload:
		if existing == nil {
			return source, fmt.Errorf("use the upload endpoint for local files")
		}
		source.AutoUpdate = false
	default:
		return source, fmt.Errorf("source type must be url or github_release")
	}
	if source.DataFamily == "" && source.ID != "" {
		source.DataFamily = "source:" + source.ID
	}
	return source, nil
}

func (s *Server) refreshIPGeoSource(ctx context.Context, id string, force bool) (store.IPGeoSource, error) {
	s.ipGeoRefreshMu.Lock()
	defer s.ipGeoRefreshMu.Unlock()
	source, err := s.store.GetIPGeoSource(ctx, id)
	if err != nil {
		return source, err
	}
	if source.Type == store.IPGeoSourceUpload {
		return source, fmt.Errorf("uploaded databases do not have a remote update source")
	}
	downloadSource := source
	source.Status = store.IPGeoSourceUpdating
	source.LastError = ""
	_, _ = s.store.UpsertIPGeoSource(ctx, source)
	now := time.Now().UTC()
	download, err := s.downloadIPGeoSourceWithRetry(ctx, source, force)
	if err != nil {
		current, currentErr := s.store.GetIPGeoSource(ctx, id)
		if currentErr != nil {
			return source, currentErr
		}
		if !sameIPGeoRemoteSource(downloadSource, current) {
			return current, nil
		}
		source = current
		source.LastCheckedAt = &now
		source.LastError = truncateText(err.Error(), 1000)
		if source.StorageFilename != "" {
			source.Status = store.IPGeoSourceReady
		} else {
			source.Status = store.IPGeoSourceError
		}
		source.NextCheckAt = nextIPGeoRetry(source, now)
		source, _ = s.store.UpsertIPGeoSource(ctx, source)
		return source, err
	}
	defer func() {
		if download.Path != "" {
			_ = os.Remove(download.Path)
		}
	}()
	current, err := s.store.GetIPGeoSource(ctx, id)
	if err != nil {
		return source, err
	}
	if !sameIPGeoRemoteSource(downloadSource, current) {
		return current, nil
	}
	source = current
	if download.Unchanged {
		source.Status = store.IPGeoSourceReady
		source.LastError = ""
		source.LastCheckedAt = &now
		source.NextCheckAt = nextIPGeoCheck(source, now)
		source.ETag = firstNonEmpty(download.ETag, source.ETag)
		source.LastModified = firstNonEmpty(download.LastModified, source.LastModified)
		source, err = s.store.UpsertIPGeoSource(ctx, source)
		return source, err
	}
	installed, err := s.installIPGeoDatabase(source, download.Path, download.OriginalFilename, download.Version)
	if err != nil {
		source.LastCheckedAt = &now
		source.LastError = truncateText(err.Error(), 1000)
		if source.StorageFilename != "" {
			source.Status = store.IPGeoSourceReady
		} else {
			source.Status = store.IPGeoSourceError
		}
		source.NextCheckAt = nextIPGeoRetry(source, now)
		source, _ = s.store.UpsertIPGeoSource(ctx, source)
		return source, err
	}
	installed.ETag = download.ETag
	installed.LastModified = download.LastModified
	installed.LastCheckedAt = &now
	installed.NextCheckAt = nextIPGeoCheck(installed, now)
	installed, err = s.store.UpsertIPGeoSource(ctx, installed)
	if err != nil {
		return installed, err
	}
	s.reloadIPGeoSources(ctx)
	return installed, nil
}

func sameIPGeoRemoteSource(left, right store.IPGeoSource) bool {
	return left.Type == right.Type &&
		left.Format == right.Format &&
		left.SourceURL == right.SourceURL &&
		left.GitHubRepository == right.GitHubRepository &&
		left.AssetPattern == right.AssetPattern
}

type ipGeoDownload struct {
	Path             string
	OriginalFilename string
	Version          string
	ETag             string
	LastModified     string
	Unchanged        bool
}

func (s *Server) downloadIPGeoSourceWithRetry(ctx context.Context, source store.IPGeoSource, force bool) (ipGeoDownload, error) {
	var lastErr error
	for attempt := 1; attempt <= ipGeoRefreshAttempts; attempt++ {
		download, err := s.downloadIPGeoSource(ctx, source, force)
		if err == nil {
			return download, nil
		}
		lastErr = err
		if attempt == ipGeoRefreshAttempts || ctx.Err() != nil {
			break
		}
		delay := time.Duration(attempt) * 500 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ipGeoDownload{}, ctx.Err()
		case <-timer.C:
		}
	}
	return ipGeoDownload{}, lastErr
}

func (s *Server) downloadIPGeoSource(ctx context.Context, source store.IPGeoSource, force bool) (ipGeoDownload, error) {
	switch source.Type {
	case store.IPGeoSourceURL:
		headers := map[string]string{}
		if !force {
			if source.ETag != "" {
				headers["If-None-Match"] = source.ETag
			}
			if source.LastModified != "" {
				headers["If-Modified-Since"] = source.LastModified
			}
		}
		download, err := downloadIPGeoURL(ctx, source.SourceURL, headers, s.cfg.IPGeoDataDir)
		if err != nil {
			return download, err
		}
		if download.OriginalFilename == "" {
			download.OriginalFilename = path.Base(strings.TrimSuffix(source.SourceURL, "/"))
		}
		download.Version = firstNonEmpty(download.ETag, download.LastModified, time.Now().UTC().Format(time.RFC3339))
		return download, nil
	case store.IPGeoSourceGitHubRelease:
		return downloadGitHubReleaseAsset(ctx, source, force, s.cfg.IPGeoDataDir)
	default:
		return ipGeoDownload{}, fmt.Errorf("unsupported source type %q", source.Type)
	}
}

func downloadIPGeoURL(ctx context.Context, rawURL string, headers map[string]string, dataDir string) (ipGeoDownload, error) {
	if err := validateHTTPURL(rawURL); err != nil {
		return ipGeoDownload{}, err
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return ipGeoDownload{}, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: ipGeoDatabaseDownloadTimeout, Transport: transport}
	result := ipGeoDownload{}
	var offset int64
	var expectedTotal int64 = -1
	var lastErr error
	defer func() {
		if lastErr != nil && result.Path != "" {
			_ = os.Remove(result.Path)
		}
	}()
	for attempt := 1; attempt <= ipGeoDownloadResumeAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return ipGeoDownload{}, err
		}
		req.Header.Set("Accept", "application/octet-stream, application/json;q=0.8")
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("User-Agent", "Authman-IPGeo/1")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if validator := firstNonEmpty(result.ETag, result.LastModified); validator != "" {
				req.Header.Set("If-Range", validator)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if !shouldRetryIPGeoDownload(ctx, attempt, offset) {
				return ipGeoDownload{}, lastErr
			}
			continue
		}
		if value := resp.Header.Get("ETag"); value != "" {
			result.ETag = value
		}
		if value := resp.Header.Get("Last-Modified"); value != "" {
			result.LastModified = value
		}
		if resp.StatusCode == http.StatusNotModified {
			_ = resp.Body.Close()
			result.Unchanged = true
			return result, nil
		}
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
			total, ok := parseIPGeoUnsatisfiedRange(resp.Header.Get("Content-Range"))
			_ = resp.Body.Close()
			if ok && total == offset {
				lastErr = nil
				return result, nil
			}
			lastErr = fmt.Errorf("download rejected byte range at offset %d", offset)
			return ipGeoDownload{}, lastErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("download returned HTTP %d", resp.StatusCode)
			return ipGeoDownload{}, lastErr
		}

		appendResponse := offset > 0 && resp.StatusCode == http.StatusPartialContent
		if appendResponse {
			start, total, ok := parseIPGeoContentRange(resp.Header.Get("Content-Range"))
			if !ok || start != offset {
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("download returned an invalid Content-Range for offset %d", offset)
				return ipGeoDownload{}, lastErr
			}
			expectedTotal = total
		} else if offset > 0 {
			// The server ignored Range. Restart this attempt from the beginning so
			// the partial prefix is never combined with a different response body.
			offset = 0
			expectedTotal = resp.ContentLength
		} else {
			expectedTotal = resp.ContentLength
		}
		if expectedTotal > maxIPGeoDatabaseBytes {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("downloaded database exceeds %d MiB", maxIPGeoDatabaseBytes/(1024*1024))
			return ipGeoDownload{}, lastErr
		}
		if result.Path == "" {
			result.OriginalFilename = responseFilename(resp, rawURL)
			ext := archiveSuffix(result.OriginalFilename)
			temp, createErr := os.CreateTemp(dataDir, ".download-*"+ext)
			if createErr != nil {
				_ = resp.Body.Close()
				return ipGeoDownload{}, createErr
			}
			result.Path = temp.Name()
			if closeErr := temp.Close(); closeErr != nil {
				_ = resp.Body.Close()
				_ = os.Remove(result.Path)
				return ipGeoDownload{}, closeErr
			}
		}
		flags := os.O_WRONLY
		if appendResponse {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		file, openErr := os.OpenFile(result.Path, flags, 0o600)
		if openErr != nil {
			_ = resp.Body.Close()
			lastErr = openErr
			return ipGeoDownload{}, openErr
		}
		written, copyErr := copyIPGeoResponse(file, resp.Body, maxIPGeoDatabaseBytes+1-offset, resp.ContentLength,
			ipGeoDownloadIdleTimeout, ipGeoDownloadProgressWindow, ipGeoDownloadMinProgress)
		closeErr := firstError(file.Close(), resp.Body.Close())
		offset += written
		if copyErr == nil && closeErr == nil && expectedTotal > 0 && offset != expectedTotal {
			copyErr = fmt.Errorf("download ended at %d bytes, expected %d", offset, expectedTotal)
		}
		if copyErr == nil && closeErr == nil {
			if offset == 0 || offset > maxIPGeoDatabaseBytes {
				lastErr = fmt.Errorf("downloaded database is empty or exceeds %d MiB", maxIPGeoDatabaseBytes/(1024*1024))
				return ipGeoDownload{}, lastErr
			}
			lastErr = nil
			return result, nil
		}
		lastErr = firstError(copyErr, closeErr)
		if !shouldRetryIPGeoDownload(ctx, attempt, offset) {
			return ipGeoDownload{}, lastErr
		}
	}
	return ipGeoDownload{}, lastErr
}

func shouldRetryIPGeoDownload(ctx context.Context, attempt int, offset int64) bool {
	maxAttempts := ipGeoDownloadConnectAttempts
	if offset > 0 {
		maxAttempts = ipGeoDownloadResumeAttempts
	}
	if attempt >= maxAttempts || ctx.Err() != nil {
		return false
	}
	delay := time.Duration(min(attempt, 5)) * time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func parseIPGeoContentRange(value string) (int64, int64, bool) {
	var start, end, total int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "bytes %d-%d/%d", &start, &end, &total); err != nil {
		return 0, 0, false
	}
	return start, total, start >= 0 && end >= start && total > end
}

func parseIPGeoUnsatisfiedRange(value string) (int64, bool) {
	var total int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "bytes */%d", &total); err != nil {
		return 0, false
	}
	return total, total >= 0
}

type ipGeoReadResult struct {
	data []byte
	err  error
}

func copyIPGeoResponse(dst io.Writer, src io.ReadCloser, maxBytes int64, expectedBytes int64, idleTimeout time.Duration, progressWindow time.Duration, minProgressBytes int64) (int64, error) {
	if idleTimeout <= 0 || progressWindow <= 0 || minProgressBytes <= 0 {
		return 0, fmt.Errorf("invalid IP database download progress limits")
	}
	progressThreshold := minProgressBytes
	if expectedBytes > 0 && expectedBytes < progressThreshold*4 {
		progressThreshold = max(int64(1), expectedBytes/4)
	}

	results := make(chan ipGeoReadResult, 1)
	acknowledge := make(chan struct{})
	done := make(chan struct{})
	go func() {
		buffer := make([]byte, 256*1024)
		for {
			n, err := src.Read(buffer)
			select {
			case results <- ipGeoReadResult{data: buffer[:n], err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
			select {
			case <-acknowledge:
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	idleTimer := time.NewTimer(idleTimeout)
	progressTimer := time.NewTimer(progressWindow)
	defer idleTimer.Stop()
	defer progressTimer.Stop()
	var written int64
	var windowStart int64
	for {
		select {
		case result := <-results:
			if len(result.data) > 0 {
				if written+int64(len(result.data)) > maxBytes {
					_ = src.Close()
					return written, fmt.Errorf("downloaded database exceeds the size limit")
				}
				n, err := dst.Write(result.data)
				written += int64(n)
				if err != nil {
					_ = src.Close()
					return written, err
				}
				if n != len(result.data) {
					_ = src.Close()
					return written, io.ErrShortWrite
				}
				resetTimer(idleTimer, idleTimeout)
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return written, nil
				}
				return written, result.err
			}
			acknowledge <- struct{}{}
		case <-idleTimer.C:
			_ = src.Close()
			return written, fmt.Errorf("IP database download made no progress for %s", idleTimeout)
		case <-progressTimer.C:
			windowProgress := written - windowStart
			if windowProgress < progressThreshold {
				_ = src.Close()
				return written, fmt.Errorf("IP database download is too slow: received %d bytes in %s, need at least %d", windowProgress, progressWindow, progressThreshold)
			}
			windowStart = written
			progressTimer.Reset(progressWindow)
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func downloadGitHubReleaseAsset(ctx context.Context, source store.IPGeoSource, force bool, dataDir string) (ipGeoDownload, error) {
	apiURL := "https://api.github.com/repos/" + source.GitHubRepository + "/releases/latest"
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return ipGeoDownload{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Authman-IPGeo/1")
	if !force && source.ETag != "" {
		req.Header.Set("If-None-Match", source.ETag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ipGeoDownload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return ipGeoDownload{Unchanged: true, ETag: source.ETag}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return ipGeoDownload{}, fmt.Errorf("GitHub latest release returned HTTP %d", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			ID                 int64     `json:"id"`
			Name               string    `json:"name"`
			Size               int64     `json:"size"`
			UpdatedAt          time.Time `json:"updated_at"`
			BrowserDownloadURL string    `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&release); err != nil {
		return ipGeoDownload{}, err
	}
	var asset *struct {
		ID                 int64     `json:"id"`
		Name               string    `json:"name"`
		Size               int64     `json:"size"`
		UpdatedAt          time.Time `json:"updated_at"`
		BrowserDownloadURL string    `json:"browser_download_url"`
	}
	for index := range release.Assets {
		matched, matchErr := path.Match(strings.ToLower(source.AssetPattern), strings.ToLower(release.Assets[index].Name))
		if matchErr != nil {
			return ipGeoDownload{}, fmt.Errorf("invalid GitHub asset pattern: %w", matchErr)
		}
		if matched {
			asset = &release.Assets[index]
			break
		}
	}
	if asset == nil {
		return ipGeoDownload{}, fmt.Errorf("no release asset matched %q", source.AssetPattern)
	}
	version := fmt.Sprintf("%s|%d|%s|%s|%d", release.TagName, asset.ID, asset.UpdatedAt.UTC().Format(time.RFC3339), asset.Name, asset.Size)
	result := ipGeoDownload{OriginalFilename: asset.Name, Version: version, ETag: resp.Header.Get("ETag")}
	if source.Version == version && source.StorageFilename != "" {
		result.Unchanged = true
		return result, nil
	}
	download, err := downloadIPGeoURL(ctx, asset.BrowserDownloadURL, nil, dataDir)
	if err != nil {
		assetAPIURL := githubReleaseAssetAPIURL(source.GitHubRepository, asset.ID)
		var fallbackErr error
		download, fallbackErr = downloadIPGeoURL(ctx, assetAPIURL, map[string]string{
			"Accept":               "application/octet-stream",
			"X-GitHub-Api-Version": "2022-11-28",
		}, dataDir)
		if fallbackErr != nil {
			return result, fmt.Errorf("release asset download failed via browser URL (%v) and GitHub Assets API (%w)", err, fallbackErr)
		}
	}
	download.OriginalFilename = asset.Name
	download.Version = version
	download.ETag = result.ETag
	return download, nil
}

func githubReleaseAssetAPIURL(repository string, assetID int64) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%d", strings.TrimSpace(repository), assetID)
}

func (s *Server) installIPGeoDatabase(source store.IPGeoSource, downloadedPath string, originalFilename string, version string) (store.IPGeoSource, error) {
	if err := os.MkdirAll(s.cfg.IPGeoDataDir, 0o750); err != nil {
		return source, err
	}
	preparedPath, preparedName, err := prepareIPGeoDatabaseFile(s.cfg.IPGeoDataDir, downloadedPath, originalFilename, source.Format)
	if err != nil {
		return source, err
	}
	if preparedPath != downloadedPath {
		defer os.Remove(preparedPath)
	}
	source.OriginalFilename = filepath.Base(preparedName)
	if strings.TrimSpace(source.Format) == "" {
		source.Format = ipGeoFormatForFilename(preparedName)
	}
	validated, err := validateIPGeoDatabase(source, preparedPath)
	if err != nil {
		return source, fmt.Errorf("unsupported or unreadable IP database: %w", err)
	}
	sha, size, err := hashFile(preparedPath)
	if err != nil {
		return source, err
	}
	ext := filepath.Ext(preparedName)
	if ext == "" {
		ext = extensionForIPGeoFormat(validated.Format)
	}
	storageFilename := source.ID + ext
	destination := filepath.Join(s.cfg.IPGeoDataDir, storageFilename)
	if err := os.Rename(preparedPath, destination); err != nil {
		return source, err
	}
	if source.StorageFilename != "" && source.StorageFilename != storageFilename {
		_ = os.Remove(filepath.Join(s.cfg.IPGeoDataDir, filepath.Base(source.StorageFilename)))
	}
	now := time.Now().UTC()
	validated.StorageFilename = storageFilename
	validated.ContentType = mime.TypeByExtension(ext)
	validated.SHA256 = sha
	validated.SizeBytes = size
	validated.Version = version
	validated.Status = store.IPGeoSourceReady
	validated.LastError = ""
	validated.LastUpdatedAt = &now
	return validated, nil
}

func prepareIPGeoDatabaseFile(dataDir string, sourcePath string, originalFilename string, databaseFormat string) (string, string, error) {
	name := filepath.Base(strings.TrimSpace(originalFilename))
	if name == "." || name == "" {
		name = filepath.Base(sourcePath)
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractIPGeoTarGzip(dataDir, sourcePath, databaseFormat)
	case strings.HasSuffix(lower, ".zip"):
		return extractIPGeoZip(dataDir, sourcePath, databaseFormat)
	case strings.HasSuffix(lower, ".gz"):
		return extractIPGeoGzip(dataDir, sourcePath, strings.TrimSuffix(name, filepath.Ext(name)))
	}
	if !supportedIPGeoDatabaseName(name) && normalizeIPGeoFormat(databaseFormat) == "" {
		return "", "", fmt.Errorf("unsupported database extension; choose a format explicitly or upload MMDB, IPDB, AWDB, XDB, DAT, DB, CZDB, or TXT")
	}
	if filepath.Ext(name) == "" {
		name += extensionForIPGeoFormat(databaseFormat)
	}
	return sourcePath, name, nil
}

func extractIPGeoGzip(dataDir string, sourcePath string, name string) (string, string, error) {
	input, err := os.Open(sourcePath)
	if err != nil {
		return "", "", err
	}
	defer input.Close()
	reader, err := gzip.NewReader(input)
	if err != nil {
		return "", "", err
	}
	defer reader.Close()
	if reader.Name != "" {
		name = filepath.Base(reader.Name)
	}
	path, err := writeExpandedIPGeoFile(dataDir, name, reader)
	return path, name, err
}

func extractIPGeoZip(dataDir string, sourcePath string, databaseFormat string) (string, string, error) {
	archive, err := zip.OpenReader(sourcePath)
	if err != nil {
		return "", "", err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		name := filepath.Base(entry.Name)
		if entry.FileInfo().IsDir() || !archiveIPGeoEntryMatches(name, databaseFormat) {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return "", "", err
		}
		path, writeErr := writeExpandedIPGeoFile(dataDir, name, reader)
		_ = reader.Close()
		return path, name, writeErr
	}
	return "", "", fmt.Errorf("archive contains no supported IP database")
}

func extractIPGeoTarGzip(dataDir string, sourcePath string, databaseFormat string) (string, string, error) {
	input, err := os.Open(sourcePath)
	if err != nil {
		return "", "", err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return "", "", err
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", err
		}
		name := filepath.Base(header.Name)
		if header.Typeflag != tar.TypeReg || !archiveIPGeoEntryMatches(name, databaseFormat) {
			continue
		}
		path, writeErr := writeExpandedIPGeoFile(dataDir, name, archive)
		return path, name, writeErr
	}
	return "", "", fmt.Errorf("archive contains no supported IP database")
}

func writeExpandedIPGeoFile(dataDir string, name string, reader io.Reader) (string, error) {
	ext := filepath.Ext(name)
	temp, err := os.CreateTemp(dataDir, ".expanded-*"+ext)
	if err != nil {
		return "", err
	}
	path := temp.Name()
	written, copyErr := io.Copy(temp, io.LimitReader(reader, maxIPGeoExpandedBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", firstError(copyErr, closeErr)
	}
	if written == 0 || written > maxIPGeoExpandedBytes {
		_ = os.Remove(path)
		return "", fmt.Errorf("expanded database is empty or too large")
	}
	return path, nil
}

func hashFile(filename string) (string, int64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (s *Server) startIPGeoUpdateLoop(ctx context.Context) {
	go func() {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case sourceID := <-s.ipGeoRefreshQ:
				job, ok := s.beginIPGeoRefresh(sourceID)
				if !ok {
					continue
				}
				if _, err := s.refreshIPGeoSource(ctx, job.SourceID, job.Force); err != nil && ctx.Err() == nil {
					s.logger.Warn("IP geolocation database refresh failed", "source_id", job.SourceID, "error", err)
				}
				s.finishIPGeoRefresh(job.SourceID)
			case <-timer.C:
				s.queueDueIPGeoSources(ctx)
				timer.Reset(ipGeoBackgroundPollInterval)
			}
		}
	}()
}

func (s *Server) queueIPGeoRefresh(job ipGeoRefreshJob) bool {
	job.SourceID = strings.TrimSpace(job.SourceID)
	if job.SourceID == "" {
		return false
	}
	s.ipGeoQueueMu.Lock()
	if pending, ok := s.ipGeoPending[job.SourceID]; ok {
		pending.Force = pending.Force || job.Force
		pending.RepeatIfActive = pending.RepeatIfActive || job.RepeatIfActive
		s.ipGeoPending[job.SourceID] = pending
		s.ipGeoQueueMu.Unlock()
		return true
	}
	if s.ipGeoActive[job.SourceID] && !job.RepeatIfActive {
		s.ipGeoQueueMu.Unlock()
		return true
	}
	s.ipGeoPending[job.SourceID] = job
	select {
	case s.ipGeoRefreshQ <- job.SourceID:
		s.ipGeoQueueMu.Unlock()
		return true
	default:
		delete(s.ipGeoPending, job.SourceID)
		s.ipGeoQueueMu.Unlock()
		s.logger.Warn("IP geolocation refresh queue is full", "source_id", job.SourceID)
		return false
	}
}

func (s *Server) beginIPGeoRefresh(sourceID string) (ipGeoRefreshJob, bool) {
	s.ipGeoQueueMu.Lock()
	defer s.ipGeoQueueMu.Unlock()
	job, ok := s.ipGeoPending[sourceID]
	if !ok {
		return ipGeoRefreshJob{}, false
	}
	delete(s.ipGeoPending, sourceID)
	s.ipGeoActive[sourceID] = true
	return job, true
}

func (s *Server) finishIPGeoRefresh(sourceID string) {
	s.ipGeoQueueMu.Lock()
	delete(s.ipGeoActive, sourceID)
	s.ipGeoQueueMu.Unlock()
}

func (s *Server) queueDueIPGeoSources(ctx context.Context) {
	now := time.Now().UTC()
	for _, source := range s.store.ListIPGeoSources(ctx) {
		if ctx.Err() != nil {
			return
		}
		if source.Type == store.IPGeoSourceUpload || !source.AutoUpdate {
			continue
		}
		if source.NextCheckAt != nil && source.NextCheckAt.After(now) {
			continue
		}
		s.queueIPGeoRefresh(ipGeoRefreshJob{SourceID: source.ID})
	}
}

func ipGeoSourceData(source store.IPGeoSource) map[string]any {
	fields := append([]string{}, source.Fields...)
	return map[string]any{
		"id": source.ID, "catalog_id": emptyStringNil(source.CatalogID), "name": source.Name, "type": source.Type,
		"format": source.Format, "data_family": source.DataFamily, "source_url": source.SourceURL,
		"github_repository": source.GitHubRepository, "asset_pattern": source.AssetPattern, "homepage": source.Homepage,
		"license": source.License, "enabled": source.Enabled, "weight": source.Weight, "auto_update": source.AutoUpdate,
		"update_interval_hours": source.UpdateIntervalHours, "original_filename": source.OriginalFilename,
		"sha256": source.SHA256, "size_bytes": source.SizeBytes, "version": source.Version, "status": source.Status,
		"last_error": emptyStringNil(source.LastError), "fields": fields, "supports_ipv4": source.SupportsIPv4,
		"supports_ipv6": source.SupportsIPv6, "last_checked_at": source.LastCheckedAt, "last_updated_at": source.LastUpdatedAt,
		"next_check_at": source.NextCheckAt, "created_at": source.CreatedAt, "updated_at": source.UpdatedAt,
	}
}

func ipGeoCatalogData(entry ipGeoCatalogEntry) map[string]any {
	return map[string]any{
		"id": entry.ID, "name": entry.Name, "description": entry.Description, "type": entry.Type, "format": entry.Format,
		"data_family": entry.DataFamily, "source_url": entry.SourceURL, "github_repository": entry.GitHubRepository,
		"asset_pattern": entry.AssetPattern, "homepage": entry.Homepage, "license": entry.License,
		"update_interval_hours": entry.UpdateIntervalHours, "scope": entry.Scope,
	}
}

func findIPGeoCatalogEntry(id string) (ipGeoCatalogEntry, bool) {
	for _, entry := range ipGeoCatalog {
		if entry.ID == strings.TrimSpace(id) {
			return entry, true
		}
	}
	return ipGeoCatalogEntry{}, false
}

func nextIPGeoCheck(source store.IPGeoSource, from time.Time) *time.Time {
	if !source.AutoUpdate || source.Type == store.IPGeoSourceUpload {
		return nil
	}
	hours := clampInt(source.UpdateIntervalHours, 1, 8760)
	next := from.Add(time.Duration(hours) * time.Hour)
	return &next
}

func nextIPGeoRetry(source store.IPGeoSource, from time.Time) *time.Time {
	if !source.AutoUpdate || source.Type == store.IPGeoSourceUpload {
		return nil
	}
	delay := 30 * time.Minute
	if strings.TrimSpace(source.StorageFilename) == "" {
		delay = 5 * time.Minute
	}
	configured := time.Duration(clampInt(source.UpdateIntervalHours, 1, 8760)) * time.Hour
	if configured < delay {
		delay = configured
	}
	next := from.Add(delay)
	return &next
}

func normalizeIPGeoFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return ""
	case "mmdb", "ipdb", "awdb", "ip2region", "qqwry", "zxinc", "czdb", "plain":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func ipGeoFormatForFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mmdb":
		return "mmdb"
	case ".ipdb":
		return "ipdb"
	case ".awdb":
		return "awdb"
	case ".xdb":
		return "ip2region"
	case ".dat":
		return "qqwry"
	case ".db":
		return "zxinc"
	case ".czdb":
		return "czdb"
	case ".txt":
		return "plain"
	default:
		return ""
	}
}

func archiveIPGeoEntryMatches(name string, databaseFormat string) bool {
	expected := extensionForIPGeoFormat(databaseFormat)
	if expected != "" {
		return strings.EqualFold(filepath.Ext(name), expected)
	}
	return supportedIPGeoDatabaseName(name)
}

func extensionForIPGeoFormat(format string) string {
	switch normalizeIPGeoFormat(format) {
	case "mmdb":
		return ".mmdb"
	case "ipdb":
		return ".ipdb"
	case "awdb":
		return ".awdb"
	case "ip2region":
		return ".xdb"
	case "qqwry":
		return ".dat"
	case "zxinc":
		return ".db"
	case "czdb":
		return ".czdb"
	case "plain":
		return ".txt"
	default:
		return ""
	}
}

func supportedIPGeoDatabaseName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mmdb", ".ipdb", ".awdb", ".xdb", ".dat", ".db", ".czdb", ".txt":
		return true
	default:
		return false
	}
}

func archiveSuffix(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	case strings.HasSuffix(lower, ".gz"):
		return ".gz"
	default:
		return filepath.Ext(name)
	}
}

func responseFilename(resp *http.Response, rawURL string) string {
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if name := filepath.Base(params["filename"]); name != "." && name != "" {
				return name
			}
		}
	}
	parsed, _ := url.Parse(rawURL)
	return filepath.Base(parsed.Path)
}

func validateHTTPURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("only http/https URLs without embedded credentials are supported")
	}
	return nil
}

func validGitHubRepository(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		if strings.ContainsAny(part, " \\?#") {
			return false
		}
	}
	return true
}

func parseFormBool(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseFormInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
