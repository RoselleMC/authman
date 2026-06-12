package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/store"
)

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
		}
	}
	return count
}
