package auth

import "time"

type PortalLinkKind string

const (
	PortalLinkPremium PortalLinkKind = "premium"
	PortalLinkOffline PortalLinkKind = "offline"
)

type PortalLinkStatus string

const (
	PortalLinkActive  PortalLinkStatus = "active"
	PortalLinkUsed    PortalLinkStatus = "used"
	PortalLinkRevoked PortalLinkStatus = "revoked"
)

type PortalLink struct {
	ID        string
	Kind      PortalLinkKind
	PlayerID  string
	ServerID  string
	TokenHash string
	Status    PortalLinkStatus
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

func NewPortalLink(kind PortalLinkKind, playerID string, serverID string, ttl time.Duration, now time.Time) (PortalLink, string, error) {
	token, err := NewOpaqueToken(32)
	if err != nil {
		return PortalLink{}, "", err
	}
	link := PortalLink{
		ID:        HashToken("portal-link-id", token),
		Kind:      kind,
		PlayerID:  playerID,
		ServerID:  serverID,
		TokenHash: HashToken("portal-link", token),
		Status:    PortalLinkActive,
		CreatedAt: now.UTC(),
		ExpiresAt: now.UTC().Add(ttl),
	}
	return link, token, nil
}

func (l PortalLink) Verify(token string, now time.Time) PortalLinkVerifyResult {
	if l.Status == PortalLinkUsed {
		return PortalLinkVerifyUsed
	}
	if l.Status == PortalLinkRevoked {
		return PortalLinkVerifyRevoked
	}
	if !now.UTC().Before(l.ExpiresAt) {
		return PortalLinkVerifyExpired
	}
	if !ConstantTimeTokenEqual("portal-link", token, l.TokenHash) {
		return PortalLinkVerifyNotFound
	}
	return PortalLinkVerifyOK
}

type PortalLinkVerifyResult string

const (
	PortalLinkVerifyOK       PortalLinkVerifyResult = "ok"
	PortalLinkVerifyExpired  PortalLinkVerifyResult = "expired"
	PortalLinkVerifyUsed     PortalLinkVerifyResult = "used"
	PortalLinkVerifyRevoked  PortalLinkVerifyResult = "revoked"
	PortalLinkVerifyNotFound PortalLinkVerifyResult = "not_found"
)
