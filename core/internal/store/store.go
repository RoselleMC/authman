package store

import (
	"context"
	"errors"
	"time"

	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/extensions"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"github.com/RoselleMC/authman/core/internal/rbac"
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

type DownstreamServerPrivilegedPassport struct {
	ServerID   string
	PassportID string
	Privileges []string
	CreatedBy  string
	CreatedAt  time.Time
	Passport   identity.Passport
}

type LimboBlueprint struct {
	ID          string
	Name        string
	Description string
	Filename    string
	ContentType string
	SizeBytes   int64
	SHA256      string
	Schematic   []byte
	Preview     map[string]any
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProfileSkin struct {
	ProfileID         string
	Model             string
	SkinPNG           []byte
	SkinContentType   string
	SkinSHA256        string
	CapePNG           []byte
	CapeContentType   string
	CapeSHA256        string
	ElytraPNG         []byte
	ElytraContentType string
	ElytraSHA256      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PassportSkin struct {
	PassportID        string
	Model             string
	SkinPNG           []byte
	SkinContentType   string
	SkinSHA256        string
	CapePNG           []byte
	CapeContentType   string
	CapeSHA256        string
	ElytraPNG         []byte
	ElytraContentType string
	ElytraSHA256      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
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

type AuditEventQuery struct {
	ActorType  string
	TargetType string
	EventType  string
	RelatedIDs []string
	Since      *time.Time
	Until      *time.Time
	Page       int
	PageSize   int
}

type IdentityListQuery struct {
	Search   string
	Kind     string
	Status   string
	Binding  string
	Sort     string
	Dir      string
	Page     int
	PageSize int
}

type PlayerPresence struct {
	ID           string
	PassportID   string
	ProfileID    string
	ServerID     string
	NodeID       string
	ProtocolName string
	UUID         string
	RemoteAddr   string
	ConnectedAt  time.Time
	LastSeenAt   time.Time
	EndedAt      *time.Time
	EndReason    string
}

type NodeActionType string

const (
	NodeActionDisconnect NodeActionType = "disconnect"
)

type NodeAction struct {
	ID           string
	NodeID       string
	Type         NodeActionType
	PresenceID   string
	PassportID   string
	ProfileID    string
	UUID         string
	ProtocolName string
	Reason       string
	CreatedAt    time.Time
	ExpiresAt    *time.Time
	AckedAt      *time.Time
}

type BanScope string

const (
	BanScopePassport BanScope = "passport"
	BanScopeProfile  BanScope = "profile"
)

type PlayerBan struct {
	ID           string
	Scope        BanScope
	TargetID     string
	Reason       string
	CreatedBy    string
	CreatedAt    time.Time
	ExpiresAt    *time.Time
	RevokedBy    string
	RevokedAt    *time.Time
	RevokeReason string
}

type OfflineCredential struct {
	PlayerID          string
	PassportID        string
	PasswordHash      string
	PasswordUpdatedAt *time.Time
	FailedAttempts    int
	LockedUntil       *time.Time
}

type PassportCredential struct {
	PassportID        string
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

type AdminPasswordReset struct {
	ID        string
	AdminID   string
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

type ExternalAPITokenStatus string

const (
	ExternalAPITokenActive   ExternalAPITokenStatus = "active"
	ExternalAPITokenDisabled ExternalAPITokenStatus = "disabled"
	ExternalAPITokenRevoked  ExternalAPITokenStatus = "revoked"
)

type ExternalAPIToken struct {
	ID               string
	Name             string
	TokenHash        string
	TokenFingerprint string
	Status           ExternalAPITokenStatus
	CreatedBy        string
	CallCount        int64
	LastUsedAt       *time.Time
	LastUsedIP       string
	LastUsedPath     string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PlayerStore interface {
	CreateOfflinePassportProfile(ctx context.Context, rawName string, protocolName string, passwordHash string) (identity.PassportProfile, error)
	UpsertPremiumPassportProfile(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.PassportProfile, error)
	GetPassportByID(ctx context.Context, id string) (identity.Passport, error)
	GetPassportByUsername(ctx context.Context, username string) (identity.Passport, error)
	GetProfileByID(ctx context.Context, id string) (identity.Profile, error)
	GetProfileByProtocolName(ctx context.Context, protocolName string) (identity.Profile, error)
	GetPassportForProfile(ctx context.Context, profileID string) (identity.Passport, error)
	GetPrimaryProfileForPassport(ctx context.Context, passportID string) (identity.Profile, error)
	GetProfileSkin(ctx context.Context, profileID string) (ProfileSkin, error)
	SetProfileSkin(ctx context.Context, profileID string, skin ProfileSkin, properties []identity.ProfileProperty) (identity.Profile, error)
	DeleteProfileSkin(ctx context.Context, profileID string, properties []identity.ProfileProperty, skinSource string) (identity.Profile, error)
	SetProfileSkinSource(ctx context.Context, profileID string, skinSource string, properties []identity.ProfileProperty) (identity.Profile, error)
	GetPassportSkin(ctx context.Context, passportID string) (PassportSkin, error)
	SetPassportSkin(ctx context.Context, passportID string, skin PassportSkin) (identity.Passport, error)
	SetPassportSkinSource(ctx context.Context, passportID string, skinSource string) (identity.Passport, error)
	DeletePassportSkin(ctx context.Context, passportID string) (identity.Passport, error)
	ListProfilesForPassport(ctx context.Context, passportID string) []identity.Profile
	ListPassports(ctx context.Context) []identity.Passport
	ListPassportsPage(ctx context.Context, query IdentityListQuery) ([]identity.Passport, int, error)
	ListProfiles(ctx context.Context) []identity.Profile
	ListProfilesPage(ctx context.Context, query IdentityListQuery) ([]identity.Profile, int, error)
	CreateProfile(ctx context.Context, profile identity.Profile) (identity.Profile, error)
	BindProfileToPassport(ctx context.Context, profileID string, passportID string, primary bool) (identity.PassportProfile, error)
	UnbindProfile(ctx context.Context, profileID string) error
	SetPassportStatus(ctx context.Context, id string, status identity.PassportStatus) (identity.Passport, error)
	SetProfileStatus(ctx context.Context, id string, status identity.ProfileStatus) (identity.Profile, error)
	GetPassportCredential(ctx context.Context, username string) (identity.Passport, PassportCredential, error)
	RecordPassportLoginFailure(ctx context.Context, passportID string, now time.Time) (PassportCredential, error)
	RecordPassportLoginSuccess(ctx context.Context, passportID string) error
	RecordPlayerSeen(ctx context.Context, passportID string, profileID string, serverID string, ip string, geo *identity.IPGeo, now time.Time) error
	UpdatePassportPassword(ctx context.Context, passportID string, passwordHash string) error
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
	GetAuditEvent(ctx context.Context, id string) (audit.Event, error)
	ListAuditEvents(ctx context.Context, limit int) []audit.Event
	ListAuditEventsPage(ctx context.Context, query AuditEventQuery) ([]audit.Event, int, error)
	ListMojangRoutes(ctx context.Context) []mojang.Route
	GetMojangRoute(ctx context.Context, id string) (mojang.Route, error)
	UpsertMojangRoute(ctx context.Context, route mojang.Route) (mojang.Route, error)
	DeleteMojangRoute(ctx context.Context, id string) error
	GetSystemSetting(ctx context.Context, key string) (map[string]any, error)
	SetSystemSetting(ctx context.Context, key string, value map[string]any) error
	ListProfilePresences(ctx context.Context, profileID string) []PlayerPresence
	ListPassportPresences(ctx context.Context, passportID string) []PlayerPresence
	UpsertPresence(ctx context.Context, presence PlayerPresence) (PlayerPresence, error)
	EndPresence(ctx context.Context, id string, reason string, endedAt time.Time) (PlayerPresence, error)
	EndProfilePresences(ctx context.Context, profileID string, reason string, endedAt time.Time) int
	EndPassportPresences(ctx context.Context, passportID string, reason string, endedAt time.Time) int
	EnqueueNodeAction(ctx context.Context, action NodeAction) (NodeAction, error)
	ListPendingNodeActions(ctx context.Context, nodeID string, now time.Time, limit int) []NodeAction
	AckNodeActions(ctx context.Context, nodeID string, ids []string, now time.Time) int
	ListBans(ctx context.Context, scope BanScope, targetID string, includeInactive bool, now time.Time) []PlayerBan
	GetBan(ctx context.Context, id string) (PlayerBan, error)
	CreateBan(ctx context.Context, ban PlayerBan) (PlayerBan, error)
	ExtendBan(ctx context.Context, id string, expiresAt time.Time) (PlayerBan, error)
	RevokeBan(ctx context.Context, id string, revokedBy string, reason string, now time.Time) (PlayerBan, error)
	ActiveBanFor(ctx context.Context, passportID string, profileID string, now time.Time) (PlayerBan, bool)
	ListDownstreamServers(ctx context.Context) []DownstreamServer
	GetDownstreamServer(ctx context.Context, idOrSlug string) (DownstreamServer, error)
	UpsertDownstreamServer(ctx context.Context, server DownstreamServer) (DownstreamServer, error)
	DeleteDownstreamServer(ctx context.Context, id string) error
	ListDownstreamServerPrivilegedPassports(ctx context.Context, serverID string, query IdentityListQuery) ([]DownstreamServerPrivilegedPassport, int, error)
	AddDownstreamServerPrivilegedPassport(ctx context.Context, serverID string, passportID string, createdBy string) (DownstreamServerPrivilegedPassport, error)
	RemoveDownstreamServerPrivilegedPassport(ctx context.Context, serverID string, passportID string) error
	DownstreamServerHasPrivilegedPassport(ctx context.Context, serverID string, passportID string) bool
	ListLimboBlueprints(ctx context.Context) []LimboBlueprint
	GetLimboBlueprint(ctx context.Context, id string) (LimboBlueprint, error)
	UpsertLimboBlueprint(ctx context.Context, blueprint LimboBlueprint) (LimboBlueprint, error)
	DeleteLimboBlueprint(ctx context.Context, id string) error
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
	UpdateAdminUserPassword(ctx context.Context, id string, passwordHash string) error
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
	SaveAdminPasswordReset(ctx context.Context, reset AdminPasswordReset) (AdminPasswordReset, error)
	GetAdminPasswordReset(ctx context.Context, tokenHash string, now time.Time) (AdminPasswordReset, error)
	MarkAdminPasswordResetUsed(ctx context.Context, id string, now time.Time) error
	ListExternalAPITokens(ctx context.Context) []ExternalAPIToken
	GetExternalAPIToken(ctx context.Context, id string) (ExternalAPIToken, error)
	CreateExternalAPIToken(ctx context.Context, token ExternalAPIToken) (ExternalAPIToken, error)
	UpdateExternalAPIToken(ctx context.Context, token ExternalAPIToken) (ExternalAPIToken, error)
	DeleteExternalAPIToken(ctx context.Context, id string) error
	AuthenticateExternalAPIToken(ctx context.Context, rawToken string, now time.Time, clientIP string, path string) (ExternalAPIToken, error)
}
