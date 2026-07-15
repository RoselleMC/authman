package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
	protocolpack "github.com/RoselleMC/authman/limbo/protocol/pack"
)

func (s *Server) handleAdminGetLimboProtocolPack(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	n, apiErr := s.requireLimboPortalByID(r)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	api.WriteJSON(w, http.StatusOK, s.limboProtocolPackData(r.Context(), n.ID), nil)
}

func (s *Server) handleAdminUploadLimboProtocolPack(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	n, nodeErr := s.requireLimboPortalByID(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, protocolpack.MaxArchiveBytes+(1<<20))
	if err := r.ParseMultipartForm(protocolpack.MaxArchiveBytes); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_protocol_pack.upload_invalid", "invalid protocol pack upload"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_protocol_pack.file_required", "protocol ZIP file is required"))
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, protocolpack.MaxArchiveBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > protocolpack.MaxArchiveBytes {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_protocol_pack.file_invalid", "protocol pack is invalid or too large"))
		return
	}
	loaded, err := protocolpack.LoadZip(raw)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "limbo_protocol_pack.validation_failed", err.Error()))
		return
	}
	metadata := portalPackMetadata(loaded)
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/zip"
	}
	bundle, err := s.store.UpsertLimboProtocolBundle(r.Context(), store.LimboProtocolBundle{
		NodeID:            n.ID,
		Name:              metadata.Name,
		Version:           metadata.Version,
		Filename:          safeProtocolPackFilename(header.Filename),
		ContentType:       contentType,
		SizeBytes:         int64(len(raw)),
		SHA256:            metadata.SHA256,
		Protocols:         metadata.Protocols,
		MinecraftVersions: metadata.MinecraftVersions,
		Archive:           raw,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "limbo_protocol_pack.save_failed", "failed to save protocol pack"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, n.ID, "limbo_protocol_pack.upload", map[string]any{
		"name":               bundle.Name,
		"version":            bundle.Version,
		"sha256":             bundle.SHA256,
		"minecraft_versions": bundle.MinecraftVersions,
	})
	s.pushNodeSync(r.Context(), n, "limbo_protocol_pack.upload")
	api.WriteJSON(w, http.StatusOK, s.limboProtocolPackData(r.Context(), n.ID), nil)
}

func (s *Server) handleAdminDeleteLimboProtocolPack(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	n, nodeErr := s.requireLimboPortalByID(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if err := s.store.DeleteLimboProtocolBundle(r.Context(), n.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "limbo_protocol_pack.delete_failed", "failed to reset protocol pack"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, n.ID, "limbo_protocol_pack.reset", nil)
	s.pushNodeSync(r.Context(), n, "limbo_protocol_pack.reset")
	api.WriteJSON(w, http.StatusOK, s.limboProtocolPackData(r.Context(), n.ID), nil)
}

func (s *Server) handleAdminDownloadLimboProtocolPack(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	n, apiErr := s.requireLimboPortalByID(r)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	bundle, _, err := s.configuredLimboProtocolBundle(r.Context(), n.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "limbo_protocol_pack.unavailable", "protocol pack is unavailable"))
		return
	}
	writeProtocolPack(w, r, bundle)
}

func (s *Server) handleNodeLimboProtocolPack(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo nodes can fetch protocol packs"))
		return
	}
	bundle, _, err := s.configuredLimboProtocolBundle(r.Context(), n.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "limbo_protocol_pack.unavailable", "protocol pack is unavailable"))
		return
	}
	writeProtocolPack(w, r, bundle)
}

func (s *Server) requireLimboPortalByID(r *http.Request) (node.Node, *api.Error) {
	n, err := s.nodes.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		return node.Node{}, api.NewError(http.StatusNotFound, "node.not_found", "node not found")
	}
	if !node.IsLimboPortal(n.Mode) {
		return node.Node{}, api.NewError(http.StatusBadRequest, "node.kind_invalid", "node is not a limbo portal")
	}
	return n, nil
}

func (s *Server) configuredLimboProtocolBundle(ctx context.Context, nodeID string) (store.LimboProtocolBundle, string, error) {
	bundle, err := s.store.GetLimboProtocolBundle(ctx, nodeID)
	if err == nil {
		return bundle, "custom", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.LimboProtocolBundle{}, "", err
	}
	loaded, err := protocolpack.Default()
	if err != nil {
		return store.LimboProtocolBundle{}, "", err
	}
	metadata := portalPackMetadata(loaded)
	raw := protocolpack.DefaultZip()
	return store.LimboProtocolBundle{
		NodeID:            nodeID,
		Name:              metadata.Name,
		Version:           metadata.Version,
		Filename:          "authman-protocols.zip",
		ContentType:       "application/zip",
		SizeBytes:         int64(len(raw)),
		SHA256:            metadata.SHA256,
		Protocols:         metadata.Protocols,
		MinecraftVersions: metadata.MinecraftVersions,
		Archive:           raw,
	}, "builtin", nil
}

func portalPackMetadata(loaded *protocolpack.Pack) protocolpack.Metadata {
	metadata := loaded.Metadata()
	metadata.Protocols = metadata.Protocols[:0]
	metadata.MinecraftVersions = metadata.MinecraftVersions[:0]
	for _, descriptor := range loaded.Manifest().Protocols {
		if !descriptor.Layout.PortalDialog {
			continue
		}
		metadata.Protocols = append(metadata.Protocols, descriptor.Protocol)
		metadata.MinecraftVersions = append(metadata.MinecraftVersions, descriptor.MinecraftVersions...)
	}
	return metadata
}

func (s *Server) limboProtocolPackData(ctx context.Context, nodeID string) map[string]any {
	bundle, source, err := s.configuredLimboProtocolBundle(ctx, nodeID)
	if err != nil {
		return map[string]any{"source": "unavailable", "error": err.Error()}
	}
	configured := limboProtocolBundleMetadata(bundle, source)
	data := map[string]any{
		"source":        source,
		"configured":    configured,
		"download_path": "/api/node/limbo/protocol-pack",
	}
	status, statusErr := s.store.GetLimboProtocolStatus(ctx, nodeID)
	if statusErr == nil {
		data["active"] = limboProtocolStatusData(status)
		data["in_sync"] = status.LastError == "" && status.SHA256 == bundle.SHA256
	} else {
		data["active"] = nil
		data["in_sync"] = false
	}
	return data
}

func limboProtocolBundleMetadata(bundle store.LimboProtocolBundle, source string) map[string]any {
	return map[string]any{
		"source":             source,
		"name":               bundle.Name,
		"version":            bundle.Version,
		"filename":           bundle.Filename,
		"content_type":       bundle.ContentType,
		"size_bytes":         bundle.SizeBytes,
		"sha256":             bundle.SHA256,
		"protocols":          bundle.Protocols,
		"minecraft_versions": bundle.MinecraftVersions,
		"created_at":         bundle.CreatedAt,
		"updated_at":         bundle.UpdatedAt,
	}
}

func limboProtocolStatusData(status store.LimboProtocolStatus) map[string]any {
	return map[string]any{
		"name":               status.Name,
		"version":            status.Version,
		"sha256":             status.SHA256,
		"protocols":          status.Protocols,
		"minecraft_versions": status.MinecraftVersions,
		"last_error":         status.LastError,
		"reported_at":        status.ReportedAt,
	}
}

func writeProtocolPack(w http.ResponseWriter, r *http.Request, bundle store.LimboProtocolBundle) {
	etag := `"` + bundle.SHA256 + `"`
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bundle.Archive)))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeProtocolPackFilename(bundle.Filename)}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bundle.Archive)
}

func safeProtocolPackFilename(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	if value == "" {
		return "authman-protocols.zip"
	}
	if !strings.HasSuffix(strings.ToLower(value), ".zip") {
		value += ".zip"
	}
	if len(value) > 160 {
		value = value[:156] + ".zip"
	}
	return value
}

func (s *Server) recordLimboProtocolStatus(ctx context.Context, n node.Node, report *nodeProtocolPackReport, now time.Time) {
	if report == nil || !node.IsLimboPortal(n.Mode) {
		return
	}
	protocols := append([]int32(nil), report.Protocols...)
	if len(protocols) > 128 {
		protocols = protocols[:128]
	}
	versions := append([]string(nil), report.MinecraftVersions...)
	if len(versions) > 256 {
		versions = versions[:256]
	}
	for i := range versions {
		versions[i] = truncateString(strings.TrimSpace(versions[i]), 40)
	}
	_, err := s.store.UpsertLimboProtocolStatus(ctx, store.LimboProtocolStatus{
		NodeID:            n.ID,
		Name:              truncateString(strings.TrimSpace(report.Name), 80),
		Version:           truncateString(strings.TrimSpace(report.Version), 80),
		SHA256:            truncateString(strings.TrimSpace(report.SHA256), 64),
		Protocols:         protocols,
		MinecraftVersions: versions,
		LastError:         truncateString(strings.TrimSpace(report.LastError), 1000),
		ReportedAt:        now.UTC(),
	})
	if err != nil {
		s.logger.Warn("failed to record limbo protocol status", "node_id", n.ID, "err", err)
	}
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
