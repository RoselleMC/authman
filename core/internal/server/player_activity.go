package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
	"github.com/google/uuid"
)

const presenceCheckWebSocketTimeout = 1500 * time.Millisecond
const presenceReconcileBatchLimit = 500

func (s *Server) StartBackground(ctx context.Context) {
	s.startPresenceReconcileLoop(ctx)
	s.startIPGeoUpdateLoop(ctx)
}

func (s *Server) startPresenceReconcileLoop(ctx context.Context) {
	interval := s.cfg.PresenceReconcileInterval
	if interval <= 0 {
		s.logger.Info("periodic presence reconcile disabled")
		return
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.reconcileActivePresences(ctx)
				timer.Reset(interval)
			}
		}
	}()
}

func (s *Server) reconcileActivePresences(ctx context.Context) {
	presences := s.store.ListActivePresences(ctx, presenceReconcileBatchLimit)
	if len(presences) == 0 {
		return
	}
	now := time.Now()
	req := backgroundPresenceRequest(ctx)
	checked := 0
	cleared := 0
	failed := 0
	for _, presence := range presences {
		if ctx.Err() != nil {
			return
		}
		result, delivered, err := s.requestPresenceCheckOverWebSocket(ctx, presence, "periodic presence reconcile")
		if err != nil {
			failed++
			s.logger.Debug("periodic websocket presence check failed", "node_id", presence.NodeID, "presence_id", presence.ID, "websocket_clients", delivered, "err", err)
			continue
		}
		checked++
		online := result.Online
		actorNode := node.Node{ID: presence.NodeID}
		if strings.TrimSpace(presence.NodeID) != "" {
			if presenceNode, err := s.nodes.Get(ctx, presence.NodeID); err == nil {
				actorNode = presenceNode
			}
		}
		s.processNodeActionAckResults(req, actorNode, []nodeActionAckResult{{
			ID:           result.RequestID,
			Type:         string(store.NodeActionPresenceCheck),
			PresenceID:   result.PresenceID,
			PassportID:   result.PassportID,
			ProfileID:    result.ProfileID,
			UUID:         result.UUID,
			ProtocolName: result.ProtocolName,
			Online:       &online,
		}}, now)
		if !online {
			cleared++
		}
	}
	if cleared > 0 {
		s.logger.Info("completed periodic presence reconcile", "checked", checked, "cleared", cleared, "failed", failed, "total", len(presences))
		return
	}
	s.logger.Debug("completed periodic presence reconcile", "checked", checked, "cleared", cleared, "failed", failed, "total", len(presences))
}

func backgroundPresenceRequest(ctx context.Context) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/internal/presence-reconcile", nil)
	req.RemoteAddr = "127.0.0.1:0"
	return req
}

func (s *Server) recordPassportProfileSeen(r *http.Request, passport identity.Passport, profile identity.Profile, serverID string, now time.Time) {
	s.recordPassportProfileSeenWithClientIP(r, passport, profile, serverID, "", now)
}

func (s *Server) recordPassportProfileSeenWithClientIP(r *http.Request, passport identity.Passport, profile identity.Profile, serverID string, ipOverride string, now time.Time) {
	ip, geo := s.requestIPGeoWithClientIP(r, ipOverride)
	_ = s.store.RecordPlayerSeen(r.Context(), passport.ID, profile.ID, strings.TrimSpace(serverID), ip, geo, now)
}

func (s *Server) recordPlayerSeen(r *http.Request, player identity.Player, serverID string, now time.Time) {
	s.recordPlayerSeenWithClientIP(r, player, serverID, "", now)
}

func (s *Server) recordPlayerSeenWithClientIP(r *http.Request, player identity.Player, serverID string, ipOverride string, now time.Time) {
	passportID := ""
	if passport, err := s.store.GetPassportForProfile(r.Context(), player.ID); err == nil {
		passportID = passport.ID
	}
	ip, geo := s.requestIPGeoWithClientIP(r, ipOverride)
	_ = s.store.RecordPlayerSeen(r.Context(), passportID, player.ID, strings.TrimSpace(serverID), ip, geo, now)
}

func playerEventDetails(player identity.Player, extra map[string]any) map[string]any {
	details := map[string]any{
		"profile_id":    player.ID,
		"protocol_name": player.ProtocolName,
		"uuid":          player.UUID.String(),
		"kind":          player.Kind,
	}
	for key, value := range extra {
		details[key] = value
	}
	return details
}

func (s *Server) enqueueDisconnectActions(ctx context.Context, presences []store.PlayerPresence, reason string, now time.Time) int {
	count := 0
	changedNodes := map[string]struct{}{}
	for _, presence := range presences {
		if strings.TrimSpace(presence.NodeID) == "" {
			continue
		}
		expiresAt := now.UTC().Add(10 * time.Minute)
		_, err := s.store.EnqueueNodeAction(ctx, store.NodeAction{
			NodeID:       presence.NodeID,
			Type:         store.NodeActionDisconnect,
			PresenceID:   presence.ID,
			PassportID:   presence.PassportID,
			ProfileID:    presence.ProfileID,
			UUID:         presence.UUID,
			ProtocolName: presence.ProtocolName,
			Reason:       strings.TrimSpace(reason),
			CreatedAt:    now.UTC(),
			ExpiresAt:    &expiresAt,
		})
		if err == nil {
			count++
			changedNodes[presence.NodeID] = struct{}{}
		}
	}
	for nodeID := range changedNodes {
		s.pushNodeIDSync(ctx, nodeID, "node_action.enqueue")
	}
	return count
}

func (s *Server) enqueuePresenceCheckActions(ctx context.Context, presences []store.PlayerPresence, reason string, now time.Time) int {
	count := 0
	changedNodes := map[string]int{}
	for _, presence := range presences {
		if strings.TrimSpace(presence.NodeID) == "" {
			continue
		}
		expiresAt := now.UTC().Add(2 * time.Minute)
		_, err := s.store.EnqueueNodeAction(ctx, store.NodeAction{
			NodeID:       presence.NodeID,
			Type:         store.NodeActionPresenceCheck,
			PresenceID:   presence.ID,
			PassportID:   presence.PassportID,
			ProfileID:    presence.ProfileID,
			UUID:         presence.UUID,
			ProtocolName: presence.ProtocolName,
			Reason:       strings.TrimSpace(reason),
			CreatedAt:    now.UTC(),
			ExpiresAt:    &expiresAt,
		})
		if err == nil {
			count++
			changedNodes[presence.NodeID]++
		}
	}
	for nodeID, actionCount := range changedNodes {
		delivered := s.pushNodeIDSync(ctx, nodeID, "presence_check.enqueue")
		args := []any{
			"node_id", nodeID,
			"actions", actionCount,
			"websocket_clients", delivered,
			"reason", reason,
		}
		if delivered == 0 {
			s.logger.Warn("queued presence check without live websocket delivery", args...)
			continue
		}
		s.logger.Info("queued presence check over node websocket", args...)
	}
	return count
}

type presenceCheckRPCResult struct {
	RequestID    string `json:"request_id"`
	PresenceID   string `json:"presence_id"`
	PassportID   string `json:"passport_id"`
	ProfileID    string `json:"profile_id"`
	UUID         string `json:"uuid"`
	ProtocolName string `json:"protocol_name"`
	Online       bool   `json:"online"`
}

func (s *Server) refreshPresencesViaWebSocket(r *http.Request, n node.Node, presences []store.PlayerPresence, reason string, now time.Time) (int, int, int) {
	checked := 0
	cleared := 0
	fallback := make([]store.PlayerPresence, 0)
	for _, presence := range presences {
		result, delivered, err := s.requestPresenceCheckOverWebSocket(r.Context(), presence, reason)
		if err != nil {
			s.logger.Warn("live websocket presence check failed", "node_id", presence.NodeID, "presence_id", presence.ID, "websocket_clients", delivered, "reason", reason, "err", err)
			fallback = append(fallback, presence)
			continue
		}
		checked++
		online := result.Online
		actorNode := n
		if strings.TrimSpace(presence.NodeID) != "" && strings.TrimSpace(presence.NodeID) != n.ID {
			if presenceNode, err := s.nodes.Get(r.Context(), presence.NodeID); err == nil {
				actorNode = presenceNode
			}
		}
		s.processNodeActionAckResults(r, actorNode, []nodeActionAckResult{{
			ID:           result.RequestID,
			Type:         string(store.NodeActionPresenceCheck),
			PresenceID:   result.PresenceID,
			PassportID:   result.PassportID,
			ProfileID:    result.ProfileID,
			UUID:         result.UUID,
			ProtocolName: result.ProtocolName,
			Online:       &online,
		}}, now)
		if !online {
			cleared++
		}
		s.logger.Info("completed live websocket presence check", "node_id", presence.NodeID, "presence_id", presence.ID, "websocket_clients", delivered, "online", online, "reason", reason)
	}
	queued := 0
	if len(fallback) > 0 {
		queued = s.enqueuePresenceCheckActions(r.Context(), fallback, reason, now)
	}
	return checked, cleared, queued
}

func (s *Server) requestPresenceCheckOverWebSocket(ctx context.Context, presence store.PlayerPresence, reason string) (presenceCheckRPCResult, int, error) {
	if s.nodeEvents == nil {
		return presenceCheckRPCResult{}, 0, errNodeEventNoLiveClient
	}
	nodeID := strings.TrimSpace(presence.NodeID)
	if nodeID == "" {
		return presenceCheckRPCResult{}, 0, errNodeEventNoLiveClient
	}
	requestID := "presence-check-" + uuid.NewString()
	payload, err := json.Marshal(map[string]any{
		"type":       "presence_check",
		"request_id": requestID,
		"data": map[string]any{
			"request_id":    requestID,
			"presence_id":   presence.ID,
			"passport_id":   presence.PassportID,
			"profile_id":    presence.ProfileID,
			"uuid":          presence.UUID,
			"protocol_name": presence.ProtocolName,
			"reason":        strings.TrimSpace(reason),
		},
	})
	if err != nil {
		return presenceCheckRPCResult{}, 0, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, presenceCheckWebSocketTimeout)
	defer cancel()
	raw, delivered, err := s.nodeEvents.request(requestCtx, nodeID, requestID, payload)
	if err != nil {
		return presenceCheckRPCResult{}, delivered, err
	}
	var result presenceCheckRPCResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return presenceCheckRPCResult{}, delivered, err
	}
	if strings.TrimSpace(result.RequestID) == "" {
		result.RequestID = requestID
	}
	if result.RequestID != requestID {
		return presenceCheckRPCResult{}, delivered, fmt.Errorf("presence check response request_id mismatch")
	}
	if strings.TrimSpace(result.PresenceID) == "" {
		result.PresenceID = presence.ID
	}
	if strings.TrimSpace(result.PassportID) == "" {
		result.PassportID = presence.PassportID
	}
	if strings.TrimSpace(result.ProfileID) == "" {
		result.ProfileID = presence.ProfileID
	}
	if strings.TrimSpace(result.UUID) == "" {
		result.UUID = presence.UUID
	}
	if strings.TrimSpace(result.ProtocolName) == "" {
		result.ProtocolName = presence.ProtocolName
	}
	return result, delivered, nil
}

func activePresencesForProfileServer(presences []store.PlayerPresence, serverID string) []store.PlayerPresence {
	serverID = strings.TrimSpace(serverID)
	out := make([]store.PlayerPresence, 0, len(presences))
	for _, presence := range presences {
		if serverID != "" && strings.TrimSpace(presence.ServerID) != serverID {
			continue
		}
		out = append(out, presence)
	}
	return out
}
