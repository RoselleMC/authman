package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/mojang"
	"github.com/RoselleMC/authman/internal/rbac"
	"github.com/RoselleMC/authman/internal/store"
)

type adminLoginRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type mojangRouteRequest struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Weight   int    `json:"weight"`
	Disabled bool   `json:"disabled"`
}

type downstreamServerRequest struct {
	ID                 string         `json:"id"`
	Slug               string         `json:"slug"`
	DisplayName        string         `json:"display_name"`
	Status             string         `json:"status"`
	RegistrationOpen   bool           `json:"registration_open"`
	PortalTheme        map[string]any `json:"portal_theme"`
	PortalConfig       map[string]any `json:"portal_config"`
	ExtensionProviders []string       `json:"extension_providers"`
}

type adminRoleUpdateRequest struct {
	Alias       string   `json:"alias"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type adminRoleCreateRequest struct {
	RoleID      string   `json:"role_id"`
	ID          string   `json:"id"`
	Alias       string   `json:"alias"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type adminUserCreateRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type adminUserUpdateRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

func (s *Server) handleAdminBootstrap(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"configured":   s.adminConfigured(),
		"username":     s.cfg.AdminUsername,
		"email":        s.cfg.AdminEmail,
		"owner_exists": s.adminConfigured(),
	}, nil)
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req adminLoginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	identifier := strings.TrimSpace(req.Username)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}
	subjectID := "bootstrap-admin"
	user := s.adminData(r.Context())
	if s.verifyAdminPassword(r.Context(), identifier, req.Password) {
		// Bootstrap owner.
	} else {
		adminUser, err := s.store.FindAdminUserByIdentifier(r.Context(), identifier)
		if err != nil || adminUser.Status != "active" {
			api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
			return
		}
		ok, verifyErr := auth.VerifyPassword(req.Password, adminUser.PasswordHash)
		if verifyErr != nil || !ok {
			api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
			return
		}
		subjectID = adminUser.ID
		user = s.adminUserData(r.Context(), adminUser)
	}
	if identifier == "" {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	if s.maybeStartAdminMFA(w, r, subjectID, user) {
		return
	}
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionAdmin, subjectID, 12*time.Hour, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	if err := s.saveSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to save session"))
		return
	}
	setSessionCookie(w, r, adminSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorAdmin, subjectID, audit.TargetPortalSession, session.ID, "admin.session.login", map[string]any{
		"identifier": identifier,
		"username":   user["username"],
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"admin":      user,
		"user":       user,
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, false)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	csrf, csrfErr := s.rotateCSRF(r.Context(), session)
	if csrfErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to refresh CSRF token"))
		return
	}
	user, userErr := s.adminDataForSession(r.Context(), session.SubjectID)
	if userErr != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "admin user no longer exists"))
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"admin":      user,
		"user":       user,
		"csrf_token": csrf,
	}, nil)
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if session, err := s.requireAdmin(r, true); err == nil {
		if cookie, cookieErr := r.Cookie(adminSessionCookie); cookieErr == nil {
			s.deleteSession(r.Context(), cookie.Value)
		}
		s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPortalSession, session.ID, "admin.session.logout", nil)
	}
	clearSessionCookie(w, r, adminSessionCookie)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminPlayerDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	events := s.store.ListAuditEvents(r.Context(), 20)
	eventData := make([]map[string]any, 0, len(events))
	for _, event := range events {
		eventData = append(eventData, auditEventSummaryData(event))
	}
	data := playerDetailData(player, eventData)
	if player.Kind == identity.PlayerKindOffline {
		_, credential, err := s.store.GetOfflineCredential(r.Context(), player.RawOfflineName)
		if err == nil {
			data["offline_credentials"] = offlineCredentialData(credential)
		}
	}
	data["extension_data"] = s.playerExtensionData(r.Context(), player, "", true)
	api.WriteJSON(w, http.StatusOK, data, nil)
}

type updatePlayerRequest struct {
	Locked *bool `json:"locked"`
}

func (s *Server) handleAdminUpdatePlayer(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req updatePlayerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if req.Locked == nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.no_update", "no supported player fields were provided"))
		return
	}
	player, err := s.store.SetPlayerLocked(r.Context(), r.PathValue("id"), *req.Locked)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	eventType := "player.unlock"
	if player.Locked {
		eventType = "player.lock"
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, player.ID, eventType, map[string]any{
		"locked": player.Locked,
	})
	api.WriteJSON(w, http.StatusOK, nil, nil)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleAdminLockPlayer(w http.ResponseWriter, r *http.Request) {
	s.handleAdminSetPlayerLocked(w, r, true)
}

func (s *Server) handleAdminUnlockPlayer(w http.ResponseWriter, r *http.Request) {
	s.handleAdminSetPlayerLocked(w, r, false)
}

func (s *Server) handleAdminSetPlayerLocked(w http.ResponseWriter, r *http.Request, locked bool) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	player, err := s.store.SetPlayerLocked(r.Context(), r.PathValue("id"), locked)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	eventType := "player.unlock"
	if player.Locked {
		eventType = "player.lock"
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, player.ID, eventType, map[string]any{"locked": player.Locked})
	api.WriteJSON(w, http.StatusOK, nil, nil)
}

func (s *Server) handleAdminResetOfflinePassword(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	if player.Kind != identity.PlayerKindOffline {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.not_offline", "password reset is only available for offline players"))
		return
	}
	var req resetPasswordRequest
	if r.ContentLength != 0 {
		if err := api.DecodeJSON(r, &req); err != nil {
			api.WriteError(w, err)
			return
		}
	}
	if req.Password == "" {
		req.Password = "temporary reset password 123"
	}
	passwordHash, err := auth.HashPassword(req.Password, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	if err := s.store.UpdateOfflinePassword(r.Context(), player.ID, passwordHash); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "auth.credential_not_found", "offline credential not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, player.ID, "offline.password.reset", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"reset_token_hint": "temporary-password-set"}, nil)
}

func (s *Server) handleAdminAuditEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	query := r.URL.Query()
	actorType := strings.TrimSpace(query.Get("actor_type"))
	targetType := strings.TrimSpace(query.Get("target_type"))
	eventType := strings.TrimSpace(query.Get("event_type"))
	since, hasSince := parseOptionalRFC3339(query.Get("since"))
	until, hasUntil := parseOptionalRFC3339(query.Get("until"))

	fetchLimit := params.Page * params.PageSize
	if fetchLimit < defaultListPageSize {
		fetchLimit = defaultListPageSize
	}
	if fetchLimit < 100 {
		fetchLimit = 100
	}
	if fetchLimit > maxAuditListFetch {
		fetchLimit = maxAuditListFetch
	}
	events := s.store.ListAuditEvents(r.Context(), fetchLimit)
	filtered := make([]audit.Event, 0, len(events))
	for _, event := range events {
		if actorType != "" && string(event.ActorType) != actorType {
			continue
		}
		if targetType != "" && string(event.Target) != targetType {
			continue
		}
		if eventType != "" && !containsFold(event.Type, eventType) {
			continue
		}
		if hasSince && event.Occurred.Before(since) {
			continue
		}
		if hasUntil && event.Occurred.After(until) {
			continue
		}
		filtered = append(filtered, event)
	}

	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, event := range filtered[start:end] {
		data = append(data, auditEventData(event))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
}

func (s *Server) handleAdminMojangRoutes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	now := time.Now().UTC()
	routeData := []map[string]any{}
	overall := "mojang_disabled"
	if s.mojangVerifier != nil {
		routes := s.mojangVerifier.RoutesSnapshot()
		healthy := 0
		for _, route := range routes {
			state := string(route.State)
			if state == "" {
				state = "healthy"
			}
			cooldown := int64(0)
			if route.CooldownUntil.After(now) {
				cooldown = int64(route.CooldownUntil.Sub(now).Seconds())
			}
			if route.Disabled {
				state = string(mojang.RouteDisabled)
			}
			if !route.Disabled && (state == "healthy" || cooldown == 0) {
				healthy++
			}
			routeData = append(routeData, map[string]any{
				"id":                         route.ID,
				"kind":                       route.Kind,
				"state":                      state,
				"url_masked":                 maskRouteURL(route),
				"weight":                     route.Weight,
				"failure_count":              route.FailureCount,
				"rate_limit_count":           route.RateLimitCount,
				"cooldown_remaining_seconds": cooldown,
				"last_error":                 route.LastFailureError,
			})
		}
		switch {
		case len(routes) == 0:
			overall = "mojang_disabled"
		case healthy == len(routes):
			overall = "mojang_healthy"
		case healthy > 0:
			overall = "mojang_degraded"
		default:
			overall = "mojang_unavailable"
		}
	}
	events := []map[string]any{}
	if s.mojangVerifier != nil {
		for _, event := range s.mojangVerifier.EventsSnapshot() {
			events = append(events, mojangEventData(event))
		}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"overall": overall,
		"proxies": routeData,
		"cache":   s.mojangCacheSnapshot(),
		"events":  events,
	}, nil)
}

func (s *Server) handleAdminCreateMojangRoute(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req mojangRouteRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	route, err := routeFromRequest(req)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	route, storeErr := s.store.UpsertMojangRoute(r.Context(), route)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "mojang.route_save_failed", "failed to save Mojang route"))
		return
	}
	s.reloadMojangRoutes(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetMojangProxy, route.ID, "mojang.route.upsert", map[string]any{
		"kind":     route.Kind,
		"weight":   route.Weight,
		"disabled": route.Disabled,
	})
	api.WriteJSON(w, http.StatusCreated, mojangRouteData(route, time.Now().UTC()), nil)
}

func (s *Server) handleAdminDeleteMojangRoute(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || id == "direct" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "mojang.route_invalid", "route cannot be deleted"))
		return
	}
	if err := s.store.DeleteMojangRoute(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "mojang.route_not_found", "Mojang route was not found"))
		return
	}
	s.reloadMojangRoutes(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetMojangProxy, id, "mojang.route.delete", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) mojangCacheSnapshot() map[string]int {
	if s.mojangVerifier == nil {
		return map[string]int{"fresh": 0, "stale": 0, "expired": 0}
	}
	return s.mojangVerifier.CacheSnapshot()
}

func maskRouteURL(route mojang.Route) string {
	if route.URL == "" {
		return "direct"
	}
	parsed, err := url.Parse(route.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "configured"
	}
	parsed.User = nil
	return parsed.String()
}

func routeFromRequest(req mojangRouteRequest) (mojang.Route, *api.Error) {
	kind := mojang.RouteKind(strings.ToLower(strings.TrimSpace(req.Kind)))
	if kind != mojang.RouteHTTP && kind != mojang.RouteSOCKS5 {
		return mojang.Route{}, api.NewError(http.StatusBadRequest, "mojang.route_invalid_kind", "route kind must be http or socks5")
	}
	routeURL := strings.TrimSpace(req.URL)
	if routeURL == "" {
		return mojang.Route{}, api.NewError(http.StatusBadRequest, "mojang.route_url_required", "route URL is required")
	}
	if err := validateRouteURL(kind, routeURL); err != nil {
		return mojang.Route{}, err
	}
	weight := req.Weight
	if weight <= 0 {
		weight = 1
	}
	if weight > 100 {
		weight = 100
	}
	id := sanitizeRouteID(req.ID)
	if id == "" {
		id = fmt.Sprintf("%s-%d", kind, time.Now().UTC().UnixNano())
	}
	return mojang.Route{
		ID:       id,
		Kind:     kind,
		URL:      routeURL,
		Weight:   weight,
		Disabled: req.Disabled,
	}, nil
}

func validateRouteURL(kind mojang.RouteKind, raw string) *api.Error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return api.NewError(http.StatusBadRequest, "mojang.route_url_invalid", "route URL is invalid")
	}
	switch kind {
	case mojang.RouteHTTP:
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return api.NewError(http.StatusBadRequest, "mojang.route_url_invalid", "HTTP proxy URL must use http or https")
		}
	case mojang.RouteSOCKS5:
		if parsed.Scheme != "socks5" {
			return api.NewError(http.StatusBadRequest, "mojang.route_url_invalid", "SOCKS5 proxy URL must use socks5")
		}
	}
	return nil
}

func sanitizeRouteID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() > 64 {
		return b.String()[:64]
	}
	return b.String()
}

func mojangRouteData(route mojang.Route, now time.Time) map[string]any {
	state := string(route.State)
	if state == "" {
		state = "healthy"
	}
	if route.Disabled {
		state = string(mojang.RouteDisabled)
	}
	cooldown := int64(0)
	if route.CooldownUntil.After(now) {
		cooldown = int64(route.CooldownUntil.Sub(now).Seconds())
	}
	return map[string]any{
		"id":                         route.ID,
		"kind":                       route.Kind,
		"state":                      state,
		"url_masked":                 maskRouteURL(route),
		"weight":                     route.Weight,
		"failure_count":              route.FailureCount,
		"rate_limit_count":           route.RateLimitCount,
		"cooldown_remaining_seconds": cooldown,
		"last_error":                 route.LastFailureError,
	}
}

func mojangEventData(event mojang.Event) map[string]any {
	retryAfter := ""
	if event.RetryAfter > 0 {
		retryAfter = event.RetryAfter.String()
	}
	return map[string]any{
		"id":          event.ID,
		"proxy_id":    event.ProxyID,
		"event_type":  event.EventType,
		"retry_after": retryAfter,
		"error":       event.Error,
		"created_at":  event.CreatedAt,
	}
}

func (s *Server) handleAdminDownstreamServers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	servers := s.store.ListDownstreamServers(r.Context())
	data := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		data = append(data, downstreamServerData(server))
	}
	api.WriteJSON(w, http.StatusOK, data, map[string]any{"count": len(data)})
}

func (s *Server) handleAdminDownstreamServerDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	server, err := s.store.GetDownstreamServer(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, downstreamServerData(server), nil)
}

func (s *Server) handleAdminCreateDownstreamServer(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req downstreamServerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	server, apiErr := downstreamServerFromRequest(req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	server, err := s.store.UpsertDownstreamServer(r.Context(), server)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, server.ID, "server.upsert", map[string]any{"slug": server.Slug})
	api.WriteJSON(w, http.StatusCreated, downstreamServerData(server), nil)
}

func (s *Server) handleAdminUpdateDownstreamServer(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.store.GetDownstreamServer(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	var req downstreamServerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	req.ID = id
	server, apiErr := downstreamServerFromRequest(req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	server, err := s.store.UpsertDownstreamServer(r.Context(), server)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, server.ID, "server.upsert", map[string]any{"slug": server.Slug})
	api.WriteJSON(w, http.StatusOK, downstreamServerData(server), nil)
}

func (s *Server) handleAdminDeleteDownstreamServer(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || id == "default" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.delete_default", "default server cannot be deleted"))
		return
	}
	if err := s.store.DeleteDownstreamServer(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, id, "server.delete", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func downstreamServerFromRequest(req downstreamServerRequest) (store.DownstreamServer, *api.Error) {
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if slug == "" {
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.slug_required", "server slug is required")
	}
	if !validSlug(slug) {
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.slug_invalid", "server slug is invalid")
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.display_name_required", "display name is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	switch status {
	case "active", "hidden", "disabled":
	default:
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.status_invalid", "server status is invalid")
	}
	return store.DownstreamServer{
		ID:                 strings.TrimSpace(req.ID),
		Slug:               slug,
		DisplayName:        name,
		Status:             status,
		RegistrationOpen:   req.RegistrationOpen,
		PortalTheme:        req.PortalTheme,
		PortalConfig:       req.PortalConfig,
		ExtensionProviders: req.ExtensionProviders,
	}, nil
}

func validSlug(slug string) bool {
	if len(slug) < 2 || len(slug) > 64 {
		return false
	}
	for _, ch := range slug {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) handleAdminExtensions(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	extensions := s.extensions.Entries(r.Context())
	api.WriteJSON(w, http.StatusOK, extensions, map[string]any{"count": len(extensions)})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	players := s.store.ListPlayers(r.Context())
	offline := 0
	premium := 0
	for _, player := range players {
		if player.Kind == identity.PlayerKindOffline {
			offline++
		} else {
			premium++
		}
	}
	events := s.store.ListAuditEvents(r.Context(), 5)
	summaries := make([]map[string]any, 0, len(events))
	for _, event := range events {
		summaries = append(summaries, auditEventSummaryData(event))
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"total_players":                 len(players),
		"premium_players":               premium,
		"offline_players":               offline,
		"recent_offline_login_failures": 0,
		"active_nodes":                  len(s.nodes.List(r.Context())),
		"mojang_status":                 "partial",
		"audit_events":                  summaries,
	}, nil)
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdminPermission(r, false, "admin.users.read"); err != nil {
		api.WriteError(w, err)
		return
	}
	users := []map[string]any{s.adminDataWithSecurity(r.Context())}
	for _, user := range s.store.ListAdminUsers(r.Context()) {
		users = append(users, s.adminUserDataWithSecurity(r.Context(), user))
	}
	api.WriteJSON(w, http.StatusOK, users, map[string]any{"count": len(users)})
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.users.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminUserCreateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.username_required", "username is required"))
		return
	}
	email := strings.TrimSpace(req.Email)
	if s.adminIdentifierMatches(r.Context(), username) || (email != "" && s.adminIdentifierMatches(r.Context(), email)) {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.user_create_failed", "admin user already exists"))
		return
	}
	roleID := rbac.RoleID(req.Role)
	if roleID == "" {
		roleID = "admin"
	}
	if roleID == "owner" && !s.adminSessionIsOwner(r.Context(), session.SubjectID) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "admin.owner_protected", "only owners can assign owner role"))
		return
	}
	if _, ok := s.findAdminRole(r.Context(), roleID); !ok {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.role_invalid", "role is invalid"))
		return
	}
	passwordHash, hashErr := auth.HashPassword(req.Password, s.passwordParams)
	if hashErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_too_weak", "password is too weak"))
		return
	}
	user, storeErr := s.store.CreateAdminUser(r.Context(), store.AdminUser{
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         roleID,
		Status:       "active",
	})
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.user_create_failed", storeErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, user.ID, "admin.user.create", map[string]any{
		"username": user.Username,
		"role":     user.Role,
	})
	api.WriteJSON(w, http.StatusCreated, s.adminUserData(r.Context(), user), nil)
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.users.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	targetID := strings.TrimSpace(r.PathValue("id"))
	if targetID == "" {
		api.WriteError(w, api.NewError(http.StatusNotFound, "admin.user_not_found", "admin user not found"))
		return
	}
	var req adminUserUpdateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.username_required", "username is required"))
		return
	}
	email := strings.TrimSpace(req.Email)
	roleID := rbac.RoleID(req.Role)
	if roleID == "" {
		roleID = "admin"
	}
	if _, ok := s.findAdminRole(r.Context(), roleID); !ok {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.role_invalid", "role is invalid"))
		return
	}
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "disabled" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.status_invalid", "status is invalid"))
		return
	}
	actorIsOwner := s.adminSessionIsOwner(r.Context(), session.SubjectID)
	if targetID == "bootstrap-admin" {
		if !actorIsOwner {
			api.WriteError(w, api.NewError(http.StatusForbidden, "admin.owner_protected", "only owners can edit owner users"))
			return
		}
		if roleID != "owner" || status != "active" {
			api.WriteError(w, api.NewError(http.StatusForbidden, "admin.bootstrap_locked", "bootstrap owner role and status cannot be changed"))
			return
		}
		for _, user := range s.store.ListAdminUsers(r.Context()) {
			if strings.EqualFold(user.Username, username) || (email != "" && strings.EqualFold(user.Email, email)) {
				api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", "username or email is already in use"))
				return
			}
		}
		profile := store.AdminProfile{AdminID: "bootstrap-admin"}
		if existingProfile, profileErr := s.store.GetAdminProfile(r.Context(), "bootstrap-admin"); profileErr == nil {
			profile = existingProfile
		}
		profile.Username = username
		profile.Email = email
		if _, profileErr := s.store.UpsertAdminProfile(r.Context(), store.AdminProfile{
			AdminID:   profile.AdminID,
			Username:  profile.Username,
			Email:     profile.Email,
			AvatarURL: profile.AvatarURL,
		}); profileErr != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save admin profile"))
			return
		}
		s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, targetID, "admin.user.update", map[string]any{
			"username": username,
			"role":     roleID,
			"status":   status,
		})
		api.WriteJSON(w, http.StatusOK, s.adminDataWithSecurity(r.Context()), nil)
		return
	}
	existing, getErr := s.store.GetAdminUser(r.Context(), targetID)
	if getErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "admin.user_not_found", "admin user not found"))
		return
	}
	if (existing.Role == "owner" || roleID == "owner") && !actorIsOwner {
		api.WriteError(w, api.NewError(http.StatusForbidden, "admin.owner_protected", "only owners can edit owner users"))
		return
	}
	if session.SubjectID == targetID && (status != "active" || roleID != existing.Role) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "admin.self_lockout", "cannot change your own role or disable your own account"))
		return
	}
	if s.adminIdentifierMatches(r.Context(), username) || (email != "" && s.adminIdentifierMatches(r.Context(), email)) {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", "username or email is already in use"))
		return
	}
	updated, updateErr := s.store.UpdateAdminUser(r.Context(), store.AdminUser{
		ID:       targetID,
		Username: username,
		Email:    email,
		Role:     roleID,
		Status:   status,
	})
	if updateErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", updateErr.Error()))
		return
	}
	profile := store.AdminProfile{AdminID: updated.ID}
	if existingProfile, profileErr := s.store.GetAdminProfile(r.Context(), updated.ID); profileErr == nil {
		profile = existingProfile
	}
	profile.Username = updated.Username
	profile.Email = updated.Email
	if _, profileErr := s.store.UpsertAdminProfile(r.Context(), store.AdminProfile{
		AdminID:   profile.AdminID,
		Username:  profile.Username,
		Email:     profile.Email,
		AvatarURL: profile.AvatarURL,
	}); profileErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save admin profile"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, updated.ID, "admin.user.update", map[string]any{
		"username": updated.Username,
		"role":     updated.Role,
		"status":   updated.Status,
	})
	api.WriteJSON(w, http.StatusOK, s.adminUserDataWithSecurity(r.Context(), updated), nil)
}

func (s *Server) handleAdminDisableUserTOTP(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.users.security.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	targetID := strings.TrimSpace(r.PathValue("id"))
	if !s.adminTargetExists(r.Context(), targetID) {
		api.WriteError(w, api.NewError(http.StatusNotFound, "admin.user_not_found", "admin user not found"))
		return
	}
	if s.adminTargetIsOwner(r.Context(), targetID) && !s.adminSessionIsOwner(r.Context(), session.SubjectID) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "admin.owner_protected", "only owners can manage owner MFA"))
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), targetID)
	security.AdminID = targetID
	security.TOTPEnabled = false
	security.TOTPSecret = ""
	if _, storeErr := s.store.UpsertAdminSecurity(r.Context(), security); storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to disable admin TOTP"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, targetID, "admin.user.totp.disable", nil)
	api.WriteJSON(w, http.StatusOK, adminSecurityData(security, s.store.ListAdminPasskeys(r.Context(), targetID)), nil)
}

func (s *Server) handleAdminDeleteUserPasskey(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.users.security.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	targetID := strings.TrimSpace(r.PathValue("id"))
	if !s.adminTargetExists(r.Context(), targetID) {
		api.WriteError(w, api.NewError(http.StatusNotFound, "admin.user_not_found", "admin user not found"))
		return
	}
	if s.adminTargetIsOwner(r.Context(), targetID) && !s.adminSessionIsOwner(r.Context(), session.SubjectID) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "admin.owner_protected", "only owners can manage owner MFA"))
		return
	}
	passkeyID := strings.TrimSpace(r.PathValue("passkey_id"))
	if err := s.store.DeleteAdminPasskey(r.Context(), targetID, passkeyID); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "auth.passkey_not_found", "passkey not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, passkeyID, "admin.user.passkey.delete", map[string]any{"admin_id": targetID})
	security, _ := s.store.GetAdminSecurity(r.Context(), targetID)
	api.WriteJSON(w, http.StatusOK, adminSecurityData(security, s.store.ListAdminPasskeys(r.Context(), targetID)), nil)
}

func (s *Server) handleAdminPermissions(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdminPermission(r, false, "admin.roles.read"); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, rbac.Catalog, map[string]any{"count": len(rbac.Catalog)})
}

func (s *Server) handleAdminRoles(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdminPermission(r, false, "admin.roles.read"); err != nil {
		api.WriteError(w, err)
		return
	}
	roles := s.store.ListAdminRoles(r.Context())
	data := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		data = append(data, adminRoleData(role))
	}
	api.WriteJSON(w, http.StatusOK, data, map[string]any{"count": len(data)})
}

func (s *Server) handleAdminCreateRole(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.roles.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminRoleCreateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	roleID := rbac.RoleID(req.RoleID)
	if roleID == "" {
		roleID = rbac.RoleID(req.ID)
	}
	if roleID == "" {
		roleID = rbac.RoleID(req.Alias)
	}
	if roleID == "" {
		roleID = rbac.RoleID(req.Name)
	}
	if roleID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "rbac.role_id_required", "role id is required"))
		return
	}
	if existing, ok := s.findAdminRole(r.Context(), roleID); ok {
		status := http.StatusConflict
		message := "role already exists"
		if existing.System {
			status = http.StatusForbidden
			message = "system role cannot be modified"
		}
		api.WriteError(w, api.NewError(status, "rbac.role_exists", message))
		return
	}
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		alias = strings.TrimSpace(req.Name)
	}
	role := rbac.Role{
		ID:          roleID,
		Name:        alias,
		Description: strings.TrimSpace(req.Description),
		Permissions: rbac.NormalizePermissions(req.Permissions),
	}
	if role.Name == "" {
		role.Name = role.ID
	}
	created, storeErr := s.store.UpsertAdminRole(r.Context(), role)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "rbac.role_save_failed", "failed to save role"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, created.ID, "admin.role.create", map[string]any{
		"role":        created.ID,
		"permissions": created.Permissions,
	})
	api.WriteJSON(w, http.StatusCreated, adminRoleData(created), nil)
}

func (s *Server) handleAdminUpdateRole(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.roles.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	roleID := rbac.RoleID(r.PathValue("id"))
	base, ok := s.findAdminRole(r.Context(), roleID)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusNotFound, "rbac.role_not_found", "role not found"))
		return
	}
	if base.System {
		api.WriteError(w, api.NewError(http.StatusForbidden, "rbac.system_role_locked", "system role cannot be modified"))
		return
	}
	var req adminRoleUpdateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	role := base
	if alias := strings.TrimSpace(req.Alias); alias != "" {
		role.Name = alias
	} else if name := strings.TrimSpace(req.Name); name != "" {
		role.Name = name
	}
	if req.Description != "" {
		role.Description = strings.TrimSpace(req.Description)
	}
	role.Permissions = rbac.NormalizePermissions(req.Permissions)
	updated, storeErr := s.store.UpsertAdminRole(r.Context(), role)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "rbac.role_save_failed", "failed to save role"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, updated.ID, "admin.role.update", map[string]any{
		"role":        updated.ID,
		"permissions": updated.Permissions,
	})
	api.WriteJSON(w, http.StatusOK, adminRoleData(updated), nil)
}

func (s *Server) handleAdminDeleteRole(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdminPermission(r, true, "admin.roles.write")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	roleID := rbac.RoleID(r.PathValue("id"))
	if roleID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "rbac.role_not_found", "role not found"))
		return
	}
	if base, ok := rbac.DefaultRole(roleID); ok && base.System {
		api.WriteError(w, api.NewError(http.StatusForbidden, "rbac.system_role_locked", "system role cannot be deleted"))
		return
	}
	if err := s.store.DeleteAdminRole(r.Context(), roleID); err != nil {
		status := http.StatusNotFound
		code := "rbac.role_not_found"
		if !strings.Contains(err.Error(), "not found") {
			status = http.StatusBadRequest
			code = "rbac.role_in_use"
		}
		api.WriteError(w, api.NewError(status, code, err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, roleID, "admin.role.delete", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminSystemSummary(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"service":     "authman",
		"environment": "docker-postgres",
		"version":     "0.1.0-dev",
	}, nil)
}

func (s *Server) adminConfigured() bool {
	return s.cfg.AdminUsername != "" && (s.cfg.AdminPassword != "" || s.cfg.AdminPasswordHash != "")
}

func (s *Server) verifyAdminPassword(ctx context.Context, identifier string, password string) bool {
	if !s.adminConfigured() || !s.adminIdentifierMatches(ctx, identifier) {
		return false
	}
	if s.cfg.AdminPasswordHash != "" {
		ok, err := auth.VerifyPassword(password, s.cfg.AdminPasswordHash)
		return err == nil && ok
	}
	return password == s.cfg.AdminPassword
}

func (s *Server) adminIdentifierMatches(ctx context.Context, identifier string) bool {
	if profile, err := s.store.GetAdminProfile(ctx, "bootstrap-admin"); err == nil {
		if strings.EqualFold(identifier, profile.Username) {
			return true
		}
		if profile.Email != "" && strings.EqualFold(identifier, profile.Email) {
			return true
		}
	}
	if strings.EqualFold(identifier, s.cfg.AdminUsername) {
		return true
	}
	return s.cfg.AdminEmail != "" && strings.EqualFold(identifier, s.cfg.AdminEmail)
}

func (s *Server) adminData(ctx context.Context) map[string]any {
	email := s.cfg.AdminEmail
	username := s.cfg.AdminUsername
	avatarURL := ""
	if profile, err := s.store.GetAdminProfile(ctx, "bootstrap-admin"); err == nil {
		if strings.TrimSpace(profile.Username) != "" {
			username = profile.Username
		}
		email = profile.Email
		avatarURL = profile.AvatarURL
	}
	role, _ := rbac.DefaultRole("owner")
	return map[string]any{
		"id":           "bootstrap-admin",
		"username":     username,
		"email":        email,
		"avatar_url":   avatarURL,
		"display_name": username,
		"role":         role.ID,
		"role_id":      role.ID,
		"role_alias":   role.Name,
		"permissions":  role.Permissions,
		"status":       "active",
	}
}

func (s *Server) adminDataWithSecurity(ctx context.Context) map[string]any {
	data := s.adminData(ctx)
	security, _ := s.store.GetAdminSecurity(ctx, "bootstrap-admin")
	data["security"] = adminSecurityData(security, s.store.ListAdminPasskeys(ctx, "bootstrap-admin"))
	return data
}

func (s *Server) adminDataForSession(ctx context.Context, subjectID string) (map[string]any, error) {
	if subjectID == "bootstrap-admin" {
		return s.adminData(ctx), nil
	}
	user, err := s.store.GetAdminUser(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	if user.Status != "active" {
		return nil, fmt.Errorf("admin user disabled: %w", store.ErrNotFound)
	}
	return s.adminUserData(ctx, user), nil
}

func (s *Server) adminUserData(ctx context.Context, user store.AdminUser) map[string]any {
	role, _ := s.findAdminRole(ctx, user.Role)
	avatarURL := ""
	if profile, err := s.store.GetAdminProfile(ctx, user.ID); err == nil {
		avatarURL = profile.AvatarURL
	}
	return map[string]any{
		"id":           user.ID,
		"username":     user.Username,
		"email":        user.Email,
		"avatar_url":   avatarURL,
		"display_name": user.Username,
		"role":         user.Role,
		"role_id":      user.Role,
		"role_alias":   role.Name,
		"permissions":  role.Permissions,
		"status":       user.Status,
		"created_at":   user.CreatedAt,
	}
}

func (s *Server) adminUserDataWithSecurity(ctx context.Context, user store.AdminUser) map[string]any {
	data := s.adminUserData(ctx, user)
	security, _ := s.store.GetAdminSecurity(ctx, user.ID)
	data["security"] = adminSecurityData(security, s.store.ListAdminPasskeys(ctx, user.ID))
	return data
}

func (s *Server) adminTargetExists(ctx context.Context, id string) bool {
	if id == "bootstrap-admin" {
		return true
	}
	_, err := s.store.GetAdminUser(ctx, id)
	return err == nil
}

func (s *Server) adminTargetIsOwner(ctx context.Context, id string) bool {
	if id == "bootstrap-admin" {
		return true
	}
	user, err := s.store.GetAdminUser(ctx, id)
	return err == nil && user.Role == "owner"
}

func (s *Server) adminSessionIsOwner(ctx context.Context, subjectID string) bool {
	return s.adminTargetIsOwner(ctx, subjectID)
}

func (s *Server) findAdminRole(ctx context.Context, id string) (rbac.Role, bool) {
	id = rbac.RoleID(id)
	for _, role := range s.store.ListAdminRoles(ctx) {
		if role.ID == id {
			return role, true
		}
	}
	return rbac.Role{}, false
}

func adminRoleData(role rbac.Role) map[string]any {
	return map[string]any{
		"id":          role.ID,
		"role_id":     role.ID,
		"alias":       role.Name,
		"name":        role.Name,
		"description": role.Description,
		"permissions": role.Permissions,
		"system":      role.System,
		"created_at":  role.CreatedAt,
		"updated_at":  role.UpdatedAt,
	}
}

func auditEventData(event audit.Event) map[string]any {
	return map[string]any{
		"id":          event.ID,
		"occurred_at": event.Occurred,
		"created_at":  event.Occurred,
		"actor_type":  event.ActorType,
		"actor_id":    event.ActorID,
		"target_type": event.Target,
		"target_id":   event.TargetID,
		"type":        event.Type,
		"event_type":  event.Type,
		"details":     event.Details,
	}
}

func auditEventSummaryData(event audit.Event) map[string]any {
	return map[string]any{
		"id":           event.ID,
		"event_type":   event.Type,
		"actor_type":   event.ActorType,
		"actor_label":  event.ActorID,
		"target_type":  event.Target,
		"target_label": event.TargetID,
		"created_at":   event.Occurred.Format(time.RFC3339),
	}
}

func (s *Server) audit(r *http.Request, actorType audit.ActorType, actorID string, target audit.TargetType, targetID string, eventType string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	_, _ = s.store.AppendAuditEvent(r.Context(), audit.NewEvent(time.Now(), actorType, actorID, target, targetID, eventType, details))
}
