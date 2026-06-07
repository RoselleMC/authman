package store

import (
	"context"
	"errors"
	"time"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/extensions"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/mojang"
)

var ErrNotFound = errors.New("not found")

type DownstreamServer struct {
	ID                 string
	Slug               string
	DisplayName        string
	Status             string
	RegistrationOpen   bool
	PortalTheme        map[string]any
	PortalConfig       map[string]any
	ExtensionProviders []string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ExtensionPlayerData struct {
	ID         string
	ServerID   string
	PlayerID   string
	Provider   string
	Schema     extensions.Schema
	Values     map[string]any
	Visibility extensions.Visibility
	Source     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type OfflineCredential struct {
	PlayerID          string
	PasswordHash      string
	PasswordUpdatedAt *time.Time
	FailedAttempts    int
	LockedUntil       *time.Time
}

type PlayerStore interface {
	CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error)
	GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error)
	PremiumNameExists(ctx context.Context, rawName string) bool
	GetPlayerByID(ctx context.Context, id string) (identity.Player, error)
	GetOfflineCredential(ctx context.Context, rawName string) (identity.Player, OfflineCredential, error)
	RecordOfflineLoginFailure(ctx context.Context, playerID string, now time.Time) (OfflineCredential, error)
	RecordOfflineLoginSuccess(ctx context.Context, playerID string) error
	ListPlayers(ctx context.Context) []identity.Player
	SetPlayerLocked(ctx context.Context, id string, locked bool) (identity.Player, error)
	UpdateOfflinePassword(ctx context.Context, id string, passwordHash string) error
	SaveSession(ctx context.Context, session auth.Session) error
	GetSession(ctx context.Context, id string) (auth.Session, error)
	UpdateSession(ctx context.Context, session auth.Session) error
	DeleteSession(ctx context.Context, id string) error
	SavePortalLink(ctx context.Context, link auth.PortalLink) error
	GetPortalLink(ctx context.Context, tokenHash string) (auth.PortalLink, error)
	MarkPortalLinkUsed(ctx context.Context, tokenHash string, now time.Time) (auth.PortalLink, error)
	AppendAuditEvent(ctx context.Context, event audit.Event) (audit.Event, error)
	ListAuditEvents(ctx context.Context, limit int) []audit.Event
	ListMojangRoutes(ctx context.Context) []mojang.Route
	UpsertMojangRoute(ctx context.Context, route mojang.Route) (mojang.Route, error)
	DeleteMojangRoute(ctx context.Context, id string) error
	ListDownstreamServers(ctx context.Context) []DownstreamServer
	GetDownstreamServer(ctx context.Context, idOrSlug string) (DownstreamServer, error)
	UpsertDownstreamServer(ctx context.Context, server DownstreamServer) (DownstreamServer, error)
	DeleteDownstreamServer(ctx context.Context, id string) error
	ListExtensionPlayerData(ctx context.Context, playerID string, serverSlug string, includePrivate bool) []ExtensionPlayerData
	UpsertExtensionPlayerData(ctx context.Context, data ExtensionPlayerData) (ExtensionPlayerData, error)
}
