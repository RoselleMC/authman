package server

import (
	"context"
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
	player, err := s.store.GetOfflinePlayer(r.Context(), strings.TrimPrefix(req.Username, "#"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	premiumNameExists := s.store.PremiumNameExists(r.Context(), player.RawOfflineName)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player": playerData(player),
		"auth": map[string]any{
			"required": !player.Locked,
			"kind":     "offline_password",
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
	player, credential, err := s.store.GetOfflineCredential(r.Context(), strings.TrimPrefix(req.Username, "#"))
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
	username := strings.TrimPrefix(strings.TrimSpace(req.Username), "#")
	if username == "" {
		return identity.Player{}, api.NewError(http.StatusBadRequest, "player.required", "player is required")
	}
	player, err := s.store.GetOfflinePlayer(ctx, username)
	if err != nil {
		return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
	}
	return player, nil
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
	player, err := s.store.GetOfflinePlayer(r.Context(), strings.TrimPrefix(req.Username, "#"))
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
