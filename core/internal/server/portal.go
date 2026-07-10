package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/identity"
)

type offlineRegisterRequest struct {
	Username    string `json:"username"`
	RawUsername string `json:"raw_username"`
	Password    string `json:"password"`
	ServerSlug  string `json:"server_slug"`
}

func (s *Server) handleOfflineRegister(w http.ResponseWriter, r *http.Request) {
	var req offlineRegisterRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
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
	username := req.Username
	if username == "" {
		username = req.RawUsername
	}
	pp, err := s.store.CreateOfflinePassportProfile(r.Context(), username, username, passwordHash, encryptedPassword, keyFingerprint)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.offline_registration_failed", err.Error()))
		return
	}
	player := identity.PlayerFromPassportProfileLink(pp)
	now := time.Now()
	s.recordPassportProfileSeen(r, pp.Passport, pp.Profile, req.ServerSlug, now)
	s.audit(r, audit.ActorPlayer, pp.Passport.ID, audit.TargetPlayer, pp.Passport.ID, "offline.register", map[string]any{
		"raw_offline_name": pp.Passport.RawOfflineName,
		"profile_id":       pp.Profile.ID,
		"protocol_name":    pp.Profile.ProtocolName,
		"server_slug":      req.ServerSlug,
	})
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, pp.Passport.ID, 24*time.Hour, now)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	session.SelectedProfileID = pp.Profile.ID
	if err := s.saveSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to save session"))
		return
	}
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"player":     portalPlayerData(player),
		"passport":   passportRowData(pp.Passport, []identity.Profile{pp.Profile}, s.store.ListPassportPresences(r.Context(), pp.Passport.ID)),
		"profiles":   []map[string]any{profileSummaryData(pp.Profile, nil)},
		"profile":    profileRowData(pp.Profile, &pp.Passport, s.store.ListProfilePresences(r.Context(), pp.Profile.ID)),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

type portalLoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	ServerSlug string `json:"server_slug"`
}

func (s *Server) handlePortalLogin(w http.ResponseWriter, r *http.Request) {
	var req portalLoginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	passport, credential, err := s.store.GetPassportCredential(r.Context(), req.Username)
	if err != nil {
		s.audit(r, audit.ActorPlayer, strings.TrimSpace(req.Username), audit.TargetPlayer, strings.TrimSpace(req.Username), "passport.session.login_failure", map[string]any{
			"reason":      "credential_not_found",
			"server_slug": req.ServerSlug,
		})
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	if passport.Status == identity.PassportStatusLocked || passport.Status == identity.PassportStatusDeleted || passportCredentialLocked(credential, time.Now()) {
		s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "passport.session.login_rejected", map[string]any{
			"reason":          "account_locked",
			"passport_status": passport.Status,
			"locked_until":    credential.LockedUntil,
			"server_slug":     req.ServerSlug,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		updatedCredential, _ := s.store.RecordPassportLoginFailure(r.Context(), passport.ID, time.Now())
		s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "passport.session.login_failure", map[string]any{
			"reason":          "password_mismatch",
			"failed_attempts": updatedCredential.FailedAttempts,
			"locked_until":    updatedCredential.LockedUntil,
			"server_slug":     req.ServerSlug,
		})
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	_ = s.store.RecordPassportLoginSuccess(r.Context(), passport.ID)
	profile, err := s.store.GetPrimaryProfileForPassport(r.Context(), passport.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.none", "passport has no profile"))
		return
	}
	player := identity.PlayerFromPassportProfile(passport, profile)
	now := time.Now()
	s.recordPassportProfileSeen(r, passport, profile, req.ServerSlug, now)
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, passport.ID, 24*time.Hour, now)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	session.SelectedProfileID = profile.ID
	if err := s.saveSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to save session"))
		return
	}
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPortalSession, session.ID, "passport.session.login", map[string]any{
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
		"server_slug":   req.ServerSlug,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":     portalPlayerData(player),
		"passport":   passportRowData(passport, s.store.ListProfilesForPassport(r.Context(), passport.ID), s.store.ListPassportPresences(r.Context(), passport.ID)),
		"profiles":   portalProfileListData(s.store.ListProfilesForPassport(r.Context(), passport.ID)),
		"profile":    profileRowData(profile, &passport, s.store.ListProfilePresences(r.Context(), profile.ID)),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) handlePortalMe(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, profile, player, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	csrf, csrfErr := s.rotateCSRF(r.Context(), session)
	if csrfErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to refresh CSRF token"))
		return
	}
	profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":     portalPlayerData(player),
		"passport":   passportRowData(passport, profiles, s.store.ListPassportPresences(r.Context(), passport.ID)),
		"profiles":   portalProfileListData(profiles),
		"profile":    profileRowData(profile, &passport, s.store.ListProfilePresences(r.Context(), profile.ID)),
		"csrf_token": csrf,
	}, nil)
}

func (s *Server) handlePortalLogout(w http.ResponseWriter, r *http.Request) {
	if session, err := s.requirePlayer(r, true); err == nil {
		if cookie, cookieErr := r.Cookie(playerSessionCookie); cookieErr == nil {
			s.deleteSession(r.Context(), cookie.Value)
		}
		s.audit(r, audit.ActorPlayer, session.SubjectID, audit.TargetPortalSession, session.ID, "player.session.logout", nil)
	}
	clearSessionCookie(w, r, playerSessionCookie)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

type portalChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handlePortalChangePassword(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	var req portalChangePasswordRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	_, credential, err := s.store.GetPassportCredential(r.Context(), passport.Username)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.credential_not_found", "offline credential not found"))
		return
	}
	ok, err := auth.VerifyPassword(req.CurrentPassword, credential.PasswordHash)
	if err != nil || !ok {
		s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "offline.password.change_failure", map[string]any{"reason": "current_password_mismatch"})
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid current password"))
		return
	}
	passwordHash, err := auth.HashPassword(req.NewPassword, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	encryptedPassword, keyFingerprint, err := s.offlinePasswordCredential(r.Context(), req.NewPassword)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.encrypt_failed", "failed to encrypt recoverable password"))
		return
	}
	if err := s.store.UpdatePassportPassword(r.Context(), passport.ID, passwordHash, encryptedPassword, keyFingerprint); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "auth.password_update_failed", "failed to update password"))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "offline.password.change", map[string]any{"profile_id": session.SelectedProfileID})
	api.WriteJSON(w, http.StatusOK, nil, nil)
}

func (s *Server) handlePortalConfig(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"registration_open":     true,
		"password_policy_hints": []string{"8-128 characters"},
		"message":               "Authman central portal",
	}, nil)
}

func (s *Server) handlePortalServers(w http.ResponseWriter, r *http.Request) {
	servers := s.store.ListDownstreamServers(r.Context())
	data := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		if server.Status == "disabled" {
			continue
		}
		if show, ok := server.PortalConfig["show_in_global"].(bool); ok && !show {
			continue
		}
		data = append(data, portalServerListData(server))
	}
	api.WriteJSON(w, http.StatusOK, data, map[string]any{"count": len(data)})
}

func (s *Server) handlePortalServerConfig(w http.ResponseWriter, r *http.Request) {
	server, err := s.store.GetDownstreamServer(r.Context(), r.PathValue("slug"))
	if err != nil || server.Status == "disabled" {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, portalServerConfigData(server), nil)
}

func (s *Server) handlePortalCheckName(w http.ResponseWriter, r *http.Request) {
	var req offlineRegisterRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := req.Username
	if username == "" {
		username = req.RawUsername
	}
	if _, err := identity.NormalizeOfflineName(username); err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"available": false, "reason": err.Error()}, nil)
		return
	}
	if _, err := s.store.GetOfflinePlayer(r.Context(), username); err == nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "already_registered"}, nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"available": true}, nil)
}

func (s *Server) handlePortalExtensionData(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	_, _, player, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session player was not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, s.playerExtensionData(r.Context(), player, r.PathValue("serverSlug"), false), nil)
}

type portalLinkLoginRequest struct {
	Token string `json:"token"`
}

type portalSelectProfileRequest struct {
	ProfileID string `json:"profile_id"`
}

type portalCreateProfileRequest struct {
	ProtocolName string `json:"protocol_name"`
}

func (s *Server) handlePortalSelectProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req portalSelectProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), strings.TrimSpace(req.ProfileID))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	passport, err := s.store.GetPassportForProfile(r.Context(), profile.ID)
	if err != nil || passport.ID != session.SubjectID {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.not_owned", "profile is not available for this passport"))
		return
	}
	session.SelectedProfileID = profile.ID
	if err := s.store.UpdateSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to update session"))
		return
	}
	s.recordPassportProfileSeen(r, passport, profile, "", time.Now())
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, profile.ID, "passport.profile.select", map[string]any{
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
	})
	profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"passport": passportRowData(passport, profiles, s.store.ListPassportPresences(r.Context(), passport.ID)),
		"profiles": portalProfileListData(profiles),
		"profile":  profileRowData(profile, &passport, s.store.ListProfilePresences(r.Context(), profile.ID)),
		"player":   portalPlayerData(identity.PlayerFromPassportProfile(passport, profile)),
	}, nil)
}

func (s *Server) handlePortalCreateProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable"))
		return
	}
	var req portalCreateProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := s.portalSettings(r.Context())
	existing := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	if len(existing) >= settings.MaxProfilesPerPassport {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.limit_reached", "this passport already has the maximum number of profiles"))
		return
	}
	name, err := identity.NormalizeProtocolName(req.ProtocolName)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	for _, profile := range existing {
		if strings.EqualFold(profile.NormalizedName, name.Normalized) {
			api.WriteError(w, api.NewError(http.StatusConflict, "profile.name_taken", "protocol name is already taken for this passport"))
			return
		}
	}
	profile, err := identity.NewOfflineProfile("", name.Protocol, passport.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	profile, err = s.store.CreateProfile(r.Context(), profile)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.create_failed", err.Error()))
		return
	}
	pp, err := s.store.BindProfileToPassport(r.Context(), profile.ID, passport.ID, false)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.bind_failed", err.Error()))
		return
	}
	session.SelectedProfileID = pp.Profile.ID
	if err := s.store.UpdateSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to update session"))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, profile.ID, "profile.self_create", map[string]any{
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
	})
	s.writePortalSessionData(w, r, passport, pp.Profile)
}

func (s *Server) handlePortalArchiveProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, target, authErr := s.portalOwnedProfile(r, session, r.PathValue("id"))
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	fallback := identity.Profile{}
	for _, profile := range profiles {
		if profile.ID == target.ID {
			continue
		}
		if profile.Status == identity.ProfileStatusActive && fallback.ID == "" {
			fallback = profile
		}
	}
	if target.Status == identity.ProfileStatusActive && fallback.ID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.last_active", "cannot archive the last active profile"))
		return
	}
	updated, err := s.store.SetProfileStatus(r.Context(), target.ID, identity.ProfileStatusArchived)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "profile.archive_failed", "failed to archive profile"))
		return
	}
	selected := updated
	if session.SelectedProfileID == target.ID {
		selected = fallback
		session.SelectedProfileID = fallback.ID
		if err := s.store.UpdateSession(r.Context(), session); err != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to update session"))
			return
		}
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, target.ID, "profile.self_archive", map[string]any{
		"profile_id":    target.ID,
		"protocol_name": target.ProtocolName,
	})
	s.writePortalSessionData(w, r, passport, selected)
}

func (s *Server) handlePortalRestoreProfile(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, target, authErr := s.portalOwnedProfile(r, session, r.PathValue("id"))
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	updated, err := s.store.SetProfileStatus(r.Context(), target.ID, identity.ProfileStatusActive)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "profile.restore_failed", "failed to restore profile"))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, target.ID, "profile.self_restore", map[string]any{
		"profile_id":    target.ID,
		"protocol_name": target.ProtocolName,
	})
	selected, err := s.store.GetProfileByID(r.Context(), session.SelectedProfileID)
	if err != nil {
		selected = updated
	}
	s.writePortalSessionData(w, r, passport, selected)
}

func (s *Server) handlePortalLinkLogin(w http.ResponseWriter, r *http.Request) {
	var req portalLinkLoginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	link, err := s.getPortalLink(r.Context(), req.Token)
	if err != nil {
		s.audit(r, audit.ActorSystem, "portal-link", audit.TargetPortalSession, auth.TokenFingerprint(req.Token), "passport.session.link_login_failure", map[string]any{"reason": "link_not_found"})
		api.WriteError(w, api.NewError(http.StatusNotFound, "portal_link.not_found", "portal link was not found"))
		return
	}
	now := time.Now()
	switch result := link.Verify(req.Token, now); result {
	case auth.PortalLinkVerifyOK:
	default:
		s.audit(r, audit.ActorPlayer, link.PlayerID, audit.TargetPortalSession, link.ID, "passport.session.link_login_failure", map[string]any{
			"reason":    string(result),
			"server_id": link.ServerID,
		})
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "portal_link."+string(result), "portal link cannot be used"))
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), link.PlayerID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive || portalLinkKind(passport) != link.Kind {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.locked", "passport is not active"))
		return
	}
	profile, err := s.portalLinkProfile(r.Context(), passport.ID, link.SuggestedProfileID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.none", "portal link has no active profile"))
		return
	}
	if _, err := s.markPortalLinkUsed(r.Context(), req.Token, now); err != nil {
		api.WriteError(w, s.portalLinkUseError(r.Context(), req.Token, now))
		return
	}
	player := identity.PlayerFromPassportProfile(passport, profile)
	s.recordPassportProfileSeen(r, passport, profile, link.ServerID, now)
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, passport.ID, 24*time.Hour, now)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	session.SelectedProfileID = profile.ID
	if err := s.saveSession(r.Context(), session); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to save session"))
		return
	}
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPortalSession, session.ID, "passport.session.link_login", map[string]any{
		"server_id":     link.ServerID,
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
		"link_id":       link.ID,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":     portalPlayerData(player),
		"passport":   passportRowData(passport, s.store.ListProfilesForPassport(r.Context(), passport.ID), s.store.ListPassportPresences(r.Context(), passport.ID)),
		"profiles":   portalProfileListData(s.store.ListProfilesForPassport(r.Context(), passport.ID)),
		"profile":    profileRowData(profile, &passport, s.store.ListProfilePresences(r.Context(), profile.ID)),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) portalSessionIdentity(ctx context.Context, session auth.Session) (identity.Passport, identity.Profile, identity.Player, error) {
	passport, err := s.store.GetPassportByID(ctx, session.SubjectID)
	if err != nil {
		return identity.Passport{}, identity.Profile{}, identity.Player{}, err
	}
	var profile identity.Profile
	if session.SelectedProfileID != "" {
		profile, err = s.store.GetProfileByID(ctx, session.SelectedProfileID)
		if err == nil {
			if owner, ownerErr := s.store.GetPassportForProfile(ctx, profile.ID); ownerErr == nil && owner.ID == passport.ID {
				return passport, profile, identity.PlayerFromPassportProfile(passport, profile), nil
			}
		}
	}
	profile, err = s.store.GetPrimaryProfileForPassport(ctx, passport.ID)
	if err != nil {
		return identity.Passport{}, identity.Profile{}, identity.Player{}, err
	}
	session.SelectedProfileID = profile.ID
	_ = s.store.UpdateSession(ctx, session)
	return passport, profile, identity.PlayerFromPassportProfile(passport, profile), nil
}

func (s *Server) portalOwnedProfile(r *http.Request, session auth.Session, profileID string) (identity.Passport, identity.Profile, *api.Error) {
	passport, err := s.store.GetPassportByID(r.Context(), session.SubjectID)
	if err != nil {
		return identity.Passport{}, identity.Profile{}, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found")
	}
	if passport.Status != identity.PassportStatusActive {
		return identity.Passport{}, identity.Profile{}, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable")
	}
	profile, err := s.store.GetProfileByID(r.Context(), strings.TrimSpace(profileID))
	if err != nil {
		return identity.Passport{}, identity.Profile{}, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found")
	}
	owner, err := s.store.GetPassportForProfile(r.Context(), profile.ID)
	if err != nil || owner.ID != passport.ID {
		return identity.Passport{}, identity.Profile{}, api.NewError(http.StatusForbidden, "profile.not_owned", "profile is not available for this passport")
	}
	return passport, profile, nil
}

func (s *Server) writePortalSessionData(w http.ResponseWriter, r *http.Request, passport identity.Passport, profile identity.Profile) {
	profiles := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":   portalPlayerData(identity.PlayerFromPassportProfile(passport, profile)),
		"passport": passportRowData(passport, profiles, s.store.ListPassportPresences(r.Context(), passport.ID)),
		"profiles": portalProfileListData(profiles),
		"profile":  profileRowData(profile, &passport, s.store.ListProfilePresences(r.Context(), profile.ID)),
	}, nil)
}

func portalProfileListData(profiles []identity.Profile) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profileSummaryData(profile, nil))
	}
	return out
}

func (s *Server) getPortalLink(ctx context.Context, token string) (auth.PortalLink, error) {
	token = strings.TrimSpace(token)
	return s.store.GetPortalLink(ctx, auth.HashToken("portal-link", token))
}

func (s *Server) markPortalLinkUsed(ctx context.Context, token string, now time.Time) (auth.PortalLink, error) {
	key := auth.HashToken("portal-link", strings.TrimSpace(token))
	return s.store.MarkPortalLinkUsed(ctx, key, now)
}

func (s *Server) portalLinkUseError(ctx context.Context, token string, now time.Time) *api.Error {
	link, err := s.getPortalLink(ctx, token)
	if err != nil {
		return api.NewError(http.StatusNotFound, "portal_link.not_found", "portal link was not found")
	}
	result := link.Verify(token, now)
	if result == auth.PortalLinkVerifyOK {
		result = auth.PortalLinkVerifyNotFound
	}
	return api.NewError(http.StatusUnauthorized, "portal_link."+string(result), "portal link cannot be used")
}
