package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/config"
	"github.com/RoselleMC/authman/core/internal/extensions"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/RoselleMC/authman/core/internal/yggdrasil"
	"github.com/go-webauthn/webauthn/webauthn"
)

type Options struct {
	Config         config.Config
	Logger         *slog.Logger
	Store          store.PlayerStore
	Nodes          nodeStore
	Extensions     *extensions.Registry
	PasswordParams auth.Argon2idParams
}

type nodeStore interface {
	Create(ctx context.Context, name string, now time.Time) (node.Node, string, error)
	CreateKind(ctx context.Context, name string, kind string, now time.Time) (node.Node, string, error)
	CreateKindForServer(ctx context.Context, name string, kind string, serverID string, now time.Time) (node.Node, string, error)
	Authenticate(ctx context.Context, token string) (node.Node, error)
	Rotate(ctx context.Context, id string, now time.Time) (node.Node, string, error)
	Heartbeat(ctx context.Context, token string, now time.Time) (node.Node, error)
	Register(ctx context.Context, registration node.Registration, now time.Time) (node.Node, error)
	Get(ctx context.Context, id string) (node.Node, error)
	Update(ctx context.Context, id string, name string, runtime map[string]any) (node.Node, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) []node.Node
}

type Server struct {
	cfg            config.Config
	logger         *slog.Logger
	coreMux        *http.ServeMux
	externalMux    *http.ServeMux
	store          store.PlayerStore
	nodes          nodeStore
	extensions     *extensions.Registry
	passwordParams auth.Argon2idParams
	mojangVerifier *mojang.SessionVerifier
	webAuthn       *webauthn.WebAuthn
	ipGeo          *ipGeoResolver
}

func New(options Options) *Server {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		cfg:            options.Config,
		logger:         logger,
		coreMux:        http.NewServeMux(),
		externalMux:    http.NewServeMux(),
		store:          options.Store,
		nodes:          options.Nodes,
		extensions:     options.Extensions,
		passwordParams: options.PasswordParams,
		mojangVerifier: newMojangVerifier(options.Config),
		ipGeo:          newIPGeoResolver(),
	}
	s.webAuthn = newWebAuthn(options.Config, logger)
	if s.store == nil {
		s.store = store.NewMemory()
	}
	if s.nodes == nil {
		s.nodes = node.NewRegistry()
	}
	if s.extensions == nil {
		s.extensions = extensions.DefaultRegistry()
	}
	s.reloadMojangRoutes(context.Background())
	s.reloadIPGeoSettings(context.Background())
	s.routes()
	return s
}

func newWebAuthn(cfg config.Config, logger *slog.Logger) *webauthn.WebAuthn {
	origins := make([]string, 0, len(cfg.CORSAllowedOrigins)+1)
	if cfg.PublicBaseURL != "" {
		origins = append(origins, strings.TrimRight(cfg.PublicBaseURL, "/"))
	}
	for _, origin := range cfg.CORSAllowedOrigins {
		if origin != "" {
			origins = append(origins, strings.TrimRight(origin, "/"))
		}
	}
	rpID := "localhost"
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err == nil && parsed.Hostname() != "" {
			rpID = parsed.Hostname()
			break
		}
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "Authman",
		RPOrigins:     origins,
	})
	if err != nil {
		logger.Warn("webauthn disabled", "error", err)
		return nil
	}
	return w
}

func newMojangVerifier(cfg config.Config) *mojang.SessionVerifier {
	return &mojang.SessionVerifier{
		Pool: &mojang.Pool{
			Routes:          cfg.MojangRoutes,
			FailureCooldown: cfg.MojangCooldown,
		},
		BaseURL:    cfg.MojangSessionURL,
		ProfileURL: cfg.MojangProfileURL,
		Timeout:    cfg.MojangTimeout,
		Cache:      mojang.NewProfileCache(cfg.MojangCacheFresh, cfg.MojangCacheStale),
	}
}

func (s *Server) configuredMojangRoutes(ctx context.Context) []mojang.Route {
	settings := s.mojangRuntimeSettings(ctx)
	routes := s.allMojangRoutes(ctx)
	if len(settings.EnabledRouteIDs) == 0 {
		return routes
	}
	enabled := make(map[string]struct{}, len(settings.EnabledRouteIDs))
	for _, id := range settings.EnabledRouteIDs {
		enabled[id] = struct{}{}
	}
	filtered := make([]mojang.Route, 0, len(routes))
	for _, route := range routes {
		if _, ok := enabled[route.ID]; ok {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (s *Server) allMojangRoutes(ctx context.Context) []mojang.Route {
	routes := append([]mojang.Route(nil), s.cfg.MojangRoutes...)
	custom := s.store.ListMojangRoutes(ctx)
	seen := make(map[string]int, len(routes)+len(custom))
	for i, route := range routes {
		seen[route.ID] = i
	}
	for _, route := range custom {
		if index, ok := seen[route.ID]; ok {
			routes[index] = route
			continue
		}
		seen[route.ID] = len(routes)
		routes = append(routes, route)
	}
	return routes
}

func (s *Server) reloadMojangRoutes(ctx context.Context) {
	routes := s.configuredMojangRoutes(ctx)
	settings := s.mojangRuntimeSettings(ctx)
	if s.mojangVerifier == nil {
		s.mojangVerifier = newMojangVerifier(s.cfg)
	}
	s.mojangVerifier.Timeout = time.Duration(settings.RequestTimeoutSeconds) * time.Second
	s.mojangVerifier.Cache = mojang.NewProfileCache(time.Duration(settings.CacheFreshSeconds)*time.Second, time.Duration(settings.CacheStaleSeconds)*time.Second)
	s.mojangVerifier.SetRoutes(routes, time.Duration(settings.FailureCooldownSeconds)*time.Second)
}

func (s *Server) reloadIPGeoSettings(ctx context.Context) {
	if s.ipGeo == nil {
		s.ipGeo = newIPGeoResolver()
	}
	settings := s.ipGeoSettings(ctx)
	routes := routesByIDs(s.allMojangRoutes(ctx), settings.EnabledRouteIDs)
	s.ipGeo.configure(routes, time.Duration(settings.CacheTTLSeconds)*time.Second, time.Duration(settings.RequestTimeoutSeconds)*time.Second)
}

func (s *Server) Handler() http.Handler {
	return requestIDMiddleware(loggingMiddleware(s.logger, corsMiddleware(s.cfg.CORSAllowedOrigins, s.coreMux)))
}

func (s *Server) ExternalHandler() http.Handler {
	return requestIDMiddleware(loggingMiddleware(s.logger, corsMiddleware(s.cfg.CORSAllowedOrigins, s.externalMux)))
}

func (s *Server) routes() {
	external := s.withExternalAPI
	adminAsset := s.withAdminSession
	s.coreMux.HandleFunc("GET /healthz", s.handleHealth)
	s.coreMux.HandleFunc("GET /readyz", s.handleReady)
	s.coreMux.HandleFunc("GET /api/meta", s.handleMeta)
	s.coreMux.HandleFunc("GET /config.js", s.handleCoreWebConfig)
	s.coreMux.HandleFunc("GET /api/portal/{path...}", s.handleInternalExternalAPINotFound)
	s.coreMux.HandleFunc("POST /api/portal/{path...}", s.handleInternalExternalAPINotFound)
	s.coreMux.HandleFunc("PUT /api/portal/{path...}", s.handleInternalExternalAPINotFound)
	s.coreMux.HandleFunc("PATCH /api/portal/{path...}", s.handleInternalExternalAPINotFound)
	s.coreMux.HandleFunc("DELETE /api/portal/{path...}", s.handleInternalExternalAPINotFound)
	s.coreMux.HandleFunc("GET /", s.handleRoot)
	s.coreMux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", s.handleHasJoined)
	s.coreMux.HandleFunc("GET /api/assets/profiles/{id}/{asset}", adminAsset(s.handleProfileSkinAsset))
	s.coreMux.HandleFunc("GET /api/assets/passports/{id}/{asset}", adminAsset(s.handlePassportSkinAsset))
	s.coreMux.HandleFunc("GET /api/assets/default-skins/{model}/{name}", s.handleDefaultSkinAsset)
	s.externalMux.HandleFunc("GET /healthz", s.handleHealth)
	s.externalMux.HandleFunc("GET /readyz", s.handleReady)
	s.externalMux.HandleFunc("GET /api/meta", s.handleMeta)
	s.externalMux.HandleFunc("GET /api/portal/config", external(s.handlePortalConfig))
	s.externalMux.HandleFunc("GET /api/portal/servers", external(s.handlePortalServers))
	s.externalMux.HandleFunc("GET /api/portal/servers/{slug}/config", external(s.handlePortalServerConfig))
	s.externalMux.HandleFunc("GET /api/assets/profiles/{id}/{asset}", external(s.handleProfileSkinAsset))
	s.externalMux.HandleFunc("GET /api/assets/passports/{id}/{asset}", external(s.handlePassportSkinAsset))
	s.externalMux.HandleFunc("GET /api/assets/default-skins/{model}/{name}", external(s.handleDefaultSkinAsset))
	s.externalMux.HandleFunc("POST /api/portal/offline/register", external(s.handleOfflineRegister))
	s.externalMux.HandleFunc("POST /api/portal/offline/check-name", external(s.handlePortalCheckName))
	s.externalMux.HandleFunc("POST /api/portal/session/login", external(s.handlePortalLogin))
	s.externalMux.HandleFunc("POST /api/portal/session/login-with-link", external(s.handlePortalLinkLogin))
	s.externalMux.HandleFunc("GET /api/portal/session/me", external(s.handlePortalMe))
	s.externalMux.HandleFunc("POST /api/portal/session/select-profile", external(s.handlePortalSelectProfile))
	s.externalMux.HandleFunc("POST /api/portal/session/logout", external(s.handlePortalLogout))
	s.externalMux.HandleFunc("POST /api/portal/profiles", external(s.handlePortalCreateProfile))
	s.externalMux.HandleFunc("POST /api/portal/profiles/{id}/archive", external(s.handlePortalArchiveProfile))
	s.externalMux.HandleFunc("POST /api/portal/profiles/{id}/restore", external(s.handlePortalRestoreProfile))
	s.externalMux.HandleFunc("POST /api/portal/security/password", external(s.handlePortalChangePassword))
	s.externalMux.HandleFunc("POST /api/portal/offline/password/change", external(s.handlePortalChangePassword))
	s.externalMux.HandleFunc("GET /api/portal/passport/skin", external(s.handlePortalPassportSkin))
	s.externalMux.HandleFunc("POST /api/portal/passport/skin", external(s.handlePortalUploadPassportSkin))
	s.externalMux.HandleFunc("POST /api/portal/passport/skin/source", external(s.handlePortalSetPassportSkinSource))
	s.externalMux.HandleFunc("DELETE /api/portal/passport/skin", external(s.handlePortalDeletePassportSkin))
	s.externalMux.HandleFunc("GET /api/portal/profile/skin", external(s.handlePortalProfileSkin))
	s.externalMux.HandleFunc("POST /api/portal/profile/skin", external(s.handlePortalUploadProfileSkin))
	s.externalMux.HandleFunc("POST /api/portal/profile/skin/source", external(s.handlePortalSetProfileSkinSource))
	s.externalMux.HandleFunc("DELETE /api/portal/profile/skin", external(s.handlePortalDeleteProfileSkin))
	s.externalMux.HandleFunc("GET /api/portal/extension-data", external(s.handlePortalExtensionData))
	s.externalMux.HandleFunc("GET /api/portal/player/extension-data", external(s.handlePortalExtensionData))
	s.externalMux.HandleFunc("GET /api/portal/player/extension-data/{serverSlug}", external(s.handlePortalExtensionData))
	s.externalMux.HandleFunc("POST /api/portal/link-login", external(s.handlePortalLinkLogin))
	s.coreMux.HandleFunc("GET /api/admin/bootstrap", s.handleAdminBootstrap)
	s.coreMux.HandleFunc("GET /api/admin/bootstrap/status", s.handleAdminBootstrap)
	s.coreMux.HandleFunc("POST /api/admin/session/login", s.handleAdminLogin)
	s.coreMux.HandleFunc("POST /api/admin/session/mfa/totp", s.handleAdminMFATOTP)
	s.coreMux.HandleFunc("POST /api/admin/session/mfa/passkey/options", s.handleAdminMFAPasskeyOptions)
	s.coreMux.HandleFunc("POST /api/admin/session/mfa/passkey/finish", s.handleAdminMFAPasskeyFinish)
	s.coreMux.HandleFunc("GET /api/admin/me", s.handleAdminMe)
	s.coreMux.HandleFunc("GET /api/admin/session/me", s.handleAdminMe)
	s.coreMux.HandleFunc("POST /api/admin/session/logout", s.handleAdminLogout)
	s.coreMux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)
	s.coreMux.HandleFunc("GET /api/admin/passports", s.handleAdminPassports)
	s.coreMux.HandleFunc("GET /api/admin/passports/{id}", s.handleAdminPassportDetail)
	s.coreMux.HandleFunc("PATCH /api/admin/passports/{id}", s.handleAdminUpdatePassport)
	s.coreMux.HandleFunc("POST /api/admin/passports/{id}/skin", s.handleAdminUploadPassportSkin)
	s.coreMux.HandleFunc("POST /api/admin/passports/{id}/skin/source", s.handleAdminSetPassportSkinSource)
	s.coreMux.HandleFunc("DELETE /api/admin/passports/{id}/skin", s.handleAdminDeletePassportSkin)
	s.coreMux.HandleFunc("POST /api/admin/passports/{id}/bans", s.handleAdminCreatePassportBan)
	s.coreMux.HandleFunc("POST /api/admin/passports/{id}/kick", s.handleAdminKickPassport)
	s.coreMux.HandleFunc("GET /api/admin/profiles", s.handleAdminProfiles)
	s.coreMux.HandleFunc("POST /api/admin/profiles", s.handleAdminCreateProfile)
	s.coreMux.HandleFunc("GET /api/admin/profiles/{id}", s.handleAdminProfileDetail)
	s.coreMux.HandleFunc("PATCH /api/admin/profiles/{id}", s.handleAdminUpdateProfile)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/skin", s.handleAdminUploadProfileSkin)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/skin/source", s.handleAdminSetProfileSkinSource)
	s.coreMux.HandleFunc("DELETE /api/admin/profiles/{id}/skin", s.handleAdminDeleteProfileSkin)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/bind", s.handleAdminBindProfile)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/unbind", s.handleAdminUnbindProfile)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/bans", s.handleAdminCreateProfileBan)
	s.coreMux.HandleFunc("POST /api/admin/profiles/{id}/kick", s.handleAdminKickProfile)
	s.coreMux.HandleFunc("DELETE /api/admin/bans/{id}", s.handleAdminRevokeBan)
	s.coreMux.HandleFunc("POST /api/admin/bans/{id}/extend", s.handleAdminExtendBan)
	s.coreMux.HandleFunc("POST /api/admin/presences/{id}/kick", s.handleAdminKickPresence)
	s.coreMux.HandleFunc("GET /api/admin/players", s.handleAdminPlayers)
	s.coreMux.HandleFunc("GET /api/admin/players/{id}", s.handleAdminPlayerDetail)
	s.coreMux.HandleFunc("PATCH /api/admin/players/{id}", s.handleAdminUpdatePlayer)
	s.coreMux.HandleFunc("POST /api/admin/players/{id}/lock", s.handleAdminLockPlayer)
	s.coreMux.HandleFunc("POST /api/admin/players/{id}/unlock", s.handleAdminUnlockPlayer)
	s.coreMux.HandleFunc("POST /api/admin/players/{id}/reset-password", s.handleAdminResetOfflinePassword)
	s.coreMux.HandleFunc("POST /api/admin/nodes", s.handleAdminCreateNode)
	s.coreMux.HandleFunc("GET /api/admin/nodes", s.handleAdminListNodes)
	s.coreMux.HandleFunc("GET /api/admin/nodes/{id}", s.handleAdminGetNode)
	s.coreMux.HandleFunc("PUT /api/admin/nodes/{id}", s.handleAdminUpdateNode)
	s.coreMux.HandleFunc("POST /api/admin/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.coreMux.HandleFunc("DELETE /api/admin/nodes/{id}", s.handleAdminDeleteNode)
	s.coreMux.HandleFunc("POST /api/admin/login-portals", s.handleAdminCreateLimboPortalNode)
	s.coreMux.HandleFunc("GET /api/admin/login-portals", s.handleAdminListLimboPortalNodes)
	s.coreMux.HandleFunc("GET /api/admin/login-portals/{id}", s.handleAdminGetNode)
	s.coreMux.HandleFunc("PUT /api/admin/login-portals/{id}", s.handleAdminUpdateNode)
	s.coreMux.HandleFunc("POST /api/admin/login-portals/{id}/rotate", s.handleAdminRotateNode)
	s.coreMux.HandleFunc("DELETE /api/admin/login-portals/{id}", s.handleAdminDeleteNode)
	s.coreMux.HandleFunc("POST /api/admin/downstream/nodes", s.handleAdminCreateDownstreamNode)
	s.coreMux.HandleFunc("GET /api/admin/downstream/nodes", s.handleAdminListDownstreamNodes)
	s.coreMux.HandleFunc("GET /api/admin/downstream/nodes/{id}", s.handleAdminGetNode)
	s.coreMux.HandleFunc("PUT /api/admin/downstream/nodes/{id}", s.handleAdminUpdateNode)
	s.coreMux.HandleFunc("POST /api/admin/downstream/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.coreMux.HandleFunc("DELETE /api/admin/downstream/nodes/{id}", s.handleAdminDeleteNode)
	s.coreMux.HandleFunc("POST /api/admin/velocity/nodes", s.handleAdminCreateNode)
	s.coreMux.HandleFunc("GET /api/admin/velocity/nodes", s.handleAdminListNodes)
	s.coreMux.HandleFunc("GET /api/admin/velocity/nodes/{id}", s.handleAdminGetNode)
	s.coreMux.HandleFunc("PUT /api/admin/velocity/nodes/{id}", s.handleAdminUpdateNode)
	s.coreMux.HandleFunc("POST /api/admin/velocity/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.coreMux.HandleFunc("POST /api/admin/velocity/nodes/{id}/disable", s.handleAdminDisableNode)
	s.coreMux.HandleFunc("DELETE /api/admin/velocity/nodes/{id}", s.handleAdminDeleteNode)
	s.coreMux.HandleFunc("GET /api/admin/downstream-servers", s.handleAdminDownstreamServers)
	s.coreMux.HandleFunc("POST /api/admin/downstream-servers", s.handleAdminCreateDownstreamServer)
	s.coreMux.HandleFunc("GET /api/admin/downstream-servers/{id}", s.handleAdminDownstreamServerDetail)
	s.coreMux.HandleFunc("PUT /api/admin/downstream-servers/{id}", s.handleAdminUpdateDownstreamServer)
	s.coreMux.HandleFunc("POST /api/admin/downstream-servers/{id}/icon", s.handleAdminUploadDownstreamServerIcon)
	s.coreMux.HandleFunc("DELETE /api/admin/downstream-servers/{id}/icon", s.handleAdminDeleteDownstreamServerIcon)
	s.coreMux.HandleFunc("DELETE /api/admin/downstream-servers/{id}", s.handleAdminDeleteDownstreamServer)
	s.coreMux.HandleFunc("GET /api/admin/limbo-blueprints", s.handleAdminLimboBlueprints)
	s.coreMux.HandleFunc("POST /api/admin/limbo-blueprints/upload", s.handleAdminUploadLimboBlueprint)
	s.coreMux.HandleFunc("GET /api/admin/limbo-blueprints/{id}", s.handleAdminLimboBlueprintDetail)
	s.coreMux.HandleFunc("PUT /api/admin/limbo-blueprints/{id}", s.handleAdminUpdateLimboBlueprint)
	s.coreMux.HandleFunc("DELETE /api/admin/limbo-blueprints/{id}", s.handleAdminDeleteLimboBlueprint)
	s.coreMux.HandleFunc("GET /api/admin/portal-settings", s.handleAdminPortalSettings)
	s.coreMux.HandleFunc("PUT /api/admin/portal-settings", s.handleAdminUpdatePortalSettings)
	s.coreMux.HandleFunc("GET /api/admin/audit-events", s.handleAdminAuditEvents)
	s.coreMux.HandleFunc("GET /api/admin/audit-events/{id}", s.handleAdminAuditEventDetail)
	s.coreMux.HandleFunc("GET /api/admin/mojang/routes", s.handleAdminMojangRoutes)
	s.coreMux.HandleFunc("POST /api/admin/mojang/routes", s.handleAdminCreateMojangRoute)
	s.coreMux.HandleFunc("PUT /api/admin/mojang/routes/{id}", s.handleAdminUpdateMojangRoute)
	s.coreMux.HandleFunc("DELETE /api/admin/mojang/routes/{id}", s.handleAdminDeleteMojangRoute)
	s.coreMux.HandleFunc("GET /api/admin/mojang/upstream/status", s.handleAdminMojangRoutes)
	s.coreMux.HandleFunc("GET /api/admin/settings/mojang", s.handleAdminMojangSettings)
	s.coreMux.HandleFunc("PUT /api/admin/settings/mojang", s.handleAdminUpdateMojangSettings)
	s.coreMux.HandleFunc("GET /api/admin/settings/ip-geo", s.handleAdminIPGeoSettings)
	s.coreMux.HandleFunc("PUT /api/admin/settings/ip-geo", s.handleAdminUpdateIPGeoSettings)
	s.coreMux.HandleFunc("GET /api/admin/external-tokens", s.handleAdminExternalTokens)
	s.coreMux.HandleFunc("POST /api/admin/external-tokens", s.handleAdminCreateExternalToken)
	s.coreMux.HandleFunc("GET /api/admin/external-tokens/{id}", s.handleAdminExternalTokenDetail)
	s.coreMux.HandleFunc("PUT /api/admin/external-tokens/{id}", s.handleAdminUpdateExternalToken)
	s.coreMux.HandleFunc("DELETE /api/admin/external-tokens/{id}", s.handleAdminRevokeExternalToken)
	s.coreMux.HandleFunc("DELETE /api/admin/external-tokens/{id}/record", s.handleAdminDeleteExternalTokenRecord)
	s.coreMux.HandleFunc("GET /api/admin/extensions", s.handleAdminExtensions)
	s.coreMux.HandleFunc("GET /api/admin/account", s.handleAdminAccount)
	s.coreMux.HandleFunc("PUT /api/admin/account/profile", s.handleAdminAccountProfile)
	s.coreMux.HandleFunc("PUT /api/admin/account/preferences", s.handleAdminAccountPreferences)
	s.coreMux.HandleFunc("POST /api/admin/account/totp/start", s.handleAdminTOTPStart)
	s.coreMux.HandleFunc("POST /api/admin/account/totp/confirm", s.handleAdminTOTPConfirm)
	s.coreMux.HandleFunc("POST /api/admin/account/totp/disable", s.handleAdminTOTPDisable)
	s.coreMux.HandleFunc("POST /api/admin/account/passkeys/options", s.handleAdminPasskeyRegisterOptions)
	s.coreMux.HandleFunc("POST /api/admin/account/passkeys/finish", s.handleAdminPasskeyRegisterFinish)
	s.coreMux.HandleFunc("DELETE /api/admin/account/passkeys/{id}", s.handleAdminPasskeyDelete)
	s.coreMux.HandleFunc("GET /api/admin/users", s.handleAdminUsers)
	s.coreMux.HandleFunc("POST /api/admin/users", s.handleAdminCreateUser)
	s.coreMux.HandleFunc("PUT /api/admin/users/{id}", s.handleAdminUpdateUser)
	s.coreMux.HandleFunc("POST /api/admin/users/{id}/totp/disable", s.handleAdminDisableUserTOTP)
	s.coreMux.HandleFunc("DELETE /api/admin/users/{id}/passkeys/{passkey_id}", s.handleAdminDeleteUserPasskey)
	s.coreMux.HandleFunc("GET /api/admin/permissions", s.handleAdminPermissions)
	s.coreMux.HandleFunc("GET /api/admin/roles", s.handleAdminRoles)
	s.coreMux.HandleFunc("POST /api/admin/roles", s.handleAdminCreateRole)
	s.coreMux.HandleFunc("PUT /api/admin/roles/{id}", s.handleAdminUpdateRole)
	s.coreMux.HandleFunc("DELETE /api/admin/roles/{id}", s.handleAdminDeleteRole)
	s.coreMux.HandleFunc("GET /api/admin/system/summary", s.handleAdminSystemSummary)
	s.coreMux.HandleFunc("POST /api/node/heartbeat", s.handleNodeHeartbeat)
	s.coreMux.HandleFunc("POST /api/node/actions/ack", s.handleNodeAckActions)
	s.coreMux.HandleFunc("POST /api/node/limbo/login-policy", s.handleNodeResolveLimboLoginPolicy)
	s.coreMux.HandleFunc("POST /api/node/limbo/sessions/verify", s.handleNodeVerifyLimboSession)
	s.coreMux.HandleFunc("POST /api/node/players/resolve", s.handleNodeResolvePlayer)
	s.coreMux.HandleFunc("POST /api/node/players/register-offline", s.handleNodeRegisterOfflinePlayer)
	s.coreMux.HandleFunc("POST /api/node/players/authenticate", s.handleNodeAuthenticatePlayer)
	s.coreMux.HandleFunc("POST /api/node/limbo/targets/resolve", s.handleNodeResolvePortalTarget)
	s.coreMux.HandleFunc("GET /api/node/limbo/blueprints/{id}", s.handleNodeLimboBlueprint)
	s.coreMux.HandleFunc("POST /api/node/limbo/transfer-grants", s.handleNodeCreateTransferGrant)
	s.coreMux.HandleFunc("POST /api/node/downstream/transfer-grants/consume", s.handleNodeConsumeTransferGrant)
	s.coreMux.HandleFunc("POST /api/node/presences/end", s.handleNodeEndPresence)
	s.coreMux.HandleFunc("POST /api/node/bans/profile", s.handleNodeCreateProfileBan)
	s.coreMux.HandleFunc("POST /api/node/bans/passport", s.handleNodeCreatePassportBan)
	s.coreMux.HandleFunc("POST /api/node/players/extension-data", s.handleNodeUpsertExtensionData)
	s.coreMux.HandleFunc("POST /api/node/portal-links", s.handleNodeCreatePortalLink)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	}, map[string]any{
		"service": "authman",
	})
}

func (s *Server) handleInternalExternalAPINotFound(w http.ResponseWriter, r *http.Request) {
	api.WriteError(w, api.NewError(http.StatusNotFound, "external_api.not_on_core_port", "external API is not available on the Core port"))
}

func playerData(player identity.Player) map[string]any {
	return map[string]any{
		"id":                  player.ID,
		"kind":                player.Kind,
		"uuid":                player.UUID.String(),
		"uuid_compact":        player.UUID.Compact(),
		"raw_offline_name":    player.RawOfflineName,
		"normalized_name":     player.NormalizedName,
		"protocol_name":       player.ProtocolName,
		"properties":          profilePropertiesData(player.ProfileProperties),
		"locked":              player.Locked,
		"registration_server": player.RegistrationServer,
		"last_seen_server":    player.LastSeenServer,
		"last_seen_at":        player.LastSeenAt,
		"last_seen_ip":        emptyStringNil(player.LastSeenIP),
		"last_seen_geo":       ipGeoData(player.LastSeenGeo),
	}
}

func profilePropertiesData(properties []identity.ProfileProperty) []map[string]any {
	out := make([]map[string]any, 0, len(properties))
	for _, property := range properties {
		out = append(out, map[string]any{
			"name":      property.Name,
			"value":     property.Value,
			"signature": property.Signature,
		})
	}
	return out
}

func nodeProfileProperties(properties []nodeProfilePropertyRequest) []identity.ProfileProperty {
	out := make([]identity.ProfileProperty, 0, len(properties))
	for _, property := range properties {
		name := strings.TrimSpace(property.Name)
		if name == "" {
			continue
		}
		out = append(out, identity.ProfileProperty{
			Name:      name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	return out
}

func (s *Server) ResolveOffline(ctx context.Context, rawName string) (identity.Player, error) {
	player, err := s.store.GetOfflinePlayer(ctx, rawName)
	if errors.Is(err, store.ErrNotFound) {
		return identity.Player{}, yggdrasil.ErrProfileNotFound
	}
	return player, err
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ready",
	}, nil)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"name":       "authman",
		"serverTime": time.Now().UTC().Format(time.RFC3339),
	}, nil)
}
