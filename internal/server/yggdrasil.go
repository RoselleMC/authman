package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/yggdrasil"
)

func (s *Server) handleYggdrasilMetadata(w http.ResponseWriter, r *http.Request) {
	writeProtocolJSON(w, http.StatusOK, yggdrasil.DefaultMetadata(s.cfg.PublicBaseURL))
}

func (s *Server) handleHasJoined(w http.ResponseWriter, r *http.Request) {
	service := yggdrasil.JoinService{
		Premium: s.mojangVerifier,
		Offline: s,
	}
	profile, err := service.HasJoined(r.Context(), yggdrasil.HasJoinedRequest{
		Username: r.URL.Query().Get("username"),
		ServerID: r.URL.Query().Get("serverId"),
		IP:       r.URL.Query().Get("ip"),
	})
	if err != nil {
		if errors.Is(err, yggdrasil.ErrProfileNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.has_joined_failed", err.Error()))
		return
	}
	s.persistPremiumProfile(r.Context(), profile)
	writeProtocolJSON(w, http.StatusOK, profile)
}

func (s *Server) persistPremiumProfile(ctx context.Context, profile yggdrasil.Profile) {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.Name) == "" {
		return
	}
	for _, property := range profile.Properties {
		if property.Name == yggdrasil.PropertyAuthmanKind && property.Value == string(identity.PlayerKindOffline) {
			return
		}
	}
	uuid, err := identity.ParseUUID(profile.ID)
	if err != nil {
		return
	}
	properties := make([]identity.ProfileProperty, 0, len(profile.Properties))
	for _, property := range profile.Properties {
		properties = append(properties, identity.ProfileProperty{
			Name:      property.Name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	_, _ = s.store.UpsertPremiumPlayer(ctx, profile.Name, uuid, properties)
}

func writeProtocolJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
