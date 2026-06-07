package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/config"
	"github.com/RoselleMC/authman/internal/extensions"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/mojang"
	"github.com/RoselleMC/authman/internal/node"
	"github.com/RoselleMC/authman/internal/store"
	"github.com/RoselleMC/authman/internal/yggdrasil"
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
	Authenticate(ctx context.Context, token string) (node.Node, error)
	Rotate(ctx context.Context, id string, now time.Time) (node.Node, string, error)
	Heartbeat(ctx context.Context, token string, now time.Time) (node.Node, error)
	Register(ctx context.Context, registration node.Registration, now time.Time) (node.Node, error)
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
	}
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
	s.routes()
	return s
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
	if s.mojangVerifier == nil {
		s.mojangVerifier = newMojangVerifier(s.cfg)
	}
	s.mojangVerifier.SetRoutes(routes, s.cfg.MojangCooldown)
}

func (s *Server) Handler() http.Handler {
	return requestIDMiddleware(loggingMiddleware(s.logger, corsMiddleware(s.cfg.CORSAllowedOrigins, s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.HandleFunc("GET /", s.handleYggdrasilMetadata)
	s.mux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", s.handleHasJoined)
	s.mux.HandleFunc("GET /api/portal/config", s.handlePortalConfig)
	s.mux.HandleFunc("GET /api/portal/servers", s.handlePortalServers)
	s.mux.HandleFunc("GET /api/portal/servers/{slug}/config", s.handlePortalServerConfig)
	s.mux.HandleFunc("POST /api/portal/offline/register", s.handleOfflineRegister)
	s.mux.HandleFunc("POST /api/portal/offline/check-name", s.handlePortalCheckName)
	s.mux.HandleFunc("POST /api/portal/session/login", s.handlePortalLogin)
	s.mux.HandleFunc("POST /api/portal/session/login-with-link", s.handlePortalLinkLogin)
	s.mux.HandleFunc("GET /api/portal/session/me", s.handlePortalMe)
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
	s.mux.HandleFunc("GET /api/admin/me", s.handleAdminMe)
	s.mux.HandleFunc("GET /api/admin/session/me", s.handleAdminMe)
	s.mux.HandleFunc("POST /api/admin/session/logout", s.handleAdminLogout)
	s.mux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)
	s.mux.HandleFunc("GET /api/admin/players", s.handleAdminPlayers)
	s.mux.HandleFunc("GET /api/admin/players/{id}", s.handleAdminPlayerDetail)
	s.mux.HandleFunc("PATCH /api/admin/players/{id}", s.handleAdminUpdatePlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/lock", s.handleAdminLockPlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/unlock", s.handleAdminUnlockPlayer)
	s.mux.HandleFunc("POST /api/admin/players/{id}/reset-password", s.handleAdminResetOfflinePassword)
	s.mux.HandleFunc("POST /api/admin/nodes", s.handleAdminCreateNode)
	s.mux.HandleFunc("GET /api/admin/nodes", s.handleAdminListNodes)
	s.mux.HandleFunc("POST /api/admin/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes", s.handleAdminCreateNode)
	s.mux.HandleFunc("GET /api/admin/velocity/nodes", s.handleAdminListNodes)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes/{id}/rotate", s.handleAdminRotateNode)
	s.mux.HandleFunc("POST /api/admin/velocity/nodes/{id}/disable", s.handleAdminDisableNode)
	s.mux.HandleFunc("DELETE /api/admin/velocity/nodes/{id}", s.handleAdminDeleteNode)
	s.mux.HandleFunc("GET /api/admin/audit-events", s.handleAdminAuditEvents)
	s.mux.HandleFunc("GET /api/admin/mojang/routes", s.handleAdminMojangRoutes)
	s.mux.HandleFunc("POST /api/admin/mojang/routes", s.handleAdminCreateMojangRoute)
	s.mux.HandleFunc("DELETE /api/admin/mojang/routes/{id}", s.handleAdminDeleteMojangRoute)
	s.mux.HandleFunc("GET /api/admin/mojang/upstream/status", s.handleAdminMojangRoutes)
	s.mux.HandleFunc("GET /api/admin/downstream-servers", s.handleAdminDownstreamServers)
	s.mux.HandleFunc("POST /api/admin/downstream-servers", s.handleAdminCreateDownstreamServer)
	s.mux.HandleFunc("GET /api/admin/downstream-servers/{id}", s.handleAdminDownstreamServerDetail)
	s.mux.HandleFunc("PUT /api/admin/downstream-servers/{id}", s.handleAdminUpdateDownstreamServer)
	s.mux.HandleFunc("DELETE /api/admin/downstream-servers/{id}", s.handleAdminDeleteDownstreamServer)
	s.mux.HandleFunc("GET /api/admin/extensions", s.handleAdminExtensions)
	s.mux.HandleFunc("GET /api/admin/users", s.handleAdminUsers)
	s.mux.HandleFunc("GET /api/admin/system/summary", s.handleAdminSystemSummary)
	s.mux.HandleFunc("POST /api/node/heartbeat", s.handleNodeHeartbeat)
	s.mux.HandleFunc("POST /api/node/players/resolve", s.handleNodeResolvePlayer)
	s.mux.HandleFunc("POST /api/node/players/authenticate", s.handleNodeAuthenticatePlayer)
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
		"locked":              player.Locked,
		"registration_server": player.RegistrationServer,
		"last_seen_server":    player.LastSeenServer,
	}
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
