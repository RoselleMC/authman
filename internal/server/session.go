package server

import (
	"context"
	"net/http"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/auth"
)

const (
	adminSessionCookie  = "authman_admin_session"
	playerSessionCookie = "authman_player_session"
	csrfHeader          = "X-CSRF-Token"
)

func (s *Server) saveSession(ctx context.Context, session auth.Session) error {
	return s.store.SaveSession(ctx, session)
}

func (s *Server) deleteSession(ctx context.Context, token string) {
	_ = s.store.DeleteSession(ctx, auth.HashToken("session", token))
}

func (s *Server) rotateCSRF(ctx context.Context, session auth.Session) (string, error) {
	csrf, err := auth.NewOpaqueToken(32)
	if err != nil {
		return "", err
	}
	session.CSRFToken = auth.HashToken("csrf", csrf)
	return csrf, s.store.UpdateSession(ctx, session)
}

func (s *Server) requireSession(r *http.Request, cookieName string, kind auth.SessionKind, csrf bool) (auth.Session, *api.Error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return auth.Session{}, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "missing session")
	}
	sessionID := auth.HashToken("session", cookie.Value)
	session, storeErr := s.store.GetSession(r.Context(), sessionID)
	if storeErr != nil || session.Kind != kind || session.Expired(time.Now()) {
		return auth.Session{}, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session is invalid or expired")
	}
	if csrf && !session.VerifyCSRF(r.Header.Get(csrfHeader)) {
		return auth.Session{}, api.NewError(http.StatusForbidden, "auth.csrf_failed", "invalid CSRF token")
	}
	return session, nil
}

func (s *Server) requireAdmin(r *http.Request, csrf bool) (auth.Session, *api.Error) {
	return s.requireSession(r, adminSessionCookie, auth.SessionAdmin, csrf)
}

func (s *Server) requirePlayer(r *http.Request, csrf bool) (auth.Session, *api.Error) {
	return s.requireSession(r, playerSessionCookie, auth.SessionPlayer, csrf)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, name string, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
