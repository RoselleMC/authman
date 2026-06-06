package server

import (
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

func (s *Server) saveSession(token string, session auth.Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[session.ID] = session
}

func (s *Server) deleteSession(token string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, auth.HashToken("session", token))
}

func (s *Server) rotateCSRF(session auth.Session) (string, error) {
	csrf, err := auth.NewOpaqueToken(32)
	if err != nil {
		return "", err
	}
	session.CSRFToken = auth.HashToken("csrf", csrf)
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[session.ID] = session
	return csrf, nil
}

func (s *Server) requireSession(r *http.Request, cookieName string, kind auth.SessionKind, csrf bool) (auth.Session, *api.Error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return auth.Session{}, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "missing session")
	}
	sessionID := auth.HashToken("session", cookie.Value)
	s.sessionsMu.RLock()
	session, ok := s.sessions[sessionID]
	s.sessionsMu.RUnlock()
	if !ok || session.Kind != kind || session.Expired(time.Now()) {
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
