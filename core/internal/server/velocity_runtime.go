package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
)

const (
	velocityRuntimeSettingKey = "velocity_runtime"
	// Frozen bootstrap ABI. Runtime feature work must keep this API and contract.
	velocityRuntimeAPIVersion        = 2
	velocityRuntimeContract          = "authman.velocity.runtime.v1"
	maxVelocityRuntimeBytes    int64 = 64 * 1024 * 1024
	maxVelocityRuntimeFiles          = 10000
	maxVelocityRuntimeExpanded       = 192 * 1024 * 1024
)

func (s *Server) handleAdminVelocityRuntimeReleases(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, s.velocityRuntimeCatalogData(r.Context()), nil)
}

func (s *Server) handleAdminUploadVelocityRuntimeRelease(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVelocityRuntimeBytes+(1<<20))
	if err := r.ParseMultipartForm(maxVelocityRuntimeBytes); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "velocity_runtime.upload_invalid", "invalid runtime JAR upload"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "velocity_runtime.file_required", "runtime JAR file is required"))
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxVelocityRuntimeBytes+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maxVelocityRuntimeBytes {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "velocity_runtime.file_invalid", "runtime JAR is invalid or too large"))
		return
	}
	metadata, err := inspectVelocityRuntimeJar(raw)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "velocity_runtime.validation_failed", err.Error()))
		return
	}

	release, found := findVelocityRuntimeReleaseBySHA(s.store.ListVelocityRuntimeReleases(r.Context()), metadata.SHA256)
	if !found {
		contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = "application/java-archive"
		}
		release, err = s.store.UpsertVelocityRuntimeRelease(r.Context(), store.VelocityRuntimeRelease{
			ID:          "velocity-runtime-" + metadata.SHA256[:20],
			Version:     metadata.Version,
			APIVersion:  metadata.APIVersion,
			Entrypoint:  metadata.Entrypoint,
			Filename:    safeVelocityRuntimeFilename(header.Filename),
			ContentType: contentType,
			SizeBytes:   int64(len(raw)),
			SHA256:      metadata.SHA256,
			Artifact:    raw,
		})
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "velocity_runtime.save_failed", "failed to save runtime release"))
			return
		}
	}
	if err := s.activateVelocityRuntimeRelease(r.Context(), release.ID); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "velocity_runtime.activate_failed", "failed to activate runtime release"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, release.ID, "velocity_runtime.upload", map[string]any{
		"version": release.Version,
		"sha256":  release.SHA256,
		"size":    release.SizeBytes,
	})
	s.pushAllNodeSync(r.Context(), "velocity_runtime.upload")
	api.WriteJSON(w, http.StatusCreated, s.velocityRuntimeCatalogData(r.Context()), nil)
}

func (s *Server) handleAdminActivateVelocityRuntimeRelease(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	release, err := s.store.GetVelocityRuntimeRelease(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "velocity_runtime.not_found", "runtime release not found"))
		return
	}
	if err := s.activateVelocityRuntimeRelease(r.Context(), release.ID); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "velocity_runtime.activate_failed", "failed to activate runtime release"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, release.ID, "velocity_runtime.activate", map[string]any{
		"version": release.Version,
		"sha256":  release.SHA256,
	})
	s.pushAllNodeSync(r.Context(), "velocity_runtime.activate")
	api.WriteJSON(w, http.StatusOK, s.velocityRuntimeCatalogData(r.Context()), nil)
}

func (s *Server) handleAdminDeleteVelocityRuntimeRelease(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	currentID := s.currentVelocityRuntimeReleaseID(r.Context())
	if id == currentID {
		api.WriteError(w, api.NewError(http.StatusConflict, "velocity_runtime.active", "active runtime release cannot be deleted"))
		return
	}
	if err := s.store.DeleteVelocityRuntimeRelease(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, api.NewError(http.StatusNotFound, "velocity_runtime.not_found", "runtime release not found"))
			return
		}
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "velocity_runtime.delete_failed", "failed to delete runtime release"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, id, "velocity_runtime.delete", nil)
	api.WriteJSON(w, http.StatusOK, s.velocityRuntimeCatalogData(r.Context()), nil)
}

func (s *Server) handleAdminVelocityNodeRuntime(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	n, apiErr := s.requireDownstreamNodeByID(r)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	api.WriteJSON(w, http.StatusOK, s.velocityRuntimeNodeData(r.Context(), n.ID), nil)
}

func (s *Server) handleNodeVelocityRuntime(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsDownstreamVelocity(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only downstream Velocity nodes can fetch runtime modules"))
		return
	}
	release, err := s.configuredVelocityRuntimeRelease(r.Context())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "velocity_runtime.unavailable", "no Velocity runtime release is configured"))
		return
	}
	writeVelocityRuntime(w, r, release)
}

func (s *Server) requireDownstreamNodeByID(r *http.Request) (node.Node, *api.Error) {
	n, err := s.nodes.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		return node.Node{}, api.NewError(http.StatusNotFound, "node.not_found", "node not found")
	}
	if !node.IsDownstreamVelocity(n.Mode) {
		return node.Node{}, api.NewError(http.StatusBadRequest, "node.kind_invalid", "node is not a downstream Velocity node")
	}
	return n, nil
}

func (s *Server) activateVelocityRuntimeRelease(ctx context.Context, id string) error {
	if _, err := s.store.GetVelocityRuntimeRelease(ctx, id); err != nil {
		return err
	}
	return s.store.SetSystemSetting(ctx, velocityRuntimeSettingKey, map[string]any{"release_id": strings.TrimSpace(id)})
}

func (s *Server) currentVelocityRuntimeReleaseID(ctx context.Context) string {
	setting, err := s.store.GetSystemSetting(ctx, velocityRuntimeSettingKey)
	if err != nil {
		return ""
	}
	id, _ := setting["release_id"].(string)
	return strings.TrimSpace(id)
}

func (s *Server) configuredVelocityRuntimeRelease(ctx context.Context) (store.VelocityRuntimeRelease, error) {
	id := s.currentVelocityRuntimeReleaseID(ctx)
	if id == "" {
		return store.VelocityRuntimeRelease{}, store.ErrNotFound
	}
	return s.store.GetVelocityRuntimeRelease(ctx, id)
}

func (s *Server) velocityRuntimeCatalogData(ctx context.Context) map[string]any {
	currentID := s.currentVelocityRuntimeReleaseID(ctx)
	releases := s.store.ListVelocityRuntimeReleases(ctx)
	rows := make([]map[string]any, 0, len(releases))
	for _, release := range releases {
		row := velocityRuntimeReleaseData(release)
		row["active"] = release.ID == currentID
		rows = append(rows, row)
	}
	return map[string]any{
		"current_release_id": currentID,
		"releases":           rows,
	}
}

func (s *Server) velocityRuntimeNodeData(ctx context.Context, nodeID string) map[string]any {
	data := map[string]any{
		"download_path": "/api/node/velocity/runtime-module",
		"configured":    nil,
		"active":        nil,
		"in_sync":       false,
	}
	release, err := s.configuredVelocityRuntimeRelease(ctx)
	if err == nil {
		data["configured"] = velocityRuntimeReleaseData(release)
	}
	status, statusErr := s.store.GetVelocityRuntimeStatus(ctx, nodeID)
	if statusErr == nil {
		data["active"] = velocityRuntimeStatusData(status)
		data["in_sync"] = err == nil && status.State == "ready" && strings.EqualFold(status.SHA256, release.SHA256)
	}
	return data
}

func velocityRuntimeReleaseData(release store.VelocityRuntimeRelease) map[string]any {
	return map[string]any{
		"id":           release.ID,
		"version":      release.Version,
		"api_version":  release.APIVersion,
		"entrypoint":   release.Entrypoint,
		"filename":     release.Filename,
		"content_type": release.ContentType,
		"size_bytes":   release.SizeBytes,
		"sha256":       release.SHA256,
		"created_at":   release.CreatedAt,
		"updated_at":   release.UpdatedAt,
	}
}

func velocityRuntimeStatusData(status store.VelocityRuntimeStatus) map[string]any {
	return map[string]any{
		"state":          status.State,
		"api_version":    status.APIVersion,
		"version":        status.Version,
		"sha256":         status.SHA256,
		"target_version": status.TargetVersion,
		"target_sha256":  status.TargetSHA256,
		"last_error":     status.LastError,
		"reported_at":    status.ReportedAt,
	}
}

func (s *Server) recordVelocityRuntimeStatus(ctx context.Context, n node.Node, report *nodeVelocityRuntimeReport, now time.Time) {
	if report == nil || !node.IsDownstreamVelocity(n.Mode) {
		return
	}
	status := store.VelocityRuntimeStatus{
		NodeID:        n.ID,
		State:         limitText(report.State, 32),
		APIVersion:    clampNonNegative(report.APIVersion),
		Version:       limitText(report.Version, 128),
		SHA256:        limitText(strings.ToLower(report.SHA256), 64),
		TargetVersion: limitText(report.TargetVersion, 128),
		TargetSHA256:  limitText(strings.ToLower(report.TargetSHA256), 64),
		LastError:     limitText(report.LastError, 2000),
		ReportedAt:    now.UTC(),
	}
	if status.State == "" {
		status.State = "not_loaded"
	}
	if _, err := s.store.UpsertVelocityRuntimeStatus(ctx, status); err != nil {
		s.logger.Warn("failed to record Velocity runtime status", "node_id", n.ID, "err", err)
	}
}

func writeVelocityRuntime(w http.ResponseWriter, r *http.Request, release store.VelocityRuntimeRelease) {
	etag := `"` + release.SHA256 + `"`
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/java-archive")
	w.Header().Set("Content-Length", strconv.Itoa(len(release.Artifact)))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeVelocityRuntimeFilename(release.Filename)}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(release.Artifact)
}

type velocityRuntimeJarMetadata struct {
	Version    string
	APIVersion int
	Entrypoint string
	SHA256     string
}

func inspectVelocityRuntimeJar(raw []byte) (velocityRuntimeJarMetadata, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime artifact is not a valid JAR")
	}
	if len(reader.File) == 0 || len(reader.File) > maxVelocityRuntimeFiles {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime JAR contains an invalid number of entries")
	}
	var manifestRaw []byte
	entries := make(map[string]struct{}, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		clean := strings.TrimPrefix(filepath.ToSlash(file.Name), "/")
		if clean == "" || strings.Contains(clean, "../") {
			return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime JAR contains an unsafe path")
		}
		if clean == "velocity-plugin.json" || clean == "META-INF/services/com.iroselle.authman.spi.AuthmanRuntimeModule" ||
			strings.HasPrefix(clean, "com/iroselle/authman/bootstrap/") || strings.HasPrefix(clean, "com/iroselle/authman/spi/") {
			return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime JAR contains bootstrap-owned classes or metadata")
		}
		expanded += file.UncompressedSize64
		if expanded > maxVelocityRuntimeExpanded {
			return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime JAR expands beyond the safety limit")
		}
		entries[clean] = struct{}{}
		if strings.EqualFold(clean, "META-INF/MANIFEST.MF") {
			stream, openErr := file.Open()
			if openErr != nil {
				return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime manifest cannot be opened")
			}
			manifestRaw, openErr = io.ReadAll(io.LimitReader(stream, 128*1024))
			_ = stream.Close()
			if openErr != nil {
				return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime manifest cannot be read")
			}
		}
	}
	if len(manifestRaw) == 0 {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime manifest is missing")
	}
	manifest := parseJarManifest(manifestRaw)
	version := strings.TrimSpace(manifest["Authman-Runtime-Version"])
	entrypoint := strings.TrimSpace(manifest["Authman-Runtime-Entrypoint"])
	apiVersion, _ := strconv.Atoi(strings.TrimSpace(manifest["Authman-Runtime-Api"]))
	contract := strings.TrimSpace(manifest["Authman-Runtime-Contract"])
	if version == "" || len(version) > 128 {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime version is missing or invalid")
	}
	if apiVersion != velocityRuntimeAPIVersion {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime API %d is not supported by Core API %d", apiVersion, velocityRuntimeAPIVersion)
	}
	if contract != velocityRuntimeContract {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime contract %q is not supported", contract)
	}
	if !validJavaClassName(entrypoint) {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime entrypoint is invalid")
	}
	if _, ok := entries[strings.ReplaceAll(entrypoint, ".", "/")+".class"]; !ok {
		return velocityRuntimeJarMetadata{}, fmt.Errorf("runtime entrypoint class is missing")
	}
	digest := sha256.Sum256(raw)
	return velocityRuntimeJarMetadata{
		Version:    version,
		APIVersion: apiVersion,
		Entrypoint: entrypoint,
		SHA256:     hex.EncodeToString(digest[:]),
	}, nil
}

func parseJarManifest(raw []byte) map[string]string {
	values := map[string]string{}
	var key string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, " ") && key != "" {
			values[key] += strings.TrimPrefix(line, " ")
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			key = ""
			continue
		}
		key = strings.TrimSpace(name)
		values[key] = strings.TrimSpace(value)
	}
	return values
}

func validJavaClassName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || !validJavaIdentifier(part) {
			return false
		}
	}
	return true
}

func validJavaIdentifier(value string) bool {
	for index, char := range value {
		if index == 0 {
			if !(char == '_' || char == '$' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z') {
				return false
			}
			continue
		}
		if !(char == '_' || char == '$' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9') {
			return false
		}
	}
	return value != ""
}

func findVelocityRuntimeReleaseBySHA(releases []store.VelocityRuntimeRelease, sha string) (store.VelocityRuntimeRelease, bool) {
	for _, release := range releases {
		if strings.EqualFold(release.SHA256, sha) {
			return release, true
		}
	}
	return store.VelocityRuntimeRelease{}, false
}

func safeVelocityRuntimeFilename(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	if value == "" {
		return "authman-runtime.jar"
	}
	if !strings.HasSuffix(strings.ToLower(value), ".jar") {
		value += ".jar"
	}
	if len(value) > 160 {
		value = value[:156] + ".jar"
	}
	return value
}

func limitText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
