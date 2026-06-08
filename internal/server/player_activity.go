package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/identity"
)

func (s *Server) recordPassportProfileSeen(r *http.Request, passport identity.Passport, profile identity.Profile, serverID string, now time.Time) {
	ip, geo := s.requestIPGeo(r)
	_ = s.store.RecordPlayerSeen(r.Context(), passport.ID, profile.ID, strings.TrimSpace(serverID), ip, geo, now)
}

func (s *Server) recordPlayerSeen(r *http.Request, player identity.Player, serverID string, now time.Time) {
	passportID := ""
	if passport, err := s.store.GetPassportForProfile(r.Context(), player.ID); err == nil {
		passportID = passport.ID
	}
	ip, geo := s.requestIPGeo(r)
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
