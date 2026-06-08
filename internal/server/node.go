package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/extensions"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/node"
	"github.com/RoselleMC/authman/internal/store"
)

type createNodeRequest struct {
	Name     string `json:"name"`
	ServerID string `json:"server_id"`
}

type nodeHeartbeatRequest struct {
	Name                string `json:"name"`
	ServerID            string `json:"server_id"`
	InstanceFingerprint string `json:"instance_fingerprint"`
	PluginVersion       string `json:"plugin_version"`
	VelocityVersion     string `json:"velocity_version"`
}

func (s *Server) handleAdminCreateNode(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req createNodeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	node, token, err := s.nodes.Create(r.Context(), req.Name, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "node.create_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, node.ID, "node.create", map[string]any{
		"name": node.Name,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"node":              s.nodeData(r.Context(), node),
		"token":             token,
		"token_once":        token,
		"token_fingerprint": node.TokenFingerprint,
	}, nil)
}

func (s *Server) handleAdminListNodes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	nodes := s.nodes.List(r.Context())
	data := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		data = append(data, s.nodeData(r.Context(), node))
	}
	api.WriteJSON(w, http.StatusOK, data, map[string]any{"count": len(data)})
}

func (s *Server) handleAdminRotateNode(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	node, token, err := s.nodes.Rotate(r.Context(), r.PathValue("id"), time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, node.ID, "node.rotate_token", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"node":              s.nodeData(r.Context(), node),
		"token":             token,
		"token_once":        token,
		"token_fingerprint": node.TokenFingerprint,
	}, nil)
}

func (s *Server) handleAdminDisableNode(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, true); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, nil, nil)
}

func (s *Server) handleAdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var target node.Node
	found := false
	for _, n := range s.nodes.List(r.Context()) {
		if n.ID == id {
			target = n
			found = true
			break
		}
	}
	if !found {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	if nodeStatus(target) == "active" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "node.delete_active", "active nodes cannot be deleted"))
		return
	}
	if err := s.nodes.Delete(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, id, "node.delete", map[string]any{
		"name":                 target.Name,
		"instance_fingerprint": target.InstanceFingerprint,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token"))
		return
	}
	if s.cfg.NodeAccessToken != "" && auth.ConstantTimeTokenEqual("node-access", token, auth.HashToken("node-access", s.cfg.NodeAccessToken)) {
		var req nodeHeartbeatRequest
		if err := api.DecodeJSON(r, &req); err != nil {
			api.WriteError(w, err)
			return
		}
		node, err := s.nodes.Register(r.Context(), node.Registration{
			Name:                req.Name,
			ServerID:            req.ServerID,
			InstanceFingerprint: req.InstanceFingerprint,
			AccessFingerprint:   auth.TokenFingerprint(token),
			PluginVersion:       req.PluginVersion,
			VelocityVersion:     req.VelocityVersion,
		}, time.Now())
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "node.register_failed", err.Error()))
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"node": s.nodeData(r.Context(), node)}, nil)
		return
	}
	node, err := s.nodes.Heartbeat(r.Context(), token, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token"))
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"node": s.nodeData(r.Context(), node)}, nil)
}

type resolvePlayerRequest struct {
	Username string `json:"username"`
}

type resolvePortalTargetRequest struct {
	ServerID      string `json:"server_id"`
	Slug          string `json:"slug"`
	RequestedHost string `json:"requested_host"`
}

type createTransferGrantRequest struct {
	PlayerID      string `json:"player_id"`
	Username      string `json:"username"`
	ServerID      string `json:"server_id"`
	Slug          string `json:"slug"`
	RequestedHost string `json:"requested_host"`
	Source        string `json:"source"`
	TTLSeconds    int    `json:"ttl_seconds"`
}

type consumeTransferGrantRequest struct {
	Token        string `json:"token"`
	TokenHash    string `json:"token_hash"`
	ServerID     string `json:"server_id"`
	UUID         string `json:"uuid"`
	ProtocolName string `json:"protocol_name"`
	Source       string `json:"source"`
}

func (s *Server) requireNode(r *http.Request) (node.Node, *api.Error) {
	token, ok := bearerToken(r)
	if !ok {
		return node.Node{}, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token")
	}
	if s.cfg.NodeAccessToken != "" && auth.ConstantTimeTokenEqual("node-access", token, auth.HashToken("node-access", s.cfg.NodeAccessToken)) {
		instance := strings.TrimSpace(r.Header.Get("X-Authman-Instance"))
		if instance == "" {
			return node.Node{}, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node instance")
		}
		for _, n := range s.nodes.List(r.Context()) {
			if !n.Disabled && n.InstanceFingerprint == instance {
				return n, nil
			}
		}
		return node.Node{}, api.NewError(http.StatusUnauthorized, "node.unauthorized", "node instance is not registered")
	}
	n, err := s.nodes.Authenticate(r.Context(), token)
	if err != nil {
		return node.Node{}, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token")
	}
	return n, nil
}

func (s *Server) handleNodeResolvePlayer(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireNode(r); err != nil {
		api.WriteError(w, err)
		return
	}
	var req resolvePlayerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, err := s.store.GetOfflinePlayer(r.Context(), normalizeNodeUsername(req.Username))
	if err != nil {
		player, err = s.store.GetPlayerByProtocolName(r.Context(), strings.TrimSpace(req.Username))
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
			return
		}
	}
	authRequired := player.Kind == identity.PlayerKindOffline && !player.Locked
	authKind := "premium"
	if player.Kind == identity.PlayerKindOffline {
		authKind = "offline_password"
	}
	premiumNameExists := player.Kind == identity.PlayerKindOffline && s.store.PremiumNameExists(r.Context(), player.RawOfflineName)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player": playerData(player),
		"auth": map[string]any{
			"required": authRequired,
			"kind":     authKind,
			"locked":   player.Locked,
		},
		"display": map[string]any{
			"strip_offline_prefix": !premiumNameExists,
			"premium_name_exists":  premiumNameExists,
		},
	}, nil)
}

type authenticatePlayerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type upsertExtensionDataRequest struct {
	PlayerID   string                `json:"player_id"`
	Username   string                `json:"username"`
	ServerID   string                `json:"server_id"`
	Provider   string                `json:"provider"`
	Visibility extensions.Visibility `json:"visibility"`
	Schema     extensions.Schema     `json:"schema"`
	Values     map[string]any        `json:"values"`
}

func (s *Server) handleNodeAuthenticatePlayer(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req authenticatePlayerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, credential, err := s.store.GetOfflineCredential(r.Context(), normalizeNodeUsername(req.Username))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	if player.Locked || credentialLocked(credential, time.Now()) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		_, _ = s.store.RecordOfflineLoginFailure(r.Context(), player.ID, time.Now())
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.failure", nil)
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	_ = s.store.RecordOfflineLoginSuccess(r.Context(), player.ID)
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.success", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"player":        playerData(player),
	}, nil)
}

func (s *Server) handleNodeResolvePortalTarget(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req resolvePortalTargetRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	server, target, apiErr := s.resolveDownstreamTarget(r.Context(), n, req.ServerID, req.Slug, req.RequestedHost)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"server": downstreamServerData(server),
		"target": store.DownstreamTargetData(target),
	}, nil)
}

func (s *Server) handleNodeCreateTransferGrant(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req createTransferGrantRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	server, target, apiErr := s.resolveDownstreamTarget(r.Context(), n, req.ServerID, req.Slug, req.RequestedHost)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	if target.Status != "active" {
		api.WriteError(w, api.NewError(http.StatusForbidden, "server.unavailable", "downstream server is not active"))
		return
	}
	player, apiErr := s.playerFromTransferGrantRequest(r.Context(), req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	if player.Locked {
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ttlSeconds := target.GrantTTLSeconds
	if req.TTLSeconds > 0 && req.TTLSeconds < ttlSeconds {
		ttlSeconds = req.TTLSeconds
	}
	if ttlSeconds < 5 {
		ttlSeconds = 5
	}
	if ttlSeconds > 300 {
		ttlSeconds = 300
	}
	now := time.Now()
	grant, rawToken, err := auth.NewTransferGrant(
		player.ID,
		server.ID,
		n.ID,
		portalGrantSource(n, req.Source),
		player.UUID.String(),
		player.ProtocolName,
		target.TransferHost,
		target.TransferPort,
		time.Duration(ttlSeconds)*time.Second,
		now,
	)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "transfer_grant.create_failed", "failed to create transfer grant"))
		return
	}
	if err := s.store.SaveTransferGrant(r.Context(), grant); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "transfer_grant.save_failed", "failed to save transfer grant"))
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.create", map[string]any{
		"server_id":     server.ID,
		"target_host":   target.TransferHost,
		"target_port":   target.TransferPort,
		"protocol_name": player.ProtocolName,
		"expires_at":    grant.ExpiresAt,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"grant":  transferGrantData(grant),
		"token":  rawToken,
		"target": store.DownstreamTargetData(target),
		"player": playerData(player),
	}, nil)
}

func (s *Server) handleNodeConsumeTransferGrant(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req consumeTransferGrantRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	serverID := strings.TrimSpace(req.ServerID)
	if serverID == "" {
		serverID = n.ServerID
	}
	if serverID == "" {
		serverID = "default"
	}
	server, err := s.store.GetDownstreamServer(r.Context(), serverID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	target := store.DownstreamTargetFromServer(server)
	if !target.GateEnabled {
		api.WriteError(w, api.NewError(http.StatusForbidden, "gate.disabled", "gate validation is disabled for this downstream server"))
		return
	}
	tokenHash := strings.TrimSpace(req.TokenHash)
	if tokenHash == "" && strings.TrimSpace(req.Token) != "" {
		tokenHash = auth.TransferGrantHash(strings.TrimSpace(req.Token))
	}
	if tokenHash == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "transfer_grant.token_required", "transfer grant token is required"))
		return
	}
	uuid := strings.TrimSpace(req.UUID)
	protocolName := strings.TrimSpace(req.ProtocolName)
	if uuid == "" || protocolName == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "transfer_grant.identity_required", "uuid and protocol name are required"))
		return
	}
	grant, err := s.store.ConsumeTransferGrant(r.Context(), tokenHash, server.ID, uuid, protocolName, n.ID, target.AllowedPortalSources, time.Now())
	if err != nil {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "transfer_grant.reject", map[string]any{
			"reason":        err.Error(),
			"protocol_name": protocolName,
			"uuid":          uuid,
			"source":        req.Source,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "transfer_grant.invalid", err.Error()))
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), grant.PlayerID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.consume", map[string]any{
		"server_id":     server.ID,
		"protocol_name": protocolName,
		"uuid":          uuid,
		"source":        req.Source,
		"portal_source": grant.PortalSource,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed": true,
		"grant":   transferGrantData(grant),
		"player":  playerData(player),
		"target":  store.DownstreamTargetData(target),
	}, nil)
}

func (s *Server) handleNodeUpsertExtensionData(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req upsertExtensionDataRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, apiErr := s.playerFromExtensionRequest(r.Context(), req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	if strings.TrimSpace(req.Provider) == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "extension.provider_required", "provider is required"))
		return
	}
	if req.Schema.Version <= 0 || req.Schema.Title == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "extension.schema_invalid", "schema is invalid"))
		return
	}
	serverID := strings.TrimSpace(req.ServerID)
	if serverID == "" {
		serverID = n.ServerID
	}
	if serverID == "" {
		serverID = "default"
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = extensions.VisibilityPlayerVisible
	}
	data, err := s.store.UpsertExtensionPlayerData(r.Context(), store.ExtensionPlayerData{
		ServerID:   serverID,
		PlayerID:   player.ID,
		Provider:   strings.TrimSpace(req.Provider),
		Schema:     req.Schema,
		Values:     req.Values,
		Visibility: visibility,
		Source:     "node_api",
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "extension.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetExtensionData, data.ID, "extension_data.upsert", map[string]any{
		"player_id": player.ID,
		"server_id": serverID,
		"provider":  data.Provider,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"extension_data": data}, nil)
}

func (s *Server) playerFromExtensionRequest(ctx context.Context, req upsertExtensionDataRequest) (identity.Player, *api.Error) {
	if req.PlayerID != "" {
		player, err := s.store.GetPlayerByID(ctx, req.PlayerID)
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
		return player, nil
	}
	username := normalizeNodeUsername(req.Username)
	if username == "" {
		return identity.Player{}, api.NewError(http.StatusBadRequest, "player.required", "player is required")
	}
	player, err := s.store.GetOfflinePlayer(ctx, username)
	if err != nil {
		player, err = s.store.GetPlayerByProtocolName(ctx, strings.TrimSpace(req.Username))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
	}
	return player, nil
}

func (s *Server) playerFromTransferGrantRequest(ctx context.Context, req createTransferGrantRequest) (identity.Player, *api.Error) {
	if strings.TrimSpace(req.PlayerID) != "" {
		player, err := s.store.GetPlayerByID(ctx, strings.TrimSpace(req.PlayerID))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
		return player, nil
	}
	username := normalizeNodeUsername(req.Username)
	if username == "" {
		return identity.Player{}, api.NewError(http.StatusBadRequest, "player.required", "player is required")
	}
	player, err := s.store.GetOfflinePlayer(ctx, username)
	if err != nil {
		player, err = s.store.GetPlayerByProtocolName(ctx, strings.TrimSpace(req.Username))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
	}
	return player, nil
}

func (s *Server) resolveDownstreamTarget(ctx context.Context, n node.Node, serverID string, slug string, requestedHost string) (store.DownstreamServer, store.DownstreamTarget, *api.Error) {
	candidate := strings.TrimSpace(serverID)
	if candidate == "" {
		candidate = strings.TrimSpace(slug)
	}
	if candidate == "" {
		host := normalizeRequestedHost(requestedHost)
		if host != "" {
			for _, server := range s.store.ListDownstreamServers(ctx) {
				for _, portalHost := range stringSliceFromAnyServer(server.PortalConfig["portal_hosts"]) {
					if strings.EqualFold(normalizeRequestedHost(portalHost), host) {
						target := store.DownstreamTargetFromServer(server)
						return server, target, nil
					}
				}
				if strings.EqualFold(server.Slug, host) {
					target := store.DownstreamTargetFromServer(server)
					return server, target, nil
				}
			}
		}
	}
	if candidate == "" {
		candidate = strings.TrimSpace(n.ServerID)
	}
	if candidate == "" {
		candidate = "default"
	}
	server, err := s.store.GetDownstreamServer(ctx, candidate)
	if err != nil {
		return store.DownstreamServer{}, store.DownstreamTarget{}, api.NewError(http.StatusNotFound, "server.not_found", "server not found")
	}
	target := store.DownstreamTargetFromServer(server)
	return server, target, nil
}

func normalizeNodeUsername(username string) string {
	return strings.TrimPrefix(strings.TrimSpace(username), "#")
}

func normalizeRequestedHost(value string) string {
	host := strings.ToLower(strings.TrimSpace(value))
	if host == "" {
		return ""
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return strings.Trim(host, "[]")
}

func portalSourceAllowed(source string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	for _, allowedSource := range allowed {
		if strings.EqualFold(strings.TrimSpace(allowedSource), source) {
			return true
		}
	}
	return false
}

func portalGrantSource(n node.Node, requested string) string {
	source := strings.TrimSpace(requested)
	if source != "" {
		return source
	}
	if strings.TrimSpace(n.Name) != "" {
		return strings.TrimSpace(n.Name)
	}
	return strings.TrimSpace(n.ID)
}

func stringSliceFromAnyServer(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return []string{}
		}
		return strings.Split(typed, ",")
	default:
		return []string{}
	}
}

type createPortalLinkRequest struct {
	Username string `json:"username"`
	ServerID string `json:"server_id"`
	TTL      string `json:"ttl"`
}

func (s *Server) handleNodeCreatePortalLink(w http.ResponseWriter, r *http.Request) {
	node, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req createPortalLinkRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, err := s.store.GetOfflinePlayer(r.Context(), normalizeNodeUsername(req.Username))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	ttl := 10 * time.Minute
	if req.TTL != "" {
		if parsed, err := time.ParseDuration(req.TTL); err == nil && parsed > 0 && parsed <= time.Hour {
			ttl = parsed
		}
	}
	link, rawToken, err := auth.NewPortalLink(auth.PortalLinkOffline, player.ID, req.ServerID, ttl, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to create portal link"))
		return
	}
	if err := s.store.SavePortalLink(r.Context(), link); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to save portal link"))
		return
	}
	s.audit(r, audit.ActorNode, node.ID, audit.TargetPlayer, player.ID, "portal_link.create", map[string]any{
		"server_id":  req.ServerID,
		"expires_at": link.ExpiresAt,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"link": map[string]any{
			"id":         link.ID,
			"kind":       link.Kind,
			"player_id":  link.PlayerID,
			"server_id":  link.ServerID,
			"expires_at": link.ExpiresAt,
			"url":        s.cfg.PublicBaseURL + "/portal/link#token=" + rawToken,
			"token":      rawToken,
		},
	}, nil)
}

func bearerToken(r *http.Request) (string, bool) {
	value := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(value, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

func (s *Server) nodeData(ctx context.Context, n node.Node) map[string]any {
	serverID := n.ServerID
	if serverID == "" {
		serverID = "default"
	}
	serverLabel := serverID
	if server, err := s.store.GetDownstreamServer(ctx, serverID); err == nil {
		serverLabel = server.DisplayName
	}
	return map[string]any{
		"id":                   n.ID,
		"name":                 n.Name,
		"server_id":            serverID,
		"server_label":         serverLabel,
		"status":               nodeStatus(n),
		"token_fingerprint":    n.TokenFingerprint,
		"instance_fingerprint": n.InstanceFingerprint,
		"plugin_version":       n.PluginVersion,
		"velocity_version":     n.VelocityVersion,
		"disabled":             n.Disabled,
		"created_at":           n.CreatedAt,
		"last_seen_at":         n.LastHeartbeatAt,
		"last_heartbeat_at":    n.LastHeartbeatAt,
	}
}

func nodeStatus(n node.Node) string {
	if n.Disabled {
		return "disabled"
	}
	if n.LastHeartbeatAt == nil {
		return "stale"
	}
	if time.Since(*n.LastHeartbeatAt) > 5*time.Minute {
		return "stale"
	}
	return "active"
}

func transferGrantData(grant auth.TransferGrant) map[string]any {
	return map[string]any{
		"id":             grant.ID,
		"player_id":      grant.PlayerID,
		"server_id":      grant.ServerID,
		"portal_node_id": grant.PortalNodeID,
		"portal_source":  grant.PortalSource,
		"gate_node_id":   grant.GateNodeID,
		"uuid":           grant.UUID,
		"protocol_name":  grant.ProtocolName,
		"target_host":    grant.TargetHost,
		"target_port":    grant.TargetPort,
		"created_at":     grant.CreatedAt,
		"expires_at":     grant.ExpiresAt,
		"consumed_at":    grant.ConsumedAt,
	}
}
