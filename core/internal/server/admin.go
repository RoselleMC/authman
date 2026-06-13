package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"github.com/RoselleMC/authman/core/internal/rbac"
	"github.com/RoselleMC/authman/core/internal/store"
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

type banRequest struct {
	Reason           string `json:"reason"`
	ExpiresAt        string `json:"expires_at"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type revokeBanRequest struct {
	Reason string `json:"reason"`
}

type extendBanRequest struct {
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	Reason           string `json:"reason"`
}

type kickRequest struct {
	Reason string `json:"reason"`
}

type mojangRuntimeSettingsRequest struct {
	EnabledRouteIDs        []string `json:"enabled_route_ids"`
	LoadBalanceStrategy    string   `json:"load_balance_strategy"`
	RequestTimeoutSeconds  int      `json:"request_timeout_seconds"`
	FailureCooldownSeconds int      `json:"failure_cooldown_seconds"`
	CacheFreshSeconds      int      `json:"cache_fresh_seconds"`
	CacheStaleSeconds      int      `json:"cache_stale_seconds"`
}

type ipGeoSettingsRequest struct {
	EnabledRouteIDs       []string `json:"enabled_route_ids"`
	CacheTTLSeconds       int      `json:"cache_ttl_seconds"`
	RequestTimeoutSeconds int      `json:"request_timeout_seconds"`
	Provider              string   `json:"provider"`
}

type downstreamServerRequest struct {
	ID                 string         `json:"id"`
	DisplayName        string         `json:"display_name"`
	Status             string         `json:"status"`
	Enabled            *bool          `json:"enabled"`
	Visible            *bool          `json:"visible"`
	RegistrationOpen   bool           `json:"registration_open"`
	RoutingConfig      map[string]any `json:"routing_config"`
	ExtensionProviders []string       `json:"extension_providers"`
}

type downstreamServerPrivilegedPassportRequest struct {
	PassportID string `json:"passport_id"`
}

type portalSettingsRequest struct {
	TransferCookieKey      string `json:"transfer_cookie_key"`
	FallbackServerID       string `json:"fallback_server_id"`
	MaxProfilesPerPassport int    `json:"max_profiles_per_passport"`
	AutoJoinSingleProfile  bool   `json:"auto_join_single_profile"`
}

type brandingSettingsRequest struct {
	ProductName string `json:"product_name"`
	CoreLabel   string `json:"core_label"`
	TitleSuffix string `json:"title_suffix"`
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

func (s *Server) handleAdminPlayers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	q := r.URL.Query()
	search := q.Get("q")
	kind := strings.TrimSpace(q.Get("kind"))
	status := strings.TrimSpace(q.Get("status"))

	players := s.store.ListPlayers(r.Context())
	sort.SliceStable(players, func(i, j int) bool {
		left := players[i].ProtocolName
		if left == "" {
			left = players[i].RawOfflineName
		}
		right := players[j].ProtocolName
		if right == "" {
			right = players[j].RawOfflineName
		}
		if left == right {
			return players[i].ID < players[j].ID
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})

	filtered := make([]identity.Player, 0, len(players))
	for _, player := range players {
		if kind != "" && string(player.Kind) != kind {
			continue
		}
		playerStatus := "active"
		if player.Locked {
			playerStatus = "locked"
		}
		if status != "" && playerStatus != status {
			continue
		}
		if search != "" &&
			!containsFold(player.ID, search) &&
			!containsFold(player.RawOfflineName, search) &&
			!containsFold(player.NormalizedName, search) &&
			!containsFold(player.ProtocolName, search) &&
			!containsFold(player.UUID.String(), search) &&
			!containsFold(player.UUID.Compact(), search) {
			continue
		}
		filtered = append(filtered, player)
	}

	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, player := range filtered[start:end] {
		data = append(data, playerRowData(player))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
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
	eventData := s.relatedAuditSummaries(r.Context(), 20, player.ID)
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

type createProfileRequest struct {
	ProtocolName string `json:"protocol_name"`
	PassportID   string `json:"passport_id"`
}

type bindProfileRequest struct {
	PassportID string `json:"passport_id"`
	Primary    bool   `json:"primary"`
}

type updatePassportRequest struct {
	Status string `json:"status"`
}

type createOfflinePassportRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type updateProfileRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleAdminPassports(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	q := r.URL.Query()
	search := q.Get("q")
	kind := strings.TrimSpace(q.Get("kind"))
	status := strings.TrimSpace(q.Get("status"))
	sortKey := strings.TrimSpace(q.Get("sort"))
	sortDir := strings.TrimSpace(q.Get("dir"))
	passports, total, err := s.store.ListPassportsPage(r.Context(), store.IdentityListQuery{
		Search:   search,
		Kind:     kind,
		Status:   status,
		Sort:     sortKey,
		Dir:      sortDir,
		Page:     params.Page,
		PageSize: params.PageSize,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "passport.query_failed", "failed to query passports"))
		return
	}
	data := make([]map[string]any, 0, len(passports))
	now := time.Now()
	for _, passport := range passports {
		profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
		presences := s.store.ListPassportPresences(r.Context(), passport.ID)
		row := passportRowData(passport, profiles, presences)
		s.enrichPassportRow(r.Context(), row, passport, now)
		data = append(data, row)
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), total, params))
}

func (s *Server) handleAdminCreateOfflinePassport(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req createOfflinePassportRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "passport.username_required", "username is required"))
		return
	}
	passwordHash, err := auth.HashPassword(req.Password, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	encryptedPassword, keyFingerprint, err := s.offlinePasswordCredential(r.Context(), req.Password)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.encrypt_failed", "failed to encrypt recoverable password"))
		return
	}
	passport, err := s.store.CreateOfflinePassport(r.Context(), username, passwordHash, encryptedPassword, keyFingerprint)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "passport.create_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.create.offline", map[string]any{
		"username":    passport.Username,
		"passport_id": passport.ID,
	})
	row := passportRowData(passport, s.store.ListProfilesForPassport(r.Context(), passport.ID), s.store.ListPassportPresences(r.Context(), passport.ID))
	s.enrichPassportRow(r.Context(), row, passport, time.Now())
	api.WriteJSON(w, http.StatusCreated, row, nil)
}

func (s *Server) handleAdminPassportDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	var credential *store.PassportCredential
	if passport.Kind == identity.PassportKindOffline {
		if _, c, err := s.store.GetPassportCredential(r.Context(), passport.Username); err == nil {
			credential = &c
		}
	}
	profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	relatedIDs := []string{passport.ID}
	for _, profile := range profiles {
		relatedIDs = append(relatedIDs, profile.ID)
	}
	eventData := s.relatedAuditSummaries(r.Context(), 20, relatedIDs...)
	presences := s.store.ListPassportPresences(r.Context(), passport.ID)
	bans := s.store.ListBans(r.Context(), store.BanScopePassport, passport.ID, true, time.Now())
	profileBans := make(map[string]store.PlayerBan)
	for _, profile := range profiles {
		if ban, ok := firstActiveBan(s.store.ListBans(r.Context(), store.BanScopeProfile, profile.ID, false, time.Now()), time.Now()); ok {
			profileBans[profile.ID] = ban
		}
	}
	data := passportDetailData(passport, profiles, credential, presences, bans, profileBans, eventData)
	data["skin"] = s.passportSkinData(r.Context(), passport)
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func (s *Server) handleAdminUpdatePassport(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req updatePassportRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	status := identity.PassportStatus(strings.TrimSpace(req.Status))
	if status == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "passport.status_required", "status is required"))
		return
	}
	passport, err := s.store.SetPassportStatus(r.Context(), r.PathValue("id"), status)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.status.update", map[string]any{"status": status})
	api.WriteJSON(w, http.StatusOK, passportRowData(passport, s.store.ListProfilesForPassport(r.Context(), passport.ID), s.store.ListPassportPresences(r.Context(), passport.ID)), nil)
}

func (s *Server) handleAdminProfiles(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	q := r.URL.Query()
	search := q.Get("q")
	status := strings.TrimSpace(q.Get("status"))
	binding := strings.TrimSpace(q.Get("binding"))
	sortKey := strings.TrimSpace(q.Get("sort"))
	sortDir := strings.TrimSpace(q.Get("dir"))
	profiles, total, err := s.store.ListProfilesPage(r.Context(), store.IdentityListQuery{
		Search:   search,
		Status:   status,
		Binding:  binding,
		Sort:     sortKey,
		Dir:      sortDir,
		Page:     params.Page,
		PageSize: params.PageSize,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "profile.query_failed", "failed to query profiles"))
		return
	}
	data := make([]map[string]any, 0, len(profiles))
	now := time.Now()
	for _, profile := range profiles {
		var passport *identity.Passport
		if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
			passport = &p
		}
		row := profileRowData(profile, passport, s.store.ListProfilePresences(r.Context(), profile.ID))
		s.enrichProfileRow(r.Context(), row, profile, passport, now)
		data = append(data, row)
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), total, params))
}

func (s *Server) handleAdminCreateProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req createProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	name, err := identity.NormalizeProtocolName(req.ProtocolName)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	profile, err := identity.NewOfflineProfile("", name.Protocol, strings.TrimSpace(req.PassportID))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	profile, err = s.store.CreateProfile(r.Context(), profile)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.create_failed", err.Error()))
		return
	}
	var passport *identity.Passport
	if strings.TrimSpace(req.PassportID) != "" {
		pp, err := s.store.BindProfileToPassport(r.Context(), profile.ID, strings.TrimSpace(req.PassportID), false)
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.bind_failed", err.Error()))
			return
		}
		passport = &pp.Passport
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.create", map[string]any{"protocol_name": profile.ProtocolName})
	api.WriteJSON(w, http.StatusCreated, profileRowData(profile, passport, s.store.ListProfilePresences(r.Context(), profile.ID)), nil)
}

func (s *Server) handleAdminProfileDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	relatedIDs := []string{profile.ID}
	if passport != nil {
		relatedIDs = append(relatedIDs, passport.ID)
	}
	eventData := s.relatedAuditSummaries(r.Context(), 20, relatedIDs...)
	presences := s.store.ListProfilePresences(r.Context(), profile.ID)
	bans := s.store.ListBans(r.Context(), store.BanScopeProfile, profile.ID, true, time.Now())
	data := profileDetailData(profile, passport, presences, bans, eventData)
	data["skin"] = s.profileSkinData(r.Context(), profile, passport)
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func (s *Server) handleAdminUpdateProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req updateProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	status := identity.ProfileStatus(strings.TrimSpace(req.Status))
	if status == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.status_required", "status is required"))
		return
	}
	profile, err := s.store.SetProfileStatus(r.Context(), r.PathValue("id"), status)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.status.update", map[string]any{"status": status})
	api.WriteJSON(w, http.StatusOK, profileRowData(profile, nil, s.store.ListProfilePresences(r.Context(), profile.ID)), nil)
}

func (s *Server) handleAdminBindProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req bindProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	pp, err := s.store.BindProfileToPassport(r.Context(), r.PathValue("id"), strings.TrimSpace(req.PassportID), req.Primary)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.bind_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, pp.Profile.ID, "profile.bind", map[string]any{"passport_id": pp.Passport.ID})
	api.WriteJSON(w, http.StatusOK, profileRowData(pp.Profile, &pp.Passport, s.store.ListProfilePresences(r.Context(), pp.Profile.ID)), nil)
}

func (s *Server) handleAdminUnbindProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := r.PathValue("id")
	if err := s.store.UnbindProfile(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile link not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, id, "profile.unbind", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminCreatePassportBan(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	ban, apiErr := s.banFromRequest(r, store.BanScopePassport, passport.ID, session.SubjectID)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, err = s.store.CreateBan(r.Context(), ban)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ban.create_failed", "failed to create ban"))
		return
	}
	now := time.Now()
	presences := s.store.ListPassportPresences(r.Context(), passport.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, "passport banned: "+ban.Reason, now)
	ended := s.store.EndPassportPresences(r.Context(), passport.ID, "passport banned", now)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.ban.create", map[string]any{
		"ban_id":          ban.ID,
		"reason":          ban.Reason,
		"expires_at":      ban.ExpiresAt,
		"ended_presences": ended,
		"queued_actions":  queued,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"ban":             banRows([]store.PlayerBan{ban})[0],
		"ended_presences": ended,
		"queued_actions":  queued,
	}, nil)
}

func (s *Server) handleAdminCreateProfileBan(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	ban, apiErr := s.banFromRequest(r, store.BanScopeProfile, profile.ID, session.SubjectID)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, err = s.store.CreateBan(r.Context(), ban)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ban.create_failed", "failed to create ban"))
		return
	}
	now := time.Now()
	presences := s.store.ListProfilePresences(r.Context(), profile.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, "profile banned: "+ban.Reason, now)
	ended := s.store.EndProfilePresences(r.Context(), profile.ID, "profile banned", now)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.ban.create", map[string]any{
		"ban_id":          ban.ID,
		"reason":          ban.Reason,
		"expires_at":      ban.ExpiresAt,
		"ended_presences": ended,
		"queued_actions":  queued,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"ban":             banRows([]store.PlayerBan{ban})[0],
		"ended_presences": ended,
		"queued_actions":  queued,
	}, nil)
}

func (s *Server) handleAdminRevokeBan(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req revokeBanRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	ban, err := s.store.RevokeBan(r.Context(), r.PathValue("id"), session.SubjectID, strings.TrimSpace(req.Reason), time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ban.not_found", "ban not found"))
		return
	}
	eventType := "ban.revoke"
	if ban.Scope == store.BanScopePassport {
		eventType = "passport.ban.revoke"
	}
	if ban.Scope == store.BanScopeProfile {
		eventType = "profile.ban.revoke"
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, ban.TargetID, eventType, map[string]any{
		"ban_id": ban.ID,
		"reason": strings.TrimSpace(req.Reason),
	})
	api.WriteJSON(w, http.StatusOK, banRows([]store.PlayerBan{ban})[0], nil)
}

func (s *Server) handleAdminExtendBan(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req extendBanRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if req.ExpiresInSeconds < 1 {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ban.expiry_invalid", "temporary ban must last at least 1 second"))
		return
	}
	if req.ExpiresInSeconds > 10*365*24*60*60 {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "ban.expiry_invalid", "ban duration is too long"))
		return
	}
	existing, err := s.store.GetBan(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ban.not_found", "ban not found"))
		return
	}
	if existing.RevokedAt != nil {
		api.WriteError(w, api.NewError(http.StatusConflict, "ban.revoked", "ban has already been revoked"))
		return
	}
	now := time.Now()
	base := now
	if existing.ExpiresAt != nil && existing.ExpiresAt.After(now) {
		base = *existing.ExpiresAt
	}
	expiresAt := base.Add(time.Duration(req.ExpiresInSeconds) * time.Second)
	ban, err := s.store.ExtendBan(r.Context(), existing.ID, expiresAt)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "ban.not_found", "ban not found"))
		return
	}
	eventType := "ban.extend"
	if ban.Scope == store.BanScopePassport {
		eventType = "passport.ban.extend"
	}
	if ban.Scope == store.BanScopeProfile {
		eventType = "profile.ban.extend"
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, ban.TargetID, eventType, map[string]any{
		"ban_id":              ban.ID,
		"reason":              strings.TrimSpace(req.Reason),
		"duration_seconds":    req.ExpiresInSeconds,
		"previous_expires_at": existing.ExpiresAt,
		"expires_at":          ban.ExpiresAt,
	})
	api.WriteJSON(w, http.StatusOK, banRows([]store.PlayerBan{ban})[0], nil)
}

func (s *Server) handleAdminKickPresence(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req kickRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	reason := normalizeKickReason(req.Reason)
	presence, err := s.store.EndPresence(r.Context(), r.PathValue("id"), reason, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "presence.not_found", "presence not found"))
		return
	}
	queued := s.enqueueDisconnectActions(r.Context(), []store.PlayerPresence{presence}, reason, time.Now())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, presence.ProfileID, "presence.kick", map[string]any{
		"presence_id":    presence.ID,
		"passport_id":    presence.PassportID,
		"profile_id":     presence.ProfileID,
		"server_id":      presence.ServerID,
		"node_id":        presence.NodeID,
		"reason":         reason,
		"queued_actions": queued,
	})
	api.WriteJSON(w, http.StatusOK, presenceRows([]store.PlayerPresence{presence})[0], nil)
}

func (s *Server) handleAdminKickPassport(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	var req kickRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	reason := normalizeKickReason(req.Reason)
	now := time.Now()
	presences := s.store.ListPassportPresences(r.Context(), passport.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, reason, now)
	ended := s.store.EndPassportPresences(r.Context(), passport.ID, reason, now)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.kick", map[string]any{
		"reason":          reason,
		"ended_presences": ended,
		"queued_actions":  queued,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ended_presences": ended, "queued_actions": queued}, nil)
}

func (s *Server) handleAdminKickProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	var req kickRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	reason := normalizeKickReason(req.Reason)
	now := time.Now()
	presences := s.store.ListProfilePresences(r.Context(), profile.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, reason, now)
	ended := s.store.EndProfilePresences(r.Context(), profile.ID, reason, now)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.kick", map[string]any{
		"reason":          reason,
		"ended_presences": ended,
		"queued_actions":  queued,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ended_presences": ended, "queued_actions": queued}, nil)
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
	encryptedPassword, keyFingerprint, err := s.offlinePasswordCredential(r.Context(), req.Password)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.encrypt_failed", "failed to encrypt recoverable password"))
		return
	}
	if err := s.store.UpdateOfflinePassword(r.Context(), player.ID, passwordHash, encryptedPassword, keyFingerprint); err != nil {
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
	relatedIDs := auditRelatedIDSet(query.Get("related_id"))
	since, hasSince := parseOptionalRFC3339(query.Get("since"))
	until, hasUntil := parseOptionalRFC3339(query.Get("until"))

	var sincePtr *time.Time
	if hasSince {
		sincePtr = &since
	}
	var untilPtr *time.Time
	if hasUntil {
		untilPtr = &until
	}
	relatedIDList := make([]string, 0, len(relatedIDs))
	for id := range relatedIDs {
		relatedIDList = append(relatedIDList, id)
	}
	events, total, err := s.store.ListAuditEventsPage(r.Context(), store.AuditEventQuery{
		ActorType:  actorType,
		TargetType: targetType,
		EventType:  eventType,
		RelatedIDs: relatedIDList,
		Since:      sincePtr,
		Until:      untilPtr,
		Page:       params.Page,
		PageSize:   params.PageSize,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "audit.query_failed", "failed to query audit events"))
		return
	}
	data := make([]map[string]any, 0, len(events))
	for _, event := range events {
		data = append(data, auditEventData(event))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), total, params))
}

func (s *Server) handleAdminAuditEventDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	event, err := s.store.GetAuditEvent(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, api.NewError(http.StatusNotFound, "audit.not_found", "audit event not found"))
		return
	}
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "audit.query_failed", "failed to query audit event"))
		return
	}
	api.WriteJSON(w, http.StatusOK, auditEventData(event), nil)
}

func (s *Server) handleAdminMojangRoutes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	kindFilter := strings.TrimSpace(query.Get("kind"))
	stateFilter := strings.TrimSpace(query.Get("state"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	now := time.Now().UTC()
	routeData := []map[string]any{}
	overall := "mojang_disabled"
	if s.mojangVerifier != nil {
		routes := s.mojangVerifier.RoutesSnapshot()
		healthy := 0
		for _, route := range routes {
			state := mojangRouteState(route)
			cooldown := int64(0)
			if route.CooldownUntil.After(now) {
				cooldown = int64(route.CooldownUntil.Sub(now).Seconds())
			}
			if state == string(mojang.RouteHealthy) {
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
	filteredRoutes := routeData[:0]
	for _, route := range routeData {
		if kindFilter != "" && fmt.Sprint(route["kind"]) != kindFilter {
			continue
		}
		if stateFilter != "" && fmt.Sprint(route["state"]) != stateFilter {
			continue
		}
		if search != "" && !containsFold(fmt.Sprint(route["id"]), search) && !containsFold(fmt.Sprint(route["url_masked"]), search) {
			continue
		}
		filteredRoutes = append(filteredRoutes, route)
	}
	sort.SliceStable(filteredRoutes, func(i, j int) bool {
		cmp := compareMapListValues(filteredRoutes[i], filteredRoutes[j], sortKey)
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(filteredRoutes), params)
	pagedRoutes := append([]map[string]any(nil), filteredRoutes[start:end]...)
	events := []map[string]any{}
	if s.mojangVerifier != nil {
		for _, event := range s.mojangVerifier.EventsSnapshot() {
			events = append(events, mojangEventData(event))
		}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"overall": overall,
		"proxies": pagedRoutes,
		"cache":   s.mojangCacheSnapshot(),
		"events":  events,
	}, listMeta(len(pagedRoutes), len(filteredRoutes), params))
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

func (s *Server) handleAdminUpdateMojangRoute(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || id == "direct" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "mojang.route_invalid", "route cannot be edited"))
		return
	}
	existing, err := s.store.GetMojangRoute(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "mojang.route_not_found", "Mojang route was not found"))
		return
	}
	var req mojangRouteRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	route, apiErr := routeFromUpdateRequest(existing, req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	route, storeErr := s.store.UpsertMojangRoute(r.Context(), route)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "mojang.route_save_failed", "failed to save Mojang route"))
		return
	}
	s.reloadMojangRoutes(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetMojangProxy, route.ID, "mojang.route.update", map[string]any{
		"kind":     route.Kind,
		"weight":   route.Weight,
		"disabled": route.Disabled,
	})
	api.WriteJSON(w, http.StatusOK, mojangRouteData(route, time.Now().UTC()), nil)
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

func (s *Server) handleAdminMojangSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := s.mojangRuntimeSettings(r.Context())
	api.WriteJSON(w, http.StatusOK, mojangSettingsData(settings, s.allMojangRoutes(r.Context())), nil)
}

func (s *Server) handleAdminUpdateMojangSettings(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req mojangRuntimeSettingsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := normalizeMojangSettings(req, s.cfg.MojangTimeout, s.cfg.MojangCooldown, s.cfg.MojangCacheFresh, s.cfg.MojangCacheStale)
	if err := s.store.SetSystemSetting(r.Context(), "mojang_upstream", mojangSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "settings.save_failed", "failed to save Mojang settings"))
		return
	}
	s.reloadMojangRoutes(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "mojang_upstream", "settings.mojang.update", mojangSettingsMap(settings))
	api.WriteJSON(w, http.StatusOK, mojangSettingsData(settings, s.allMojangRoutes(r.Context())), nil)
}

func (s *Server) handleAdminIPGeoSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := s.ipGeoSettings(r.Context())
	api.WriteJSON(w, http.StatusOK, ipGeoSettingsData(settings, s.allMojangRoutes(r.Context())), nil)
}

func (s *Server) handleAdminUpdateIPGeoSettings(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req ipGeoSettingsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := normalizeIPGeoSettings(req)
	if err := s.store.SetSystemSetting(r.Context(), "ip_geo", ipGeoSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "settings.save_failed", "failed to save IP geolocation settings"))
		return
	}
	s.reloadIPGeoSettings(r.Context())
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "ip_geo", "settings.ip_geo.update", ipGeoSettingsMap(settings))
	api.WriteJSON(w, http.StatusOK, ipGeoSettingsData(settings, s.allMojangRoutes(r.Context())), nil)
}

func (s *Server) handleAdminBrandingSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, brandingSettingsData(s.brandingSettings(r.Context())), nil)
}

func (s *Server) handleAdminUpdateBrandingSettings(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req brandingSettingsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := normalizeBrandingSettings(req)
	if err := s.store.SetSystemSetting(r.Context(), "branding", brandingSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "settings.save_failed", "failed to save branding settings"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "branding", "settings.branding.update", brandingSettingsMap(settings))
	api.WriteJSON(w, http.StatusOK, brandingSettingsData(settings), nil)
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

func routeFromUpdateRequest(existing mojang.Route, req mojangRouteRequest) (mojang.Route, *api.Error) {
	kind := existing.Kind
	if strings.TrimSpace(req.Kind) != "" {
		kind = mojang.RouteKind(strings.ToLower(strings.TrimSpace(req.Kind)))
	}
	if kind != mojang.RouteHTTP && kind != mojang.RouteSOCKS5 {
		return mojang.Route{}, api.NewError(http.StatusBadRequest, "mojang.route_invalid_kind", "route kind must be http or socks5")
	}
	routeURL := strings.TrimSpace(req.URL)
	if routeURL == "" {
		routeURL = existing.URL
	}
	if err := validateRouteURL(kind, routeURL); err != nil {
		return mojang.Route{}, err
	}
	weight := req.Weight
	if weight <= 0 {
		weight = existing.Weight
	}
	if weight <= 0 {
		weight = 1
	}
	if weight > 100 {
		weight = 100
	}
	return mojang.Route{
		ID:       existing.ID,
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

type mojangRuntimeSettings struct {
	EnabledRouteIDs        []string
	LoadBalanceStrategy    string
	RequestTimeoutSeconds  int
	FailureCooldownSeconds int
	CacheFreshSeconds      int
	CacheStaleSeconds      int
}

type ipGeoSettings struct {
	EnabledRouteIDs       []string
	CacheTTLSeconds       int
	RequestTimeoutSeconds int
	Provider              string
}

type brandingSettings struct {
	ProductName string
	CoreLabel   string
	TitleSuffix string
}

func (s *Server) mojangRuntimeSettings(ctx context.Context) mojangRuntimeSettings {
	defaults := normalizeMojangSettings(mojangRuntimeSettingsRequest{
		LoadBalanceStrategy:    "weighted_round_robin",
		RequestTimeoutSeconds:  int(s.cfg.MojangTimeout.Seconds()),
		FailureCooldownSeconds: int(s.cfg.MojangCooldown.Seconds()),
		CacheFreshSeconds:      int(s.cfg.MojangCacheFresh.Seconds()),
		CacheStaleSeconds:      int(s.cfg.MojangCacheStale.Seconds()),
	}, s.cfg.MojangTimeout, s.cfg.MojangCooldown, s.cfg.MojangCacheFresh, s.cfg.MojangCacheStale)
	raw, err := s.store.GetSystemSetting(ctx, "mojang_upstream")
	if errors.Is(err, store.ErrNotFound) {
		return defaults
	}
	if err != nil {
		return defaults
	}
	return normalizeMojangSettings(mojangRuntimeSettingsRequest{
		EnabledRouteIDs:        stringList(raw["enabled_route_ids"]),
		LoadBalanceStrategy:    stringValue(raw["load_balance_strategy"], defaults.LoadBalanceStrategy),
		RequestTimeoutSeconds:  intValue(raw["request_timeout_seconds"], defaults.RequestTimeoutSeconds),
		FailureCooldownSeconds: intValue(raw["failure_cooldown_seconds"], defaults.FailureCooldownSeconds),
		CacheFreshSeconds:      intValue(raw["cache_fresh_seconds"], defaults.CacheFreshSeconds),
		CacheStaleSeconds:      intValue(raw["cache_stale_seconds"], defaults.CacheStaleSeconds),
	}, s.cfg.MojangTimeout, s.cfg.MojangCooldown, s.cfg.MojangCacheFresh, s.cfg.MojangCacheStale)
}

func (s *Server) ipGeoSettings(ctx context.Context) ipGeoSettings {
	defaults := normalizeIPGeoSettings(ipGeoSettingsRequest{
		CacheTTLSeconds:       86400,
		RequestTimeoutSeconds: 3,
		Provider:              "ip-api.com",
	})
	raw, err := s.store.GetSystemSetting(ctx, "ip_geo")
	if errors.Is(err, store.ErrNotFound) {
		return defaults
	}
	if err != nil {
		return defaults
	}
	return normalizeIPGeoSettings(ipGeoSettingsRequest{
		EnabledRouteIDs:       stringList(raw["enabled_route_ids"]),
		CacheTTLSeconds:       intValue(raw["cache_ttl_seconds"], defaults.CacheTTLSeconds),
		RequestTimeoutSeconds: intValue(raw["request_timeout_seconds"], defaults.RequestTimeoutSeconds),
		Provider:              stringValue(raw["provider"], defaults.Provider),
	})
}

func (s *Server) brandingSettings(ctx context.Context) brandingSettings {
	defaults := normalizeBrandingSettings(brandingSettingsRequest{
		ProductName: "Authman",
		CoreLabel:   "Core",
		TitleSuffix: "Authman Core",
	})
	raw, err := s.store.GetSystemSetting(ctx, "branding")
	if errors.Is(err, store.ErrNotFound) {
		return defaults
	}
	if err != nil {
		return defaults
	}
	return normalizeBrandingSettings(brandingSettingsRequest{
		ProductName: stringValue(raw["product_name"], defaults.ProductName),
		CoreLabel:   stringValue(raw["core_label"], defaults.CoreLabel),
		TitleSuffix: stringValue(raw["title_suffix"], defaults.TitleSuffix),
	})
}

func normalizeMojangSettings(req mojangRuntimeSettingsRequest, timeout time.Duration, cooldown time.Duration, fresh time.Duration, stale time.Duration) mojangRuntimeSettings {
	settings := mojangRuntimeSettings{
		EnabledRouteIDs:        cleanIDs(req.EnabledRouteIDs),
		LoadBalanceStrategy:    strings.TrimSpace(req.LoadBalanceStrategy),
		RequestTimeoutSeconds:  req.RequestTimeoutSeconds,
		FailureCooldownSeconds: req.FailureCooldownSeconds,
		CacheFreshSeconds:      req.CacheFreshSeconds,
		CacheStaleSeconds:      req.CacheStaleSeconds,
	}
	if settings.LoadBalanceStrategy == "" {
		settings.LoadBalanceStrategy = "weighted_round_robin"
	}
	if settings.RequestTimeoutSeconds <= 0 {
		settings.RequestTimeoutSeconds = int(timeout.Seconds())
	}
	if settings.FailureCooldownSeconds <= 0 {
		settings.FailureCooldownSeconds = int(cooldown.Seconds())
	}
	if settings.CacheFreshSeconds <= 0 {
		settings.CacheFreshSeconds = int(fresh.Seconds())
	}
	if settings.CacheStaleSeconds <= 0 {
		settings.CacheStaleSeconds = int(stale.Seconds())
	}
	settings.RequestTimeoutSeconds = clampInt(settings.RequestTimeoutSeconds, 1, 60)
	settings.FailureCooldownSeconds = clampInt(settings.FailureCooldownSeconds, 5, 3600)
	settings.CacheFreshSeconds = clampInt(settings.CacheFreshSeconds, 1, 86400)
	settings.CacheStaleSeconds = clampInt(settings.CacheStaleSeconds, settings.CacheFreshSeconds, 604800)
	return settings
}

func normalizeIPGeoSettings(req ipGeoSettingsRequest) ipGeoSettings {
	settings := ipGeoSettings{
		EnabledRouteIDs:       cleanIDs(req.EnabledRouteIDs),
		CacheTTLSeconds:       req.CacheTTLSeconds,
		RequestTimeoutSeconds: req.RequestTimeoutSeconds,
		Provider:              strings.TrimSpace(req.Provider),
	}
	if settings.Provider == "" {
		settings.Provider = "ip-api.com"
	}
	if settings.CacheTTLSeconds <= 0 {
		settings.CacheTTLSeconds = 86400
	}
	if settings.RequestTimeoutSeconds <= 0 {
		settings.RequestTimeoutSeconds = 3
	}
	settings.CacheTTLSeconds = clampInt(settings.CacheTTLSeconds, 60, 604800)
	settings.RequestTimeoutSeconds = clampInt(settings.RequestTimeoutSeconds, 1, 30)
	return settings
}

func normalizeBrandingSettings(req brandingSettingsRequest) brandingSettings {
	settings := brandingSettings{
		ProductName: strings.TrimSpace(req.ProductName),
		CoreLabel:   strings.TrimSpace(req.CoreLabel),
		TitleSuffix: strings.TrimSpace(req.TitleSuffix),
	}
	if settings.ProductName == "" {
		settings.ProductName = "Authman"
	}
	if settings.CoreLabel == "" {
		settings.CoreLabel = "Core"
	}
	if settings.TitleSuffix == "" {
		settings.TitleSuffix = settings.ProductName + " " + settings.CoreLabel
	}
	return settings
}

func mojangSettingsData(settings mojangRuntimeSettings, routes []mojang.Route) map[string]any {
	return map[string]any{
		"enabled_route_ids":        settings.EnabledRouteIDs,
		"load_balance_strategy":    settings.LoadBalanceStrategy,
		"request_timeout_seconds":  settings.RequestTimeoutSeconds,
		"failure_cooldown_seconds": settings.FailureCooldownSeconds,
		"cache_fresh_seconds":      settings.CacheFreshSeconds,
		"cache_stale_seconds":      settings.CacheStaleSeconds,
		"available_routes":         routeChoiceData(routes),
	}
}

func mojangSettingsMap(settings mojangRuntimeSettings) map[string]any {
	return map[string]any{
		"enabled_route_ids":        settings.EnabledRouteIDs,
		"load_balance_strategy":    settings.LoadBalanceStrategy,
		"request_timeout_seconds":  settings.RequestTimeoutSeconds,
		"failure_cooldown_seconds": settings.FailureCooldownSeconds,
		"cache_fresh_seconds":      settings.CacheFreshSeconds,
		"cache_stale_seconds":      settings.CacheStaleSeconds,
	}
}

func ipGeoSettingsData(settings ipGeoSettings, routes []mojang.Route) map[string]any {
	return map[string]any{
		"enabled_route_ids":       settings.EnabledRouteIDs,
		"cache_ttl_seconds":       settings.CacheTTLSeconds,
		"request_timeout_seconds": settings.RequestTimeoutSeconds,
		"provider":                settings.Provider,
		"available_routes":        routeChoiceData(routes),
	}
}

func ipGeoSettingsMap(settings ipGeoSettings) map[string]any {
	return map[string]any{
		"enabled_route_ids":       settings.EnabledRouteIDs,
		"cache_ttl_seconds":       settings.CacheTTLSeconds,
		"request_timeout_seconds": settings.RequestTimeoutSeconds,
		"provider":                settings.Provider,
	}
}

func brandingSettingsData(settings brandingSettings) map[string]any {
	return brandingSettingsMap(settings)
}

func brandingSettingsMap(settings brandingSettings) map[string]any {
	return map[string]any{
		"product_name": strings.TrimSpace(settings.ProductName),
		"core_label":   strings.TrimSpace(settings.CoreLabel),
		"title_suffix": strings.TrimSpace(settings.TitleSuffix),
	}
}

func routeChoiceData(routes []mojang.Route) []map[string]any {
	out := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		out = append(out, map[string]any{
			"id":         route.ID,
			"kind":       route.Kind,
			"url_masked": maskRouteURL(route),
			"weight":     route.Weight,
			"disabled":   route.Disabled,
		})
	}
	return out
}

func routesByIDs(routes []mojang.Route, ids []string) []mojang.Route {
	if len(ids) == 0 {
		return routes
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	out := make([]mojang.Route, 0, len(routes))
	for _, route := range routes {
		if _, ok := wanted[route.ID]; ok {
			out = append(out, route)
		}
	}
	return out
}

func cleanIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(value any, fallback string) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var out int
		if _, err := fmt.Sscanf(typed, "%d", &out); err == nil {
			return out
		}
	}
	return fallback
}

func boolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func mojangRouteData(route mojang.Route, now time.Time) map[string]any {
	state := mojangRouteState(route)
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

func mojangRouteState(route mojang.Route) string {
	if route.Disabled {
		return string(mojang.RouteDisabled)
	}
	if route.State != "" {
		return string(route.State)
	}
	if route.Kind == mojang.RouteDirect {
		return string(mojang.RouteHealthy)
	}
	return "unknown"
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
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	servers := s.store.ListDownstreamServers(r.Context())
	filtered := make([]store.DownstreamServer, 0, len(servers))
	for _, server := range servers {
		data := downstreamServerData(server)
		if search != "" && !containsFold(server.ID, search) && !containsFold(server.Slug, search) && !containsFold(server.DisplayName, search) && !containsFold(fmt.Sprint(data["target"]), search) {
			continue
		}
		filtered = append(filtered, server)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		cmp := 0
		switch sortKey {
		case "name":
			cmp = strings.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName))
		case "status":
			cmp = strings.Compare(a.Status, b.Status)
		case "updated":
			cmp = compareTime(a.UpdatedAt, b.UpdatedAt)
		default:
			cmp = strings.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName))
		}
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, server := range filtered[start:end] {
		data = append(data, downstreamServerData(server))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
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
	s.pushAllNodeSync(r.Context(), "server.upsert")
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
	s.pushAllNodeSync(r.Context(), "server.upsert")
	api.WriteJSON(w, http.StatusOK, downstreamServerData(server), nil)
}

func (s *Server) handleAdminUploadDownstreamServerIcon(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	server, err := s.store.GetDownstreamServer(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	if err := r.ParseMultipartForm(512 * 1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.icon_multipart_invalid", "invalid multipart upload"))
		return
	}
	icon, err := readServerIconDataURI(r, "icon")
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.icon_invalid", err.Error()))
		return
	}
	if server.PortalConfig == nil {
		server.PortalConfig = map[string]any{}
	}
	server.PortalConfig["server_icon"] = icon
	server, err = s.store.UpsertDownstreamServer(r.Context(), server)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, server.ID, "server.icon.update", map[string]any{
		"slug": server.Slug,
	})
	s.pushAllNodeSync(r.Context(), "server.icon.update")
	api.WriteJSON(w, http.StatusOK, downstreamServerData(server), nil)
}

func (s *Server) handleAdminDeleteDownstreamServerIcon(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	server, err := s.store.GetDownstreamServer(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	if server.PortalConfig == nil {
		server.PortalConfig = map[string]any{}
	}
	delete(server.PortalConfig, "server_icon")
	server, err = s.store.UpsertDownstreamServer(r.Context(), server)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, server.ID, "server.icon.delete", map[string]any{
		"slug": server.Slug,
	})
	s.pushAllNodeSync(r.Context(), "server.icon.delete")
	api.WriteJSON(w, http.StatusOK, downstreamServerData(server), nil)
}

func (s *Server) handleAdminListDownstreamServerPrivilegedPassports(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	serverID := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.store.GetDownstreamServer(r.Context(), serverID); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	params := parseListPageParams(r)
	q := r.URL.Query()
	rows, total, err := s.store.ListDownstreamServerPrivilegedPassports(r.Context(), serverID, store.IdentityListQuery{
		Search:   q.Get("q"),
		Kind:     strings.TrimSpace(q.Get("kind")),
		Status:   strings.TrimSpace(q.Get("status")),
		Sort:     strings.TrimSpace(q.Get("sort")),
		Dir:      strings.TrimSpace(q.Get("dir")),
		Page:     params.Page,
		PageSize: params.PageSize,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "server.privileged_query_failed", "failed to query privileged passports"))
		return
	}
	data := make([]map[string]any, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		item := downstreamServerPrivilegedPassportData(row, s.store.ListProfilesForPassport(r.Context(), row.Passport.ID), s.store.ListPassportPresences(r.Context(), row.Passport.ID))
		s.enrichPassportRow(r.Context(), item, row.Passport, now)
		data = append(data, item)
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), total, params))
}

func (s *Server) handleAdminAddDownstreamServerPrivilegedPassport(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	serverID := strings.TrimSpace(r.PathValue("id"))
	var req downstreamServerPrivilegedPassportRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	passportID := strings.TrimSpace(req.PassportID)
	if passportID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "passport.required", "passport is required"))
		return
	}
	allow, err := s.store.AddDownstreamServerPrivilegedPassport(r.Context(), serverID, passportID, session.SubjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, api.NewError(http.StatusNotFound, "server_or_passport.not_found", "server or passport not found"))
			return
		}
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.privileged_add_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, serverID, "server.privileged_passport.add", map[string]any{
		"passport_id": allow.PassportID,
		"username":    allow.Passport.Username,
	})
	s.pushAllNodeSync(r.Context(), "server.privileged_passport.add")
	item := downstreamServerPrivilegedPassportData(allow, s.store.ListProfilesForPassport(r.Context(), allow.Passport.ID), s.store.ListPassportPresences(r.Context(), allow.Passport.ID))
	s.enrichPassportRow(r.Context(), item, allow.Passport, time.Now())
	api.WriteJSON(w, http.StatusCreated, item, nil)
}

func (s *Server) handleAdminRemoveDownstreamServerPrivilegedPassport(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	serverID := strings.TrimSpace(r.PathValue("id"))
	passportID := strings.TrimSpace(r.PathValue("passportID"))
	if err := s.store.RemoveDownstreamServerPrivilegedPassport(r.Context(), serverID, passportID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, api.NewError(http.StatusNotFound, "server.privileged_passport.not_found", "privileged passport entry not found"))
			return
		}
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.privileged_remove_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, serverID, "server.privileged_passport.remove", map[string]any{
		"passport_id": passportID,
	})
	s.pushAllNodeSync(r.Context(), "server.privileged_passport.remove")
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminDeleteDownstreamServer(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.id_required", "server id is required"))
		return
	}
	if err := s.store.DeleteDownstreamServer(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	settings := s.portalSettings(r.Context())
	if strings.EqualFold(settings.FallbackServerID, id) {
		settings.FallbackServerID = ""
		_ = s.store.SetSystemSetting(r.Context(), "portal", portalSettingsMap(settings))
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetDownstreamServer, id, "server.delete", nil)
	s.pushAllNodeSync(r.Context(), "server.delete")
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminPortalSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := s.portalSettings(r.Context())
	api.WriteJSON(w, http.StatusOK, portalSettingsData(settings, s.store.ListDownstreamServers(r.Context())), nil)
}

func (s *Server) handleAdminUpdatePortalSettings(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req portalSettingsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	cookieKey := strings.TrimSpace(req.TransferCookieKey)
	if cookieKey == "" {
		cookieKey = "authman:transfer_grant"
	}
	settings := portalSettings{
		TransferCookieKey:      cookieKey,
		FallbackServerID:       normalizePortalFallbackServerID(req.FallbackServerID),
		MaxProfilesPerPassport: clampMaxProfiles(req.MaxProfilesPerPassport),
		AutoJoinSingleProfile:  req.AutoJoinSingleProfile,
	}
	if settings.FallbackServerID != "" {
		if _, err := s.store.GetDownstreamServer(r.Context(), settings.FallbackServerID); err != nil {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "portal_settings.fallback_not_found", "fallback server not found"))
			return
		}
	}
	if err := s.store.SetSystemSetting(r.Context(), "portal", portalSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "portal_settings.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "portal-settings", "portal_settings.update", portalSettingsMap(settings))
	s.pushAllNodeSync(r.Context(), "portal_settings.update")
	api.WriteJSON(w, http.StatusOK, portalSettingsData(settings, s.store.ListDownstreamServers(r.Context())), nil)
}

type portalSettings struct {
	TransferCookieKey      string
	FallbackServerID       string
	MaxProfilesPerPassport int
	// AutoJoinSingleProfile skips the profile manager when a passport owns
	// exactly one profile. Off by default: the manager is the standard flow.
	AutoJoinSingleProfile bool
}

func clampMaxProfiles(value int) int {
	if value < 1 {
		return 3
	}
	if value > 16 {
		return 16
	}
	return value
}

func (s *Server) portalSettings(ctx context.Context) portalSettings {
	defaults := portalSettings{
		TransferCookieKey:      "authman:transfer_grant",
		FallbackServerID:       "",
		MaxProfilesPerPassport: 3,
	}
	raw, err := s.store.GetSystemSetting(ctx, "portal")
	if err == nil {
		return portalSettings{
			TransferCookieKey:      stringValue(raw["transfer_cookie_key"], defaults.TransferCookieKey),
			FallbackServerID:       normalizePortalFallbackServerID(stringValue(raw["fallback_server_id"], defaults.FallbackServerID)),
			MaxProfilesPerPassport: clampMaxProfiles(intValue(raw["max_profiles_per_passport"], defaults.MaxProfilesPerPassport)),
			AutoJoinSingleProfile:  boolValue(raw["auto_join_single_profile"], false),
		}
	}
	return defaults
}

func normalizePortalFallbackServerID(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "disconnect") {
		return ""
	}
	return value
}

func portalSettingsMap(settings portalSettings) map[string]any {
	return map[string]any{
		"transfer_cookie_key":       strings.TrimSpace(settings.TransferCookieKey),
		"fallback_server_id":        strings.TrimSpace(settings.FallbackServerID),
		"max_profiles_per_passport": clampMaxProfiles(settings.MaxProfilesPerPassport),
		"auto_join_single_profile":  settings.AutoJoinSingleProfile,
	}
}

func portalSettingsData(settings portalSettings, servers []store.DownstreamServer) map[string]any {
	available := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		available = append(available, map[string]any{
			"id":           server.ID,
			"slug":         server.Slug,
			"display_name": server.DisplayName,
			"status":       server.Status,
		})
	}
	return map[string]any{
		"transfer_cookie_key":       strings.TrimSpace(settings.TransferCookieKey),
		"fallback_server_id":        strings.TrimSpace(settings.FallbackServerID),
		"max_profiles_per_passport": clampMaxProfiles(settings.MaxProfilesPerPassport),
		"auto_join_single_profile":  settings.AutoJoinSingleProfile,
		"available_servers":         available,
	}
}

func downstreamServerFromRequest(req downstreamServerRequest) (store.DownstreamServer, *api.Error) {
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.display_name_required", "display name is required")
	}
	status := statusFromServerFlags(req.Enabled, req.Visible, req.Status)
	if status == "" {
		status = "active"
	}
	switch status {
	case "active", "hidden", "disabled":
	default:
		return store.DownstreamServer{}, api.NewError(http.StatusBadRequest, "server.status_invalid", "server status is invalid")
	}
	routingConfig := map[string]any{}
	for key, value := range req.RoutingConfig {
		routingConfig[key] = value
	}
	if raw, ok := routingConfig["motd"].(string); ok {
		routingConfig["motd"] = store.LimitMiniMessageLines(raw, store.DefaultMOTDMaxLines)
	}
	return store.DownstreamServer{
		ID:                 strings.TrimSpace(req.ID),
		Slug:               strings.TrimSpace(req.ID),
		DisplayName:        name,
		Status:             status,
		RegistrationOpen:   req.RegistrationOpen,
		PortalTheme:        map[string]any{},
		PortalConfig:       routingConfig,
		ExtensionProviders: req.ExtensionProviders,
	}, nil
}

func statusFromServerFlags(enabled *bool, visible *bool, fallback string) string {
	if enabled == nil && visible == nil {
		return strings.TrimSpace(fallback)
	}
	if enabled != nil && !*enabled {
		return "disabled"
	}
	if visible != nil && !*visible {
		return "hidden"
	}
	return "active"
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
	events := s.store.ListAuditEvents(r.Context(), 500)
	summaries := make([]map[string]any, 0, 5)
	recentOfflineFailures := 0
	recentWindow := time.Now().UTC().Add(-24 * time.Hour)
	for _, event := range events {
		if len(summaries) < 5 {
			summaries = append(summaries, auditEventSummaryData(event))
		}
		if event.Occurred.After(recentWindow) && (event.Type == "offline.password.failure" || event.Type == "passport.session.login_failure") {
			recentOfflineFailures++
		}
	}
	if len(summaries) == 0 {
		summaries = []map[string]any{}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"total_players":                 len(players),
		"premium_players":               premium,
		"offline_players":               offline,
		"recent_offline_login_failures": recentOfflineFailures,
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
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	roleFilter := strings.TrimSpace(query.Get("kind"))
	statusFilter := strings.TrimSpace(query.Get("status"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	users := []map[string]any{s.adminDataWithSecurity(r.Context())}
	for _, user := range s.store.ListAdminUsers(r.Context()) {
		users = append(users, s.adminUserDataWithSecurity(r.Context(), user))
	}
	filtered := users[:0]
	for _, user := range users {
		if roleFilter != "" && fmt.Sprint(user["role"]) != roleFilter {
			continue
		}
		if statusFilter != "" && fmt.Sprint(user["status"]) != statusFilter {
			continue
		}
		if search != "" && !containsFold(fmt.Sprint(user["username"]), search) && !containsFold(fmt.Sprint(user["email"]), search) && !containsFold(fmt.Sprint(user["display_name"]), search) {
			continue
		}
		filtered = append(filtered, user)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		cmp := compareMapListValues(filtered[i], filtered[j], sortKey)
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(filtered), params)
	data := append([]map[string]any(nil), filtered[start:end]...)
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
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
	params := parseListPageParams(r)
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	sortKey := strings.TrimSpace(query.Get("sort"))
	sortDir := strings.TrimSpace(query.Get("dir"))
	roles := s.store.ListAdminRoles(r.Context())
	data := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		row := adminRoleData(role)
		if search != "" && !containsFold(fmt.Sprint(row["id"]), search) && !containsFold(fmt.Sprint(row["name"]), search) && !containsFold(fmt.Sprint(row["alias"]), search) && !containsFold(fmt.Sprint(row["description"]), search) {
			continue
		}
		data = append(data, row)
	}
	sort.SliceStable(data, func(i, j int) bool {
		cmp := compareMapListValues(data[i], data[j], sortKey)
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(data), params)
	paged := append([]map[string]any(nil), data[start:end]...)
	api.WriteJSON(w, http.StatusOK, paged, listMeta(len(paged), len(data), params))
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
	if override := s.bootstrapAdminPasswordHash(ctx); override != "" {
		ok, err := auth.VerifyPassword(password, override)
		return err == nil && ok
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

func randomServerID(prefix string) (string, error) {
	token, err := auth.NewOpaqueToken(18)
	if err != nil {
		return "", err
	}
	token = strings.NewReplacer("-", "", "_", "").Replace(token)
	if len(token) > 24 {
		token = token[:24]
	}
	return prefix + "-" + strings.ToLower(token), nil
}

func auditEventData(event audit.Event) map[string]any {
	clientIP := detailText(event.Details["client_ip"])
	return map[string]any{
		"id":             event.ID,
		"occurred_at":    event.Occurred,
		"created_at":     event.Occurred,
		"schema_version": event.SchemaVersion,
		"category":       event.Category,
		"outcome":        event.Outcome,
		"source":         event.Source,
		"session_id":     event.SessionID,
		"correlation_id": event.CorrelationID,
		"actor_type":     event.ActorType,
		"actor_id":       event.ActorID,
		"target_type":    event.Target,
		"target_id":      event.TargetID,
		"type":           event.Type,
		"event_type":     event.Type,
		"client_ip":      emptyStringNil(clientIP),
		"client_geo":     event.Details["client_geo"],
		"details":        event.Details,
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

func (s *Server) relatedAuditSummaries(ctx context.Context, limit int, ids ...string) []map[string]any {
	idSet := auditRelatedIDSet(strings.Join(ids, ","))
	if len(idSet) == 0 {
		return []map[string]any{}
	}
	events := s.store.ListAuditEvents(ctx, 500)
	out := make([]map[string]any, 0, limit)
	for _, event := range events {
		if auditEventMatchesIDs(event, idSet) {
			out = append(out, auditEventSummaryData(event))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func auditRelatedIDSet(raw string) map[string]struct{} {
	idSet := map[string]struct{}{}
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			idSet[id] = struct{}{}
		}
	}
	return idSet
}

func auditEventMatchesIDs(event audit.Event, ids map[string]struct{}) bool {
	if _, ok := ids[event.ActorID]; ok {
		return true
	}
	if _, ok := ids[event.TargetID]; ok {
		return true
	}
	for _, value := range event.Details {
		switch typed := value.(type) {
		case string:
			if _, ok := ids[typed]; ok {
				return true
			}
		case []string:
			for _, item := range typed {
				if _, ok := ids[item]; ok {
					return true
				}
			}
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					if _, exists := ids[text]; exists {
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Server) enrichPassportRow(ctx context.Context, row map[string]any, passport identity.Passport, now time.Time) {
	if lockedUntil := s.passportLockedUntil(ctx, passport); lockedUntil != nil {
		row["locked_until"] = lockedUntil
	}
	if ban, ok := firstActiveBan(s.store.ListBans(ctx, store.BanScopePassport, passport.ID, false, now), now); ok {
		row["active_ban"] = banRows([]store.PlayerBan{ban})[0]
		row["ban_expires_at"] = ban.ExpiresAt
	}
}

func (s *Server) enrichProfileRow(ctx context.Context, row map[string]any, profile identity.Profile, passport *identity.Passport, now time.Time) {
	if passport != nil {
		if lockedUntil := s.passportLockedUntil(ctx, *passport); lockedUntil != nil {
			row["locked_until"] = lockedUntil
		}
		if ban, ok := firstActiveBan(s.store.ListBans(ctx, store.BanScopePassport, passport.ID, false, now), now); ok {
			row["active_ban"] = banRows([]store.PlayerBan{ban})[0]
			row["ban_expires_at"] = ban.ExpiresAt
		}
	}
	if ban, ok := firstActiveBan(s.store.ListBans(ctx, store.BanScopeProfile, profile.ID, false, now), now); ok {
		row["active_ban"] = banRows([]store.PlayerBan{ban})[0]
		row["ban_expires_at"] = ban.ExpiresAt
	}
}

func (s *Server) passportLockedUntil(ctx context.Context, passport identity.Passport) *time.Time {
	if passport.Kind != identity.PassportKindOffline {
		return nil
	}
	_, credential, err := s.store.GetPassportCredential(ctx, passport.Username)
	if err != nil {
		return nil
	}
	return credential.LockedUntil
}

func sortPassports(passports []identity.Passport, key string, dir string) {
	desc := dir == "desc"
	sort.SliceStable(passports, func(i, j int) bool {
		a := passports[i]
		b := passports[j]
		cmp := 0
		switch key {
		case "username":
			cmp = strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
		case "kind":
			cmp = strings.Compare(string(a.Kind), string(b.Kind))
		case "status":
			cmp = strings.Compare(string(a.Status), string(b.Status))
		case "uuid":
			cmp = strings.Compare(a.UUID.String(), b.UUID.String())
		case "lastSeen":
			cmp = compareTimePtr(a.LastSeenAt, b.LastSeenAt)
		case "created":
			cmp = compareTime(a.CreatedAt, b.CreatedAt)
		default:
			cmp = compareTime(b.CreatedAt, a.CreatedAt)
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func sortProfiles(profiles []identity.Profile, key string, dir string) {
	desc := dir == "desc"
	sort.SliceStable(profiles, func(i, j int) bool {
		a := profiles[i]
		b := profiles[j]
		cmp := 0
		switch key {
		case "protocol":
			cmp = strings.Compare(strings.ToLower(a.ProtocolName), strings.ToLower(b.ProtocolName))
		case "uuid":
			cmp = strings.Compare(a.UUID.String(), b.UUID.String())
		case "status":
			cmp = strings.Compare(string(a.Status), string(b.Status))
		case "lastSeen":
			cmp = compareTimePtr(a.LastSeenAt, b.LastSeenAt)
		case "created":
			cmp = compareTime(a.CreatedAt, b.CreatedAt)
		default:
			cmp = strings.Compare(strings.ToLower(a.ProtocolName), strings.ToLower(b.ProtocolName))
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareMapListValues(a map[string]any, b map[string]any, key string) int {
	if key == "" {
		key = "name"
	}
	left := mapListSortValue(a, key)
	right := mapListSortValue(b, key)
	return strings.Compare(strings.ToLower(left), strings.ToLower(right))
}

func mapListSortValue(row map[string]any, key string) string {
	switch key {
	case "route":
		key = "id"
	case "name":
		if value := fmt.Sprint(row["display_name"]); value != "" && value != "<nil>" {
			return value
		}
		if value := fmt.Sprint(row["username"]); value != "" && value != "<nil>" {
			return value
		}
	case "count":
		key = "request_count"
	case "created":
		key = "created_at"
	case "updated":
		key = "updated_at"
	}
	value := fmt.Sprint(row[key])
	if value == "<nil>" {
		return ""
	}
	return value
}

func compareTimePtr(a *time.Time, b *time.Time) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	return compareTime(*a, *b)
}

func compareTime(a time.Time, b time.Time) int {
	if a.Equal(b) {
		return 0
	}
	if a.Before(b) {
		return -1
	}
	return 1
}

func (s *Server) banFromRequest(r *http.Request, scope store.BanScope, targetID string, createdBy string) (store.PlayerBan, *api.Error) {
	var req banRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		return store.PlayerBan{}, err
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return store.PlayerBan{}, api.NewError(http.StatusBadRequest, "ban.reason_required", "ban reason is required")
	}
	if len(reason) > 500 {
		return store.PlayerBan{}, api.NewError(http.StatusBadRequest, "ban.reason_too_long", "ban reason is too long")
	}
	expiresAt, apiErr := parseBanExpiry(req)
	if apiErr != nil {
		return store.PlayerBan{}, apiErr
	}
	return store.PlayerBan{
		Scope:     scope,
		TargetID:  targetID,
		Reason:    reason,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
}

func parseBanExpiry(req banRequest) (*time.Time, *api.Error) {
	if strings.TrimSpace(req.ExpiresAt) != "" {
		value, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
		if err != nil {
			return nil, api.NewError(http.StatusBadRequest, "ban.expiry_invalid", "ban expiry must be RFC3339")
		}
		if !value.After(time.Now()) {
			return nil, api.NewError(http.StatusBadRequest, "ban.expiry_invalid", "ban expiry must be in the future")
		}
		value = value.UTC()
		return &value, nil
	}
	if req.ExpiresInSeconds > 0 {
		if req.ExpiresInSeconds < 1 {
			return nil, api.NewError(http.StatusBadRequest, "ban.expiry_invalid", "temporary ban must last at least 1 second")
		}
		value := time.Now().Add(time.Duration(req.ExpiresInSeconds) * time.Second).UTC()
		return &value, nil
	}
	return nil, nil
}

func normalizeKickReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "kicked by administrator"
	}
	if len(reason) > 500 {
		return reason[:500]
	}
	return reason
}

func (s *Server) audit(r *http.Request, actorType audit.ActorType, actorID string, target audit.TargetType, targetID string, eventType string, details map[string]any) {
	s.auditWithClientIP(r, "", actorType, actorID, target, targetID, eventType, details)
}

func (s *Server) auditWithClientIP(r *http.Request, ipOverride string, actorType audit.ActorType, actorID string, target audit.TargetType, targetID string, eventType string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	if _, ok := details["request_id"]; !ok {
		if requestID, ok := r.Context().Value(requestIDKey{}).(string); ok && requestID != "" {
			details["request_id"] = requestID
		}
	}
	if _, ok := details["path"]; !ok {
		details["path"] = r.URL.Path
	}
	if _, ok := details["method"]; !ok {
		details["method"] = r.Method
	}
	if _, ok := details["source"]; !ok {
		details["source"] = "core-http"
	}
	if _, ok := details["client_ip"]; !ok {
		if ip := normalizeClientIPValue(ipOverride); ip != "" {
			details["client_ip"] = ip
		} else {
			details["client_ip"] = clientIP(r)
		}
	}
	if _, ok := details["client_geo"]; !ok {
		if ip := detailText(details["client_ip"]); ip != "" {
			if geo := s.lookupIPGeo(r.Context(), ip); geo != nil {
				details["client_geo"] = geo
			}
		}
	}
	if _, ok := details["user_agent"]; !ok {
		details["user_agent"] = strings.TrimSpace(r.UserAgent())
	}
	_, _ = s.store.AppendAuditEvent(r.Context(), audit.NewEvent(time.Now(), actorType, actorID, target, targetID, eventType, details))
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if first, _, ok := strings.Cut(forwarded, ","); ok {
			return strings.TrimSpace(first)
		}
		return forwarded
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, ok := strings.Cut(r.RemoteAddr, ":")
	if ok && host != "" {
		return strings.Trim(host, "[]")
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func normalizeClientIPValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.Trim(strings.TrimSpace(value), "[]")
}

func (s *Server) requestIPGeo(r *http.Request) (string, *identity.IPGeo) {
	return s.requestIPGeoWithClientIP(r, "")
}

func (s *Server) requestIPGeoWithClientIP(r *http.Request, ipOverride string) (string, *identity.IPGeo) {
	ip := normalizeClientIPValue(ipOverride)
	if ip == "" {
		ip = clientIP(r)
	}
	return ip, s.lookupIPGeo(r.Context(), ip)
}

func (s *Server) lookupIPGeo(ctx context.Context, ip string) *identity.IPGeo {
	if s.ipGeo == nil {
		return nil
	}
	return s.ipGeo.lookup(ctx, ip)
}

func detailText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}
