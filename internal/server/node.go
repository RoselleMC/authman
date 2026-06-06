package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/node"
)

type createNodeRequest struct {
	Name     string `json:"name"`
	ServerID string `json:"server_id"`
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
		"node":              nodeData(node),
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
		data = append(data, nodeData(node))
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
		"node":              nodeData(node),
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

func (s *Server) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token"))
		return
	}
	node, err := s.nodes.Heartbeat(r.Context(), token, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token"))
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"node": nodeData(node)}, nil)
}

type resolvePlayerRequest struct {
	Username string `json:"username"`
}

func (s *Server) requireNode(r *http.Request) (node.Node, *api.Error) {
	token, ok := bearerToken(r)
	if !ok {
		return node.Node{}, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token")
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
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player": playerData(player),
		"auth": map[string]any{
			"required": !player.Locked,
			"kind":     "offline_password",
			"locked":   player.Locked,
		},
	}, nil)
}

type authenticatePlayerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
	if player.Locked {
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.failure", nil)
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.success", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"player":        playerData(player),
	}, nil)
}

type createPortalLinkRequest struct {
	Username string `json:"username"`
	ServerID string `json:"server_id"`
	TTL      string `json:"ttl"`
}

func (s *Server) handleNodeCreatePortalLink(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token"))
		return
	}
	node, err := s.nodes.Authenticate(r.Context(), token)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token"))
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
	s.portalLinksMu.Lock()
	s.portalLinks[link.TokenHash] = link
	s.portalLinksMu.Unlock()
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

func nodeData(n node.Node) map[string]any {
	return map[string]any{
		"id":                n.ID,
		"name":              n.Name,
		"server_id":         "default",
		"server_label":      "Default Server",
		"status":            nodeStatus(n),
		"token_fingerprint": n.TokenFingerprint,
		"disabled":          n.Disabled,
		"created_at":        n.CreatedAt,
		"last_seen_at":      n.LastHeartbeatAt,
		"last_heartbeat_at": n.LastHeartbeatAt,
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
