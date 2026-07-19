package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/node"
	"golang.org/x/net/websocket"
)

const nodeCommunicationSettingKey = "node_communication"

type nodeCommunicationSettings struct {
	WebSocketEnabled             bool `json:"websocket_enabled"`
	HeartbeatIntervalSeconds     int  `json:"heartbeat_interval_seconds"`
	WebSocketReconnectMinSeconds int  `json:"websocket_reconnect_min_seconds"`
	WebSocketReconnectMaxSeconds int  `json:"websocket_reconnect_max_seconds"`
	WebSocketPingIntervalSeconds int  `json:"websocket_ping_interval_seconds"`
}

func defaultNodeCommunicationSettings() nodeCommunicationSettings {
	return nodeCommunicationSettings{
		WebSocketEnabled:             true,
		HeartbeatIntervalSeconds:     60,
		WebSocketReconnectMinSeconds: 2,
		WebSocketReconnectMaxSeconds: 60,
		WebSocketPingIntervalSeconds: 25,
	}
}

func normalizeNodeCommunicationSettings(settings nodeCommunicationSettings) nodeCommunicationSettings {
	out := settings
	if out.HeartbeatIntervalSeconds < 10 {
		out.HeartbeatIntervalSeconds = 10
	}
	if out.HeartbeatIntervalSeconds > 600 {
		out.HeartbeatIntervalSeconds = 600
	}
	if out.WebSocketReconnectMinSeconds < 1 {
		out.WebSocketReconnectMinSeconds = 1
	}
	if out.WebSocketReconnectMinSeconds > 300 {
		out.WebSocketReconnectMinSeconds = 300
	}
	if out.WebSocketReconnectMaxSeconds < out.WebSocketReconnectMinSeconds {
		out.WebSocketReconnectMaxSeconds = out.WebSocketReconnectMinSeconds
	}
	if out.WebSocketReconnectMaxSeconds > 900 {
		out.WebSocketReconnectMaxSeconds = 900
	}
	if out.WebSocketPingIntervalSeconds < 5 {
		out.WebSocketPingIntervalSeconds = 5
	}
	if out.WebSocketPingIntervalSeconds > 300 {
		out.WebSocketPingIntervalSeconds = 300
	}
	return out
}

func (s *Server) nodeCommunicationSettings(ctx context.Context) nodeCommunicationSettings {
	settings := defaultNodeCommunicationSettings()
	raw, err := s.store.GetSystemSetting(ctx, nodeCommunicationSettingKey)
	if err != nil || raw == nil {
		return settings
	}
	if value, ok := raw["websocket_enabled"].(bool); ok {
		settings.WebSocketEnabled = value
	}
	if value := intValue(raw["heartbeat_interval_seconds"], 0); value > 0 {
		settings.HeartbeatIntervalSeconds = value
	}
	if value := intValue(raw["websocket_reconnect_min_seconds"], 0); value > 0 {
		settings.WebSocketReconnectMinSeconds = value
	}
	if value := intValue(raw["websocket_reconnect_max_seconds"], 0); value > 0 {
		settings.WebSocketReconnectMaxSeconds = value
	}
	if value := intValue(raw["websocket_ping_interval_seconds"], 0); value > 0 {
		settings.WebSocketPingIntervalSeconds = value
	}
	return normalizeNodeCommunicationSettings(settings)
}

func nodeCommunicationSettingsMap(settings nodeCommunicationSettings) map[string]any {
	settings = normalizeNodeCommunicationSettings(settings)
	return map[string]any{
		"websocket_enabled":               settings.WebSocketEnabled,
		"heartbeat_interval_seconds":      settings.HeartbeatIntervalSeconds,
		"websocket_reconnect_min_seconds": settings.WebSocketReconnectMinSeconds,
		"websocket_reconnect_max_seconds": settings.WebSocketReconnectMaxSeconds,
		"websocket_ping_interval_seconds": settings.WebSocketPingIntervalSeconds,
	}
}

func (s *Server) handleAdminNodeCommunicationSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, nodeCommunicationSettingsMap(s.nodeCommunicationSettings(r.Context())), nil)
}

func (s *Server) handleAdminUpdateNodeCommunicationSettings(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	var req nodeCommunicationSettings
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	settings := normalizeNodeCommunicationSettings(req)
	if err := s.store.SetSystemSetting(r.Context(), nodeCommunicationSettingKey, nodeCommunicationSettingsMap(settings)); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "node_communication.save_failed", "failed to save node communication settings"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetSystem, "node-communication", "node_communication.update", nodeCommunicationSettingsMap(settings))
	s.pushAllNodeSync(r.Context(), "node_communication.update")
	api.WriteJSON(w, http.StatusOK, nodeCommunicationSettingsMap(settings), nil)
}

type nodeEventHub struct {
	mu      sync.Mutex
	clients map[string]map[*nodeEventClient]struct{}
	pending map[string]chan json.RawMessage
}

type nodeEventClient struct {
	nodeID string
	send   chan []byte
}

func newNodeEventHub() *nodeEventHub {
	return &nodeEventHub{
		clients: map[string]map[*nodeEventClient]struct{}{},
		pending: map[string]chan json.RawMessage{},
	}
}

func (h *nodeEventHub) register(nodeID string, client *nodeEventClient) func() {
	h.mu.Lock()
	if h.clients[nodeID] == nil {
		h.clients[nodeID] = map[*nodeEventClient]struct{}{}
	}
	h.clients[nodeID][client] = struct{}{}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		if clients := h.clients[nodeID]; clients != nil {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.clients, nodeID)
			}
		}
		h.mu.Unlock()
		close(client.send)
	}
}

func (h *nodeEventHub) broadcast(nodeID string, payload []byte) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for client := range h.clients[nodeID] {
		select {
		case client.send <- payload:
			count++
		default:
		}
	}
	return count
}

var errNodeEventNoLiveClient = errors.New("no live node websocket client")

func nodeEventRequestKey(nodeID string, requestID string) string {
	return nodeID + "\x00" + requestID
}

func (h *nodeEventHub) request(ctx context.Context, nodeID string, requestID string, payload []byte) (json.RawMessage, int, error) {
	if nodeID == "" || requestID == "" {
		return nil, 0, errNodeEventNoLiveClient
	}
	ch := make(chan json.RawMessage, 1)
	key := nodeEventRequestKey(nodeID, requestID)
	h.mu.Lock()
	if h.pending == nil {
		h.pending = map[string]chan json.RawMessage{}
	}
	h.pending[key] = ch
	delivered := 0
	for client := range h.clients[nodeID] {
		select {
		case client.send <- payload:
			delivered++
		default:
		}
	}
	if delivered == 0 {
		delete(h.pending, key)
		h.mu.Unlock()
		return nil, 0, errNodeEventNoLiveClient
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, key)
		h.mu.Unlock()
	}()
	select {
	case raw := <-ch:
		return raw, delivered, nil
	case <-ctx.Done():
		return nil, delivered, ctx.Err()
	}
}

func (h *nodeEventHub) resolve(nodeID string, requestID string, payload json.RawMessage) bool {
	if nodeID == "" || requestID == "" {
		return false
	}
	key := nodeEventRequestKey(nodeID, requestID)
	h.mu.Lock()
	ch := h.pending[key]
	h.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- payload:
		return true
	default:
		return false
	}
}

func (s *Server) handleNodeEvents(w http.ResponseWriter, r *http.Request) {
	websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error {
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			s.serveNodeEvents(ws)
		},
	}.ServeHTTP(w, r)
}

func (s *Server) serveNodeEvents(ws *websocket.Conn) {
	defer ws.Close()
	r := ws.Request()
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		_ = sendNodeEventError(ws, nodeErr.Code, nodeErr.Message)
		return
	}
	settings := s.nodeCommunicationSettings(r.Context())
	if !settings.WebSocketEnabled {
		_ = sendNodeEventError(ws, "node_events.disabled", "node websocket events are disabled")
		return
	}
	client := &nodeEventClient{
		nodeID: n.ID,
		send:   make(chan []byte, 8),
	}
	unregister := s.nodeEvents.register(n.ID, client)
	defer unregister()
	if payload, err := s.nodeEventPayload(r.Context(), "sync", n, "connected"); err == nil {
		client.send <- payload
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var message string
			if err := websocket.Message.Receive(ws, &message); err != nil {
				return
			}
			s.handleNodeEventClientMessage(n.ID, []byte(message))
		}
	}()
	pingEvery := time.Duration(settings.WebSocketPingIntervalSeconds) * time.Second
	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case payload, ok := <-client.send:
			if !ok {
				return
			}
			if _, err := ws.Write(payload); err != nil {
				return
			}
		case <-ticker.C:
			payload, _ := json.Marshal(map[string]any{
				"type":    "ping",
				"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
			if _, err := ws.Write(payload); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleNodeEventClientMessage(nodeID string, payload []byte) {
	var message struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return
	}
	if message.Type != "presence_check_result" {
		return
	}
	if len(message.Data) == 0 || string(message.Data) == "null" {
		message.Data = payload
	}
	s.nodeEvents.resolve(nodeID, message.RequestID, message.Data)
}

func sendNodeEventError(ws *websocket.Conn, code string, message string) error {
	payload, err := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return err
	}
	return websocket.Message.Send(ws, string(payload))
}

func (s *Server) nodeEventPayload(ctx context.Context, typ string, n node.Node, reason string) ([]byte, error) {
	data := s.nodeSyncPayload(ctx, n)
	payload := map[string]any{
		"type":    typ,
		"reason":  reason,
		"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
		"data":    data,
	}
	return json.Marshal(payload)
}

func (s *Server) nodeSyncPayload(ctx context.Context, n node.Node) map[string]any {
	payload := map[string]any{
		"node":               s.nodeData(ctx, n),
		"runtime_config":     s.nodeRuntimeConfig(ctx, n),
		"player_messages":    s.playerMessagesPayload(ctx, n),
		"actions":            nodeActionRows(s.store.ListPendingNodeActions(ctx, n.ID, time.Now(), 50)),
		"downstream_servers": s.nodeDownstreamServerChoices(ctx, n),
	}
	if node.IsLimboPortal(n.Mode) {
		payload["protocol_pack"] = s.limboProtocolPackData(ctx, n.ID)
	} else if node.IsDownstreamVelocity(n.Mode) {
		payload["runtime_module"] = s.velocityRuntimeNodeData(ctx, n.ID)
	}
	return payload
}

func (s *Server) pushNodeSync(ctx context.Context, n node.Node, reason string) int {
	if s.nodeEvents == nil {
		return 0
	}
	payload, err := s.nodeEventPayload(ctx, "sync", n, reason)
	if err != nil {
		return 0
	}
	return s.nodeEvents.broadcast(n.ID, payload)
}

func (s *Server) pushNodeIDSync(ctx context.Context, nodeID string, reason string) int {
	if nodeID == "" {
		return 0
	}
	n, err := s.nodes.Get(ctx, nodeID)
	if err != nil {
		return 0
	}
	return s.pushNodeSync(ctx, n, reason)
}

func (s *Server) pushAllNodeSync(ctx context.Context, reason string) {
	for _, n := range s.nodes.List(ctx) {
		s.pushNodeSync(ctx, n, reason)
	}
}

func (s *Server) pushNodeRevoked(nodeID string) {
	if s.nodeEvents == nil || nodeID == "" {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":    "revoked",
		"reason":  "node.revoked",
		"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}
	s.nodeEvents.broadcast(nodeID, payload)
}
