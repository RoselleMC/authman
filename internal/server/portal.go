package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/api"
	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/identity"
)

type offlineRegisterRequest struct {
	Username    string `json:"username"`
	RawUsername string `json:"raw_username"`
	Password    string `json:"password"`
	ServerSlug  string `json:"server_slug"`
}

func (s *Server) handleOfflineRegister(w http.ResponseWriter, r *http.Request) {
	var req offlineRegisterRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	passwordHash, err := auth.HashPassword(req.Password, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	username := req.Username
	if username == "" {
		username = req.RawUsername
	}
	player, err := s.store.CreateOfflinePlayer(r.Context(), username, passwordHash)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "player.offline_registration_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorPlayer, player.ID, audit.TargetPlayer, player.ID, "offline.register", map[string]any{
		"raw_offline_name": player.RawOfflineName,
	})
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, player.ID, 24*time.Hour, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	s.saveSession(sessionToken, session)
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"player":     portalPlayerData(player),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

type portalLoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	ServerSlug string `json:"server_slug"`
}

func (s *Server) handlePortalLogin(w http.ResponseWriter, r *http.Request) {
	var req portalLoginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	player, credential, err := s.store.GetOfflineCredential(r.Context(), req.Username)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	ok, err := auth.VerifyPassword(req.Password, credential.PasswordHash)
	if err != nil || !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid username or password"))
		return
	}
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, player.ID, 24*time.Hour, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	s.saveSession(sessionToken, session)
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorPlayer, player.ID, audit.TargetPortalSession, session.ID, "player.session.login", nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":     portalPlayerData(player),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) handlePortalMe(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), session.SubjectID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session player was not found"))
		return
	}
	csrf, csrfErr := s.rotateCSRF(session)
	if csrfErr != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to refresh CSRF token"))
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"player": portalPlayerData(player), "csrf_token": csrf}, nil)
}

func (s *Server) handlePortalLogout(w http.ResponseWriter, r *http.Request) {
	if session, err := s.requirePlayer(r, true); err == nil {
		if cookie, cookieErr := r.Cookie(playerSessionCookie); cookieErr == nil {
			s.deleteSession(cookie.Value)
		}
		s.audit(r, audit.ActorPlayer, session.SubjectID, audit.TargetPortalSession, session.ID, "player.session.logout", nil)
	}
	clearSessionCookie(w, r, playerSessionCookie)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true}, nil)
}

type portalChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handlePortalChangePassword(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), session.SubjectID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session player was not found"))
		return
	}
	var req portalChangePasswordRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	_, credential, err := s.store.GetOfflineCredential(r.Context(), player.RawOfflineName)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.credential_not_found", "offline credential not found"))
		return
	}
	ok, err := auth.VerifyPassword(req.CurrentPassword, credential.PasswordHash)
	if err != nil || !ok {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.invalid_credentials", "invalid current password"))
		return
	}
	passwordHash, err := auth.HashPassword(req.NewPassword, s.passwordParams)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "auth.password_policy_failed", "password does not satisfy policy"))
		return
	}
	if err := s.store.UpdateOfflinePassword(r.Context(), player.ID, passwordHash); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "auth.password_update_failed", "failed to update password"))
		return
	}
	s.audit(r, audit.ActorPlayer, player.ID, audit.TargetPlayer, player.ID, "offline.password.change", nil)
	api.WriteJSON(w, http.StatusOK, nil, nil)
}

func (s *Server) handlePortalConfig(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"registration_open":     true,
		"password_policy_hints": []string{"8-128 characters"},
		"message":               "Authman central portal",
	}, nil)
}

func (s *Server) handlePortalServers(w http.ResponseWriter, r *http.Request) {
	servers := portalServersData()
	api.WriteJSON(w, http.StatusOK, servers, map[string]any{"count": len(servers)})
}

func (s *Server) handlePortalServerConfig(w http.ResponseWriter, r *http.Request) {
	server, ok := portalServerData(r.PathValue("slug"))
	if !ok {
		api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, server, nil)
}

func (s *Server) handlePortalCheckName(w http.ResponseWriter, r *http.Request) {
	var req offlineRegisterRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	username := req.Username
	if username == "" {
		username = req.RawUsername
	}
	if _, err := identity.NormalizeOfflineName(username); err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"available": false, "reason": err.Error()}, nil)
		return
	}
	if _, err := s.store.GetOfflinePlayer(r.Context(), username); err == nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "already_registered"}, nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"available": true}, nil)
}

func (s *Server) handlePortalExtensionData(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), session.SubjectID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session player was not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, s.extensions.PlayerData(r.Context(), player, r.PathValue("serverSlug")), nil)
}

type portalLinkLoginRequest struct {
	Token string `json:"token"`
}

func (s *Server) handlePortalLinkLogin(w http.ResponseWriter, r *http.Request) {
	var req portalLinkLoginRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	link, ok := s.getPortalLink(req.Token)
	if !ok {
		api.WriteError(w, api.NewError(http.StatusNotFound, "portal_link.not_found", "portal link was not found"))
		return
	}
	switch result := link.Verify(req.Token, time.Now()); result {
	case auth.PortalLinkVerifyOK:
	default:
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "portal_link."+string(result), "portal link cannot be used"))
		return
	}
	player, err := s.store.GetPlayerByID(r.Context(), link.PlayerID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "player.not_found", "player not found"))
		return
	}
	session, sessionToken, csrfToken, err := auth.NewSession(auth.SessionPlayer, player.ID, 24*time.Hour, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "system.token_failed", "failed to create session"))
		return
	}
	s.saveSession(sessionToken, session)
	s.markPortalLinkUsed(req.Token)
	setSessionCookie(w, r, playerSessionCookie, sessionToken, session.ExpiresAt)
	s.audit(r, audit.ActorPlayer, player.ID, audit.TargetPortalSession, session.ID, "player.session.link_login", map[string]any{
		"server_id": link.ServerID,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"player":     portalPlayerData(player),
		"csrf_token": csrfToken,
		"expires_at": session.ExpiresAt,
	}, nil)
}

func (s *Server) handleAdminPlayers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(r, false); err != nil {
		api.WriteError(w, err)
		return
	}
	params := parseListPageParams(r)
	q := r.URL.Query()
	search := q.Get("q")
	kind := strings.TrimSpace(q.Get("kind"))
	status := strings.TrimSpace(q.Get("status"))

	players := s.store.ListPlayers(r.Context())
	sort.SliceStable(players, func(i, j int) bool {
		left := players[i].ProtocolName
		if left == "" {
			left = players[i].RawOfflineName
		}
		right := players[j].ProtocolName
		if right == "" {
			right = players[j].RawOfflineName
		}
		if left == right {
			return players[i].ID < players[j].ID
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})

	filtered := make([]identity.Player, 0, len(players))
	for _, player := range players {
		if kind != "" && string(player.Kind) != kind {
			continue
		}
		playerStatus := "active"
		if player.Locked {
			playerStatus = "locked"
		}
		if status != "" && playerStatus != status {
			continue
		}
		if search != "" &&
			!containsFold(player.ID, search) &&
			!containsFold(player.RawOfflineName, search) &&
			!containsFold(player.NormalizedName, search) &&
			!containsFold(player.ProtocolName, search) &&
			!containsFold(player.UUID.String(), search) &&
			!containsFold(player.UUID.Compact(), search) {
			continue
		}
		filtered = append(filtered, player)
	}

	start, end := pageBounds(len(filtered), params)
	data := make([]map[string]any, 0, end-start)
	for _, player := range filtered[start:end] {
		data = append(data, playerRowData(player))
	}
	api.WriteJSON(w, http.StatusOK, data, listMeta(len(data), len(filtered), params))
}

func (s *Server) getPortalLink(token string) (auth.PortalLink, bool) {
	token = strings.TrimSpace(token)
	s.portalLinksMu.RLock()
	defer s.portalLinksMu.RUnlock()
	link, ok := s.portalLinks[auth.HashToken("portal-link", token)]
	return link, ok
}

func (s *Server) markPortalLinkUsed(token string) {
	key := auth.HashToken("portal-link", strings.TrimSpace(token))
	s.portalLinksMu.Lock()
	defer s.portalLinksMu.Unlock()
	link, ok := s.portalLinks[key]
	if !ok {
		return
	}
	now := time.Now().UTC()
	link.Status = auth.PortalLinkUsed
	link.UsedAt = &now
	s.portalLinks[key] = link
}
