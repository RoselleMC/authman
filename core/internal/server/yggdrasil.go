package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/yggdrasil"
)

func (s *Server) handleYggdrasilMetadata(w http.ResponseWriter, r *http.Request) {
	writeProtocolJSON(w, http.StatusOK, yggdrasil.DefaultMetadata(s.cfg.PublicBaseURL))
}

func (s *Server) handleHasJoined(w http.ResponseWriter, r *http.Request) {
	req := yggdrasil.HasJoinedRequest{
		Username: r.URL.Query().Get("username"),
		ServerID: r.URL.Query().Get("serverId"),
		IP:       r.URL.Query().Get("ip"),
	}
	service := yggdrasil.JoinService{
		Premium: s.mojangVerifier,
		Offline: s,
	}
	profile, err := service.HasJoined(r.Context(), req)
	if err != nil {
		if errors.Is(err, yggdrasil.ErrProfileNotFound) {
			s.audit(r, audit.ActorSystem, "yggdrasil", audit.TargetPlayer, strings.TrimSpace(req.Username), "yggdrasil.has_joined_not_found", map[string]any{
				"username":  req.Username,
				"server_id": req.ServerID,
				"ip":        req.IP,
			})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.audit(r, audit.ActorSystem, "yggdrasil", audit.TargetPlayer, strings.TrimSpace(req.Username), "yggdrasil.has_joined_failure", map[string]any{
			"username":  req.Username,
			"server_id": req.ServerID,
			"ip":        req.IP,
			"reason":    err.Error(),
		})
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.has_joined_failed", err.Error()))
		return
	}
	s.recordHasJoined(r, req, profile)
	writeProtocolJSON(w, http.StatusOK, profile)
}

func (s *Server) recordHasJoined(r *http.Request, req yggdrasil.HasJoinedRequest, profile yggdrasil.Profile) {
	if playerID := authmanPlayerID(profile); playerID != "" {
		player, err := s.store.GetPlayerByID(r.Context(), playerID)
		if err == nil {
			s.recordPlayerSeen(r, player, req.ServerID, time.Now())
			s.audit(r, audit.ActorPlayer, player.ID, audit.TargetPlayer, player.ID, "yggdrasil.has_joined", playerEventDetails(player, map[string]any{
				"server_id": req.ServerID,
				"ip":        req.IP,
				"kind":      identity.PlayerKindOffline,
			}))
		}
		return
	}
	pp, ok := s.persistPremiumProfile(r.Context(), profile)
	if !ok {
		return
	}
	s.recordPassportProfileSeen(r, pp.Passport, pp.Profile, req.ServerID, time.Now())
	s.audit(r, audit.ActorPlayer, pp.Passport.ID, audit.TargetPlayer, pp.Profile.ID, "yggdrasil.has_joined", map[string]any{
		"passport_id":   pp.Passport.ID,
		"profile_id":    pp.Profile.ID,
		"protocol_name": pp.Profile.ProtocolName,
		"server_id":     req.ServerID,
		"ip":            req.IP,
		"kind":          identity.PlayerKindPremium,
	})
}

func (s *Server) persistPremiumProfile(ctx context.Context, profile yggdrasil.Profile) (identity.PassportProfile, bool) {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.Name) == "" {
		return identity.PassportProfile{}, false
	}
	for _, property := range profile.Properties {
		if property.Name == yggdrasil.PropertyAuthmanKind && property.Value == string(identity.PlayerKindOffline) {
			return identity.PassportProfile{}, false
		}
	}
	uuid, err := identity.ParseUUID(profile.ID)
	if err != nil {
		return identity.PassportProfile{}, false
	}
	properties := make([]identity.ProfileProperty, 0, len(profile.Properties))
	for _, property := range profile.Properties {
		properties = append(properties, identity.ProfileProperty{
			Name:      property.Name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	pp, err := s.store.UpsertPremiumPassportProfile(ctx, profile.Name, uuid, properties)
	return pp, err == nil
}

func authmanPlayerID(profile yggdrasil.Profile) string {
	for _, property := range profile.Properties {
		if property.Name == yggdrasil.PropertyAuthmanPlayer {
			return strings.TrimSpace(property.Value)
		}
	}
	return ""
}

func writeProtocolJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
