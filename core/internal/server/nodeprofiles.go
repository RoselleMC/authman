package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/node"
	"github.com/RoselleMC/authman/core/internal/store"
)

// Passport-centric node resolution: limbo authenticates a passport and then
// explicitly creates or selects one of its profiles. No profile is created
// implicitly anymore.

// findPassportByLoginName resolves the name a player connects with to its
// passport: offline passports by normalized offline name, then premium
// passports by Mojang name.
func (s *Server) findPassportByLoginName(ctx context.Context, username string) (identity.Passport, error) {
	if passport, err := s.store.GetPassportByUsername(ctx, username); err == nil {
		return passport, nil
	}
	return s.store.GetPremiumPassportByUsername(ctx, username)
}

func passportData(passport identity.Passport) map[string]any {
	return map[string]any{
		"id":       passport.ID,
		"uuid":     passport.UUID.String(),
		"kind":     string(passport.Kind),
		"username": passport.Username,
		"locked":   passport.Status == identity.PassportStatusLocked,
	}
}

func nodeProfileSummaries(profiles []identity.Profile, primaryID string) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]any{
			"id":            profile.ID,
			"uuid":          profile.UUID.String(),
			"protocol_name": profile.ProtocolName,
			"primary":       profile.ID == primaryID,
		})
	}
	return out
}

func (s *Server) profilePolicyData(ctx context.Context, profileCount int) map[string]any {
	settings := s.portalSettings(ctx)
	return map[string]any{
		"max_profiles":             settings.MaxProfilesPerPassport,
		"can_create":               profileCount < settings.MaxProfilesPerPassport,
		"auto_join_single_profile": settings.AutoJoinSingleProfile,
	}
}

// nodePassportResolveData assembles the resolve/register response for a
// passport: optional primary-profile player payload plus the full profile
// list and creation policy.
func (s *Server) nodePassportResolveData(ctx context.Context, passport identity.Passport, verified bool) map[string]any {
	profiles := s.store.ListProfilesForPassport(ctx, passport.ID)
	primaryID := ""
	var playerPayload any
	authLocked := passport.Status == identity.PassportStatusLocked
	if primary, err := s.store.GetPrimaryProfileForPassport(ctx, passport.ID); err == nil {
		primaryID = primary.ID
		player := identity.PlayerFromPassportProfile(passport, primary)
		player.ProfileProperties = s.effectiveProfileProperties(ctx, primary, &passport)
		playerPayload = playerData(player)
		authLocked = player.Locked
	}
	authRequired := passport.Kind == identity.PassportKindOffline && !authLocked && !verified
	authKind := "premium"
	if passport.Kind == identity.PassportKindOffline {
		authKind = "offline_password"
	}
	return map[string]any{
		"player":         playerPayload,
		"passport":       passportData(passport),
		"profiles":       nodeProfileSummaries(profiles, primaryID),
		"profile_policy": s.profilePolicyData(ctx, len(profiles)),
		"auth": map[string]any{
			"required": authRequired,
			"kind":     authKind,
			"locked":   authLocked,
			"username": passport.Username,
		},
	}
}

type nodeCreateProfileRequest struct {
	PassportID   string `json:"passport_id"`
	Username     string `json:"username"`
	ProtocolName string `json:"protocol_name"`
	RemoteIP     string `json:"remote_ip"`
}

// handleNodeCreateProfile creates a profile for an authenticated passport on
// behalf of the limbo portal profile dialog. The new profile becomes primary
// so the downstream gate resolves the freshly chosen identity.
func (s *Server) handleNodeCreateProfile(w http.ResponseWriter, r *http.Request) {
	n, nodeErr := s.requireNode(r)
	if nodeErr != nil {
		api.WriteError(w, nodeErr)
		return
	}
	if !node.IsLimboPortal(n.Mode) {
		api.WriteError(w, api.NewError(http.StatusForbidden, "node.kind_forbidden", "only limbo portal nodes can create profiles"))
		return
	}
	var req nodeCreateProfileRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	// Prefer the passport id captured at authentication time so a profile is
	// never created on the wrong passport when an offline and a premium
	// passport share the same login name.
	var passport identity.Passport
	var passportErr error
	if id := strings.TrimSpace(req.PassportID); id != "" {
		passport, passportErr = s.store.GetPassportByID(r.Context(), id)
	} else {
		passport, passportErr = s.findPassportByLoginName(r.Context(), strings.TrimSpace(req.Username))
	}
	if passportErr != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable"))
		return
	}
	settings := s.portalSettings(r.Context())
	existing := s.store.ListProfilesForPassport(r.Context(), passport.ID)
	if len(existing) >= settings.MaxProfilesPerPassport {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.limit_reached", "this passport already has the maximum number of profiles"))
		return
	}
	name, err := identity.NormalizeProtocolName(req.ProtocolName)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	if _, err := s.store.GetProfileByProtocolName(r.Context(), name.Protocol); err == nil {
		api.WriteError(w, api.NewError(http.StatusConflict, "profile.name_taken", "protocol name is already taken"))
		return
	}
	profile, err := identity.NewOfflineProfile("", name.Protocol, passport.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.invalid_name", err.Error()))
		return
	}
	profile, err = s.store.CreateProfile(r.Context(), profile)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			api.WriteError(w, api.NewError(http.StatusConflict, "profile.name_taken", "protocol name is already taken"))
			return
		}
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.create_failed", err.Error()))
		return
	}
	if _, err := s.store.BindProfileToPassport(r.Context(), profile.ID, passport.ID, true); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.bind_failed", err.Error()))
		return
	}
	s.recordPassportProfileSeenWithClientIP(r, passport, profile, n.ServerID, req.RemoteIP, time.Now())
	s.auditWithClientIP(r, req.RemoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, profile.ID, "profile.node_create", map[string]any{
		"passport_id":   passport.ID,
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
		"server_id":     n.ServerID,
		"remote_ip":     req.RemoteIP,
	})
	api.WriteJSON(w, http.StatusCreated, s.nodePassportResolveData(r.Context(), passport, passport.Kind == identity.PassportKindPremium), nil)
}

// promoteGrantProfilePrimary marks the explicitly selected profile as primary
// so the downstream gate (which resolves by passport login name) applies the
// identity the player just picked in the limbo dialog.
func (s *Server) promoteGrantProfilePrimary(r *http.Request, n node.Node, player identity.Player, remoteIP string) {
	passport, err := s.store.GetPassportForProfile(r.Context(), player.ID)
	if err != nil {
		return
	}
	if primary, err := s.store.GetPrimaryProfileForPassport(r.Context(), passport.ID); err == nil && primary.ID == player.ID {
		return
	}
	if _, err := s.store.BindProfileToPassport(r.Context(), player.ID, passport.ID, true); err != nil {
		return
	}
	s.auditWithClientIP(r, remoteIP, audit.ActorNode, n.ID, audit.TargetPlayer, player.ID, "profile.primary_change", map[string]any{
		"passport_id":   passport.ID,
		"profile_id":    player.ID,
		"protocol_name": player.ProtocolName,
		"reason":        "limbo profile selection",
		"remote_ip":     remoteIP,
	})
}

var _ = store.ErrNotFound
