package auth

import "time"

type TransferGrant struct {
	ID           string
	PlayerID     string
	ServerID     string
	PortalNodeID string
	PortalSource string
	GateNodeID   string
	TokenHash    string
	UUID         string
	ProtocolName string
	TargetHost   string
	TargetPort   int
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
}

func NewTransferGrant(playerID string, serverID string, portalNodeID string, portalSource string, uuid string, protocolName string, targetHost string, targetPort int, ttl time.Duration, now time.Time) (TransferGrant, string, error) {
	token, err := NewOpaqueToken(32)
	if err != nil {
		return TransferGrant{}, "", err
	}
	grant := TransferGrant{
		ID:           HashToken("transfer-grant-id", token),
		PlayerID:     playerID,
		ServerID:     serverID,
		PortalNodeID: portalNodeID,
		PortalSource: portalSource,
		TokenHash:    HashToken("transfer-grant", token),
		UUID:         uuid,
		ProtocolName: protocolName,
		TargetHost:   targetHost,
		TargetPort:   targetPort,
		CreatedAt:    now.UTC(),
		ExpiresAt:    now.UTC().Add(ttl),
	}
	return grant, token, nil
}

func TransferGrantHash(token string) string {
	return HashToken("transfer-grant", token)
}
