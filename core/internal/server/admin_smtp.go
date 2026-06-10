package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/store"
)

const (
	smtpSettingsKey         = "smtp"
	smtpLastMessageKey      = "smtp_last_message"
	bootstrapAdminAuthKey   = "bootstrap_admin_auth"
	passwordResetTokenScope = "admin-password-reset"
)

type smtpSettingsRequest struct {
	Enabled              bool   `json:"enabled"`
	DeliveryMode         string `json:"delivery_mode"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Security             string `json:"security"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	ClearPassword        bool   `json:"clear_password"`
	FromName             string `json:"from_name"`
	FromEmail            string `json:"from_email"`
	ReplyTo              string `json:"reply_to"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
	ResetTokenTTLMinutes int    `json:"reset_token_ttl_minutes"`
}

type smtpTestRequest struct {
	To string `json:"to"`
}

type adminPasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type adminPasswordResetRequest struct {
	Identifier string `json:"identifier"`
}

type adminPasswordResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type smtpSettings struct {
	Enabled              bool
	DeliveryMode         string
	Host                 string
	Port                 int
	Security             string
	Username             string
	Password             string
	FromName             string
	FromEmail            string
	ReplyTo              string
	TimeoutSeconds       int
	ResetTokenTTLMinutes int
}

func (s *Server) handleAdminSMTPSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := s.smtpSettings(r.Context())
	last, _ := s.store.GetSystemSetting(r.Context(), smtpLastMessageKey)
	api.WriteJSON(w, http.StatusOK, smtpSettingsData(settings, last), nil)
}

func (s *Server) handleAdminUpdateSMTPSettings(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req smtpSettingsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	existing := s.smtpSettings(r.Context())
	settings, apiErr := normalizeSMTPSettings(req, existing.Password)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	if req.ClearPassword {
		settings.Password = ""
	}
	if strings.TrimSpace(req.Password) != "" {
		settings.Password = req.Password
	}
	if err := s.store.SetSystemSetting(r.Context(), smtpSettingsKey, smtpSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "admin.smtp_save_failed", "failed to save SMTP settings"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, smtpSettingsKey, "admin.smtp.update", map[string]any{
		"enabled":       settings.Enabled,
		"delivery_mode": settings.DeliveryMode,
		"host":          settings.Host,
		"from_email":    settings.FromEmail,
	})
	api.WriteJSON(w, http.StatusOK, smtpSettingsData(settings, nil), nil)
}

func (s *Server) handleAdminSMTPTest(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req smtpTestRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	to := strings.TrimSpace(req.To)
	if to == "" {
		user, userErr := s.adminDataForSession(r.Context(), session.SubjectID)
		if userErr == nil {
			to, _ = user["email"].(string)
		}
	}
	if to == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.smtp_to_required", "test recipient is required"))
		return
	}
	settings := s.smtpSettings(r.Context())
	result, sendErr := s.sendAdminMail(r.Context(), settings, to, "Authman SMTP test", "This is a test message from Authman Core.")
	if sendErr != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "admin.smtp_send_failed", sendErr.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, smtpSettingsKey, "admin.smtp.test", map[string]any{
		"to":            to,
		"delivery_mode": result,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "delivery": result}, nil)
}

func (s *Server) handleAdminAccountPassword(w http.ResponseWriter, r *http.Request) {
	session, err := s.requireAdmin(r, true)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var req adminPasswordChangeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if err := auth.ValidatePassword(req.NewPassword, auth.DefaultPasswordPolicy); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_invalid", err.Error()))
		return
	}
	if !s.verifyAdminCurrentPassword(r.Context(), session.SubjectID, req.CurrentPassword) {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "current password is invalid"))
		return
	}
	if err := s.updateAdminPassword(r.Context(), session.SubjectID, req.NewPassword); err != nil {
		api.WriteError(w, err)
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, session.SubjectID, "admin.account.password_update", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var req adminPasswordResetRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	settings := s.smtpSettings(r.Context())
	adminID, email := s.findPasswordResetTarget(r.Context(), identifier)
	if adminID != "" && email != "" && settings.Enabled {
		if rawToken, tokenErr := auth.NewOpaqueToken(32); tokenErr == nil {
			expiresAt := time.Now().UTC().Add(time.Duration(settings.ResetTokenTTLMinutes) * time.Minute)
			reset, saveErr := s.store.SaveAdminPasswordReset(r.Context(), store.AdminPasswordReset{
				AdminID:   adminID,
				TokenHash: auth.HashToken(passwordResetTokenScope, rawToken),
				ExpiresAt: expiresAt,
				CreatedAt: time.Now().UTC(),
			})
			if saveErr == nil {
				link := s.adminPasswordResetURL(r, rawToken)
				body := fmt.Sprintf("A password reset was requested for your Authman Core administrator account.\n\nOpen this link to set a new password:\n%s\n\nThis link expires at %s.\n\nIf you did not request this, ignore this message.", link, expiresAt.Format(time.RFC3339))
				if _, sendErr := s.sendAdminMail(r.Context(), settings, email, "Reset your Authman Core password", body); sendErr != nil {
					s.logger.Warn("password reset mail failed", "error", sendErr)
				} else {
					s.audit(r, audit.ActorAdmin, adminID, audit.TargetSystem, reset.ID, "admin.password_reset.request", map[string]any{"email": email})
				}
			}
		}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleAdminPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var req adminPasswordResetConfirmRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.reset_token_required", "reset token is required"))
		return
	}
	reset, err := s.store.GetAdminPasswordReset(r.Context(), auth.HashToken(passwordResetTokenScope, strings.TrimSpace(req.Token)), time.Now().UTC())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.reset_token_invalid", "reset token is invalid or expired"))
		return
	}
	if err := auth.ValidatePassword(req.NewPassword, auth.DefaultPasswordPolicy); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_invalid", err.Error()))
		return
	}
	if err := s.updateAdminPassword(r.Context(), reset.AdminID, req.NewPassword); err != nil {
		api.WriteError(w, err)
		return
	}
	_ = s.store.MarkAdminPasswordResetUsed(r.Context(), reset.ID, time.Now().UTC())
	s.audit(r, audit.ActorAdmin, reset.AdminID, audit.TargetSystem, reset.ID, "admin.password_reset.confirm", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) smtpSettings(ctx context.Context) smtpSettings {
	defaults, _ := normalizeSMTPSettings(smtpSettingsRequest{
		DeliveryMode:         "log",
		Security:             "starttls",
		Port:                 587,
		FromName:             "Authman Core",
		FromEmail:            "authman@localhost",
		TimeoutSeconds:       10,
		ResetTokenTTLMinutes: 30,
	}, "")
	raw, err := s.store.GetSystemSetting(ctx, smtpSettingsKey)
	if err != nil {
		return defaults
	}
	settings, apiErr := normalizeSMTPSettings(smtpSettingsRequest{
		Enabled:              boolValue(raw["enabled"], defaults.Enabled),
		DeliveryMode:         stringValue(raw["delivery_mode"], defaults.DeliveryMode),
		Host:                 stringValue(raw["host"], defaults.Host),
		Port:                 intValue(raw["port"], defaults.Port),
		Security:             stringValue(raw["security"], defaults.Security),
		Username:             stringValue(raw["username"], defaults.Username),
		Password:             stringValue(raw["password"], defaults.Password),
		FromName:             stringValue(raw["from_name"], defaults.FromName),
		FromEmail:            stringValue(raw["from_email"], defaults.FromEmail),
		ReplyTo:              stringValue(raw["reply_to"], defaults.ReplyTo),
		TimeoutSeconds:       intValue(raw["timeout_seconds"], defaults.TimeoutSeconds),
		ResetTokenTTLMinutes: intValue(raw["reset_token_ttl_minutes"], defaults.ResetTokenTTLMinutes),
	}, stringValue(raw["password"], defaults.Password))
	if apiErr != nil {
		return defaults
	}
	return settings
}

func normalizeSMTPSettings(req smtpSettingsRequest, existingPassword string) (smtpSettings, *api.Error) {
	settings := smtpSettings{
		Enabled:              req.Enabled,
		DeliveryMode:         strings.ToLower(strings.TrimSpace(req.DeliveryMode)),
		Host:                 strings.TrimSpace(req.Host),
		Port:                 req.Port,
		Security:             strings.ToLower(strings.TrimSpace(req.Security)),
		Username:             strings.TrimSpace(req.Username),
		Password:             existingPassword,
		FromName:             strings.TrimSpace(req.FromName),
		FromEmail:            strings.TrimSpace(req.FromEmail),
		ReplyTo:              strings.TrimSpace(req.ReplyTo),
		TimeoutSeconds:       req.TimeoutSeconds,
		ResetTokenTTLMinutes: req.ResetTokenTTLMinutes,
	}
	if settings.DeliveryMode == "" {
		settings.DeliveryMode = "smtp"
	}
	if settings.DeliveryMode != "smtp" && settings.DeliveryMode != "log" {
		return smtpSettings{}, api.NewError(http.StatusBadRequest, "admin.smtp_delivery_invalid", "delivery mode must be smtp or log")
	}
	if settings.Security == "" {
		settings.Security = "starttls"
	}
	if settings.Security != "none" && settings.Security != "starttls" && settings.Security != "tls" {
		return smtpSettings{}, api.NewError(http.StatusBadRequest, "admin.smtp_security_invalid", "SMTP security must be none, starttls, or tls")
	}
	if settings.Port <= 0 {
		switch settings.Security {
		case "tls":
			settings.Port = 465
		default:
			settings.Port = 587
		}
	}
	settings.Port = clampInt(settings.Port, 1, 65535)
	if settings.FromName == "" {
		settings.FromName = "Authman Core"
	}
	if settings.FromEmail == "" {
		settings.FromEmail = "authman@localhost"
	}
	settings.TimeoutSeconds = clampInt(settings.TimeoutSeconds, 1, 60)
	settings.ResetTokenTTLMinutes = clampInt(settings.ResetTokenTTLMinutes, 5, 1440)
	return settings, nil
}

func smtpSettingsData(settings smtpSettings, last map[string]any) map[string]any {
	data := smtpSettingsMap(settings)
	delete(data, "password")
	data["password_set"] = settings.Password != ""
	if last != nil {
		data["last_message"] = last
	}
	return data
}

func smtpSettingsMap(settings smtpSettings) map[string]any {
	return map[string]any{
		"enabled":                 settings.Enabled,
		"delivery_mode":           settings.DeliveryMode,
		"host":                    settings.Host,
		"port":                    settings.Port,
		"security":                settings.Security,
		"username":                settings.Username,
		"password":                settings.Password,
		"from_name":               settings.FromName,
		"from_email":              settings.FromEmail,
		"reply_to":                settings.ReplyTo,
		"timeout_seconds":         settings.TimeoutSeconds,
		"reset_token_ttl_minutes": settings.ResetTokenTTLMinutes,
	}
}

func (s *Server) sendAdminMail(ctx context.Context, settings smtpSettings, to string, subject string, body string) (string, error) {
	if !settings.Enabled {
		return "", fmt.Errorf("SMTP is disabled")
	}
	if strings.TrimSpace(to) == "" {
		return "", fmt.Errorf("recipient is required")
	}
	if settings.DeliveryMode == "log" {
		_ = s.store.SetSystemSetting(ctx, smtpLastMessageKey, map[string]any{
			"to":         to,
			"subject":    subject,
			"body":       body,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
		s.logger.Info("SMTP log delivery", "to", to, "subject", subject)
		return "log", nil
	}
	if settings.Host == "" {
		return "", fmt.Errorf("SMTP host is required")
	}
	from := settings.FromEmail
	message := buildSMTPMessage(settings, to, subject, body)
	addr := net.JoinHostPort(settings.Host, fmt.Sprint(settings.Port))
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	dialer := &net.Dialer{Timeout: timeout}
	var client *smtp.Client
	var err error
	if settings.Security == "tls" {
		conn, tlsErr := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12})
		if tlsErr != nil {
			return "", tlsErr
		}
		client, err = smtp.NewClient(conn, settings.Host)
	} else {
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return "", dialErr
		}
		client, err = smtp.NewClient(conn, settings.Host)
	}
	if err != nil {
		return "", err
	}
	defer client.Close()
	if settings.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("SMTP server does not support STARTTLS")
		}
	}
	if settings.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)); err != nil {
			return "", err
		}
	}
	if err := client.Mail(from); err != nil {
		return "", err
	}
	if err := client.Rcpt(to); err != nil {
		return "", err
	}
	writer, err := client.Data()
	if err != nil {
		return "", err
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	if err := client.Quit(); err != nil {
		return "", err
	}
	return "smtp", nil
}

func buildSMTPMessage(settings smtpSettings, to string, subject string, body string) string {
	from := settings.FromEmail
	if settings.FromName != "" {
		from = fmt.Sprintf("%s <%s>", quoteSMTPHeader(settings.FromName), settings.FromEmail)
	}
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + quoteSMTPHeader(subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	if settings.ReplyTo != "" {
		headers = append(headers, "Reply-To: "+settings.ReplyTo)
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"
}

func quoteSMTPHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func (s *Server) findPasswordResetTarget(ctx context.Context, identifier string) (string, string) {
	if identifier == "" {
		return "", ""
	}
	if s.adminIdentifierMatches(ctx, identifier) {
		data := s.adminData(ctx)
		email, _ := data["email"].(string)
		return "bootstrap-admin", strings.TrimSpace(email)
	}
	user, err := s.store.FindAdminUserByIdentifier(ctx, identifier)
	if err != nil || user.Status != "active" {
		return "", ""
	}
	return user.ID, strings.TrimSpace(user.Email)
}

func (s *Server) verifyAdminCurrentPassword(ctx context.Context, adminID string, password string) bool {
	if adminID == "bootstrap-admin" {
		return s.verifyAdminPassword(ctx, s.cfg.AdminUsername, password)
	}
	user, err := s.store.GetAdminUser(ctx, adminID)
	if err != nil {
		return false
	}
	ok, verifyErr := auth.VerifyPassword(password, user.PasswordHash)
	return verifyErr == nil && ok
}

func (s *Server) updateAdminPassword(ctx context.Context, adminID string, password string) *api.Error {
	hash, err := auth.HashPassword(password, s.passwordParams)
	if err != nil {
		return api.NewError(http.StatusBadRequest, "auth.password_invalid", err.Error())
	}
	if adminID == "bootstrap-admin" {
		if err := s.store.SetSystemSetting(ctx, bootstrapAdminAuthKey, map[string]any{"password_hash": hash, "updated_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return api.NewError(http.StatusInternalServerError, "admin.password_save_failed", "failed to save password")
		}
		return nil
	}
	if err := s.store.UpdateAdminUserPassword(ctx, adminID, hash); err != nil {
		return api.NewError(http.StatusInternalServerError, "admin.password_save_failed", "failed to save password")
	}
	return nil
}

func (s *Server) bootstrapAdminPasswordHash(ctx context.Context) string {
	raw, err := s.store.GetSystemSetting(ctx, bootstrapAdminAuthKey)
	if err != nil {
		return ""
	}
	return stringValue(raw["password_hash"], "")
}

func (s *Server) adminPasswordResetURL(r *http.Request, token string) string {
	base := strings.TrimRight(s.cfg.PublicBaseURL, "/")
	if base == "" {
		scheme := "http"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
		} else if r.TLS != nil {
			scheme = "https"
		}
		host := r.Host
		if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
			host = strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
		}
		base = scheme + "://" + host + requestBasePath(r, s.cfg.HTTPBasePath)
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + "/login" + separator + "reset_token=" + url.QueryEscape(token)
}
