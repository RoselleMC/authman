package store

import (
	"context"
	"errors"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/identity"
)

var ErrNotFound = errors.New("not found")

type PlayerStore interface {
	CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error)
	GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error)
	GetPlayerByID(ctx context.Context, id string) (identity.Player, error)
	GetOfflineCredential(ctx context.Context, rawName string) (identity.Player, OfflineCredential, error)
	ListPlayers(ctx context.Context) []identity.Player
	SetPlayerLocked(ctx context.Context, id string, locked bool) (identity.Player, error)
	UpdateOfflinePassword(ctx context.Context, id string, passwordHash string) error
	AppendAuditEvent(ctx context.Context, event audit.Event) (audit.Event, error)
	ListAuditEvents(ctx context.Context, limit int) []audit.Event
}
