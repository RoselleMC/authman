package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"io"
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
	Name                string                      `json:"name"`
	ServerID            string                      `json:"server_id"`
	Mode                string                      `json:"mode"`
	Kind                string                      `json:"kind"`
	InstanceFingerprint string                      `json:"instance_fingerprint"`
	PluginVersion       string                      `json:"plugin_version"`
	VelocityVersion     string                      `json:"velocity_version"`
	Status              *nodeDownstreamStatusReport `json:"status"`
	ProtocolPack        *nodeProtocolPackReport     `json:"protocol_pack"`
}

type nodeDownstreamStatusReport struct {
	OnlinePlayers int `json:"online_players"`
	MaxPlayers    int `json:"max_players"`
}

type nodeProtocolPackReport struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	SHA256            string   `json:"sha256"`
	Protocols         []int32  `json:"protocols"`
	MinecraftVersions []string `json:"minecraft_versions"`
	LastError         string   `json:"last_error"`
}

type ackNodeActionsRequest struct {
	IDs     []string              `json:"ids"`
	Results []nodeActionAckResult `json:"results"`
}

type nodeActionAckResult struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	PresenceID   string `json:"presence_id"`
	PassportID   string `json:"passport_id"`
	ProfileID    string `json:"profile_id"`
	UUID         string `json:"uuid"`
	ProtocolName string `json:"protocol_name"`
	Online       *bool  `json:"online"`
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
	s.pushNodeSync(r.Context(), n, "node.update")
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
	s.pushNodeSync(r.Context(), node, "node.rotate_token")
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"node":              s.nodeData(r.Context(), node),
		"token":             token,
		"token_once":        token,
		"token_fingerprint": node.TokenFingerprint,
	}, nil)
}

func (s *Server) handleAdminDisableNode(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdminPermission(r, true, "nodes.write")
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	target, err := s.nodes.Get(r.Context(), id)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	if err := s.nodes.Delete(r.Context(), id); err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "node.not_found", "node not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetNode, id, "node.disable", map[string]any{
		"name":                 target.Name,
		"instance_fingerprint": target.InstanceFingerprint,
	})
	s.pushNodeRevoked(id)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
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
	s.pushNodeRevoked(id)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

func (s *Server) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "missing node token"))
		return
	}
	req, hasBody, decodeErr := decodeNodeHeartbeatRequest(r)
	if decodeErr != nil {
		api.WriteError(w, decodeErr)
		return
	}
	now := time.Now()
	if s.cfg.NodeAccessToken != "" && auth.ConstantTimeTokenEqual("node-access", token, auth.HashToken("node-access", s.cfg.NodeAccessToken)) {
		if !hasBody {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "system.invalid_json", "invalid JSON request body"))
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
		}, now)
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusForbidden, "node.revoked", err.Error()))
			return
		}
		s.recordNodeDownstreamStatus(r.Context(), node, req.Status, now)
		s.recordLimboProtocolStatus(r.Context(), node, req.ProtocolPack, now)
		api.WriteJSON(w, http.StatusOK, s.nodeSyncPayload(r.Context(), node), nil)
		return
	}
	node, err := s.nodes.Heartbeat(r.Context(), token, now)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "node.unauthorized", "invalid node token"))
		return
	}
	if hasBody {
		s.recordNodeDownstreamStatus(r.Context(), node, req.Status, now)
		s.recordLimboProtocolStatus(r.Context(), node, req.ProtocolPack, now)
	}
	api.WriteJSON(w, http.StatusOK, s.nodeSyncPayload(r.Context(), node), nil)
}

func decodeNodeHeartbeatRequest(r *http.Request) (nodeHeartbeatRequest, bool, *api.Error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nodeHeartbeatRequest{}, false, api.NewError(http.StatusBadRequest, "system.invalid_json", "invalid JSON request body")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nodeHeartbeatRequest{}, false, nil
	}
	var req nodeHeartbeatRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nodeHeartbeatRequest{}, false, api.NewError(http.StatusBadRequest, "system.invalid_json", "invalid JSON request body")
	}
	return req, true, nil
}

func (s *Server) recordNodeDownstreamStatus(ctx context.Context, n node.Node, report *nodeDownstreamStatusReport, now time.Time) {
	if report == nil || !node.IsDownstreamVelocity(n.Mode) {
		return
	}
	serverID := strings.TrimSpace(n.ServerID)
	if serverID == "" {
		return
	}
	status, err := s.store.UpsertDownstreamServerStatus(ctx, store.DownstreamServerStatus{
		ServerID:      serverID,
		NodeID:        n.ID,
		OnlinePlayers: clampNonNegative(report.OnlinePlayers),
		MaxPlayers:    clampNonNegative(report.MaxPlayers),
		Source:        "heartbeat",
		ReportedAt:    now.UTC(),
	})
	if err != nil {
		s.logger.Warn("failed to record downstream server status", "node_id", n.ID, "server_id", serverID, "err", err)
		return
	}
	s.logger.Debug("recorded downstream server status", "node_id", n.ID, "server_id", serverID, "online_players", status.OnlinePlayers, "max_players", status.MaxPlayers)
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
	now := time.Now()
	s.processNodeActionAckResults(r, n, req.Results, now)
	ids := append([]string(nil), req.IDs...)
	for _, result := range req.Results {
		if trimmed := strings.TrimSpace(result.ID); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	acked := s.store.AckNodeActions(r.Context(), n.ID, ids, now)
	s.pushNodeSync(r.Context(), n, "node_action.ack")
	api.WriteJSON(w, http.StatusOK, map[string]any{"acked": acked}, nil)
}

func (s *Server) processNodeActionAckResults(r *http.Request, n node.Node, results []nodeActionAckResult, now time.Time) {
	for _, result := range results {
		if !strings.EqualFold(strings.TrimSpace(result.Type), string(store.NodeActionPresenceCheck)) {
			continue
		}
		if result.Online == nil || *result.Online {
			s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(result.ProfileID), "presence.check", map[string]any{
				"action_id":     strings.TrimSpace(result.ID),
				"presence_id":   strings.TrimSpace(result.PresenceID),
				"passport_id":   strings.TrimSpace(result.PassportID),
				"profile_id":    strings.TrimSpace(result.ProfileID),
				"uuid":          strings.TrimSpace(result.UUID),
				"protocol_name": strings.TrimSpace(result.ProtocolName),
				"online":        result.Online != nil && *result.Online,
			})
			continue
		}
		presenceID := strings.TrimSpace(result.PresenceID)
		if presenceID == "" {
			continue
		}
		presence, err := s.store.EndPresence(r.Context(), presenceID, "presence_check_absent", now)
		if err != nil {
			s.logger.Warn("failed to end stale presence after node check", "node_id", n.ID, "action_id", result.ID, "presence_id", presenceID, "err", err)
			continue
		}
		s.audit(r, audit.ActorNode, n.ID, audit.TargetPlayer, presence.ProfileID, "presence.reconcile", map[string]any{
			"action_id":     strings.TrimSpace(result.ID),
			"presence_id":   presence.ID,
			"passport_id":   presence.PassportID,
			"profile_id":    presence.ProfileID,
			"server_id":     presence.ServerID,
			"node_id":       presence.NodeID,
			"uuid":          presence.UUID,
			"protocol_name": presence.ProtocolName,
			"reason":        "presence_check_absent",
		})
	}
}

type resolvePlayerRequest struct {
	Username          string                       `json:"username"`
	LoginMode         string                       `json:"login_mode"`
	AuthSource        string                       `json:"auth_source"`
	Verified          bool                         `json:"verified"`
	VerifiedUUID      string                       `json:"verified_uuid"`
	RemoteIP          string                       `json:"remote_ip"`
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

type limboLoginPolicyRequest struct {
	Username        string `json:"username"`
	ClaimedUUID     string `json:"claimed_uuid"`
	RemoteIP        string `json:"remote_ip"`
	ProtocolVersion int    `json:"protocol_version"`
	RequestedHost   string `json:"requested_host"`
}

type resolvePortalTargetRequest struct {
	ServerID        string `json:"server_id"`
	Slug            string `json:"slug"`
	RequestedHost   string `json:"requested_host"`
	RemoteIP        string `json:"remote_ip"`
	ProtocolVersion int    `json:"protocol_version"`
}

type createTransferGrantRequest struct {
	PlayerID        string `json:"player_id"`
	Username        string `json:"username"`
	ServerID        string `json:"server_id"`
	Slug            string `json:"slug"`
	RequestedHost   string `json:"requested_host"`
	Source          string `json:"source"`
	RemoteIP        string `json:"remote_ip"`
	TTLSeconds      int    `json:"ttl_seconds"`
	ProtocolVersion int    `json:"protocol_version"`
}

type consumeTransferGrantRequest struct {
	Token        string `json:"token"`
	TokenHash    string `json:"token_hash"`
	ServerID     string `json:"server_id"`
	UUID         string `json:"uuid"`
	ProtocolName string `json:"protocol_name"`
	Source       string `json:"source"`
	RemoteIP     string `json:"remote_ip"`
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
	username := strings.TrimSpace(req.Username)
	serverID := strings.TrimSpace(req.ServerID)
	profile, err := s.verifyLimboMojangSession(r.Context(), username, serverID)
	if err != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "limbo.session.verify_failure", map[string]any{
			"username":        req.Username,
			"server_id":       req.ServerID,
			"remote_ip":       req.RemoteIP,
			"requested_host":  req.RequestedHost,
			"protocol":        req.ProtocolVersion,
			"reason":          err.Error(),
			"verification_by": "mojang",
		})
		if errors.Is(err, mojang.ErrAllRoutesFailed) || !errors.Is(err, yggdrasil.ErrProfileNotFound) {
			if passport, ok := s.resolveOfflineAfterMojangFailure(r.Context(), req.Username); ok {
				s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "limbo.session.verify_offline_fallback", (map[string]any{
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
	passportPreexisting := false
	if uuid, parseErr := identity.ParseUUID(profile.ID); parseErr == nil {
		if existing, existingErr := s.store.GetPassportByID(r.Context(), uuid.String()); existingErr == nil && existing.Kind == identity.PassportKindPremium {
			passportPreexisting = true
		}
	}
	passport, ok := s.persistPremiumPassport(r.Context(), profile)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "session.persist_failed", "failed to persist premium passport"))
		return
	}
	_ = s.store.SetPassportPremiumTextures(r.Context(), passport.ID, profilePropertiesToIdentity(profile.Properties))
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "limbo.session.verify_success", map[string]any{
		"passport_id":          passport.ID,
		"username":             profile.Name,
		"uuid":                 passport.UUID.String(),
		"remote_ip":            req.RemoteIP,
		"requested_host":       req.RequestedHost,
		"protocol":             req.ProtocolVersion,
		"verification_by":      "mojang",
		"passport_preexisting": passportPreexisting,
	})
	properties := make([]identity.ProfileProperty, 0, len(profile.Properties))
	for _, property := range profile.Properties {
		properties = append(properties, identity.ProfileProperty{Name: property.Name, Value: property.Value, Signature: property.Signature})
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"profile": map[string]any{
			"uuid":                 passport.UUID.String(),
			"name":                 profile.Name,
			"properties":           profilePropertiesData(properties),
			"source":               "mojang",
			"verified":             true,
			"passport_id":          passport.ID,
			"passport_preexisting": passportPreexisting,
		},
	}, nil)
}

func (s *Server) verifyLimboMojangSession(ctx context.Context, username string, serverID string) (yggdrasil.Profile, error) {
	return s.mojangVerifier.HasJoined(ctx, yggdrasil.HasJoinedRequest{
		Username: username,
		ServerID: serverID,
	})
}

func (s *Server) handleNodeResolveLimboLoginPolicy(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can resolve login policy"))
		return
	}
	var req limboLoginPolicyRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "session.username_required", "username is required"))
		return
	}
	writePolicy := func(mode string, reason string, premium *yggdrasil.Profile) {
		details := map[string]any{
			"username":         username,
			"claimed_uuid":     strings.TrimSpace(req.ClaimedUUID),
			"login_mode":       mode,
			"reason":           reason,
			"remote_ip":        req.RemoteIP,
			"requested_host":   req.RequestedHost,
			"protocol_version": req.ProtocolVersion,
		}
		if premium != nil {
			details["premium_uuid"] = dashedProfileUUID(premium.ID)
			details["premium_name"] = premium.Name
			details["claimed_uuid_matches_mojang"] = sameProfileUUID(req.ClaimedUUID, premium.ID)
		}
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, username, "limbo.login_policy", details)
		data := map[string]any{
			"login_mode": mode,
			"reason":     reason,
		}
		if premium != nil {
			data["premium_profile"] = map[string]any{
				"uuid":   dashedProfileUUID(premium.ID),
				"id":     strings.TrimSpace(premium.ID),
				"name":   premium.Name,
				"source": "mojang",
			}
		}
		api.WriteJSON(w, http.StatusOK, data, nil)
	}
	if s.mojangVerifier == nil {
		if _, ok := s.resolveOfflineAfterMojangFailure(r.Context(), username); ok {
			writePolicy("offline", "registered_offline_during_mojang_outage", nil)
			return
		}
		api.WriteError(w, api.NewError(http.StatusServiceUnavailable, "mojang.verifier_unavailable", "Mojang verifier is not configured"))
		return
	}
	profile, err := s.mojangVerifier.LookupProfileByName(r.Context(), username)
	if err == nil {
		if sameProfileUUID(req.ClaimedUUID, profile.ID) {
			writePolicy("hybrid", "premium_claim_uuid_match", &profile)
			return
		}
		writePolicy("offline", "premium_claim_uuid_mismatch", &profile)
		return
	}
	if errors.Is(err, yggdrasil.ErrProfileNotFound) {
		writePolicy("offline", "premium_profile_not_found", nil)
		return
	}
	if _, ok := s.resolveOfflineAfterMojangFailure(r.Context(), username); ok {
		writePolicy("offline", "registered_offline_during_mojang_outage", nil)
		return
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, username, "limbo.login_policy_failure", map[string]any{
		"username":         username,
		"claimed_uuid":     strings.TrimSpace(req.ClaimedUUID),
		"remote_ip":        req.RemoteIP,
		"requested_host":   req.RequestedHost,
		"protocol_version": req.ProtocolVersion,
		"reason":           err.Error(),
	})
	api.WriteError(w, api.NewError(http.StatusServiceUnavailable, "mojang.verifier_unavailable", err.Error()))
}

func (s *Server) resolveOfflineAfterMojangFailure(ctx context.Context, username string) (identity.Passport, bool) {
	passport, err := s.store.GetPassportByUsername(ctx, strings.TrimSpace(username))
	if err != nil || passport.Kind != identity.PassportKindOffline {
		return identity.Passport{}, false
	}
	return passport, true
}

func sameProfileUUID(left string, right string) bool {
	leftUUID, leftErr := identity.ParseUUID(left)
	rightUUID, rightErr := identity.ParseUUID(right)
	return leftErr == nil && rightErr == nil && leftUUID == rightUUID
}

func vanillaOfflineLoginUUID(username string) identity.UUID {
	sum := md5.Sum([]byte("OfflinePlayer:" + username))
	var out identity.UUID
	copy(out[:], sum[:])
	out[6] = (out[6] & 0x0f) | 0x30
	out[8] = (out[8] & 0x3f) | 0x80
	return out
}

func dashedProfileUUID(value string) string {
	uuid, err := identity.ParseUUID(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return uuid.String()
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
	if player, passport, grant, ok := s.resolvePendingGrantPlayer(r.Context(), n, req); ok {
		data := s.nodePassportResolveDataWithPlayer(r.Context(), passport, player, true, req.Verified, req.RemoteIP)
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "player.resolve", map[string]any{
			"requested_username": strings.TrimSpace(req.Username),
			"passport_id":        passport.ID,
			"passport_kind":      string(passport.Kind),
			"player_id":          player.ID,
			"grant_id":           grant.ID,
			"resolution":         "pending_transfer_grant",
			"protocol_name":      player.ProtocolName,
			"uuid":               player.UUID.String(),
			"auth":               data["auth"],
			"kind":               node.NormalizeKind(n.Mode),
			"remote_ip":          req.RemoteIP,
		})
		api.WriteJSON(w, http.StatusOK, data, nil)
		return
	}
	passport, apiErr := s.resolveNodePassport(r.Context(), n, req)
	if apiErr != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "player.resolve_failure", map[string]any{
			"reason":    apiErr.Message,
			"username":  strings.TrimSpace(req.Username),
			"kind":      node.NormalizeKind(n.Mode),
			"remote_ip": req.RemoteIP,
		})
		api.WriteError(w, apiErr)
		return
	}
	data := s.nodePassportResolveData(r.Context(), passport, req.Verified, req.RemoteIP)
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "player.resolve", map[string]any{
		"requested_username": strings.TrimSpace(req.Username),
		"passport_id":        passport.ID,
		"passport_kind":      string(passport.Kind),
		"auth":               data["auth"],
		"kind":               node.NormalizeKind(n.Mode),
		"remote_ip":          req.RemoteIP,
	})
	api.WriteJSON(w, http.StatusOK, data, nil)
}

func (s *Server) resolvePendingGrantPlayer(ctx context.Context, n node.Node, req resolvePlayerRequest) (identity.Player, identity.Passport, auth.TransferGrant, bool) {
	if !node.IsDownstreamVelocity(n.Mode) {
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	if strings.TrimSpace(req.LoginMode) != "" || req.Verified {
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	serverID := strings.TrimSpace(n.ServerID)
	username := strings.TrimSpace(req.Username)
	if serverID == "" || username == "" {
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	grant, err := s.store.GetPendingTransferGrantByProtocolName(ctx, serverID, username, time.Now())
	if err != nil {
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	player, err := s.store.GetPlayerByID(ctx, grant.PlayerID)
	if err != nil {
		s.logger.Warn("pending transfer grant points to missing player", "grant_id", grant.ID, "player_id", grant.PlayerID, "server_id", serverID, "protocol_name", grant.ProtocolName, "err", err)
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	passport, err := s.store.GetPassportForProfile(ctx, player.ID)
	if err != nil {
		s.logger.Warn("pending transfer grant points to unbound profile", "grant_id", grant.ID, "player_id", player.ID, "server_id", serverID, "protocol_name", grant.ProtocolName, "err", err)
		return identity.Player{}, identity.Passport{}, auth.TransferGrant{}, false
	}
	if uuid, err := identity.ParseUUID(grant.UUID); err == nil {
		player.UUID = uuid
	}
	player.ProtocolName = grant.ProtocolName
	return player, passport, grant, true
}

func (s *Server) resolveNodePassport(ctx context.Context, n node.Node, req resolvePlayerRequest) (identity.Passport, *api.Error) {
	if req.Verified {
		if !node.IsLimboPortal(n.Mode) {
			return identity.Passport{}, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can submit verified session profiles")
		}
		if strings.TrimSpace(req.LoginMode) != "online" {
			return identity.Passport{}, api.NewError(http.StatusBadRequest, "session.login_mode_invalid", "verified session must use online login mode")
		}
		uuid, err := identity.ParseUUID(req.VerifiedUUID)
		if err != nil {
			return identity.Passport{}, api.NewError(http.StatusBadRequest, "session.uuid_invalid", "verified UUID is invalid")
		}
		passport, err := s.store.UpsertPremiumPassport(ctx, strings.TrimSpace(req.Username), uuid)
		if err != nil {
			return identity.Passport{}, api.NewError(http.StatusInternalServerError, "premium.upsert_failed", "failed to persist premium passport")
		}
		if props := nodeProfileProperties(req.ProfileProperties); len(props) > 0 {
			_ = s.store.SetPassportPremiumTextures(ctx, passport.ID, props)
		}
		return passport, nil
	}
	if strings.TrimSpace(req.LoginMode) == "offline" {
		passport, err := s.store.GetPassportByUsername(ctx, strings.TrimSpace(req.Username))
		if err != nil {
			return identity.Passport{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
		}
		return passport, nil
	}
	if passport, err := s.findPassportByLoginName(ctx, strings.TrimSpace(req.Username)); err == nil {
		return passport, nil
	}
	// Legacy fallback: a downstream gate may resolve a bare profile protocol
	// name; map it back to its passport.
	player, err := s.store.GetPlayerByProtocolName(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		return identity.Passport{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
	}
	passport, err := s.store.GetPassportForProfile(ctx, player.ID)
	if err != nil {
		return identity.Passport{}, api.NewError(http.StatusNotFound, "player.not_found", "player not found")
	}
	return passport, nil
}

type authenticatePlayerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	RemoteIP string `json:"remote_ip"`
}

type rememberPortalAuthRequest struct {
	PassportID string `json:"passport_id"`
	RemoteIP   string `json:"remote_ip"`
	Reason     string `json:"reason"`
}

type registerOfflinePlayerRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	RequestedHost string `json:"requested_host"`
	RemoteIP      string `json:"remote_ip"`
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
	// Authenticate at the passport level: a freshly registered passport has no
	// profiles yet, so password verification must not require one.
	passport, credential, err := s.store.GetPassportCredential(r.Context(), normalizeNodeUsername(req.Username))
	if err != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.Username), "offline.password.failure", map[string]any{
			"reason":    "credential_not_found",
			"username":  strings.TrimSpace(req.Username),
			"remote_ip": req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	if passport.Status == identity.PassportStatusLocked || passportCredentialLocked(credential, time.Now()) {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPassport, passport.ID, "offline.password.rejected", map[string]any{
			"reason":       "account_locked",
			"locked_until": credential.LockedUntil,
			"remote_ip":    req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		updatedCredential, _ := s.store.RecordPassportLoginFailure(r.Context(), passport.ID, time.Now())
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPassport, passport.ID, "offline.password.failure", map[string]any{
			"reason":          "password_mismatch",
			"failed_attempts": updatedCredential.FailedAttempts,
			"locked_until":    updatedCredential.LockedUntil,
			"remote_ip":       req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	_ = s.store.RecordPassportLoginSuccess(r.Context(), passport.ID)
	var cache store.PortalAuthCache
	remembered := false
	if next, ok, cacheErr := s.rememberPortalAuthCache(r.Context(), passport.ID, req.RemoteIP, time.Now()); cacheErr != nil {
		s.logger.Warn("failed to remember portal auth cache after offline password success", "passport_id", passport.ID, "remote_ip", req.RemoteIP, "err", cacheErr)
	} else {
		cache = next
		remembered = ok
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPassport, passport.ID, "offline.password.success", map[string]any{
		"server_id":                 n.ServerID,
		"remote_ip":                 req.RemoteIP,
		"portal_auth_cache_written": remembered,
	})
	response := map[string]any{"authenticated": true, "passport_id": passport.ID}
	if remembered {
		response["auth_cache"] = portalAuthCacheData(cache, true)
	}
	if primary, err := s.store.GetPrimaryProfileForPassport(r.Context(), passport.ID); err == nil {
		player := identity.PlayerFromPassportProfile(passport, primary)
		player.ProfileProperties = s.effectiveProfileProperties(r.Context(), primary, &passport)
		s.recordPlayerSeenWithClientIP(r, player, n.ServerID, req.RemoteIP, time.Now())
		response["player"] = playerData(player)
	}
	api.WriteJSON(w, http.StatusOK, response, nil)
}

func (s *Server) handleNodeRememberPortalAuth(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can remember portal auth"))
		return
	}
	var req rememberPortalAuthRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	passportID := strings.TrimSpace(req.PassportID)
	if passportID == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "passport.required", "passport id is required"))
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), passportID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not active"))
		return
	}
	cache, remembered, err := s.rememberPortalAuthCache(r.Context(), passport.ID, req.RemoteIP, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_auth_cache.save_failed", "failed to remember portal auth"))
		return
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPassport, passport.ID, "portal.auth_cache.remember", map[string]any{
		"remote_ip":  req.RemoteIP,
		"reason":     strings.TrimSpace(req.Reason),
		"remembered": remembered,
		"expires_at": cache.ExpiresAt,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{"auth_cache": portalAuthCacheData(cache, remembered)}, nil)
}

func (s *Server) handleNodeRegisterOfflinePlayer(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can register offline passports"))
		return
	}
	var req registerOfflinePlayerRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.username_required", "username is required"))
		return
	}
	passwordHash, err := auth.HashPassword(req.Password, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	encryptedPassword, keyFingerprint, err := s.offlinePasswordCredential(r.Context(), req.Password)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "password_recovery.encrypt_failed", "failed to encrypt recoverable password"))
		return
	}
	passport, err := s.store.CreateOfflinePassport(r.Context(), username, passwordHash, encryptedPassword, keyFingerprint)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.offline_registration_failed", err.Error()))
		return
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, passport.ID, "offline.register", map[string]any{
		"passport_id":    passport.ID,
		"requested_host": req.RequestedHost,
		"server_id":      n.ServerID,
		"remote_ip":      req.RemoteIP,
	})
	if _, _, cacheErr := s.rememberPortalAuthCache(r.Context(), passport.ID, req.RemoteIP, time.Now()); cacheErr != nil {
		s.logger.Warn("failed to remember portal auth cache after offline registration", "passport_id", passport.ID, "remote_ip", req.RemoteIP, "err", cacheErr)
	}
	data := s.nodePassportResolveData(r.Context(), passport, false, req.RemoteIP)
	// The player just proved this passport by setting its password.
	if auth, ok := data["auth"].(map[string]any); ok {
		auth["required"] = false
	}
	api.WriteJSON(w, http.StatusCreated, data, nil)
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
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, strings.TrimSpace(req.ServerID), "portal.target.resolve_failure", map[string]any{
			"slug":           req.Slug,
			"requested_host": req.RequestedHost,
			"remote_ip":      req.RemoteIP,
			"reason":         apiErr.Message,
		})
		api.WriteError(w, apiErr)
		return
	}
	if apiErr := validateDownstreamProtocol(target, req.ProtocolVersion); apiErr != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "portal.target.resolve_failure", map[string]any{
			"slug":             server.Slug,
			"requested_host":   req.RequestedHost,
			"remote_ip":        req.RemoteIP,
			"protocol_version": req.ProtocolVersion,
			"reason":           apiErr.Message,
		})
		api.WriteError(w, apiErr)
		return
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "portal.target.resolve", map[string]any{
		"slug":           server.Slug,
		"requested_host": req.RequestedHost,
		"remote_ip":      req.RemoteIP,
		"target_host":    target.TransferHost,
		"target_port":    target.TransferPort,
		"protocol":       req.ProtocolVersion,
	})
	data := map[string]any{
		"server":          s.downstreamServerData(r.Context(), server),
		"target":          s.downstreamTargetData(r.Context(), server, target),
		"player_messages": s.playerMessagesPayloadForServer(r.Context(), server, "limbo_portal"),
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
	s.writeNodeTransferGrant(w, r, n, req, nodeTransferGrantOptions{
		CreateEvent: "transfer_grant.create",
		RejectEvent: "transfer_grant.reject",
	})
}

func (s *Server) handleNodeCreateDownstreamTransfer(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsDownstreamVelocity(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.downstream_required", "downstream transfer command requires a downstream node"))
		return
	}
	var req createTransferGrantRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	s.writeNodeTransferGrant(w, r, n, req, nodeTransferGrantOptions{
		CreateEvent:           "transfer_grant.command_create",
		RejectEvent:           "transfer_grant.reject",
		RequireExplicitTarget: true,
		RejectSameServer:      true,
		DirectCommand:         true,
	})
}

type nodeTransferGrantOptions struct {
	CreateEvent           string
	RejectEvent           string
	RequireExplicitTarget bool
	RejectSameServer      bool
	DirectCommand         bool
}

func (s *Server) writeNodeTransferGrant(w http.ResponseWriter, r *http.Request, n node.Node, req createTransferGrantRequest, opts nodeTransferGrantOptions) {
	createEvent := strings.TrimSpace(opts.CreateEvent)
	if createEvent == "" {
		createEvent = "transfer_grant.create"
	}
	rejectEvent := strings.TrimSpace(opts.RejectEvent)
	if rejectEvent == "" {
		rejectEvent = "transfer_grant.reject"
	}
	if opts.RequireExplicitTarget && strings.TrimSpace(req.ServerID) == "" && strings.TrimSpace(req.Slug) == "" && strings.TrimSpace(req.RequestedHost) == "" {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.target_required", "target downstream server is required"))
		return
	}
	server, target, apiErr := s.resolveDownstreamTarget(r.Context(), n, req.ServerID, req.Slug, req.RequestedHost)
	if apiErr != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, strings.TrimSpace(req.ServerID), rejectEvent, map[string]any{
			"reason":         apiErr.Message,
			"slug":           req.Slug,
			"requested_host": req.RequestedHost,
			"remote_ip":      req.RemoteIP,
		})
		api.WriteError(w, apiErr)
		return
	}
	if opts.RejectSameServer && strings.TrimSpace(n.ServerID) != "" && server.ID == strings.TrimSpace(n.ServerID) {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, rejectEvent, map[string]any{
			"reason":    "same_downstream_server",
			"server_id": server.ID,
			"remote_ip": req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusBadRequest, "server.same_target", "target downstream server must be different from the current server"))
		return
	}
	if target.Status != "active" && target.Status != "hidden" {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, rejectEvent, map[string]any{
			"reason":    "server_unavailable",
			"status":    target.Status,
			"remote_ip": req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "server.unavailable", "downstream server is not active"))
		return
	}
	if apiErr := validateDownstreamProtocol(target, req.ProtocolVersion); apiErr != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, rejectEvent, map[string]any{
			"reason":           apiErr.Message,
			"server_id":        server.ID,
			"remote_ip":        req.RemoteIP,
			"protocol_version": req.ProtocolVersion,
		})
		api.WriteError(w, apiErr)
		return
	}
	player, apiErr := s.playerFromTransferGrantRequest(r.Context(), req)
	if apiErr != nil {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, strings.TrimSpace(req.PlayerID+req.Username), rejectEvent, map[string]any{
			"reason":    apiErr.Message,
			"player_id": req.PlayerID,
			"username":  req.Username,
			"server_id": server.ID,
			"remote_ip": req.RemoteIP,
		})
		api.WriteError(w, apiErr)
		return
	}
	if player.Locked {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, rejectEvent, playerEventDetails(player, map[string]any{
			"reason":    "account_locked",
			"server_id": server.ID,
			"remote_ip": req.RemoteIP,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.account_locked", "account is locked"))
		return
	}
	if ban, banned := s.activeBanForPlayer(r.Context(), player); banned {
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, rejectEvent, playerEventDetails(player, map[string]any{
			"reason":     "banned",
			"server_id":  server.ID,
			"ban_id":     ban.ID,
			"ban_scope":  ban.Scope,
			"expires_at": ban.ExpiresAt,
			"remote_ip":  req.RemoteIP,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.banned", ban.Reason))
		return
	}
	if target.Status == "hidden" {
		passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
		if err != nil || !s.store.DownstreamServerHasPrivilegedPassport(r.Context(), server.ID, passport.ID) {
			passportID := ""
			if err == nil {
				passportID = passport.ID
			}
			s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, rejectEvent, playerEventDetails(player, map[string]any{
				"reason":      "server_hidden",
				"server_id":   server.ID,
				"passport_id": passportID,
				"remote_ip":   req.RemoteIP,
			}))
			api.WriteError(w, api.NewError(http.StatusForbidden, "server.privileged_required", "privileged passport is required for this server"))
			return
		}
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
	// Serialize the promote-to-primary + grant creation per passport so two
	// concurrent sessions of the same passport selecting different profiles
	// cannot interleave and leave the gate-resolved primary inconsistent with
	// the grant that was just issued.
	if strings.TrimSpace(req.PlayerID) != "" && !opts.DirectCommand {
		if passport, err := s.store.GetPassportForProfile(r.Context(), player.ID); err == nil {
			unlock := s.passportLocks.lock(passport.ID)
			defer unlock()
		}
		s.promoteGrantProfilePrimary(r, n, player, req.RemoteIP)
	}
	source := portalGrantSource(n, req.Source)
	if opts.DirectCommand {
		source = downstreamCommandGrantSource(n, req.Source)
	}
	grant, rawToken, err := auth.NewTransferGrant(
		player.ID,
		server.ID,
		n.ID,
		source,
		req.RemoteIP,
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
	if normalizeClientIPValue(req.RemoteIP) != "" {
		s.recordPlayerSeenWithClientIP(r, player, server.ID, req.RemoteIP, now)
	}
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, createEvent, map[string]any{
		"server_id":     server.ID,
		"source":        source,
		"source_kind":   map[bool]string{true: "downstream_command", false: "portal"}[opts.DirectCommand],
		"target_host":   target.TransferHost,
		"target_port":   target.TransferPort,
		"protocol":      req.ProtocolVersion,
		"protocol_name": player.ProtocolName,
		"expires_at":    grant.ExpiresAt,
		"remote_ip":     req.RemoteIP,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"grant":  transferGrantData(grant),
		"token":  rawToken,
		"target": store.DownstreamTargetData(target),
		"player": playerData(player),
	}, nil)
}

func validateDownstreamProtocol(target store.DownstreamTarget, protocolVersion int) *api.Error {
	if protocolVersion <= 0 {
		return nil
	}
	minProtocol := target.MinProtocolVersion
	if minProtocol <= 0 {
		minProtocol = store.DefaultMinDownstreamProtocol
	}
	if protocolVersion < minProtocol {
		return api.NewError(http.StatusForbidden, "server.protocol_too_old", "client protocol is older than this downstream server allows")
	}
	if target.MaxProtocolVersion > 0 && protocolVersion > target.MaxProtocolVersion {
		return api.NewError(http.StatusForbidden, "server.protocol_too_new", "client protocol is newer than this downstream server allows")
	}
	return nil
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
	if target.Status == "disabled" {
		api.WriteError(w, api.NewError(http.StatusForbidden, "server.unavailable", "downstream server is disabled"))
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
		s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetDownstreamServer, server.ID, "transfer_grant.reject", map[string]any{
			"reason":        err.Error(),
			"protocol_name": protocolName,
			"uuid":          uuid,
			"source":        req.Source,
			"remote_ip":     req.RemoteIP,
		})
		api.WriteError(w, api.NewError(http.StatusForbidden, "transfer_grant.invalid", err.Error()))
		return
	}
	gateRemoteIP := normalizeClientIPValue(req.RemoteIP)
	portalRemoteIP := normalizeClientIPValue(grant.RemoteIP)
	effectiveRemoteIP := portalRemoteIP
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
		s.auditWithClientIP(r, effectiveRemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.reject", playerEventDetails(player, map[string]any{
			"reason":           "banned",
			"server_id":        server.ID,
			"ban_id":           ban.ID,
			"ban_scope":        ban.Scope,
			"expires_at":       ban.ExpiresAt,
			"remote_ip":        effectiveRemoteIP,
			"portal_remote_ip": portalRemoteIP,
			"gate_remote_ip":   gateRemoteIP,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "auth.banned", ban.Reason))
		return
	}
	passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_bound", "profile is not bound to a passport"))
		return
	}
	privilegedPassport := s.store.DownstreamServerHasPrivilegedPassport(r.Context(), server.ID, passport.ID)
	if target.Status == "hidden" && !privilegedPassport {
		s.auditWithClientIP(r, effectiveRemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.reject", playerEventDetails(player, map[string]any{
			"reason":           "server_hidden",
			"server_id":        server.ID,
			"passport_id":      passport.ID,
			"remote_ip":        effectiveRemoteIP,
			"portal_remote_ip": portalRemoteIP,
			"gate_remote_ip":   gateRemoteIP,
		}))
		api.WriteError(w, api.NewError(http.StatusForbidden, "server.privileged_required", "privileged passport is required for this server"))
		return
	}
	now := time.Now()
	remoteAddr := effectiveRemoteIP
	if existing := activePresencesForProfileServer(s.store.ListProfilePresences(r.Context(), player.ID), server.ID); len(existing) > 0 {
		remaining, checked, cleared, queuedChecks := s.refreshProfileServerPresenceConflict(r, n, player.ID, server.ID, existing, now)
		if len(remaining) > 0 {
			s.rejectProfileAlreadyOnline(w, r, n, player, server.ID, effectiveRemoteIP, remaining, checked, cleared, queuedChecks)
			return
		}
		s.logger.Info("cleared stale presence conflict before creating downstream presence", "profile_id", player.ID, "server_id", server.ID, "node_id", n.ID, "checked", checked, "cleared", cleared, "queued_presence_checks", queuedChecks)
	}
	presenceInput := store.PlayerPresence{
		PassportID:   passport.ID,
		ProfileID:    player.ID,
		ServerID:     server.ID,
		NodeID:       n.ID,
		ProtocolName: protocolName,
		UUID:         uuid,
		RemoteAddr:   remoteAddr,
		ConnectedAt:  now,
		LastSeenAt:   now,
	}
	presence, err := s.store.UpsertPresence(r.Context(), presenceInput)
	if err != nil {
		existing := activePresencesForProfileServer(s.store.ListProfilePresences(r.Context(), player.ID), server.ID)
		if len(existing) == 0 {
			s.logger.Warn("failed to create player presence", "profile_id", player.ID, "server_id", server.ID, "node_id", n.ID, "err", err)
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "presence.create_failed", "failed to create player presence"))
			return
		}
		remaining, checked, cleared, queuedChecks := s.refreshProfileServerPresenceConflict(r, n, player.ID, server.ID, existing, now)
		if len(remaining) > 0 {
			s.rejectProfileAlreadyOnline(w, r, n, player, server.ID, effectiveRemoteIP, remaining, checked, cleared, queuedChecks)
			return
		}
		presence, err = s.store.UpsertPresence(r.Context(), presenceInput)
		if err != nil {
			s.logger.Warn("failed to create player presence after clearing stale conflict", "profile_id", player.ID, "server_id", server.ID, "node_id", n.ID, "err", err)
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "presence.create_failed", "failed to create player presence"))
			return
		}
	}
	if effectiveRemoteIP != "" {
		s.recordPlayerSeenWithClientIP(r, player, server.ID, effectiveRemoteIP, now)
	}
	s.auditWithClientIP(r, effectiveRemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "transfer_grant.consume", map[string]any{
		"server_id":               server.ID,
		"protocol_name":           protocolName,
		"uuid":                    uuid,
		"source":                  req.Source,
		"portal_source":           grant.PortalSource,
		"presence_id":             presence.ID,
		"remote_ip":               effectiveRemoteIP,
		"portal_remote_ip":        portalRemoteIP,
		"gate_remote_ip":          gateRemoteIP,
		"gate_remote_ip_rejected": gateRemoteIP != "",
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed":  true,
		"grant":    transferGrantData(grant),
		"player":   playerData(player),
		"presence": presenceRows([]store.PlayerPresence{presence})[0],
		"target":   store.DownstreamTargetData(target),
		"passport": map[string]any{
			"id":         passport.ID,
			"privileged": privilegedPassport,
		},
	}, nil)
}

func (s *Server) refreshProfileServerPresenceConflict(r *http.Request, n node.Node, profileID string, serverID string, existing []store.PlayerPresence, now time.Time) ([]store.PlayerPresence, int, int, int) {
	checked, cleared, queuedChecks := s.refreshPresencesViaWebSocket(r, n, existing, "profile already online conflict", now)
	remaining := activePresencesForProfileServer(s.store.ListProfilePresences(r.Context(), profileID), serverID)
	return remaining, checked, cleared, queuedChecks
}

func (s *Server) rejectProfileAlreadyOnline(w http.ResponseWriter, r *http.Request, n node.Node, player identity.Player, serverID string, remoteIP string, existing []store.PlayerPresence, checked int, cleared int, queuedChecks int) {
	presenceIDs := make([]string, 0, len(existing))
	nodeIDs := make([]string, 0, len(existing))
	for _, presence := range existing {
		if strings.TrimSpace(presence.ID) != "" {
			presenceIDs = append(presenceIDs, presence.ID)
		}
		if strings.TrimSpace(presence.NodeID) != "" {
			nodeIDs = append(nodeIDs, presence.NodeID)
		}
	}
	s.auditWithClientIP(r, remoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "presence.reject", playerEventDetails(player, map[string]any{
		"reason":                    "profile_already_online_on_server",
		"server_id":                 serverID,
		"node_id":                   n.ID,
		"remote_ip":                 remoteIP,
		"existing_presence_ids":     presenceIDs,
		"existing_node_ids":         nodeIDs,
		"websocket_presence_checks": checked,
		"cleared_stale_presences":   cleared,
		"queued_presence_checks":    queuedChecks,
		"presence_check_trigger":    "transfer_grant_consume_conflict",
		"presence_check_required":   true,
	}))
	err := api.NewError(http.StatusConflict, "presence.profile_already_online", "profile is already online on this server")
	err.Details = map[string]any{
		"profile_id":                player.ID,
		"server_id":                 serverID,
		"existing_presence_ids":     presenceIDs,
		"websocket_presence_checks": checked,
		"cleared_stale_presences":   cleared,
		"queued_presence_checks":    queuedChecks,
		"presence_check_required":   true,
	}
	api.WriteError(w, err)
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

func downstreamCommandGrantSource(n node.Node, requested string) string {
	source := strings.TrimSpace(requested)
	if source == "" {
		source = strings.TrimSpace(n.ServerID)
	}
	if source == "" {
		source = strings.TrimSpace(n.Name)
	}
	if source == "" {
		source = strings.TrimSpace(n.ID)
	}
	return "downstream-command:" + source
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
	ProfileID string `json:"profile_id"`
	Username  string `json:"username"`
	ServerID  string `json:"server_id"`
	TTL       string `json:"ttl"`
}

func (s *Server) handleNodeCreatePortalLink(w http.ResponseWriter, r *http.Request) {
	currentNode, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsDownstreamVelocity(currentNode.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only downstream nodes can create portal links"))
		return
	}
	var req createPortalLinkRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	var player identity.Player
	var err error
	if profileID := strings.TrimSpace(req.ProfileID); profileID != "" {
		profile, profileErr := s.store.GetProfileByID(r.Context(), profileID)
		if profileErr == nil {
			passport, passportErr := s.store.GetPassportForProfile(r.Context(), profile.ID)
			if passportErr == nil {
				player = identity.PlayerFromPassportProfile(passport, profile)
			} else {
				err = passportErr
			}
		} else {
			err = profileErr
		}
	} else {
		player, err = s.store.GetPlayerByProtocolName(r.Context(), strings.TrimSpace(req.Username))
	}
	if err != nil {
		s.audit(r, audit.ActorNode, currentNode.ID, audit.TargetPlayer, strings.TrimSpace(req.ProfileID), "portal_link.create_failure", map[string]any{
			"reason":     "profile_not_found_or_ambiguous",
			"profile_id": strings.TrimSpace(req.ProfileID),
			"username":   strings.TrimSpace(req.Username),
			"server_id":  currentNode.ServerID,
		})
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile was not found or the protocol name is ambiguous"))
		return
	}
	if username := strings.TrimSpace(req.Username); username != "" && !strings.EqualFold(username, player.ProtocolName) {
		api.WriteError(w, api.NewError(http.StatusConflict, "profile.identity_mismatch", "profile id and protocol name do not identify the same profile"))
		return
	}
	passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
	if err != nil {
		s.audit(r, audit.ActorNode, currentNode.ID, audit.TargetPlayer, player.ID, "portal_link.create_failure", playerEventDetails(player, map[string]any{
			"reason":    "passport_not_found",
			"server_id": currentNode.ServerID,
		}))
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive || player.Locked {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.locked", "passport or profile is not active"))
		return
	}
	ttl, ttlErr := portalLinkTTL(req.TTL, 0)
	if ttlErr != nil {
		api.WriteError(w, ttlErr)
		return
	}
	serverID := strings.TrimSpace(currentNode.ServerID)
	if serverID == "" {
		serverID = strings.TrimSpace(req.ServerID)
	}
	linkKind := auth.PortalLinkOffline
	if passport.Kind == identity.PassportKindPremium {
		linkKind = auth.PortalLinkPremium
	}
	link, rawToken, err := auth.NewPortalLink(linkKind, passport.ID, serverID, ttl, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to create portal link"))
		return
	}
	link.SuggestedProfileID = player.ID
	link.IssuedByNodeID = currentNode.ID
	if err := s.store.SavePortalLink(r.Context(), link); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to save portal link"))
		return
	}
	s.audit(r, audit.ActorNode, currentNode.ID, audit.TargetPlayer, passport.ID, "portal_link.create", map[string]any{
		"server_id":     serverID,
		"expires_at":    link.ExpiresAt,
		"profile_id":    player.ID,
		"protocol_name": player.ProtocolName,
	})
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"link": map[string]any{
			"id":                   link.ID,
			"kind":                 link.Kind,
			"passport_id":          link.PlayerID,
			"player_id":            link.PlayerID,
			"suggested_profile_id": link.SuggestedProfileID,
			"server_id":            link.ServerID,
			"expires_at":           link.ExpiresAt,
			"url":                  s.playerPortalLinkURL(rawToken),
			"token":                rawToken,
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
	data := map[string]any{
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
	if node.IsLimboPortal(n.Mode) {
		data["protocol_pack"] = s.limboProtocolPackData(ctx, n.ID)
	}
	return data
}

func (s *Server) nodeRuntimeConfig(ctx context.Context, n node.Node) map[string]any {
	kind := node.NormalizeKind(n.Mode)
	communication := s.nodeCommunicationSettings(ctx)
	base := map[string]any{
		"node_name":                       strings.TrimSpace(n.Name),
		"server_id":                       strings.TrimSpace(n.ServerID),
		"heartbeat_interval_seconds":      communication.HeartbeatIntervalSeconds,
		"websocket_enabled":               communication.WebSocketEnabled,
		"websocket_reconnect_min_seconds": communication.WebSocketReconnectMinSeconds,
		"websocket_reconnect_max_seconds": communication.WebSocketReconnectMaxSeconds,
		"websocket_ping_interval_seconds": communication.WebSocketPingIntervalSeconds,
		"resolve_raw_offline_names":       true,
		"max_password_attempts":           3,
		"chat_cooldown_millis":            150,
		"auth_timeout_seconds":            90,
		"completion_delay_seconds":        3,
		"transfer_cookie_key":             "authman:transfer_grant",
		"email_verification_mode":         "disabled",
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
	base["proxy_protocol_enabled"] = false
	base["proxy_protocol_restrict_trusted_proxies"] = false
	base["proxy_protocol_trusted_proxies"] = ""
	base["proxy_protocol_header_timeout_millis"] = 5000
	mergeRuntimeConfig(base, n.RuntimeConfig, []string{
		"proxy_protocol_enabled",
		"proxy_protocol_restrict_trusted_proxies",
		"proxy_protocol_trusted_proxies",
		"proxy_protocol_header_timeout_millis",
	})
	return base
}

func (s *Server) nodeDownstreamServerChoices(ctx context.Context, n node.Node) []map[string]any {
	if !node.IsDownstreamVelocity(n.Mode) {
		return []map[string]any{}
	}
	current := strings.TrimSpace(n.ServerID)
	servers := s.store.ListDownstreamServers(ctx)
	sort.SliceStable(servers, func(i, j int) bool {
		return strings.ToLower(servers[i].DisplayName) < strings.ToLower(servers[j].DisplayName)
	})
	out := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		if server.Status == "disabled" || strings.TrimSpace(server.ID) == current {
			continue
		}
		target := store.DownstreamTargetFromServer(server)
		out = append(out, map[string]any{
			"id":            server.ID,
			"slug":          server.Slug,
			"display_name":  server.DisplayName,
			"status":        server.Status,
			"transfer_host": target.TransferHost,
			"transfer_port": target.TransferPort,
		})
	}
	return out
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
		"remote_ip":      grant.RemoteIP,
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
