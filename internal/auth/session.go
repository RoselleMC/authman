package auth

import (
	"time"
)

type SessionKind string

const (
	SessionAdmin  SessionKind = "admin"
	SessionPlayer SessionKind = "player"
)

type Session struct {
	ID        string
	Kind      SessionKind
	SubjectID string
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func NewSession(kind SessionKind, subjectID string, ttl time.Duration, now time.Time) (Session, string, string, error) {
	sessionToken, err := NewOpaqueToken(32)
	if err != nil {
		return Session{}, "", "", err
	}
	csrf, err := NewOpaqueToken(32)
	if err != nil {
		return Session{}, "", "", err
	}
	session := Session{
		ID:        HashToken("session", sessionToken),
		Kind:      kind,
		SubjectID: subjectID,
		CSRFToken: HashToken("csrf", csrf),
		CreatedAt: now.UTC(),
		ExpiresAt: now.UTC().Add(ttl),
	}
	return session, sessionToken, csrf, nil
}

func (s Session) Expired(now time.Time) bool {
	return !now.UTC().Before(s.ExpiresAt)
}

func (s Session) VerifyCSRF(csrf string) bool {
	return ConstantTimeTokenEqual("csrf", csrf, s.CSRFToken)
}
