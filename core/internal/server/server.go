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
	mux            *http.ServeMux
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
		mux:            http.NewServeMux(),
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
		BaseURL: cfg.MojangSessionURL,
		Timeout: cfg.MojangTimeout,
		Cache:   mojang.NewProfileCache(cfg.MojangCacheFresh, cfg.MojangCacheStale),
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
	return requestIDMiddleware(loggingMiddleware(s.logger, corsMiddleware(s.cfg.CORSAllowedOrigins, s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.HandleFunc("GET /config.js", s.handleCoreWebConfig)
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", s.handleHasJoined)
	s.mux.HandleFunc("GET /api/portal/config", s.handlePortalConfig)
	s.mux.HandleFunc("GET /api/portal/servers", s.handlePortalServers)
	s.mux.HandleFunc("GET /api/portal/servers/{slug}/config", s.handlePortalServerConfig)
	s.mux.HandleFunc("GET /api/assets/profiles/{id}/{asset}", s.handleProfileSkinAsset)
	s.mux.HandleFunc("GET /api/assets/passports/{id}/avatar.png", s.handlePassportAvatarAsset)
	s.mux.HandleFunc("GET /api/assets/default-skins/{model}/{name}", s.handleDefaultSkinAsset)
	s.mux.HandleFunc("POST /api/portal/offline/register", s.handleOfflineRegister)
	s.mux.HandleFunc("POST /api/portal/offline/check-name", s.handlePortalCheckName)
	s.mux.HandleFunc("POST /api/portal/session/login", s.handlePortalLogin)
	s.mux.HandleFunc("POST /api/portal/session/login-with-link", s.handlePortalLinkLogin)
	s.mux.HandleFunc("GET /api/portal/session/me", s.handlePortalMe)
	s.mux.HandleFunc("POST /api/portal/session/select-profile", s.handlePortalSelectProfile)
	s.mux.HandleFunc("POST /api/portal/session/logout", s.handlePortalLogout)
	s.mux.HandleFunc("POST /api/portal/security/password", s.handlePortalChangePassword)
	s.mux.HandleFunc("POST /api/portal/offline/password/change", s.handlePortalChangePassword)
	s.mux.HandleFunc("GET /api/portal/extension-data", s.handlePortalExtensionData)
	s.mux.HandleFunc("GET /api/portal/player/extension-data", s.handlePortalExtensionData)
	s.mux.HandleFunc("GET /api/portal/player/extension-data/{serverSlug}", s.handlePortalExtensionData)
	s.mux.HandleFunc("POST /api/portal/link-login", s.handlePortalLinkLogin)
	s.mux.HandleFunc("GET /api/admin/bootstrap", s.handleAdminBootstrap)
	s.mux.HandleFunc("GET /api/admin/bootstrap/status", s.handleAdminBootstrap)
	s.mux.HandleFunc("POST /api/admin/session/login", s.handleAdminLogin)
	s.mux.HandleFunc("POST /api/admin/session/mfa/totp", s.handleAdminMFATOTP)
	s.mux.HandleFunc("POST /api/admin/session/mfa/passkey/options", s.handleAdminMFAPasskeyOptions)
	s.mux.HandleFunc("POST /api/admin/session/mfa/passkey/finish", s.handleAdminMFAPasskeyFinish)
	s.mux.HandleFunc("GET /api/admin/me", s.handleAdminMe)
	s.mux.HandleFunc("GET /api/admin/session/me", s.handleAdminMe)
	s.mux.HandleFunc("POST /api/admin/session/logout", s.handleAdminLogout)
	s.mux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)
	s.mux.HandleFunc("GET /api/admin/passports", s.handleAdminPassports)
	s.mux.HandleFunc("GET /api/admin/passports/{id}", s.handleAdminPassportDetail)
	s.mux.HandleFunc("PATCH /api/admin/passports/{id}", s.handleAdminUpdatePassport)
	s.mux.HandleFunc("POST /api/admin/passports/{id}/bans", s.handleAdminCreatePassportBan)
	s.mux.HandleFunc("POST /api/admin/passports/{id}/kick", s.handleAdminKickPassport)
	s.mux.HandleFunc("GET /api/admin/profiles", s.handleAdminProfiles)
	s.mux.HandleFunc("POST /api/admin/profiles", s.handleAdminCreateProfile)
	s.mux.HandleFunc("GET /api/admin/profiles/{id}", s.handleAdminProfileDetail)
	s.mux.HandleFunc("PATCH /api/admin/profiles/{id}", s.handleAdminUpdateProfile)
	s.mux.HandleFunc("POST /api/admin/profiles/{id}/skin", s.handleAdminUploadProfileSkin)
	s.mux.HandleFunc("DELETE /api/admin/profiles/{id}/skin", s.handleAdminDeleteProfileSkin)
	s.mux.HandleFunc("POST /api/admin/profiles/{id}/bind", s.handleAdminBindProfile)
	s.mux.HandleFunc("POST /api/admin/profiles/{id}/unbind", s.handleAdminUnbindProfile)
	s.mux.HandleFunc("POST /api/admin/profiles/{id}/bans", s.handleAdminCreateProfileBan)
	s.mux.HandleFunc("POST /api/admin/profiles/{id}/kick", s.handleAdminKickProfile)
	s.mux.HandleFunc("DELETE /api/admin/bans/{id}", s.handleAdminRevokeBan)
	s.mux.HandleFunc("POST /api/admin/bans/{id}/extend", s.handleAdminExtendBan)
	s.mux.HandleFunc("POST /api/admin/presences/{id}/kick", s.handleAdminKickPresence)
	s.mux.HandleFunc("GET /api/admin/players", s.handleAdminPlayers)
	s.mux.HandleFunc("GET /api/admin/players/{id}", s.handleAdminPlayerDetail)
	s.mux.HandleFunc("PATCH /api/admin/players/{id}", s.handleAdminUpdatePlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/lock", s.handleAdminLockPlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/unlock", s.handleAdminUnlockPlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/reset-password", s.handleAdminResetOfflinePassword)
	s.mux.HandleFunc("POST /api/admin/nodes", s.handleAdminCreateNode)
	s.mux.HandleFunc("GET /api/admin/nodes", s.handleAdminListNodes)
	s.mux.HandleFunc("POST /api/admin/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("POST /api/admin/login-portals", s.handleAdminCreateLimboPortalNode)
	s.mux.HandleFunc("GET /api/admin/login-portals", s.handleAdminListLimboPortalNodes)
	s.mux.HandleFunc("GET /api/admin/login-portals/{id}", s.handleAdminGetNode)
	s.mux.HandleFunc("PUT /api/admin/login-portals/{id}", s.handleAdminUpdateNode)
	s.mux.HandleFunc("POST /api/admin/login-portals/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("DELETE /api/admin/login-portals/{id}", s.handleAdminDeleteNode)
	s.mux.HandleFunc("POST /api/admin/downstream/nodes", s.handleAdminCreateDownstreamNode)
	s.mux.HandleFunc("GET /api/admin/downstream/nodes", s.handleAdminListDownstreamNodes)
	s.mux.HandleFunc("GET /api/admin/downstream/nodes/{id}", s.handleAdminGetNode)
	s.mux.HandleFunc("PUT /api/admin/downstream/nodes/{id}", s.handleAdminUpdateNode)
	s.mux.HandleFunc("POST /api/admin/downstream/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("DELETE /api/admin/downstream/nodes/{id}", s.handleAdminDeleteNode)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes", s.handleAdminCreateNode)
	s.mux.HandleFunc("GET /api/admin/velocity/nodes", s.handleAdminListNodes)
	s.mux.HandleFunc("GET /api/admin/velocity/nodes/{id}", s.handleAdminGetNode)
	s.mux.HandleFunc("PUT /api/admin/velocity/nodes/{id}", s.handleAdminUpdateNode)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes/{id}/disable", s.handleAdminDisableNode)
	s.mux.HandleFunc("DELETE /api/admin/velocity/nodes/{id}", s.handleAdminDeleteNode)
	s.mux.HandleFunc("GET /api/admin/downstream-servers", s.handleAdminDownstreamServers)
	s.mux.HandleFunc("POST /api/admin/downstream-servers", s.handleAdminCreateDownstreamServer)
	s.mux.HandleFunc("GET /api/admin/downstream-servers/{id}", s.handleAdminDownstreamServerDetail)
	s.mux.HandleFunc("PUT /api/admin/downstream-servers/{id}", s.handleAdminUpdateDownstreamServer)
	s.mux.HandleFunc("DELETE /api/admin/downstream-servers/{id}", s.handleAdminDeleteDownstreamServer)
	s.mux.HandleFunc("GET /api/admin/limbo-blueprints", s.handleAdminLimboBlueprints)
	s.mux.HandleFunc("POST /api/admin/limbo-blueprints/upload", s.handleAdminUploadLimboBlueprint)
	s.mux.HandleFunc("GET /api/admin/limbo-blueprints/{id}", s.handleAdminLimboBlueprintDetail)
	s.mux.HandleFunc("PUT /api/admin/limbo-blueprints/{id}", s.handleAdminUpdateLimboBlueprint)
	s.mux.HandleFunc("DELETE /api/admin/limbo-blueprints/{id}", s.handleAdminDeleteLimboBlueprint)
	s.mux.HandleFunc("GET /api/admin/portal-settings", s.handleAdminPortalSettings)
	s.mux.HandleFunc("PUT /api/admin/portal-settings", s.handleAdminUpdatePortalSettings)
	s.mux.HandleFunc("GET /api/admin/audit-events", s.handleAdminAuditEvents)
	s.mux.HandleFunc("GET /api/admin/audit-events/{id}", s.handleAdminAuditEventDetail)
	s.mux.HandleFunc("GET /api/admin/mojang/routes", s.handleAdminMojangRoutes)
	s.mux.HandleFunc("POST /api/admin/mojang/routes", s.handleAdminCreateMojangRoute)
	s.mux.HandleFunc("PUT /api/admin/mojang/routes/{id}", s.handleAdminUpdateMojangRoute)
	s.mux.HandleFunc("DELETE /api/admin/mojang/routes/{id}", s.handleAdminDeleteMojangRoute)
	s.mux.HandleFunc("GET /api/admin/mojang/upstream/status", s.handleAdminMojangRoutes)
	s.mux.HandleFunc("GET /api/admin/settings/mojang", s.handleAdminMojangSettings)
	s.mux.HandleFunc("PUT /api/admin/settings/mojang", s.handleAdminUpdateMojangSettings)
	s.mux.HandleFunc("GET /api/admin/settings/ip-geo", s.handleAdminIPGeoSettings)
	s.mux.HandleFunc("PUT /api/admin/settings/ip-geo", s.handleAdminUpdateIPGeoSettings)
	s.mux.HandleFunc("GET /api/admin/extensions", s.handleAdminExtensions)
	s.mux.HandleFunc("GET /api/admin/account", s.handleAdminAccount)
	s.mux.HandleFunc("PUT /api/admin/account/profile", s.handleAdminAccountProfile)
	s.mux.HandleFunc("PUT /api/admin/account/preferences", s.handleAdminAccountPreferences)
	s.mux.HandleFunc("POST /api/admin/account/totp/start", s.handleAdminTOTPStart)
	s.mux.HandleFunc("POST /api/admin/account/totp/confirm", s.handleAdminTOTPConfirm)
	s.mux.HandleFunc("POST /api/admin/account/totp/disable", s.handleAdminTOTPDisable)
	s.mux.HandleFunc("POST /api/admin/account/passkeys/options", s.handleAdminPasskeyRegisterOptions)
	s.mux.HandleFunc("POST /api/admin/account/passkeys/finish", s.handleAdminPasskeyRegisterFinish)
	s.mux.HandleFunc("DELETE /api/admin/account/passkeys/{id}", s.handleAdminPasskeyDelete)
	s.mux.HandleFunc("GET /api/admin/users", s.handleAdminUsers)
	s.mux.HandleFunc("POST /api/admin/users", s.handleAdminCreateUser)
	s.mux.HandleFunc("PUT /api/admin/users/{id}", s.handleAdminUpdateUser)
	s.mux.HandleFunc("POST /api/admin/users/{id}/totp/disable", s.handleAdminDisableUserTOTP)
	s.mux.HandleFunc("DELETE /api/admin/users/{id}/passkeys/{passkey_id}", s.handleAdminDeleteUserPasskey)
	s.mux.HandleFunc("GET /api/admin/permissions", s.handleAdminPermissions)
	s.mux.HandleFunc("GET /api/admin/roles", s.handleAdminRoles)
	s.mux.HandleFunc("POST /api/admin/roles", s.handleAdminCreateRole)
	s.mux.HandleFunc("PUT /api/admin/roles/{id}", s.handleAdminUpdateRole)
	s.mux.HandleFunc("DELETE /api/admin/roles/{id}", s.handleAdminDeleteRole)
	s.mux.HandleFunc("GET /api/admin/system/summary", s.handleAdminSystemSummary)
	s.mux.HandleFunc("POST /api/node/heartbeat", s.handleNodeHeartbeat)
	s.mux.HandleFunc("POST /api/node/actions/ack", s.handleNodeAckActions)
	s.mux.HandleFunc("POST /api/node/limbo/sessions/verify", s.handleNodeVerifyLimboSession)
	s.mux.HandleFunc("POST /api/node/players/resolve", s.handleNodeResolvePlayer)
	s.mux.HandleFunc("POST /api/node/players/authenticate", s.handleNodeAuthenticatePlayer)
	s.mux.HandleFunc("POST /api/node/limbo/targets/resolve", s.handleNodeResolvePortalTarget)
	s.mux.HandleFunc("GET /api/node/limbo/blueprints/{id}", s.handleNodeLimboBlueprint)
	s.mux.HandleFunc("POST /api/node/limbo/transfer-grants", s.handleNodeCreateTransferGrant)
	s.mux.HandleFunc("POST /api/node/downstream/transfer-grants/consume", s.handleNodeConsumeTransferGrant)
	s.mux.HandleFunc("POST /api/node/presences/end", s.handleNodeEndPresence)
	s.mux.HandleFunc("POST /api/node/bans/profile", s.handleNodeCreateProfileBan)
	s.mux.HandleFunc("POST /api/node/bans/passport", s.handleNodeCreatePassportBan)
	s.mux.HandleFunc("POST /api/node/players/extension-data", s.handleNodeUpsertExtensionData)
	s.mux.HandleFunc("POST /api/node/portal-links", s.handleNodeCreatePortalLink)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	}, map[string]any{
		"service": "authman",
	})
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
