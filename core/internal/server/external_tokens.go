package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/store"
)

type externalTokenCreateRequest struct {
	Name string `json:"name"`
}

type externalTokenUpdateRequest struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (s *Server) withExternalAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.requireExternalAPI(r); err != nil {
			api.WriteError(w, err)
			return
		}
		next(w, r)
	}
}

func (s *Server) withExternalAPIOrAdminSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.requireAdmin(r, false); err == nil {
			next(w, r)
			return
		}
		if _, err := s.requireExternalAPI(r); err != nil {
			api.WriteError(w, err)
			return
		}
		next(w, r)
	}
}

func (s *Server) withAdminSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.requireAdmin(r, false); err != nil {
			api.WriteError(w, err)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireExternalAPI(r *http.Request) (store.ExternalAPIToken, *api.Error) {
	token, _ := bearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Authman-External-Token"))
	}
	if token == "" {
		return store.ExternalAPIToken{}, api.NewError(http.StatusUnauthorized, "external_api.token_required", "external API token is required")
	}
	authorized, err := s.store.AuthenticateExternalAPIToken(r.Context(), token, time.Now(), clientIP(r), r.URL.Path)
	if err != nil {
		return store.ExternalAPIToken{}, api.NewError(http.StatusUnauthorized, "external_api.token_invalid", "external API token is invalid or disabled")
	}
	return authorized, nil
}

func (s *Server) handleAdminExternalTokens(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdminPermission(r, false, "external_api.read"); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	status := strings.TrimSpace(query.Get("status"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	rows := make([]map[string]any, 0)
	for _, token := range s.store.ListExternalAPITokens(r.Context()) {
		if status != "" && string(token.Status) != status {
			continue
		}
		if search != "" && !containsFold(token.Name, search) && !containsFold(token.TokenFingerprint, search) && !containsFold(token.LastUsedIP, search) && !containsFold(token.LastUsedPath, search) {
			continue
		}
		rows = append(rows, externalTokenData(token))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		cmp := compareMapListValues(rows[i], rows[j], sortKey)
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(rows), params)
	data := append([]map[string]any(nil), rows[start:end]...)
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(rows), params))
}

func (s *Server) handleAdminExternalTokenDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdminPermission(r, false, "external_api.read"); err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	token, storeErr := s.store.GetExternalAPIToken(r.Context(), id)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "external_api.token_not_found", "external API token not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, externalTokenData(token), nil)
}

func (s *Server) handleAdminCreateExternalToken(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "external_api.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req externalTokenCreateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "external_api.name_required", "token name is required"))
		return
	}
	raw, tokenErr := auth.NewOpaqueToken(32)
	if tokenErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "external_api.token_create_failed", "failed to generate token"))
		return
	}
	created, storeErr := s.store.CreateExternalAPIToken(r.Context(), store.ExternalAPIToken{
		Name:             name,
		TokenHash:        auth.HashToken("external-api", raw),
		TokenFingerprint: auth.TokenFingerprint(raw),
		Status:           store.ExternalAPITokenActive,
		CreatedBy:        session.SubjectID,
	})
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "external_api.token_create_failed", storeErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, created.ID, "external_api.token.create", map[string]any{
		"name":              created.Name,
		"token_fingerprint": created.TokenFingerprint,
	})
	data := externalTokenData(created)
	data["token_once"] = raw
	api.WriteJSON(w, http.StatusCreated, data, nil)
}

func (s *Server) handleAdminUpdateExternalToken(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "external_api.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	existing, storeErr := s.store.GetExternalAPIToken(r.Context(), id)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "external_api.token_not_found", "external API token not found"))
		return
	}
	var req externalTokenUpdateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	status := normalizeExternalTokenStatus(req.Status, existing.Status)
	updated, updateErr := s.store.UpdateExternalAPIToken(r.Context(), store.ExternalAPIToken{
		ID:     existing.ID,
		Name:   name,
		Status: status,
	})
	if updateErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "external_api.token_update_failed", updateErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, updated.ID, "external_api.token.update", map[string]any{
		"name":   updated.Name,
		"status": updated.Status,
	})
	api.WriteJSON(w, http.StatusOK, externalTokenData(updated), nil)
}

func (s *Server) handleAdminRevokeExternalToken(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "external_api.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	existing, storeErr := s.store.GetExternalAPIToken(r.Context(), id)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "external_api.token_not_found", "external API token not found"))
		return
	}
	existing.Status = store.ExternalAPITokenRevoked
	updated, updateErr := s.store.UpdateExternalAPIToken(r.Context(), existing)
	if updateErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "external_api.token_update_failed", updateErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, updated.ID, "external_api.token.revoke", map[string]any{
		"name":              updated.Name,
		"token_fingerprint": updated.TokenFingerprint,
	})
	api.WriteJSON(w, http.StatusOK, externalTokenData(updated), nil)
}

func (s *Server) handleAdminDeleteExternalTokenRecord(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "external_api.delete")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	existing, storeErr := s.store.GetExternalAPIToken(r.Context(), id)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "external_api.token_not_found", "external API token not found"))
		return
	}
	if existing.Status != store.ExternalAPITokenRevoked {
		api.WriteError(w, api.NewError(http.StatusConflict, "external_api.token_not_revoked", "external API token must be revoked before it can be deleted"))
		return
	}
	if deleteErr := s.store.DeleteExternalAPIToken(r.Context(), id); deleteErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "external_api.token_delete_failed", deleteErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, existing.ID, "external_api.token.delete", map[string]any{
		"name":              existing.Name,
		"token_fingerprint": existing.TokenFingerprint,
	})
	api.WriteJSON(w, http.StatusOK, externalTokenData(existing), nil)
}

func normalizeExternalTokenStatus(raw string, fallback store.ExternalAPITokenStatus) store.ExternalAPITokenStatus {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(store.ExternalAPITokenActive):
		return store.ExternalAPITokenActive
	case string(store.ExternalAPITokenDisabled):
		return store.ExternalAPITokenDisabled
	case string(store.ExternalAPITokenRevoked):
		return store.ExternalAPITokenRevoked
	default:
		if fallback == "" {
			return store.ExternalAPITokenActive
		}
		return fallback
	}
}

func externalTokenData(token store.ExternalAPIToken) map[string]any {
	return map[string]any{
		"id":                token.ID,
		"name":              token.Name,
		"token_fingerprint": token.TokenFingerprint,
		"status":            token.Status,
		"created_by":        token.CreatedBy,
		"call_count":        token.CallCount,
		"last_used_at":      token.LastUsedAt,
		"last_used_ip":      token.LastUsedIP,
		"last_used_path":    token.LastUsedPath,
		"created_at":        token.CreatedAt,
		"updated_at":        token.UpdatedAt,
	}
}
