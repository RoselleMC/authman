package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/api"
	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/identity"
	skinlib "github.com/RoselleMC/authman/core/internal/skin"
	"github.com/RoselleMC/authman/core/internal/store"
)

const maxSkinUploadBytes = 2 * 1024 * 1024
const maxServerIconUploadBytes = 256 * 1024

type effectiveSkin struct {
	Source          string
	Model           string
	Variant         string
	SkinURL         string
	CapeURL         string
	ElytraURL       string
	AvatarURL       string
	HasCustomSkin   bool
	HasCustomCape   bool
	HasCustomElytra bool
	Properties      []identity.ProfileProperty
}

type updateProfileSkinSourceRequest struct {
	UsePassportSkin bool   `json:"use_passport_skin"`
	Source          string `json:"source"`
}

type updatePassportSkinSourceRequest struct {
	UseUpstreamSkin bool   `json:"use_upstream_skin"`
	Source          string `json:"source"`
}

func (s *Server) handleAdminUploadProfileSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.multipart_invalid", "invalid multipart upload"))
		return
	}
	profile, skinRow, apiErr := s.saveUploadedProfileSkin(r.Context(), r, profile)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.skin.update", map[string]any{
		"model":       skinRow.Model,
		"skin_sha256": skinRow.SkinSHA256,
		"cape":        len(skinRow.CapePNG) > 0,
		"elytra":      len(skinRow.ElytraPNG) > 0,
	})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, passport), nil)
}

func (s *Server) handleAdminSetProfileSkinSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	var req updateProfileSkinSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		if req.UsePassportSkin {
			source = "passport"
		} else {
			source = "none"
		}
	}
	props := removeTexturesProperty(profile.ProfileProperties)
	if source == "passport" {
		if passport == nil {
			api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.passport_required", "profile is not bound to a passport"))
			return
		}
	} else if custom, err := s.store.GetProfileSkin(r.Context(), profile.ID); err == nil && len(custom.SkinPNG) > 0 {
		property, err := s.profileCustomTexturesProperty(profile, custom)
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.textures_failed", err.Error()))
			return
		}
		source = "custom"
		props = replaceTexturesProperty(profile.ProfileProperties, property)
	} else {
		source = "none"
	}
	profile, err = s.store.SetProfileSkinSource(r.Context(), profile.ID, source, props)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.skin.source_update", map[string]any{
		"source": source,
	})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, passport), nil)
}

func (s *Server) handleAdminUploadPassportSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.multipart_invalid", "invalid multipart upload"))
		return
	}
	passport, skinRow, apiErr := s.saveUploadedPassportSkin(r.Context(), r, passport)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.skin.update", map[string]any{
		"model":       skinRow.Model,
		"skin_sha256": skinRow.SkinSHA256,
		"cape":        len(skinRow.CapePNG) > 0,
		"elytra":      len(skinRow.ElytraPNG) > 0,
	})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handleAdminDeletePassportSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.DeletePassportSkin(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.skin.delete", map[string]any{})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handleAdminSetPassportSkinSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "passport.not_found", "passport not found"))
		return
	}
	var req updatePassportSkinSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	source := passportSkinSourceFromRequest(passport, req)
	passport, err = s.store.SetPassportSkinSource(r.Context(), passport.ID, source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, passport.ID, "passport.skin.source_update", map[string]any{
		"source": source,
	})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handlePortalProfileSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, profile, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session profile was not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, &passport), nil)
}

func (s *Server) handlePortalPassportSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, false)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handlePortalUploadPassportSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable"))
		return
	}
	if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.multipart_invalid", "invalid multipart upload"))
		return
	}
	passport, skinRow, apiErr := s.saveUploadedPassportSkin(r.Context(), r, passport)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "passport.skin.self_update", map[string]any{
		"model":       skinRow.Model,
		"skin_sha256": skinRow.SkinSHA256,
		"cape":        len(skinRow.CapePNG) > 0,
		"elytra":      len(skinRow.ElytraPNG) > 0,
	})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handlePortalDeletePassportSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable"))
		return
	}
	passport, err = s.store.DeletePassportSkin(r.Context(), passport.ID)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.delete_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "passport.skin.self_delete", map[string]any{})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handlePortalSetPassportSkinSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, _, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session passport was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "passport.not_editable", "passport is not editable"))
		return
	}
	var req updatePassportSkinSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	source := passportSkinSourceFromRequest(passport, req)
	passport, err = s.store.SetPassportSkinSource(r.Context(), passport.ID, source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, passport.ID, "passport.skin.self_source_update", map[string]any{
		"source": source,
	})
	api.WriteJSON(w, http.StatusOK, s.passportSkinData(r.Context(), passport), nil)
}

func (s *Server) handlePortalUploadProfileSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, profile, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session profile was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive || profile.Status != identity.ProfileStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.not_editable", "profile is not editable"))
		return
	}
	if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.multipart_invalid", "invalid multipart upload"))
		return
	}
	profile, skinRow, apiErr := s.saveUploadedProfileSkin(r.Context(), r, profile)
	if apiErr != nil {
		api.WriteError(w, apiErr)
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, profile.ID, "profile.skin.self_update", map[string]any{
		"model":       skinRow.Model,
		"skin_sha256": skinRow.SkinSHA256,
		"cape":        len(skinRow.CapePNG) > 0,
		"elytra":      len(skinRow.ElytraPNG) > 0,
	})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, &passport), nil)
}

func (s *Server) handlePortalSetProfileSkinSource(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, profile, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session profile was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive || profile.Status != identity.ProfileStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.not_editable", "profile is not editable"))
		return
	}
	var req updateProfileSkinSourceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, err)
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		if req.UsePassportSkin {
			source = "passport"
		} else {
			source = "none"
		}
	}
	props := removeTexturesProperty(profile.ProfileProperties)
	if source == "passport" {
		// Keep the profile's own UUID/name while inheriting the passport's effective texture payload.
	} else if custom, err := s.store.GetProfileSkin(r.Context(), profile.ID); err == nil && len(custom.SkinPNG) > 0 {
		property, err := s.profileCustomTexturesProperty(profile, custom)
		if err != nil {
			api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.textures_failed", err.Error()))
			return
		}
		source = "custom"
		props = replaceTexturesProperty(profile.ProfileProperties, property)
	} else {
		source = "none"
	}
	profile, err = s.store.SetProfileSkinSource(r.Context(), profile.ID, source, props)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, profile.ID, "profile.skin.self_source_update", map[string]any{
		"source": source,
	})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, &passport), nil)
}

func (s *Server) handlePortalDeleteProfileSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requirePlayer(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	passport, profile, _, err := s.portalSessionIdentity(r.Context(), session)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusUnauthorized, "auth.unauthenticated", "session profile was not found"))
		return
	}
	if passport.Status != identity.PassportStatusActive || profile.Status != identity.ProfileStatusActive {
		api.WriteError(w, api.NewError(http.StatusForbidden, "profile.not_editable", "profile is not editable"))
		return
	}
	props := removeTexturesProperty(profile.ProfileProperties)
	source := "none"
	if passport.Kind == identity.PassportKindPremium {
		source = "mojang"
	}
	profile, err = s.store.DeleteProfileSkin(r.Context(), profile.ID, props, source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.delete_failed", err.Error()))
		return
	}
	s.audit(r, audit.ActorPlayer, passport.ID, audit.TargetPlayer, profile.ID, "profile.skin.self_delete", map[string]any{})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, &passport), nil)
}

func (s *Server) handleAdminDeleteProfileSkin(w http.ResponseWriter, r *http.Request) {
	session, authErr := s.requireAdmin(r, true)
	if authErr != nil {
		api.WriteError(w, authErr)
		return
	}
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusNotFound, "profile.not_found", "profile not found"))
		return
	}
	props := removeTexturesProperty(profile.ProfileProperties)
	source := "none"
	if passport, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil && passport.Kind == identity.PassportKindPremium {
		source = "mojang"
	}
	profile, err = s.store.DeleteProfileSkin(r.Context(), profile.ID, props, source)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.delete_failed", err.Error()))
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.skin.delete", map[string]any{})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, passport), nil)
}

func (s *Server) handleProfileSkinAsset(w http.ResponseWriter, r *http.Request) {
	profile, err := s.store.GetProfileByID(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	eff := s.effectiveSkinForProfile(r.Context(), profile, passport)
	asset := strings.TrimSpace(r.PathValue("asset"))
	switch asset {
	case "skin.png":
		s.serveEffectiveSkinPNG(w, r, eff, profile.UUID)
	case "cape.png":
		s.serveOptionalTexture(w, r, eff.CapeURL, customTextureBytesFromURL(r.Context(), s.store, eff.CapeURL, "cape"))
	case "elytra.png":
		s.serveOptionalTexture(w, r, eff.ElytraURL, customTextureBytesFromURL(r.Context(), s.store, eff.ElytraURL, "elytra"))
	case "avatar.png":
		s.serveProfileAvatar(w, r, eff, profile.UUID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handlePassportSkinAsset(w http.ResponseWriter, r *http.Request) {
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	eff := s.effectiveSkinForPassport(r.Context(), passport)
	asset := strings.TrimSpace(r.PathValue("asset"))
	switch asset {
	case "skin.png":
		s.serveEffectiveSkinPNG(w, r, eff, passport.UUID)
	case "cape.png":
		s.serveOptionalTexture(w, r, eff.CapeURL, customTextureBytesFromURL(r.Context(), s.store, eff.CapeURL, "cape"))
	case "elytra.png":
		s.serveOptionalTexture(w, r, eff.ElytraURL, customTextureBytesFromURL(r.Context(), s.store, eff.ElytraURL, "elytra"))
	case "avatar.png":
		s.serveProfileAvatar(w, r, eff, passport.UUID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleDefaultSkinAsset(w http.ResponseWriter, r *http.Request) {
	raw, err := skinlib.DefaultSkinPNG(strings.TrimSuffix(r.PathValue("name"), ".png"), r.PathValue("model"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writePNG(w, raw)
}

func (s *Server) profileSkinData(ctx context.Context, profile identity.Profile, passport *identity.Passport) map[string]any {
	eff := s.effectiveSkinForProfile(ctx, profile, passport)
	defaultSkin := skinlib.DefaultForUUID(profile.UUID)
	versionTimes := []time.Time{profile.UpdatedAt}
	if passport != nil {
		versionTimes = append(versionTimes, passport.UpdatedAt)
	}
	var updatedAt *time.Time
	if custom, err := s.store.GetProfileSkin(ctx, profile.ID); err == nil {
		updatedAt = &custom.UpdatedAt
		versionTimes = append(versionTimes, custom.UpdatedAt)
	}
	return map[string]any{
		"source":            profile.SkinSource,
		"use_passport_skin": profile.SkinSource == "passport",
		"effective_source":  eff.Source,
		"model":             eff.Model,
		"default_variant":   defaultSkin.Name,
		"default_model":     defaultSkin.Model,
		"skin_url":          cacheBustedAssetURL(eff.SkinURL, versionTimes...),
		"cape_url":          emptyStringNil(cacheBustedAssetURL(eff.CapeURL, versionTimes...)),
		"elytra_url":        emptyStringNil(cacheBustedAssetURL(eff.ElytraURL, versionTimes...)),
		"avatar_url":        cacheBustedAssetURL(eff.AvatarURL, versionTimes...),
		"has_custom_skin":   eff.HasCustomSkin,
		"has_custom_cape":   eff.HasCustomCape,
		"has_custom_elytra": eff.HasCustomElytra,
		"updated_at":        updatedAt,
		"passport_skin":     s.optionalPassportSkinData(ctx, passport),
	}
}

func (s *Server) passportSkinData(ctx context.Context, passport identity.Passport) map[string]any {
	eff := s.effectiveSkinForPassport(ctx, passport)
	defaultSkin := skinlib.DefaultForUUID(passport.UUID)
	versionTimes := []time.Time{passport.UpdatedAt}
	var updatedAt *time.Time
	if custom, err := s.store.GetPassportSkin(ctx, passport.ID); err == nil {
		updatedAt = &custom.UpdatedAt
		versionTimes = append(versionTimes, custom.UpdatedAt)
	}
	source := store.NormalizePassportSkinSource(passport.Kind, passport.SkinSource)
	return map[string]any{
		"source":            source,
		"use_upstream_skin": eff.Source != "custom" && source == store.PassportSkinSourceUpstream,
		"effective_source":  eff.Source,
		"model":             eff.Model,
		"default_variant":   defaultSkin.Name,
		"default_model":     defaultSkin.Model,
		"skin_url":          cacheBustedAssetURL(eff.SkinURL, versionTimes...),
		"cape_url":          emptyStringNil(cacheBustedAssetURL(eff.CapeURL, versionTimes...)),
		"elytra_url":        emptyStringNil(cacheBustedAssetURL(eff.ElytraURL, versionTimes...)),
		"avatar_url":        cacheBustedAssetURL(eff.AvatarURL, versionTimes...),
		"has_custom_skin":   eff.HasCustomSkin,
		"has_custom_cape":   eff.HasCustomCape,
		"has_custom_elytra": eff.HasCustomElytra,
		"updated_at":        updatedAt,
	}
}

func (s *Server) optionalPassportSkinData(ctx context.Context, passport *identity.Passport) any {
	if passport == nil {
		return nil
	}
	return s.passportSkinData(ctx, *passport)
}

func (s *Server) saveUploadedProfileSkin(ctx context.Context, r *http.Request, profile identity.Profile) (identity.Profile, store.ProfileSkin, *api.Error) {
	existingSkin, existingErr := s.store.GetProfileSkin(ctx, profile.ID)
	if existingErr != nil && !errors.Is(existingErr, store.ErrNotFound) {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusInternalServerError, "skin.load_failed", existingErr.Error())
	}
	skinPNG, skinType, err := readPNGPart(r, "skin", false)
	if err != nil {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusBadRequest, "skin.skin_invalid", err.Error())
	}
	if len(skinPNG) == 0 {
		skinPNG = existingSkin.SkinPNG
		skinType = existingSkin.SkinContentType
	}
	if len(skinPNG) == 0 {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusBadRequest, "skin.skin_required", "skin file is required before custom texture settings can be saved")
	}
	capePNG, capeType, err := readPNGPart(r, "cape", false)
	if err != nil {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusBadRequest, "skin.cape_invalid", err.Error())
	}
	if len(capePNG) == 0 && r.MultipartForm.Value["cape"] == nil {
		capePNG = existingSkin.CapePNG
		capeType = existingSkin.CapeContentType
	}
	elytraPNG, elytraType, err := readPNGPart(r, "elytra", false)
	if err != nil {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusBadRequest, "skin.elytra_invalid", err.Error())
	}
	if len(elytraPNG) == 0 && r.MultipartForm.Value["elytra"] == nil {
		elytraPNG = existingSkin.ElytraPNG
		elytraType = existingSkin.ElytraContentType
	}
	model := normalizeUploadModel(r.FormValue("model"))
	skinRow := store.ProfileSkin{
		ProfileID:         profile.ID,
		Model:             model,
		SkinPNG:           skinPNG,
		SkinContentType:   skinType,
		SkinSHA256:        sha256Hex(skinPNG),
		CapePNG:           capePNG,
		CapeContentType:   capeType,
		CapeSHA256:        sha256Hex(capePNG),
		ElytraPNG:         elytraPNG,
		ElytraContentType: elytraType,
		ElytraSHA256:      sha256Hex(elytraPNG),
	}
	property, err := s.profileCustomTexturesProperty(profile, skinRow)
	if err != nil {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusInternalServerError, "skin.textures_failed", err.Error())
	}
	updated, err := s.store.SetProfileSkin(ctx, profile.ID, skinRow, replaceTexturesProperty(profile.ProfileProperties, property))
	if err != nil {
		return profile, store.ProfileSkin{}, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error())
	}
	return updated, skinRow, nil
}

func (s *Server) saveUploadedPassportSkin(ctx context.Context, r *http.Request, passport identity.Passport) (identity.Passport, store.PassportSkin, *api.Error) {
	existingSkin, existingErr := s.store.GetPassportSkin(ctx, passport.ID)
	if existingErr != nil && !errors.Is(existingErr, store.ErrNotFound) {
		return passport, store.PassportSkin{}, api.NewError(http.StatusInternalServerError, "skin.load_failed", existingErr.Error())
	}
	skinPNG, skinType, err := readPNGPart(r, "skin", false)
	if err != nil {
		return passport, store.PassportSkin{}, api.NewError(http.StatusBadRequest, "skin.skin_invalid", err.Error())
	}
	if len(skinPNG) == 0 {
		skinPNG = existingSkin.SkinPNG
		skinType = existingSkin.SkinContentType
	}
	if len(skinPNG) == 0 {
		return passport, store.PassportSkin{}, api.NewError(http.StatusBadRequest, "skin.skin_required", "skin file is required before custom texture settings can be saved")
	}
	capePNG, capeType, err := readPNGPart(r, "cape", false)
	if err != nil {
		return passport, store.PassportSkin{}, api.NewError(http.StatusBadRequest, "skin.cape_invalid", err.Error())
	}
	if len(capePNG) == 0 && r.MultipartForm.Value["cape"] == nil {
		capePNG = existingSkin.CapePNG
		capeType = existingSkin.CapeContentType
	}
	elytraPNG, elytraType, err := readPNGPart(r, "elytra", false)
	if err != nil {
		return passport, store.PassportSkin{}, api.NewError(http.StatusBadRequest, "skin.elytra_invalid", err.Error())
	}
	if len(elytraPNG) == 0 && r.MultipartForm.Value["elytra"] == nil {
		elytraPNG = existingSkin.ElytraPNG
		elytraType = existingSkin.ElytraContentType
	}
	skinRow := store.PassportSkin{
		PassportID:        passport.ID,
		Model:             normalizeUploadModel(r.FormValue("model")),
		SkinPNG:           skinPNG,
		SkinContentType:   skinType,
		SkinSHA256:        sha256Hex(skinPNG),
		CapePNG:           capePNG,
		CapeContentType:   capeType,
		CapeSHA256:        sha256Hex(capePNG),
		ElytraPNG:         elytraPNG,
		ElytraContentType: elytraType,
		ElytraSHA256:      sha256Hex(elytraPNG),
	}
	updated, err := s.store.SetPassportSkin(ctx, passport.ID, skinRow)
	if err != nil {
		return passport, store.PassportSkin{}, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error())
	}
	return updated, skinRow, nil
}

func passportSkinSourceFromRequest(passport identity.Passport, req updatePassportSkinSourceRequest) string {
	source := strings.TrimSpace(req.Source)
	if source == "" {
		if req.UseUpstreamSkin {
			source = store.PassportSkinSourceUpstream
		} else {
			source = store.PassportSkinSourceCustom
		}
	}
	return store.NormalizePassportSkinSource(passport.Kind, source)
}

func (s *Server) profileCustomTexturesProperty(profile identity.Profile, skinRow store.ProfileSkin) (identity.ProfileProperty, error) {
	return skinlib.BuildTexturesProperty(profile.UUID, profile.ProtocolName, s.cfg.PublicBaseURL, skinlib.TextureURLs{
		Skin:   "/api/assets/profiles/" + profile.ID + "/skin.png",
		Cape:   optionalProfileAssetURL(profile.ID, "cape.png", skinRow.CapePNG),
		Elytra: optionalProfileAssetURL(profile.ID, "elytra.png", skinRow.ElytraPNG),
		Model:  skinRow.Model,
	})
}

func (s *Server) passportCustomTexturesProperty(passport identity.Passport, skinRow store.PassportSkin) (identity.ProfileProperty, error) {
	return skinlib.BuildTexturesProperty(passport.UUID, passport.Username, s.cfg.PublicBaseURL, skinlib.TextureURLs{
		Skin:   "/api/assets/passports/" + passport.ID + "/skin.png",
		Cape:   optionalPassportAssetURL(passport.ID, "cape.png", skinRow.CapePNG),
		Elytra: optionalPassportAssetURL(passport.ID, "elytra.png", skinRow.ElytraPNG),
		Model:  skinRow.Model,
	})
}

func (s *Server) effectiveSkinForProfile(ctx context.Context, profile identity.Profile, passport *identity.Passport) effectiveSkin {
	if profile.SkinSource == "passport" && passport != nil {
		inherited := s.effectiveSkinForPassport(ctx, *passport)
		inherited.Source = "passport"
		inherited.AvatarURL = "/api/assets/profiles/" + profile.ID + "/avatar.png"
		return inherited
	}
	if custom, err := s.store.GetProfileSkin(ctx, profile.ID); err == nil && len(custom.SkinPNG) > 0 {
		return effectiveSkin{
			Source:          "custom",
			Model:           custom.Model,
			SkinURL:         "/api/assets/profiles/" + profile.ID + "/skin.png",
			CapeURL:         optionalProfileAssetURL(profile.ID, "cape.png", custom.CapePNG),
			ElytraURL:       optionalProfileAssetURL(profile.ID, "elytra.png", custom.ElytraPNG),
			AvatarURL:       "/api/assets/profiles/" + profile.ID + "/avatar.png",
			HasCustomSkin:   true,
			HasCustomCape:   len(custom.CapePNG) > 0,
			HasCustomElytra: len(custom.ElytraPNG) > 0,
			Properties:      append([]identity.ProfileProperty(nil), profile.ProfileProperties...),
		}
	}
	props := s.effectiveProfileProperties(ctx, profile, passport)
	urls := skinlib.TextureURLsFromProperty(props)
	if urls.Skin != "" {
		source := "mojang"
		if passport == nil || passport.Kind != identity.PassportKindPremium {
			source = "textures"
		}
		return effectiveSkin{
			Source:     source,
			Model:      urls.Model,
			SkinURL:    urls.Skin,
			CapeURL:    urls.Cape,
			ElytraURL:  urls.Elytra,
			AvatarURL:  "/api/assets/profiles/" + profile.ID + "/avatar.png",
			Properties: props,
		}
	}
	def := skinlib.DefaultForUUID(profile.UUID)
	return effectiveSkin{
		Source:     "default",
		Model:      def.Model,
		Variant:    def.Name,
		SkinURL:    "/api/assets/default-skins/" + def.Model + "/" + def.Name + ".png",
		AvatarURL:  "/api/assets/profiles/" + profile.ID + "/avatar.png",
		Properties: props,
	}
}

func (s *Server) effectiveSkinForPassport(ctx context.Context, passport identity.Passport) effectiveSkin {
	source := store.NormalizePassportSkinSource(passport.Kind, passport.SkinSource)
	if source == store.PassportSkinSourceCustom {
		if custom, err := s.store.GetPassportSkin(ctx, passport.ID); err == nil && len(custom.SkinPNG) > 0 {
			properties := []identity.ProfileProperty(nil)
			if property, err := s.passportCustomTexturesProperty(passport, custom); err == nil {
				properties = []identity.ProfileProperty{property}
			}
			return effectiveSkin{
				Source:          "custom",
				Model:           custom.Model,
				SkinURL:         "/api/assets/passports/" + passport.ID + "/skin.png",
				CapeURL:         optionalPassportAssetURL(passport.ID, "cape.png", custom.CapePNG),
				ElytraURL:       optionalPassportAssetURL(passport.ID, "elytra.png", custom.ElytraPNG),
				AvatarURL:       "/api/assets/passports/" + passport.ID + "/avatar.png",
				HasCustomSkin:   true,
				HasCustomCape:   len(custom.CapePNG) > 0,
				HasCustomElytra: len(custom.ElytraPNG) > 0,
				Properties:      properties,
			}
		}
	}
	if passport.Kind == identity.PassportKindPremium && source == store.PassportSkinSourceUpstream {
		props := s.store.GetPassportPremiumTextures(ctx, passport.ID)
		if len(props) == 0 {
			if primary, err := s.store.GetPrimaryProfileForPassport(ctx, passport.ID); err == nil {
				props = append([]identity.ProfileProperty(nil), primary.ProfileProperties...)
			}
		}
		if len(props) > 0 {
			urls := skinlib.TextureURLsFromProperty(props)
			model := urls.Model
			if model != "slim" {
				model = "wide"
			}
			if urls.Skin == "" {
				def := skinlib.DefaultForUUID(passport.UUID)
				model = def.Model
				urls.Skin = "/api/assets/default-skins/" + def.Model + "/" + def.Name + ".png"
			}
			return effectiveSkin{
				Source:     "mojang",
				Model:      model,
				SkinURL:    urls.Skin,
				CapeURL:    urls.Cape,
				ElytraURL:  urls.Elytra,
				AvatarURL:  "/api/assets/passports/" + passport.ID + "/avatar.png",
				Properties: props,
			}
		}
	}
	def := skinlib.DefaultForUUID(passport.UUID)
	return effectiveSkin{
		Source:     "default",
		Model:      def.Model,
		Variant:    def.Name,
		SkinURL:    "/api/assets/default-skins/" + def.Model + "/" + def.Name + ".png",
		AvatarURL:  "/api/assets/passports/" + passport.ID + "/avatar.png",
		Properties: nil,
	}
}

func (s *Server) effectiveProfileProperties(ctx context.Context, profile identity.Profile, passport *identity.Passport) []identity.ProfileProperty {
	if profile.SkinSource == "passport" && passport != nil {
		return append([]identity.ProfileProperty(nil), s.effectiveSkinForPassport(ctx, *passport).Properties...)
	}
	if len(profile.ProfileProperties) > 0 {
		return append([]identity.ProfileProperty(nil), profile.ProfileProperties...)
	}
	return nil
}

func (s *Server) effectiveProfilePropertiesByID(ctx context.Context, profileID string) []identity.ProfileProperty {
	profile, err := s.store.GetProfileByID(ctx, profileID)
	if err != nil {
		return nil
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(ctx, profile.ID); err == nil {
		passport = &p
	}
	return s.effectiveProfileProperties(ctx, profile, passport)
}

func (s *Server) serveEffectiveSkinPNG(w http.ResponseWriter, r *http.Request, eff effectiveSkin, fallbackUUID identity.UUID) {
	if eff.HasCustomSkin {
		if raw := customTextureBytesFromURL(r.Context(), s.store, eff.SkinURL, "skin"); len(raw) > 0 {
			writePNG(w, raw)
			return
		}
	}
	if eff.SkinURL != "" && strings.HasPrefix(eff.SkinURL, "http") {
		http.Redirect(w, r, eff.SkinURL, http.StatusFound)
		return
	}
	def := skinlib.DefaultForUUID(fallbackUUID)
	raw, err := skinlib.DefaultSkinPNG(def.Name, def.Model)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writePNG(w, raw)
}

func (s *Server) serveOptionalTexture(w http.ResponseWriter, r *http.Request, url string, raw []byte) {
	if len(raw) > 0 {
		writePNG(w, raw)
		return
	}
	if url != "" && strings.HasPrefix(url, "http") {
		http.Redirect(w, r, url, http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveProfileAvatar(w http.ResponseWriter, r *http.Request, eff effectiveSkin, fallbackUUID identity.UUID) {
	raw := []byte(nil)
	if eff.HasCustomSkin {
		raw = customTextureBytesFromURL(r.Context(), s.store, eff.SkinURL, "skin")
	} else if eff.SkinURL != "" && strings.HasPrefix(eff.SkinURL, "http") {
		raw = fetchRemoteTexture(r.Context(), eff.SkinURL)
	}
	if len(raw) == 0 {
		def := skinlib.DefaultForUUID(fallbackUUID)
		raw, _ = skinlib.DefaultSkinPNG(def.Name, def.Model)
	}
	if len(raw) == 0 {
		http.NotFound(w, r)
		return
	}
	writeAvatar(w, r, raw)
}

func readPNGPart(r *http.Request, field string, required bool) ([]byte, string, error) {
	file, header, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) && !required {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSkinUploadBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 {
		if required {
			return nil, "", errors.New("file is required")
		}
		return nil, "", nil
	}
	if len(raw) > maxSkinUploadBytes {
		return nil, "", errors.New("file is too large")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil {
		return nil, "", errors.New("file must be a PNG image")
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "image/png"
	}
	return raw, contentType, nil
}

func readServerIconDataURI(r *http.Request, field string) (string, error) {
	file, _, err := r.FormFile(field)
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxServerIconUploadBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("file is required")
	}
	if len(raw) > maxServerIconUploadBytes {
		return "", errors.New("file is too large")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("file must be a PNG image")
	}
	if cfg.Width != 64 || cfg.Height != 64 {
		return "", errors.New("server icon must be 64x64 pixels")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func writePNG(w http.ResponseWriter, raw []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func writeAvatar(w http.ResponseWriter, r *http.Request, skinPNG []byte) {
	avatar, err := skinlib.AvatarPNG(skinPNG, 8)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writePNG(w, avatar)
}

func fetchRemoteTexture(ctx context.Context, url string) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxSkinUploadBytes+1))
	if err != nil || len(raw) > maxSkinUploadBytes {
		return nil
	}
	return raw
}

func replaceTexturesProperty(properties []identity.ProfileProperty, replacement identity.ProfileProperty) []identity.ProfileProperty {
	out := make([]identity.ProfileProperty, 0, len(properties)+1)
	added := false
	for _, property := range properties {
		if property.Name == "textures" {
			if !added {
				out = append(out, replacement)
				added = true
			}
			continue
		}
		out = append(out, property)
	}
	if !added {
		out = append(out, replacement)
	}
	return out
}

func removeTexturesProperty(properties []identity.ProfileProperty) []identity.ProfileProperty {
	out := make([]identity.ProfileProperty, 0, len(properties))
	for _, property := range properties {
		if property.Name != "textures" {
			out = append(out, property)
		}
	}
	return out
}

func customSkinBytes(ctx context.Context, st store.PlayerStore, profileID string) ([]byte, string, bool) {
	skinRow, err := st.GetProfileSkin(ctx, profileID)
	if err != nil || len(skinRow.SkinPNG) == 0 {
		return nil, "", false
	}
	return skinRow.SkinPNG, skinRow.SkinContentType, true
}

func customTextureBytesFromURL(ctx context.Context, st store.PlayerStore, url string, asset string) []byte {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "/api/assets/profiles/") {
		profileID := strings.TrimSuffix(strings.TrimPrefix(url, "/api/assets/profiles/"), "/"+asset+".png")
		skinRow, err := st.GetProfileSkin(ctx, profileID)
		if err != nil {
			return nil
		}
		switch asset {
		case "skin":
			return skinRow.SkinPNG
		case "cape":
			return skinRow.CapePNG
		case "elytra":
			return skinRow.ElytraPNG
		}
	}
	if strings.HasPrefix(url, "/api/assets/passports/") {
		passportID := strings.TrimSuffix(strings.TrimPrefix(url, "/api/assets/passports/"), "/"+asset+".png")
		skinRow, err := st.GetPassportSkin(ctx, passportID)
		if err != nil {
			return nil
		}
		switch asset {
		case "skin":
			return skinRow.SkinPNG
		case "cape":
			return skinRow.CapePNG
		case "elytra":
			return skinRow.ElytraPNG
		}
	}
	return nil
}

func optionalProfileAssetURL(profileID string, asset string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return "/api/assets/profiles/" + profileID + "/" + asset
}

func optionalPassportAssetURL(passportID string, asset string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return "/api/assets/passports/" + passportID + "/" + asset
}

func sha256Hex(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeUploadModel(model string) string {
	if strings.EqualFold(strings.TrimSpace(model), "slim") {
		return "slim"
	}
	return "wide"
}
