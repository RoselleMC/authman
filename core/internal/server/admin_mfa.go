package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	adminMFATTL        = 5 * time.Minute
	trustedDeviceTTL   = 30 * 24 * time.Hour
	passkeyRegisterTTL = 5 * time.Minute
)

type adminPreferencesRequest struct {
	MFARequirement  string `json:"mfa_requirement"`
	PreferredLocale string `json:"preferred_locale"`
	PreferredTheme  string `json:"preferred_theme"`
}

type adminProfileRequest struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type adminTOTPVerifyRequest struct {
	Code string `json:"code"`
}

type adminMFATOTPRequest struct {
	Code        string `json:"code"`
	TrustDevice bool   `json:"trust_device"`
}

type adminPasskeyRegisterOptionsRequest struct {
	Name string `json:"name"`
}

type adminPasskeyRegisterFinishRequest struct {
	ChallengeID string          `json:"challenge_id"`
	Name        string          `json:"name"`
	Credential  json.RawMessage `json:"credential"`
}

func (s *Server) maybeStartAdminMFA(w http.ResponseWriter, r *http.Request, adminID string, user map[string]any) bool {
	security, _ := s.store.GetAdminSecurity(r.Context(), adminID)
	methods := s.adminMFAMethods(r.Context(), adminID, security)
	if len(methods) == 0 {
		return false
	}
	if security.MFARequirement != "always" {
		if cookie, err := r.Cookie(adminTrustedCookie); err == nil && cookie.Value != "" {
			device, err := s.store.GetAdminTrustedDevice(r.Context(), auth.HashToken("admin-trusted", cookie.Value), time.Now())
			if err == nil && device.AdminID == adminID {
				return false
			}
		}
	}
	pending, err := s.store.SavePendingAdminMFA(r.Context(), store.PendingAdminMFA{
		AdminID:   adminID,
		ExpiresAt: time.Now().UTC().Add(adminMFATTL),
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to start MFA"))
		return true
	}
	setSessionCookie(w, r, adminMFACookie, pending.ID, pending.ExpiresAt)
	s.audit(r, audit.ActorAdmin, adminID, audit.TargetPortalSession, pending.ID, "admin.session.mfa_required", map[string]any{"methods": methods})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"mfa_required": true,
		"methods":      methods,
		"user":         user,
		"expires_at":   pending.ExpiresAt,
	}, nil)
	return true
}

func (s *Server) adminMFAMethods(ctx context.Context, adminID string, security store.AdminSecurity) []string {
	methods := make([]string, 0, 2)
	if security.TOTPEnabled && security.TOTPSecret != "" {
		methods = append(methods, "totp")
	}
	if len(s.store.ListAdminPasskeys(ctx, adminID)) > 0 {
		methods = append(methods, "passkey")
	}
	return methods
}

func (s *Server) handleAdminMFATOTP(w http.ResponseWriter, r *http.Request) {
	var req adminMFATOTPRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	pending, apiErr := s.pendingAdminMFA(r)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), pending.AdminID)
	if !security.TOTPEnabled || !auth.VerifyTOTP(security.TOTPSecret, req.Code, time.Now()) {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.mfa_invalid", "invalid verification code"))
		return
	}
	s.completeAdminMFA(w, r, pending, req.TrustDevice)
}

func (s *Server) handleAdminMFAPasskeyOptions(w http.ResponseWriter, r *http.Request) {
	pending, err := s.pendingAdminMFA(r)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	user := s.webAuthnUser(r.Context(), pending.AdminID)
	if s.webAuthn == nil || len(user.credentials) == 0 {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_unavailable", "passkey is unavailable"))
		return
	}
	assertion, session, webErr := s.webAuthn.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if webErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "auth.passkey_start_failed", "failed to start passkey verification"))
		return
	}
	sessionJSON, _ := json.Marshal(session)
	pending.WebAuthnSessionJSON = sessionJSON
	if err := s.store.DeletePendingAdminMFA(r.Context(), pending.ID); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to rotate MFA challenge"))
		return
	}
	pending, storeErr := s.store.SavePendingAdminMFA(r.Context(), pending)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to save MFA challenge"))
		return
	}
	setSessionCookie(w, r, adminMFACookie, pending.ID, pending.ExpiresAt)
	api.WriteJSON(w, http.StatusOK, map[string]any{"challenge_id": pending.ID, "options": assertion}, nil)
}

func (s *Server) handleAdminMFAPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	pending, apiErr := s.pendingAdminMFA(r)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(pending.WebAuthnSessionJSON, &session); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_challenge_missing", "passkey challenge is missing"))
		return
	}
	user := s.webAuthnUser(r.Context(), pending.AdminID)
	credential, webErr := s.webAuthn.FinishLogin(user, session, requestWithJSON(r, body))
	if webErr != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.passkey_invalid", "passkey verification failed"))
		return
	}
	s.updateUsedPasskey(r.Context(), pending.AdminID, *credential)
	trust := strings.EqualFold(r.URL.Query().Get("trust_device"), "true")
	s.completeAdminMFA(w, r, pending, trust)
}

func (s *Server) completeAdminMFA(w http.ResponseWriter, r *http.Request, pending store.PendingAdminMFA, trustDevice bool) {
	if time.Now().UTC().After(pending.ExpiresAt) {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.mfa_expired", "MFA challenge expired"))
		return
	}
	user, userErr := s.adminDataForSession(r.Context(), pending.AdminID)
	if userErr != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "admin user no longer exists"))
		return
	}
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionAdmin, pending.AdminID, 12*time.Hour, time.Now())
	if err != nil || s.saveSession(r.Context(), session) != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.session_failed", "failed to create session"))
		return
	}
	if trustDevice {
		token, err := auth.NewOpaqueToken(32)
		if err == nil {
			_, _ = s.store.CreateAdminTrustedDevice(r.Context(), store.AdminTrustedDevice{
				AdminID:   pending.AdminID,
				TokenHash: auth.HashToken("admin-trusted", token),
				UserAgent: r.UserAgent(),
				ExpiresAt: time.Now().UTC().Add(trustedDeviceTTL),
			})
			setSessionCookie(w, r, adminTrustedCookie, token, time.Now().UTC().Add(trustedDeviceTTL))
		}
	}
	_ = s.store.DeletePendingAdminMFA(r.Context(), pending.ID)
	clearSessionCookie(w, r, adminMFACookie)
	setSessionCookie(w, r, adminSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorAdmin, pending.AdminID, audit.TargetPortalSession, session.ID, "admin.session.mfa_success", map[string]any{"trusted": trustDevice})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"admin":      user,
		"user":       user,
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) pendingAdminMFA(r *http.Request) (store.PendingAdminMFA, *api.Error) {
	cookie, err := r.Cookie(adminMFACookie)
	if err != nil || cookie.Value == "" {
		return store.PendingAdminMFA{}, api.NewError(http.StatusUnauthorized, "auth.mfa_required", "MFA challenge is required")
	}
	pending, storeErr := s.store.GetPendingAdminMFA(r.Context(), cookie.Value)
	if storeErr != nil || time.Now().UTC().After(pending.ExpiresAt) {
		return store.PendingAdminMFA{}, api.NewError(http.StatusUnauthorized, "auth.mfa_expired", "MFA challenge expired")
	}
	return pending, nil
}

func (s *Server) handleAdminAccount(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, false)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	user, userErr := s.adminDataForSession(r.Context(), session.SubjectID)
	if userErr != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "admin user no longer exists"))
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"user":     user,
		"security": adminSecurityData(security, s.store.ListAdminPasskeys(r.Context(), session.SubjectID)),
		"webauthn": map[string]any{"enabled": s.webAuthn != nil},
	}, nil)
}

func (s *Server) handleAdminAccountProfile(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	avatarURL := strings.TrimSpace(req.AvatarURL)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.username_required", "username is required"))
		return
	}
	if avatarURL != "" && !validAdminAvatarURL(avatarURL) {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.avatar_invalid", "avatar must be an http/https URL or supported image upload"))
		return
	}

	if session.SubjectID == "bootstrap-admin" {
		if user, conflictErr := s.store.FindAdminUserByIdentifier(r.Context(), username); conflictErr == nil && user.ID != session.SubjectID {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", "username is already in use"))
			return
		}
		if email != "" {
			if user, conflictErr := s.store.FindAdminUserByIdentifier(r.Context(), email); conflictErr == nil && user.ID != session.SubjectID {
				api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", "email is already in use"))
				return
			}
		}
		if _, profileErr := s.store.UpsertAdminProfile(r.Context(), store.AdminProfile{
			AdminID:   session.SubjectID,
			Username:  username,
			Email:     email,
			AvatarURL: avatarURL,
		}); profileErr != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save profile"))
			return
		}
	} else {
		if s.adminIdentifierMatches(r.Context(), username) || (email != "" && s.adminIdentifierMatches(r.Context(), email)) {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", "username or email is already in use"))
			return
		}
		user, updateErr := s.store.UpdateAdminUserProfile(r.Context(), session.SubjectID, username, email)
		if updateErr != nil {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.profile_conflict", updateErr.Error()))
			return
		}
		if _, profileErr := s.store.UpsertAdminProfile(r.Context(), store.AdminProfile{
			AdminID:   session.SubjectID,
			Username:  user.Username,
			Email:     user.Email,
			AvatarURL: avatarURL,
		}); profileErr != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save profile"))
			return
		}
	}

	user, userErr := s.adminDataForSession(r.Context(), session.SubjectID)
	if userErr != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "admin user no longer exists"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, session.SubjectID, "admin.account.profile_update", map[string]any{
		"username": username,
		"email":    email,
		"avatar":   avatarURL != "",
	})
	api.WriteJSON(w, http.StatusOK, user, nil)
}

var adminAvatarDataURLPattern = regexp.MustCompile(`^data:image/(png|jpe?g|webp|gif|svg\+xml);base64,[A-Za-z0-9+/]+={0,2}$`)

func validAdminAvatarURL(value string) bool {
	if len(value) > 256*1024 {
		return false
	}
	if strings.HasPrefix(value, "data:image/") {
		return adminAvatarDataURLPattern.MatchString(value)
	}
	parsed, parseErr := url.ParseRequestURI(value)
	return parseErr == nil && parsed.Scheme != "" && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (s *Server) handleAdminAccountPreferences(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminPreferencesRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	if req.MFARequirement == "always" || req.MFARequirement == "new_device" {
		security.MFARequirement = req.MFARequirement
	}
	if req.PreferredLocale == "system" || req.PreferredLocale == "en" || req.PreferredLocale == "zh" {
		security.PreferredLocale = req.PreferredLocale
	}
	if req.PreferredTheme == "system" || req.PreferredTheme == "light" || req.PreferredTheme == "dark" {
		security.PreferredTheme = req.PreferredTheme
	}
	updated, storeErr := s.store.UpsertAdminSecurity(r.Context(), security)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save account settings"))
		return
	}
	api.WriteJSON(w, http.StatusOK, adminSecurityData(updated, s.store.ListAdminPasskeys(r.Context(), session.SubjectID)), nil)
}

func (s *Server) handleAdminTOTPStart(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	secret, secretErr := auth.NewTOTPSecret()
	if secretErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create TOTP secret"))
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	security.TOTPSecret = secret
	security.TOTPEnabled = false
	updated, storeErr := s.store.UpsertAdminSecurity(r.Context(), security)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to save TOTP secret"))
		return
	}
	user, _ := s.adminDataForSession(r.Context(), session.SubjectID)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"secret":      secret,
		"otpauth_url": auth.TOTPAuthURL("Authman", user["username"].(string), updated.TOTPSecret),
	}, nil)
}

func (s *Server) handleAdminTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminTOTPVerifyRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	if security.TOTPSecret == "" || !auth.VerifyTOTP(security.TOTPSecret, req.Code, time.Now()) {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.mfa_invalid", "invalid verification code"))
		return
	}
	security.TOTPEnabled = true
	updated, storeErr := s.store.UpsertAdminSecurity(r.Context(), security)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to enable TOTP"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, session.SubjectID, "admin.totp.enable", nil)
	api.WriteJSON(w, http.StatusOK, adminSecurityData(updated, s.store.ListAdminPasskeys(r.Context(), session.SubjectID)), nil)
}

func (s *Server) handleAdminTOTPDisable(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	security.TOTPEnabled = false
	security.TOTPSecret = ""
	updated, storeErr := s.store.UpsertAdminSecurity(r.Context(), security)
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.account_save_failed", "failed to disable TOTP"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, session.SubjectID, "admin.totp.disable", nil)
	api.WriteJSON(w, http.StatusOK, adminSecurityData(updated, s.store.ListAdminPasskeys(r.Context(), session.SubjectID)), nil)
}

func (s *Server) handleAdminPasskeyRegisterOptions(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	if s.webAuthn == nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_unavailable", "passkey is unavailable"))
		return
	}
	var req adminPasskeyRegisterOptionsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	user := s.webAuthnUser(r.Context(), session.SubjectID)
	creation, webSession, webErr := s.webAuthn.BeginRegistration(user, webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
		UserVerification: protocol.VerificationRequired,
	}))
	if webErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "auth.passkey_start_failed", "failed to start passkey registration"))
		return
	}
	sessionJSON, _ := json.Marshal(webSession)
	pending, storeErr := s.store.SavePendingAdminMFA(r.Context(), store.PendingAdminMFA{
		AdminID:             session.SubjectID,
		WebAuthnSessionJSON: sessionJSON,
		ExpiresAt:           time.Now().UTC().Add(passkeyRegisterTTL),
	})
	if storeErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to save passkey challenge"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"challenge_id": pending.ID, "name": name, "options": creation}, nil)
}

func (s *Server) handleAdminPasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminPasskeyRegisterFinishRequest
	if err := decodeJSONLoose(r, &req); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "system.invalid_json", "invalid JSON request body"))
		return
	}
	pending, storeErr := s.store.GetPendingAdminMFA(r.Context(), req.ChallengeID)
	if storeErr != nil || pending.AdminID != session.SubjectID || time.Now().UTC().After(pending.ExpiresAt) {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_challenge_missing", "passkey challenge is missing"))
		return
	}
	var webSession webauthn.SessionData
	if err := json.Unmarshal(pending.WebAuthnSessionJSON, &webSession); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_challenge_missing", "passkey challenge is missing"))
		return
	}
	credential, webErr := s.webAuthn.FinishRegistration(s.webAuthnUser(r.Context(), session.SubjectID), webSession, requestWithJSON(r, req.Credential))
	if webErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_invalid", "passkey registration failed"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	passkey, createErr := s.store.CreateAdminPasskey(r.Context(), store.AdminPasskey{
		AdminID:    session.SubjectID,
		Name:       name,
		Credential: *credential,
	})
	if createErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.passkey_save_failed", "failed to save passkey"))
		return
	}
	_ = s.store.DeletePendingAdminMFA(r.Context(), pending.ID)
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, passkey.ID, "admin.passkey.create", map[string]any{"name": passkey.Name})
	security, _ := s.store.GetAdminSecurity(r.Context(), session.SubjectID)
	api.WriteJSON(w, http.StatusCreated, adminSecurityData(security, s.store.ListAdminPasskeys(r.Context(), session.SubjectID)), nil)
}

func (s *Server) handleAdminPasskeyDelete(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := s.store.DeleteAdminPasskey(r.Context(), session.SubjectID, id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "auth.passkey_not_found", "passkey not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, id, "admin.passkey.delete", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) webAuthnUser(ctx context.Context, adminID string) adminWebAuthnUser {
	user, err := s.adminDataForSession(ctx, adminID)
	username := adminID
	if err == nil {
		if value, ok := user["username"].(string); ok && value != "" {
			username = value
		}
	}
	passkeys := s.store.ListAdminPasskeys(ctx, adminID)
	credentials := make([]webauthn.Credential, 0, len(passkeys))
	for _, passkey := range passkeys {
		credentials = append(credentials, passkey.Credential)
	}
	return adminWebAuthnUser{id: adminID, username: username, credentials: credentials}
}

func (s *Server) updateUsedPasskey(ctx context.Context, adminID string, credential webauthn.Credential) {
	for _, passkey := range s.store.ListAdminPasskeys(ctx, adminID) {
		if bytes.Equal(passkey.Credential.ID, credential.ID) {
			_ = s.store.UpdateAdminPasskeyCredential(ctx, passkey.ID, credential, time.Now())
			return
		}
	}
}

type adminWebAuthnUser struct {
	id          string
	username    string
	credentials []webauthn.Credential
}

func (u adminWebAuthnUser) WebAuthnID() []byte {
	sum := sha256.Sum256([]byte("authman-admin:" + u.id))
	return sum[:]
}

func (u adminWebAuthnUser) WebAuthnName() string {
	return u.username
}

func (u adminWebAuthnUser) WebAuthnDisplayName() string {
	return u.username
}

func (u adminWebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func adminSecurityData(security store.AdminSecurity, passkeys []store.AdminPasskey) map[string]any {
	data := make([]map[string]any, 0, len(passkeys))
	for _, passkey := range passkeys {
		lastUsed := any(nil)
		if passkey.LastUsedAt != nil {
			lastUsed = passkey.LastUsedAt
		}
		data = append(data, map[string]any{
			"id":           passkey.ID,
			"name":         passkey.Name,
			"created_at":   passkey.CreatedAt,
			"last_used_at": lastUsed,
		})
	}
	return map[string]any{
		"totp_enabled":     security.TOTPEnabled,
		"passkeys":         data,
		"mfa_requirement":  security.MFARequirement,
		"preferred_locale": security.PreferredLocale,
		"preferred_theme":  security.PreferredTheme,
	}
}

func requestWithJSON(r *http.Request, raw []byte) *http.Request {
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	clone.ContentLength = int64(len(raw))
	clone.Header = r.Header.Clone()
	clone.Header.Set("Content-Type", "application/json")
	return clone
}

func decodeJSONLoose(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	return decoder.Decode(dst)
}
