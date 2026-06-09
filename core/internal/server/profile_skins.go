package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/png"
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
	skinPNG, skinType, err := readPNGPart(r, "skin", true)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.skin_invalid", err.Error()))
		return
	}
	capePNG, capeType, err := readPNGPart(r, "cape", false)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.cape_invalid", err.Error()))
		return
	}
	elytraPNG, elytraType, err := readPNGPart(r, "elytra", false)
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusBadRequest, "skin.elytra_invalid", err.Error()))
		return
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
	property, err := skinlib.BuildTexturesProperty(profile.UUID, profile.ProtocolName, s.cfg.PublicBaseURL, skinlib.TextureURLs{
		Skin:   "/api/assets/profiles/" + profile.ID + "/skin.png",
		Cape:   optionalAssetURL(profile.ID, "cape.png", capePNG),
		Elytra: optionalAssetURL(profile.ID, "elytra.png", elytraPNG),
		Model:  model,
	})
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.textures_failed", err.Error()))
		return
	}
	profile, err = s.store.SetProfileSkin(r.Context(), profile.ID, skinRow, replaceTexturesProperty(profile.ProfileProperties, property))
	if err != nil {
		api.WriteError(w, api.NewError(http.StatusInternalServerError, "skin.save_failed", err.Error()))
		return
	}
	var passport *identity.Passport
	if p, err := s.store.GetPassportForProfile(r.Context(), profile.ID); err == nil {
		passport = &p
	}
	s.audit(r, audit.ActorAdmin, session.SubjectID, audit.TargetPlayer, profile.ID, "profile.skin.update", map[string]any{
		"model":       model,
		"skin_sha256": skinRow.SkinSHA256,
		"cape":        len(capePNG) > 0,
		"elytra":      len(elytraPNG) > 0,
	})
	api.WriteJSON(w, http.StatusOK, s.profileSkinData(r.Context(), profile, passport), nil)
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
		s.serveOptionalTexture(w, r, eff.CapeURL, customCapeBytes(r.Context(), s.store, profile.ID))
	case "elytra.png":
		s.serveOptionalTexture(w, r, eff.ElytraURL, customElytraBytes(r.Context(), s.store, profile.ID))
	case "avatar.png":
		s.serveProfileAvatar(w, r, eff, profile.UUID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handlePassportAvatarAsset(w http.ResponseWriter, r *http.Request) {
	passport, err := s.store.GetPassportByID(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if profile, err := s.store.GetPrimaryProfileForPassport(r.Context(), passport.ID); err == nil {
		eff := s.effectiveSkinForProfile(r.Context(), profile, &passport)
		s.serveProfileAvatar(w, r, eff, profile.UUID)
		return
	}
	def := skinlib.DefaultForUUID(passport.UUID)
	raw, err := skinlib.DefaultSkinPNG(def.Name, def.Model)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeAvatar(w, r, raw)
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
	var updatedAt *time.Time
	if custom, err := s.store.GetProfileSkin(ctx, profile.ID); err == nil {
		updatedAt = &custom.UpdatedAt
	}
	return map[string]any{
		"source":            profile.SkinSource,
		"effective_source":  eff.Source,
		"model":             eff.Model,
		"default_variant":   defaultSkin.Name,
		"default_model":     defaultSkin.Model,
		"skin_url":          eff.SkinURL,
		"cape_url":          emptyStringNil(eff.CapeURL),
		"elytra_url":        emptyStringNil(eff.ElytraURL),
		"avatar_url":        eff.AvatarURL,
		"has_custom_skin":   eff.HasCustomSkin,
		"has_custom_cape":   eff.HasCustomCape,
		"has_custom_elytra": eff.HasCustomElytra,
		"updated_at":        updatedAt,
	}
}

func (s *Server) effectiveSkinForProfile(ctx context.Context, profile identity.Profile, passport *identity.Passport) effectiveSkin {
	if custom, err := s.store.GetProfileSkin(ctx, profile.ID); err == nil && len(custom.SkinPNG) > 0 {
		return effectiveSkin{
			Source:          "custom",
			Model:           custom.Model,
			SkinURL:         "/api/assets/profiles/" + profile.ID + "/skin.png",
			CapeURL:         optionalAssetURL(profile.ID, "cape.png", custom.CapePNG),
			ElytraURL:       optionalAssetURL(profile.ID, "elytra.png", custom.ElytraPNG),
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

func (s *Server) effectiveProfileProperties(ctx context.Context, profile identity.Profile, passport *identity.Passport) []identity.ProfileProperty {
	if len(profile.ProfileProperties) > 0 {
		return append([]identity.ProfileProperty(nil), profile.ProfileProperties...)
	}
	if passport != nil && passport.Kind == identity.PassportKindPremium {
		if primary, err := s.store.GetPrimaryProfileForPassport(ctx, passport.ID); err == nil && len(primary.ProfileProperties) > 0 {
			return append([]identity.ProfileProperty(nil), primary.ProfileProperties...)
		}
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
		if raw, _, ok := customSkinBytes(r.Context(), s.store, strings.TrimSuffix(strings.TrimPrefix(eff.SkinURL, "/api/assets/profiles/"), "/skin.png")); ok {
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
		raw, _, _ = customSkinBytes(r.Context(), s.store, strings.TrimSuffix(strings.TrimPrefix(eff.SkinURL, "/api/assets/profiles/"), "/skin.png"))
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

func customCapeBytes(ctx context.Context, st store.PlayerStore, profileID string) []byte {
	skinRow, err := st.GetProfileSkin(ctx, profileID)
	if err != nil {
		return nil
	}
	return skinRow.CapePNG
}

func customElytraBytes(ctx context.Context, st store.PlayerStore, profileID string) []byte {
	skinRow, err := st.GetProfileSkin(ctx, profileID)
	if err != nil {
		return nil
	}
	return skinRow.ElytraPNG
}

func optionalAssetURL(profileID string, asset string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return "/api/assets/profiles/" + profileID + "/" + asset
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
