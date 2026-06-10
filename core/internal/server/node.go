package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/extensions"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/RoselleMC/authman/core/internal/yggdrasil"
)

type createNodeRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ServerID string `json:"server_id"`
}

type updateNodeRequest struct {
	Name          string         `json:"name"`
	RuntimeConfig map[string]any `json:"runtime_config"`
}

type nodeHeartbeatRequest struct {
	Name                string `json:"name"`
	ServerID            string `json:"server_id"`
	Mode                string `json:"mode"`
	Kind                string `json:"kind"`
	InstanceFingerprint string `json:"instance_fingerprint"`
	PluginVersion       string `json:"plugin_version"`
	VelocityVersion     string `json:"velocity_version"`
}

type ackNodeActionsRequest struct {
	IDs []string `json:"ids"`
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
	s.createAdminNode(w, r, session.SubjectID, req)
}

func (s *Server) createAdminNode(w http.ResponseWriter, r *http.Request, adminID string, req createNodeRequest) {
	nodeKind := node.NormalizeKind(req.Kind)
	serverID := strings.TrimSpace(req.ServerID)
	if nodeKind == "downstream_velocity" {
		if serverID == "" {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "server.required", "downstream node requires a server id"))
			return
		}
		if _, err := s.store.GetDownstreamServer(r.Context(), serverID); err != nil {
			api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
			return
		}
	}
	created, token, err := s.nodes.CreateKindForServer(r.Context(), req.Name, nodeKind, serverID, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "node.create_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, adminID, audit.TargetNode, created.ID, "node.create", map[string]any{
		"name": created.Name,
		"kind": node.NormalizeKind(created.Mode),
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"node":              s.nodeData(r.Context(), created),
		"token":             token,
		"token_once":        token,
		"token_fingerprint": created.TokenFingerprint,
	}, nil)
}

func (s *Server) handleAdminListNodes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	s.writeAdminNodes(w, r, "")
}

func (s *Server) handleAdminCreateLimboPortalNode(w http.ResponseWriter, r *http.Request) {
	s.handleAdminCreateNodeWithKind(w, r, "limbo_portal")
}

func (s *Server) handleAdminCreateDownstreamNode(w http.ResponseWriter, r *http.Request) {
	s.handleAdminCreateNodeWithKind(w, r, "downstream_velocity")
}

func (s *Server) handleAdminListLimboPortalNodes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	s.writeAdminNodes(w, r, "limbo_portal")
}

func (s *Server) handleAdminListDownstreamNodes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	s.writeAdminNodes(w, r, "downstream_velocity")
}

func (s *Server) writeAdminNodes(w http.ResponseWriter, r *http.Request, kind string) {
	params := parseListPageParams(r)
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	status := strings.TrimSpace(q.Get("status"))
	sortKey := strings.TrimSpace(q.Get("sort"))
	sortDir := strings.TrimSpace(q.Get("dir"))
	nodes := s.nodes.List(r.Context())
	filtered := make([]node.Node, 0, len(nodes))
	kind = strings.TrimSpace(kind)
	for _, n := range nodes {
		nodeState := nodeStatus(n)
		if status == "" && n.Disabled {
			continue
		}
		if kind != "" && node.NormalizeKind(n.Mode) != kind {
			continue
		}
		if status != "" && nodeState != status {
			continue
		}
		if search != "" && !containsFold(n.ID, search) && !containsFold(n.Name, search) && !containsFold(n.PluginVersion, search) && !containsFold(n.VelocityVersion, search) {
			continue
		}
		filtered = append(filtered, n)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		cmp := 0
		switch sortKey {
		case "name":
			cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "status":
			cmp = strings.Compare(nodeStatus(a), nodeStatus(b))
		case "seen":
			cmp = compareTimePtr(a.LastHeartbeatAt, b.LastHeartbeatAt)
		case "created":
			cmp = compareTime(a.CreatedAt, b.CreatedAt)
		default:
			cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
		if sortDir == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, n := range filtered[start:end] {
		data = append(data, s.nodeData(r.Context(), n))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
}

func (s *Server) handleAdminCreateNodeWithKind(w http.ResponseWriter, r *http.Request, kind string) {
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
	if strings.TrimSpace(req.Kind) == "" {
		req.Kind = kind
	}
	s.createAdminNode(w, r, session.SubjectID, req)
}

func (s *Server) handleAdminGetNode(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	n, err := s.nodes.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, s.nodeData(r.Context(), n), nil)
}

func (s *Server) handleAdminUpdateNode(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var req updateNodeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	n, err := s.nodes.Update(r.Context(), id, req.Name, req.RuntimeConfig)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, n.ID, "node.update", map[string]any{
		"name": n.Name,
		"kind": node.NormalizeKind(n.Mode),
	})
	api.WriteJSON(w, http.StatusOK, s.nodeData(r.Context(), n), nil)
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
	if err := s.nodes.Delete(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, id, "node.revoke", map[string]any{
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
			Mode:                req.Mode,
			Kind:                req.Kind,
			InstanceFingerprint: req.InstanceFingerprint,
			AccessFingerprint:   auth.TokenFingerprint(token),
			PluginVersion:       req.PluginVersion,
			VelocityVersion:     req.VelocityVersion,
		}, time.Now())
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusForbidden, "node.revoked", err.Error()))
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"node":           s.nodeData(r.Context(), node),
			"runtime_config": s.nodeRuntimeConfig(r.Context(), node),
			"actions":        nodeActionRows(s.store.ListPendingNodeActions(r.Context(), node.ID, time.Now(), 50)),
		}, nil)
		return
	}
	node, err := s.nodes.Heartbeat(r.Context(), token, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token"))
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"node":           s.nodeData(r.Context(), node),
		"runtime_config": s.nodeRuntimeConfig(r.Context(), node),
		"actions":        nodeActionRows(s.store.ListPendingNodeActions(r.Context(), node.ID, time.Now(), 50)),
	}, nil)
}

func (s *Server) handleNodeAckActions(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req ackNodeActionsRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	acked := s.store.AckNodeActions(r.Context(), n.ID, req.IDs, time.Now())
	api.WriteJSON(w, http.StatusOK, map[string]any{"acked": acked}, nil)
}

type resolvePlayerRequest struct {
	Username          string                       `json:"username"`
	LoginMode         string                       `json:"login_mode"`
	AuthSource        string                       `json:"auth_source"`
	Verified          bool                         `json:"verified"`
	VerifiedUUID      string                       `json:"verified_uuid"`
	ProfileProperties []nodeProfilePropertyRequest `json:"profile_properties"`
}

type nodeProfilePropertyRequest struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature"`
}

type verifyLimboSessionRequest struct {
	Username        string `json:"username"`
	ServerID        string `json:"server_id"`
	RemoteIP        string `json:"remote_ip"`
	ProtocolVersion int    `json:"protocol_version"`
	RequestedHost   string `json:"requested_host"`
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

type endPresenceRequest struct {
	PresenceID string `json:"presence_id"`
	ProfileID  string `json:"profile_id"`
	ServerID   string `json:"server_id"`
	Reason     string `json:"reason"`
}

type nodeBanRequest struct {
	ProfileID        string `json:"profile_id"`
	PassportID       string `json:"passport_id"`
	Username         string `json:"username"`
	Reason           string `json:"reason"`
	ExpiresAt        string `json:"expires_at"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
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

func (s *Server) handleNodeVerifyLimboSession(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can verify premium sessions"))
		return
	}
	var req verifyLimboSessionRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.ServerID) == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "session.proof_required", "username and server_id are required"))
		return
	}
	if s.mojangVerifier == nil {
		api.WriteError(w, api.NewError(http.StatusServiceUnavailable, "mojang.verifier_unavailable", "Mojang verifier is not configured"))
		return
	}
	profile, err := s.mojangVerifier.HasJoined(r.Context(), yggdrasil.HasJoinedRequest{
		Username: strings.TrimSpace(req.Username),
		ServerID: strings.TrimSpace(req.ServerID),
		IP:       strings.TrimSpace(req.RemoteIP),
	})
	if err != nil {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "limbo.session.verify_failure", map[string]any{
			"username":        req.Username,
			"server_id":       req.ServerID,
			"remote_ip":       req.RemoteIP,
			"requested_host":  req.RequestedHost,
			"protocol":        req.ProtocolVersion,
			"reason":          err.Error(),
			"verification_by": "mojang",
		})
		if errors.Is(err, mojang.ErrAllRoutesFailed) || !errors.Is(err, yggdrasil.ErrProfileNotFound) {
			if player, ok := s.resolveOfflineAfterMojangFailure(r.Context(), req.Username); ok {
				s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "limbo.session.verify_offline_fallback", playerEventDetails(player, map[string]any{
					"username":        req.Username,
					"server_id":       req.ServerID,
					"remote_ip":       req.RemoteIP,
					"requested_host":  req.RequestedHost,
					"protocol":        req.ProtocolVersion,
					"reason":          err.Error(),
					"verification_by": "mojang",
				}))
				api.WriteError(w, api.NewError(http.StatusUnauthorized, "session.verify_failed", "Mojang verifier is unavailable; registered offline passport may authenticate with password"))
				return
			}
			api.WriteError(w, api.NewError(http.StatusServiceUnavailable, "mojang.verifier_unavailable", err.Error()))
			return
		}
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "session.verify_failed", err.Error()))
		return
	}
	pp, ok := s.persistPremiumProfile(r.Context(), profile)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "session.persist_failed", "failed to persist premium profile"))
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, pp.Profile.ID, "limbo.session.verify_success", map[string]any{
		"passport_id":     pp.Passport.ID,
		"profile_id":      pp.Profile.ID,
		"username":        profile.Name,
		"uuid":            pp.Passport.UUID.String(),
		"requested_host":  req.RequestedHost,
		"protocol":        req.ProtocolVersion,
		"verification_by": "mojang",
	})
	player := identity.PlayerFromPassportProfileLink(pp)
	player.ProfileProperties = s.effectiveProfileProperties(r.Context(), pp.Profile, &pp.Passport)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"profile": map[string]any{
			"uuid":       pp.Passport.UUID.String(),
			"name":       profile.Name,
			"properties": profilePropertiesData(player.ProfileProperties),
			"source":     "mojang",
			"verified":   true,
		},
		"player": playerData(player),
	}, nil)
}

func (s *Server) resolveOfflineAfterMojangFailure(ctx context.Context, username string) (identity.Player, bool) {
	player, err := s.ResolveOffline(ctx, strings.TrimSpace(username))
	if err != nil || player.Kind != identity.PlayerKindOffline {
		return identity.Player{}, false
	}
	return player, true
}

func (s *Server) handleNodeResolvePlayer(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req resolvePlayerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, apiErr := s.resolveNodePlayer(r.Context(), n, req)
	if apiErr != nil {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "player.resolve_failure", map[string]any{
			"reason":   apiErr.Message,
			"username": strings.TrimSpace(req.Username),
			"kind":     node.NormalizeKind(n.Mode),
		})
		api.WriteError(w, apiErr)
		return
	}
	player.ProfileProperties = s.effectiveProfilePropertiesByID(r.Context(), player.ID)
	authRequired := player.Kind == identity.PlayerKindOffline && !player.Locked
	authKind := "premium"
	if player.Kind == identity.PlayerKindOffline {
		authKind = "offline_password"
	}
	authUsername := player.ProtocolName
	if passport, err := s.store.GetPassportForProfile(r.Context(), player.ID); err == nil && passport.Username != "" {
		authUsername = passport.Username
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "player.resolve", playerEventDetails(player, map[string]any{
		"requested_username": strings.TrimSpace(req.Username),
		"auth_required":      authRequired,
		"auth_kind":          authKind,
		"auth_username":      authUsername,
		"kind":               node.NormalizeKind(n.Mode),
	}))
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player": playerData(player),
		"auth": map[string]any{
			"required": authRequired,
			"kind":     authKind,
			"locked":   player.Locked,
			"username": authUsername,
		},
	}, nil)
}

func (s *Server) resolveNodePlayer(ctx context.Context, n node.Node, req resolvePlayerRequest) (identity.Player, *api.Error) {
	if req.Verified {
		if !node.IsLimboPortal(n.Mode) {
			return identity.Player{}, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can submit verified session profiles")
		}
		if strings.TrimSpace(req.LoginMode) != "online" {
			return identity.Player{}, api.NewError(http.StatusBadRequest, "session.login_mode_invalid", "verified session must use online login mode")
		}
		uuid, err := identity.ParseUUID(req.VerifiedUUID)
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusBadRequest, "session.uuid_invalid", "verified UUID is invalid")
		}
		pp, err := s.store.UpsertPremiumPassportProfile(ctx, strings.TrimSpace(req.Username), uuid, nodeProfileProperties(req.ProfileProperties))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusInternalServerError, "premium.upsert_failed", "failed to persist premium profile")
		}
		player := identity.PlayerFromPassportProfileLink(pp)
		player.ProfileProperties = s.effectiveProfileProperties(ctx, pp.Profile, &pp.Passport)
		return player, nil
	}
	player, err := s.store.GetOfflinePlayer(ctx, normalizeNodeUsername(req.Username))
	if err != nil {
		player, err = s.store.GetPlayerByProtocolName(ctx, strings.TrimSpace(req.Username))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
	}
	return player, nil
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
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "offline.password.failure", map[string]any{
			"reason":   "credential_not_found",
			"username": strings.TrimSpace(req.Username),
		})
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	if player.Locked || credentialLocked(credential, time.Now()) {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.rejected", playerEventDetails(player, map[string]any{
			"reason":       "account_locked",
			"locked_until": credential.LockedUntil,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		updatedCredential, _ := s.store.RecordOfflineLoginFailure(r.Context(), player.ID, time.Now())
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.failure", playerEventDetails(player, map[string]any{
			"reason":          "password_mismatch",
			"failed_attempts": updatedCredential.FailedAttempts,
			"locked_until":    updatedCredential.LockedUntil,
		}))
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	_ = s.store.RecordOfflineLoginSuccess(r.Context(), player.ID)
	s.recordPlayerSeen(r, player, n.ServerID, time.Now())
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "offline.password.success", playerEventDetails(player, map[string]any{
		"server_id": n.ServerID,
	}))
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
		s.audit(r, audit.ActorNode, n.ID, audit.TargetDownstreamServer, strings.TrimSpace(req.ServerID), "portal.target.resolve_failure", map[string]any{
			"slug":           req.Slug,
			"requested_host": req.RequestedHost,
			"reason":         apiErr.Message,
		})
		api.WriteError(w, apiErr)
		return
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "portal.target.resolve", map[string]any{
		"slug":           server.Slug,
		"requested_host": req.RequestedHost,
		"target_host":    target.TransferHost,
		"target_port":    target.TransferPort,
	})
	data := map[string]any{
		"server": downstreamServerData(server),
		"target": store.DownstreamTargetData(target),
	}
	if blueprintID := strings.TrimSpace(stringFromAnyServer(server.PortalConfig["limbo_blueprint_id"])); blueprintID != "" {
		if blueprint, err := s.store.GetLimboBlueprint(r.Context(), blueprintID); err == nil {
			data["limbo_blueprint"] = limboBlueprintData(blueprint, false)
		} else {
			data["limbo_blueprint"] = map[string]any{"id": blueprintID, "missing": true}
		}
	}
	api.WriteJSON(w, http.StatusOK, data, nil)
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
		s.audit(r, audit.ActorNode, n.ID, audit.TargetDownstreamServer, strings.TrimSpace(req.ServerID), "transfer_grant.reject", map[string]any{
			"reason":         apiErr.Message,
			"slug":           req.Slug,
			"requested_host": req.RequestedHost,
		})
		api.WriteError(w, apiErr)
		return
	}
	if target.Status != "active" {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "transfer_grant.reject", map[string]any{
			"reason": "server_unavailable",
			"status": target.Status,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "server.unavailable", "downstream server is not active"))
		return
	}
	player, apiErr := s.playerFromTransferGrantRequest(r.Context(), req)
	if apiErr != nil {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.PlayerID+req.Username), "transfer_grant.reject", map[string]any{
			"reason":    apiErr.Message,
			"player_id": req.PlayerID,
			"username":  req.Username,
			"server_id": server.ID,
		})
		api.WriteError(w, apiErr)
		return
	}
	if player.Locked {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.reject", playerEventDetails(player, map[string]any{
			"reason":    "account_locked",
			"server_id": server.ID,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	if ban, banned := s.activeBanForPlayer(r.Context(), player); banned {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.reject", playerEventDetails(player, map[string]any{
			"reason":     "banned",
			"server_id":  server.ID,
			"ban_id":     ban.ID,
			"ban_scope":  ban.Scope,
			"expires_at": ban.ExpiresAt,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.banned", ban.Reason))
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
	s.recordPlayerSeen(r, player, server.ID, now)
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
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.id_required", "server id is required"))
		return
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
	if player.Locked {
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	if ban, banned := s.activeBanForPlayer(r.Context(), player); banned {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.reject", playerEventDetails(player, map[string]any{
			"reason":     "banned",
			"server_id":  server.ID,
			"ban_id":     ban.ID,
			"ban_scope":  ban.Scope,
			"expires_at": ban.ExpiresAt,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.banned", ban.Reason))
		return
	}
	passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_bound", "profile is not bound to a passport"))
		return
	}
	now := time.Now()
	presence, err := s.store.UpsertPresence(r.Context(), store.PlayerPresence{
		PassportID:   passport.ID,
		ProfileID:    player.ID,
		ServerID:     server.ID,
		NodeID:       n.ID,
		ProtocolName: protocolName,
		UUID:         uuid,
		RemoteAddr:   clientIP(r),
		ConnectedAt:  now,
		LastSeenAt:   now,
	})
	if err != nil {
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "presence.reject", playerEventDetails(player, map[string]any{
			"reason":    "profile_already_online_on_server",
			"server_id": server.ID,
			"node_id":   n.ID,
		}))
		api.WriteError(w, api.NewError(http.StatusConflict, "presence.profile_already_online", "profile is already online on this server"))
		return
	}
	s.recordPlayerSeen(r, player, server.ID, now)
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.consume", map[string]any{
		"server_id":     server.ID,
		"protocol_name": protocolName,
		"uuid":          uuid,
		"source":        req.Source,
		"portal_source": grant.PortalSource,
		"presence_id":   presence.ID,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed":  true,
		"grant":    transferGrantData(grant),
		"player":   playerData(player),
		"presence": presenceRows([]store.PlayerPresence{presence})[0],
		"target":   store.DownstreamTargetData(target),
	}, nil)
}

func (s *Server) handleNodeEndPresence(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req endPresenceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "disconnect"
	}
	if strings.TrimSpace(req.PresenceID) != "" {
		presence, err := s.store.EndPresence(r.Context(), req.PresenceID, reason, time.Now())
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusNotFound, "presence.not_found", "presence not found"))
			return
		}
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, presence.ProfileID, "presence.end", map[string]any{
			"presence_id": presence.ID,
			"passport_id": presence.PassportID,
			"profile_id":  presence.ProfileID,
			"server_id":   presence.ServerID,
			"node_id":     presence.NodeID,
			"reason":      reason,
		})
		api.WriteJSON(w, http.StatusOK, presenceRows([]store.PlayerPresence{presence})[0], nil)
		return
	}
	profileID := strings.TrimSpace(req.ProfileID)
	serverID := strings.TrimSpace(req.ServerID)
	if profileID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "presence.profile_required", "profile id is required"))
		return
	}
	ended := 0
	for _, presence := range s.store.ListProfilePresences(r.Context(), profileID) {
		if serverID != "" && presence.ServerID != serverID {
			continue
		}
		if _, err := s.store.EndPresence(r.Context(), presence.ID, reason, time.Now()); err == nil {
			ended++
		}
	}
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, profileID, "presence.end", map[string]any{
		"profile_id":      profileID,
		"server_id":       serverID,
		"reason":          reason,
		"ended_presences": ended,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ended_presences": ended}, nil)
}

func (s *Server) handleNodeCreateProfileBan(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req nodeBanRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, apiErr := s.playerFromNodeBanRequest(r.Context(), req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, apiErr := banFromNodeRequest(req, store.BanScopeProfile, player.ID, n.ID)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, err := s.store.CreateBan(r.Context(), ban)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ban.create_failed", "failed to create ban"))
		return
	}
	now := time.Now()
	presences := s.store.ListProfilePresences(r.Context(), player.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, "profile banned: "+ban.Reason, now)
	ended := s.store.EndProfilePresences(r.Context(), player.ID, "profile banned", now)
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "profile.ban.create", playerEventDetails(player, map[string]any{
		"ban_id":          ban.ID,
		"reason":          ban.Reason,
		"expires_at":      ban.ExpiresAt,
		"ended_presences": ended,
		"queued_actions":  queued,
		"server_id":       n.ServerID,
	}))
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"ban":             banRows([]store.PlayerBan{ban})[0],
		"ended_presences": ended,
		"queued_actions":  queued,
	}, nil)
}

func (s *Server) handleNodeCreatePassportBan(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	var req nodeBanRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	passport, apiErr := s.passportFromNodeBanRequest(r.Context(), req)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, apiErr := banFromNodeRequest(req, store.BanScopePassport, passport.ID, n.ID)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	ban, err := s.store.CreateBan(r.Context(), ban)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "ban.create_failed", "failed to create ban"))
		return
	}
	now := time.Now()
	presences := s.store.ListPassportPresences(r.Context(), passport.ID)
	queued := s.enqueueDisconnectActions(r.Context(), presences, "passport banned: "+ban.Reason, now)
	ended := s.store.EndPassportPresences(r.Context(), passport.ID, "passport banned", now)
	s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "passport.ban.create", map[string]any{
		"ban_id":          ban.ID,
		"reason":          ban.Reason,
		"expires_at":      ban.ExpiresAt,
		"ended_presences": ended,
		"queued_actions":  queued,
		"server_id":       n.ServerID,
		"username":        passport.Username,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"ban":             banRows([]store.PlayerBan{ban})[0],
		"ended_presences": ended,
		"queued_actions":  queued,
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
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.id_required", "server id is required"))
		return
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

func (s *Server) activeBanForPlayer(ctx context.Context, player identity.Player) (store.PlayerBan, bool) {
	passport, err := s.store.GetPassportForProfile(ctx, player.ID)
	if err != nil {
		return store.PlayerBan{}, false
	}
	return s.store.ActiveBanFor(ctx, passport.ID, player.ID, time.Now())
}

func (s *Server) playerFromNodeBanRequest(ctx context.Context, req nodeBanRequest) (identity.Player, *api.Error) {
	if strings.TrimSpace(req.ProfileID) != "" {
		player, err := s.store.GetPlayerByID(ctx, strings.TrimSpace(req.ProfileID))
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
		return player, nil
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return identity.Player{}, api.NewError(http.StatusBadRequest, "player.required", "player is required")
	}
	player, err := s.store.GetOfflinePlayer(ctx, normalizeNodeUsername(username))
	if err != nil {
		player, err = s.store.GetPlayerByProtocolName(ctx, username)
		if err != nil {
			return identity.Player{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
	}
	return player, nil
}

func (s *Server) passportFromNodeBanRequest(ctx context.Context, req nodeBanRequest) (identity.Passport, *api.Error) {
	if strings.TrimSpace(req.PassportID) != "" {
		passport, err := s.store.GetPassportByID(ctx, strings.TrimSpace(req.PassportID))
		if err != nil {
			return identity.Passport{}, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found")
		}
		return passport, nil
	}
	player, apiErr := s.playerFromNodeBanRequest(ctx, req)
	if apiErr != nil {
		return identity.Passport{}, apiErr
	}
	passport, err := s.store.GetPassportForProfile(ctx, player.ID)
	if err != nil {
		return identity.Passport{}, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found")
	}
	return passport, nil
}

func banFromNodeRequest(req nodeBanRequest, scope store.BanScope, targetID string, createdBy string) (store.PlayerBan, *api.Error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return store.PlayerBan{}, api.NewError(http.StatusBadRequest, "ban.reason_required", "ban reason is required")
	}
	if len(reason) > 500 {
		return store.PlayerBan{}, api.NewError(http.StatusBadRequest, "ban.reason_too_long", "ban reason is too long")
	}
	expiresAt, apiErr := parseBanExpiry(banRequest{
		ExpiresAt:        req.ExpiresAt,
		ExpiresInSeconds: req.ExpiresInSeconds,
	})
	if apiErr != nil {
		return store.PlayerBan{}, apiErr
	}
	return store.PlayerBan{
		Scope:     scope,
		TargetID:  targetID,
		Reason:    reason,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
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
		if !node.IsLimboPortal(n.Mode) {
			candidate = strings.TrimSpace(n.ServerID)
		}
	}
	if candidate == "" {
		settings := s.portalSettings(ctx)
		if settings.FallbackServerID == "" {
			return store.DownstreamServer{}, store.DownstreamTarget{}, api.NewError(http.StatusNotFound, "portal.target_unmatched", "no downstream server matched the requested host")
		}
		candidate = settings.FallbackServerID
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

func stringFromAnyServer(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func boolFromAnyServer(value any, fallback bool) bool {
	if flag, ok := value.(bool); ok {
		return flag
	}
	if text, ok := value.(string); ok {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
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
		s.audit(r, audit.ActorNode, node.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "portal_link.create_failure", map[string]any{
			"reason":    "player_not_found",
			"username":  strings.TrimSpace(req.Username),
			"server_id": req.ServerID,
		})
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
	if err != nil {
		s.audit(r, audit.ActorNode, node.ID, audit.TargetPlayer, player.ID, "portal_link.create_failure", playerEventDetails(player, map[string]any{
			"reason":    "passport_not_found",
			"server_id": req.ServerID,
		}))
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	ttl := 10 * time.Minute
	if req.TTL != "" {
		if parsed, err := time.ParseDuration(req.TTL); err == nil && parsed > 0 && parsed <= time.Hour {
			ttl = parsed
		}
	}
	link, rawToken, err := auth.NewPortalLink(auth.PortalLinkOffline, passport.ID, req.ServerID, ttl, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to create portal link"))
		return
	}
	if err := s.store.SavePortalLink(r.Context(), link); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to save portal link"))
		return
	}
	s.audit(r, audit.ActorNode, node.ID, audit.TargetPlayer, passport.ID, "portal_link.create", map[string]any{
		"server_id":     req.ServerID,
		"expires_at":    link.ExpiresAt,
		"profile_id":    player.ID,
		"protocol_name": player.ProtocolName,
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
	serverLabel := serverID
	if server, err := s.store.GetDownstreamServer(ctx, serverID); err == nil {
		serverLabel = server.DisplayName
	}
	return map[string]any{
		"id":                   n.ID,
		"name":                 n.Name,
		"kind":                 node.NormalizeKind(n.Mode),
		"mode":                 node.NormalizeKind(n.Mode),
		"server_id":            serverID,
		"server_label":         serverLabel,
		"runtime_config":       s.nodeRuntimeConfig(ctx, n),
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

func (s *Server) nodeRuntimeConfig(ctx context.Context, n node.Node) map[string]any {
	kind := node.NormalizeKind(n.Mode)
	base := map[string]any{
		"node_name":                    strings.TrimSpace(n.Name),
		"server_id":                    strings.TrimSpace(n.ServerID),
		"heartbeat_interval_seconds":   60,
		"resolve_raw_offline_names":    true,
		"max_password_attempts":        3,
		"chat_cooldown_millis":         150,
		"auth_timeout_seconds":         90,
		"completion_delay_seconds":     3,
		"transfer_cookie_key":          "authman:transfer_grant",
		"dialog_enabled":               true,
		"dialog_fallback_chat_enabled": true,
		"email_verification_mode":      "disabled",
	}
	if kind == "downstream_velocity" {
		base["downstream_initial_server"] = ""
		base["downstream_holding_server"] = ""
		base["downstream_validation_timeout_seconds"] = 10
		base["gate_initial_server"] = ""
		base["gate_holding_server"] = ""
		base["gate_validation_timeout_seconds"] = 10
		mergeRuntimeConfig(base, n.RuntimeConfig, []string{
			"node_name",
			"server_id",
			"heartbeat_interval_seconds",
			"resolve_raw_offline_names",
			"transfer_cookie_key",
			"downstream_initial_server",
			"downstream_holding_server",
			"downstream_validation_timeout_seconds",
			"gate_initial_server",
			"gate_holding_server",
			"gate_validation_timeout_seconds",
		})
		if base["downstream_initial_server"] == "" {
			base["downstream_initial_server"] = base["gate_initial_server"]
		}
		if base["downstream_holding_server"] == "" {
			base["downstream_holding_server"] = base["gate_holding_server"]
		}
		if base["downstream_validation_timeout_seconds"] == 10 {
			base["downstream_validation_timeout_seconds"] = base["gate_validation_timeout_seconds"]
		}
		return base
	}
	settings := s.portalSettings(ctx)
	if settings.TransferCookieKey != "" {
		base["transfer_cookie_key"] = settings.TransferCookieKey
	}
	base["dialog_enabled"] = settings.DialogEnabled
	base["dialog_fallback_chat_enabled"] = settings.DialogFallbackChat
	return base
}

func mergeRuntimeConfig(base map[string]any, overrides map[string]any, allowed []string) {
	if len(overrides) == 0 {
		return
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, value := range overrides {
		if _, ok := allowedSet[key]; !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			base[key] = strings.TrimSpace(v)
		case bool:
			base[key] = v
		case float64, float32, int, int32, int64, json.Number:
			base[key] = v
		}
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
