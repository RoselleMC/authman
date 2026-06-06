package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RoselleMC/authman/internal/api"
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
	writeProtocolJSON(w, http.StatusOK, profile)
}

func writeProtocolJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
