package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/identity"
)

type adminCreatePortalLinkRequest struct {
	SuggestedProfileID string `json:"suggested_profile_id"`
	ServerID           string `json:"server_id"`
	TTL                string `json:"ttl"`
	ExpiresInSeconds   int    `json:"expires_in_seconds"`
}

func (s *Server) handleAdminCreatePortalLink(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdminPermission(r, true, "players.portal_link.create")
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.locked", "passport is not active"))
		return
	}
	var req adminCreatePortalLinkRequest
	if r.ContentLength != 0 {
		if err := api.DecodeJSON(r, &req); err != nil {
			api.WriteError(w, err)
			return
		}
	}
	profile, err := s.requirePortalLinkProfile(r.Context(), passport.ID, req.SuggestedProfileID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "profile.not_available", "suggested profile is not active or does not belong to this passport"))
		return
	}
	ttl, ttlErr := portalLinkTTL(req.TTL, req.ExpiresInSeconds)
	if ttlErr != nil {
		api.WriteError(w, ttlErr)
		return
	}
	serverID := strings.TrimSpace(req.ServerID)
	if serverID != "" {
		if _, err := s.store.GetDownstreamServer(r.Context(), serverID); err != nil {
			api.WriteError(w, api.NewError(http.StatusNotFound, "server.not_found", "server not found"))
			return
		}
	}
	link, rawToken, err := auth.NewPortalLink(portalLinkKind(passport), passport.ID, serverID, ttl, time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to create portal link"))
		return
	}
	link.SuggestedProfileID = profile.ID
	if err := s.store.SavePortalLink(r.Context(), link); err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "portal_link.create_failed", "failed to save portal link"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "portal_link.create", map[string]any{
		"link_id":       link.ID,
		"profile_id":    profile.ID,
		"protocol_name": profile.ProtocolName,
		"server_id":     link.ServerID,
		"expires_at":    link.ExpiresAt,
	})
	data := portalLinkData(link)
	data["url"] = s.playerPortalLinkURL(rawToken)
	data["token"] = rawToken
	api.WriteJSON(w, http.StatusCreated, map[string]any{"link": data}, nil)
}

func (s *Server) handleAdminRevokePortalLink(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdminPermission(r, true, "players.portal_link.create")
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	link, err := s.store.RevokePortalLink(r.Context(), strings.TrimSpace(r.PathValue("id")), time.Now())
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "portal_link.not_active", "active portal link not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPortalSession, link.ID, "portal_link.revoke", map[string]any{
		"passport_id": link.PlayerID,
		"profile_id":  link.SuggestedProfileID,
	})
	api.WriteJSON(w, http.StatusOK, portalLinkData(link), nil)
}

func (s *Server) portalLinkProfile(ctx context.Context, passportID string, suggestedProfileID string) (identity.Profile, error) {
	profileID := strings.TrimSpace(suggestedProfileID)
	if profileID != "" {
		profile, err := s.store.GetProfileByID(ctx, profileID)
		if err == nil && profile.Status == identity.ProfileStatusActive {
			if _, bindingErr := s.store.GetProfilePassportBinding(ctx, profile.ID, passportID); bindingErr == nil {
				return profile, nil
			}
		}
	}
	profile, err := s.store.GetPrimaryProfileForPassport(ctx, passportID)
	if err == nil && profile.Status == identity.ProfileStatusActive {
		return profile, nil
	}
	for _, candidate := range s.store.ListProfilesForPassport(ctx, passportID) {
		if candidate.Status == identity.ProfileStatusActive {
			return candidate, nil
		}
	}
	return identity.Profile{}, errors.New("passport has no active profile")
}

func (s *Server) requirePortalLinkProfile(ctx context.Context, passportID string, profileID string) (identity.Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return s.portalLinkProfile(ctx, passportID, "")
	}
	profile, err := s.store.GetProfileByID(ctx, profileID)
	if err != nil || profile.Status != identity.ProfileStatusActive {
		return identity.Profile{}, errors.New("profile is not active")
	}
	if _, err := s.store.GetProfilePassportBinding(ctx, profile.ID, passportID); err != nil {
		return identity.Profile{}, errors.New("profile does not belong to passport")
	}
	return profile, nil
}

func portalLinkTTL(raw string, expiresInSeconds int) (time.Duration, *api.Error) {
	ttl := 10 * time.Minute
	if expiresInSeconds > 0 {
		ttl = time.Duration(expiresInSeconds) * time.Second
	} else if strings.TrimSpace(raw) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return 0, api.NewError(http.StatusBadRequest, "portal_link.ttl_invalid", "portal link ttl is invalid")
		}
		ttl = parsed
	}
	if ttl <= 0 || ttl > time.Hour {
		return 0, api.NewError(http.StatusBadRequest, "portal_link.ttl_invalid", "portal link ttl must be between one second and one hour")
	}
	return ttl, nil
}

func portalLinkKind(passport identity.Passport) auth.PortalLinkKind {
	if passport.Kind == identity.PassportKindPremium {
		return auth.PortalLinkPremium
	}
	return auth.PortalLinkOffline
}

func portalLinkData(link auth.PortalLink) map[string]any {
	return map[string]any{
		"id":                   link.ID,
		"kind":                 link.Kind,
		"passport_id":          link.PlayerID,
		"player_id":            link.PlayerID,
		"suggested_profile_id": emptyStringNil(link.SuggestedProfileID),
		"server_id":            emptyStringNil(link.ServerID),
		"issued_by_node_id":    emptyStringNil(link.IssuedByNodeID),
		"status":               link.Status,
		"created_at":           link.CreatedAt,
		"expires_at":           link.ExpiresAt,
		"used_at":              link.UsedAt,
		"revoked_at":           link.RevokedAt,
	}
}

func portalLinkRows(links []auth.PortalLink) []map[string]any {
	rows := make([]map[string]any, 0, len(links))
	for _, link := range links {
		rows = append(rows, portalLinkData(link))
	}
	return rows
}

func (s *Server) playerPortalLinkURL(rawToken string) string {
	baseURL := strings.TrimSpace(s.cfg.PlayerPortalBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(s.cfg.PublicBaseURL)
	}
	return strings.TrimRight(baseURL, "/") + "/link#token=" + rawToken
}
