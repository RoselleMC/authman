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
	"github.com/RoselleMC/authman/internal/rbac"
	"github.com/go-webauthn/webauthn/webauthn"
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

type AdminUser struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AdminProfile struct {
	AdminID   string
	Username  string
	Email     string
	AvatarURL string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AdminSecurity struct {
	AdminID         string
	TOTPEnabled     bool
	TOTPSecret      string
	MFARequirement  string
	PreferredLocale string
	PreferredTheme  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AdminPasskey struct {
	ID         string
	AdminID    string
	Name       string
	Credential webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type PendingAdminMFA struct {
	ID                  string
	AdminID             string
	WebAuthnSessionJSON []byte
	ExpiresAt           time.Time
}

type AdminTrustedDevice struct {
	ID        string
	AdminID   string
	TokenHash string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type PlayerStore interface {
	CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error)
	UpsertPremiumPlayer(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.Player, error)
	GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error)
	GetPlayerByProtocolName(ctx context.Context, protocolName string) (identity.Player, error)
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
	SaveTransferGrant(ctx context.Context, grant auth.TransferGrant) error
	ConsumeTransferGrant(ctx context.Context, tokenHash string, serverID string, uuid string, protocolName string, gateNodeID string, allowedPortalSources []string, now time.Time) (auth.TransferGrant, error)
	ListExtensionPlayerData(ctx context.Context, playerID string, serverSlug string, includePrivate bool) []ExtensionPlayerData
	UpsertExtensionPlayerData(ctx context.Context, data ExtensionPlayerData) (ExtensionPlayerData, error)
	ListAdminUsers(ctx context.Context) []AdminUser
	GetAdminUser(ctx context.Context, id string) (AdminUser, error)
	FindAdminUserByIdentifier(ctx context.Context, identifier string) (AdminUser, error)
	CreateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error)
	UpdateAdminUserProfile(ctx context.Context, id string, username string, email string) (AdminUser, error)
	UpdateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error)
	GetAdminProfile(ctx context.Context, adminID string) (AdminProfile, error)
	UpsertAdminProfile(ctx context.Context, profile AdminProfile) (AdminProfile, error)
	ListAdminRoles(ctx context.Context) []rbac.Role
	UpsertAdminRole(ctx context.Context, role rbac.Role) (rbac.Role, error)
	DeleteAdminRole(ctx context.Context, id string) error
	GetAdminSecurity(ctx context.Context, adminID string) (AdminSecurity, error)
	UpsertAdminSecurity(ctx context.Context, security AdminSecurity) (AdminSecurity, error)
	ListAdminPasskeys(ctx context.Context, adminID string) []AdminPasskey
	CreateAdminPasskey(ctx context.Context, passkey AdminPasskey) (AdminPasskey, error)
	UpdateAdminPasskeyCredential(ctx context.Context, id string, credential webauthn.Credential, lastUsedAt time.Time) error
	DeleteAdminPasskey(ctx context.Context, adminID string, id string) error
	SavePendingAdminMFA(ctx context.Context, pending PendingAdminMFA) (PendingAdminMFA, error)
	GetPendingAdminMFA(ctx context.Context, id string) (PendingAdminMFA, error)
	DeletePendingAdminMFA(ctx context.Context, id string) error
	CreateAdminTrustedDevice(ctx context.Context, device AdminTrustedDevice) (AdminTrustedDevice, error)
	GetAdminTrustedDevice(ctx context.Context, tokenHash string, now time.Time) (AdminTrustedDevice, error)
}
