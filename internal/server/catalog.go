package server

import (
	"context"
	"time"

	"github.com/RoselleMC/authman/internal/extensions"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/store"
)

func portalPlayerData(player identity.Player) map[string]any {
	return map[string]any{
		"id":                        player.ID,
		"uuid":                      player.UUID.String(),
		"raw_name":                  player.RawOfflineName,
		"raw_offline_name":          player.RawOfflineName,
		"protocol_name":             player.ProtocolName,
		"kind":                      player.Kind,
		"registration_server_label": emptyStringNil(player.RegistrationServer),
		"last_seen_server_label":    emptyStringNil(player.LastSeenServer),
		"last_seen_at":              player.LastSeenAt,
		"last_seen_ip":              emptyStringNil(player.LastSeenIP),
		"last_seen_geo":             ipGeoData(player.LastSeenGeo),
		"connected_servers":         []map[string]any{},
	}
}

func passportRowData(passport identity.Passport, profiles []identity.Profile, presences []store.PlayerPresence) map[string]any {
	return map[string]any{
		"id":                  passport.ID,
		"kind":                passport.Kind,
		"uuid":                passport.UUID.String(),
		"uuid_compact":        passport.UUID.Compact(),
		"username":            passport.Username,
		"avatar_url":          "/api/assets/passports/" + passport.ID + "/avatar.png",
		"username_normalized": passport.UsernameNormalized,
		"raw_offline_name":    passport.RawOfflineName,
		"status":              passport.Status,
		"profile_count":       len(profiles),
		"online":              len(presences) > 0,
		"presence_count":      len(presences),
		"primary_profile":     primaryProfileSummary(profiles),
		"registration_server": emptyStringNil(passport.RegistrationServer),
		"last_seen_server":    emptyStringNil(passport.LastSeenServer),
		"last_seen_at":        passport.LastSeenAt,
		"last_seen_ip":        emptyStringNil(passport.LastSeenIP),
		"last_seen_geo":       ipGeoData(passport.LastSeenGeo),
		"active_ban":          nil,
		"ban_expires_at":      nil,
		"locked_until":        nil,
		"created_at":          passport.CreatedAt,
		"updated_at":          passport.UpdatedAt,
	}
}

func passportDetailData(passport identity.Passport, profiles []identity.Profile, credential *store.PassportCredential, presences []store.PlayerPresence, bans []store.PlayerBan, profileBans map[string]store.PlayerBan, events []map[string]any) map[string]any {
	data := passportRowData(passport, profiles, presences)
	if credential != nil {
		data["locked_until"] = credential.LockedUntil
	}
	if ban, ok := firstActiveBan(bans, time.Now()); ok {
		row := banRows([]store.PlayerBan{ban})[0]
		data["active_ban"] = row
		data["ban_expires_at"] = ban.ExpiresAt
	}
	profileRows := make([]map[string]any, 0, len(profiles))
	passportBan, passportBanned := firstActiveBan(bans, time.Now())
	for _, profile := range profiles {
		row := profileSummaryData(profile, presencesForProfile(presences, profile.ID))
		if credential != nil {
			row["locked_until"] = credential.LockedUntil
		}
		if ban, ok := profileBans[profile.ID]; ok {
			row["active_ban"] = banRows([]store.PlayerBan{ban})[0]
			row["ban_expires_at"] = ban.ExpiresAt
		} else if passportBanned {
			row["active_ban"] = banRows([]store.PlayerBan{passportBan})[0]
			row["ban_expires_at"] = passportBan.ExpiresAt
		}
		profileRows = append(profileRows, row)
	}
	data["profiles"] = profileRows
	data["presences"] = presenceRows(presences)
	data["bans"] = banRows(bans)
	data["credential"] = nil
	if credential != nil {
		data["credential"] = map[string]any{
			"password_updated_at": credential.PasswordUpdatedAt,
			"failed_attempts":     credential.FailedAttempts,
			"locked_until":        credential.LockedUntil,
		}
	}
	data["audit_events"] = events
	return data
}

func profileRowData(profile identity.Profile, passport *identity.Passport, presences []store.PlayerPresence) map[string]any {
	data := profileSummaryData(profile, presences)
	data["uuid_compact"] = profile.UUID.Compact()
	data["display_name"] = profile.DisplayName
	data["status"] = profile.Status
	data["skin_source"] = profile.SkinSource
	data["last_seen_server"] = emptyStringNil(profile.LastSeenServer)
	data["last_seen_at"] = profile.LastSeenAt
	data["last_seen_ip"] = emptyStringNil(profile.LastSeenIP)
	data["last_seen_geo"] = ipGeoData(profile.LastSeenGeo)
	data["active_ban"] = nil
	data["ban_expires_at"] = nil
	data["locked_until"] = nil
	data["created_at"] = profile.CreatedAt
	data["updated_at"] = profile.UpdatedAt
	data["passport"] = nil
	if passport != nil {
		data["passport"] = map[string]any{
			"id":       passport.ID,
			"kind":     passport.Kind,
			"username": passport.Username,
			"status":   passport.Status,
		}
	}
	return data
}

func profileDetailData(profile identity.Profile, passport *identity.Passport, presences []store.PlayerPresence, bans []store.PlayerBan, events []map[string]any) map[string]any {
	data := profileRowData(profile, passport, presences)
	data["properties"] = profilePropertiesData(profile.ProfileProperties)
	data["presences"] = presenceRows(presences)
	data["bans"] = banRows(bans)
	data["audit_events"] = events
	data["extension_data"] = []map[string]any{}
	return data
}

func primaryProfileSummary(profiles []identity.Profile) any {
	if len(profiles) == 0 {
		return nil
	}
	return profileSummaryData(profiles[0], nil)
}

func profileSummaryData(profile identity.Profile, presences []store.PlayerPresence) map[string]any {
	return map[string]any{
		"id":              profile.ID,
		"uuid":            profile.UUID.String(),
		"avatar_url":      "/api/assets/profiles/" + profile.ID + "/avatar.png",
		"protocol_name":   profile.ProtocolName,
		"normalized_name": profile.NormalizedName,
		"display_name":    profile.DisplayName,
		"status":          profile.Status,
		"online":          len(presences) > 0,
		"presence_count":  len(presences),
		"last_seen_ip":    emptyStringNil(profile.LastSeenIP),
		"last_seen_geo":   ipGeoData(profile.LastSeenGeo),
		"active_ban":      nil,
		"ban_expires_at":  nil,
		"locked_until":    nil,
	}
}

func presencesForProfile(presences []store.PlayerPresence, profileID string) []store.PlayerPresence {
	out := []store.PlayerPresence{}
	for _, presence := range presences {
		if presence.ProfileID == profileID {
			out = append(out, presence)
		}
	}
	return out
}

func presenceRows(presences []store.PlayerPresence) []map[string]any {
	out := make([]map[string]any, 0, len(presences))
	for _, presence := range presences {
		out = append(out, map[string]any{
			"id":            presence.ID,
			"passport_id":   presence.PassportID,
			"profile_id":    presence.ProfileID,
			"server_id":     presence.ServerID,
			"node_id":       presence.NodeID,
			"protocol_name": presence.ProtocolName,
			"uuid":          presence.UUID,
			"remote_addr":   presence.RemoteAddr,
			"connected_at":  presence.ConnectedAt,
			"last_seen_at":  presence.LastSeenAt,
		})
	}
	return out
}

func nodeActionRows(actions []store.NodeAction) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, map[string]any{
			"id":            action.ID,
			"type":          action.Type,
			"presence_id":   action.PresenceID,
			"passport_id":   action.PassportID,
			"profile_id":    action.ProfileID,
			"uuid":          action.UUID,
			"protocol_name": action.ProtocolName,
			"reason":        action.Reason,
			"created_at":    action.CreatedAt,
			"expires_at":    action.ExpiresAt,
		})
	}
	return out
}

func banRows(bans []store.PlayerBan) []map[string]any {
	out := make([]map[string]any, 0, len(bans))
	for _, ban := range bans {
		out = append(out, map[string]any{
			"id":            ban.ID,
			"scope":         ban.Scope,
			"target_id":     ban.TargetID,
			"reason":        ban.Reason,
			"created_by":    ban.CreatedBy,
			"created_at":    ban.CreatedAt,
			"expires_at":    ban.ExpiresAt,
			"revoked_by":    ban.RevokedBy,
			"revoked_at":    ban.RevokedAt,
			"revoke_reason": ban.RevokeReason,
		})
	}
	return out
}

func firstActiveBan(bans []store.PlayerBan, now time.Time) (store.PlayerBan, bool) {
	for _, ban := range bans {
		if ban.RevokedAt != nil {
			continue
		}
		if ban.ExpiresAt == nil || ban.ExpiresAt.After(now.UTC()) {
			return ban, true
		}
	}
	return store.PlayerBan{}, false
}

func ipGeoData(geo *identity.IPGeo) any {
	if geo == nil {
		return nil
	}
	return geo
}

func playerRowData(player identity.Player) map[string]any {
	status := "active"
	if player.Locked {
		status = "locked"
	}
	rawName := player.RawOfflineName
	if rawName == "" {
		rawName = player.ProtocolName
	}
	return map[string]any{
		"id":                     player.ID,
		"uuid":                   player.UUID.String(),
		"raw_name":               rawName,
		"raw_offline_name":       player.RawOfflineName,
		"protocol_name":          player.ProtocolName,
		"kind":                   player.Kind,
		"status":                 status,
		"locked":                 player.Locked,
		"last_seen_at":           player.LastSeenAt,
		"last_seen_server_label": emptyStringNil(player.LastSeenServer),
	}
}

func playerDetailData(player identity.Player, events []map[string]any) map[string]any {
	row := playerRowData(player)
	row["registration_server_label"] = emptyStringNil(player.RegistrationServer)
	row["created_at"] = time.Now().UTC().Format(time.RFC3339)
	row["profile"] = map[string]any{
		"skin_source": "none",
		"properties":  []map[string]any{},
	}
	if player.Kind == identity.PlayerKindOffline {
		row["offline_credentials"] = map[string]any{
			"password_updated_at": nil,
			"failed_attempts":     0,
			"locked_until":        nil,
		}
	} else {
		row["offline_credentials"] = nil
	}
	row["identities"] = []map[string]any{}
	row["sessions"] = []map[string]any{}
	row["audit_events"] = events
	row["extension_data"] = []map[string]any{}
	return row
}

func emptyStringNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func portalServerListData(server store.DownstreamServer) map[string]any {
	theme := server.PortalTheme
	return map[string]any{
		"slug":              server.Slug,
		"display_name":      server.DisplayName,
		"description":       stringFromMap(theme, "description"),
		"primary_color":     stringFromMap(theme, "primary_color"),
		"accent_color":      stringFromMap(theme, "accent_color"),
		"portal_message":    stringFromMap(theme, "portal_message"),
		"registration_open": server.RegistrationOpen,
		"prefer_dark":       boolFromMap(server.PortalConfig, "prefer_dark"),
	}
}

func portalServerConfigData(server store.DownstreamServer) map[string]any {
	data := portalServerListData(server)
	data["current_context"] = true
	data["portal_config"] = server.PortalConfig
	data["extension_providers"] = server.ExtensionProviders
	data["target"] = store.DownstreamTargetData(store.DownstreamTargetFromServer(server))
	return data
}

func downstreamServerData(server store.DownstreamServer) map[string]any {
	return map[string]any{
		"id":                  server.ID,
		"slug":                server.Slug,
		"display_name":        server.DisplayName,
		"status":              server.Status,
		"registration_open":   server.RegistrationOpen,
		"portal_theme":        server.PortalTheme,
		"portal_config":       server.PortalConfig,
		"target":              store.DownstreamTargetData(store.DownstreamTargetFromServer(server)),
		"extension_providers": server.ExtensionProviders,
		"created_at":          server.CreatedAt,
		"updated_at":          server.UpdatedAt,
	}
}

func stringFromMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func boolFromMap(values map[string]any, key string) bool {
	if value, ok := values[key].(bool); ok {
		return value
	}
	return false
}

func (s *Server) playerExtensionData(ctx context.Context, player identity.Player, serverSlug string, includePrivate bool) []extensions.PlayerData {
	rows := s.extensions.PlayerData(ctx, player, serverSlug)
	for _, row := range s.store.ListExtensionPlayerData(ctx, player.ID, serverSlug, includePrivate) {
		serverDisplay := row.ServerID
		serverSlug := row.ServerID
		if server, err := s.store.GetDownstreamServer(ctx, row.ServerID); err == nil {
			serverDisplay = server.DisplayName
			serverSlug = server.Slug
		}
		rows = append(rows, extensions.PlayerData{
			ServerSlug:        serverSlug,
			ServerDisplayName: serverDisplay,
			Provider:          row.Provider,
			Schema:            row.Schema,
			Values:            row.Values,
			UpdatedAt:         row.UpdatedAt.Format(time.RFC3339),
		})
	}
	return rows
}

func credentialLocked(credential store.OfflineCredential, now time.Time) bool {
	return credential.LockedUntil != nil && credential.LockedUntil.After(now.UTC())
}

func passportCredentialLocked(credential store.PassportCredential, now time.Time) bool {
	return credential.LockedUntil != nil && credential.LockedUntil.After(now.UTC())
}

func offlineCredentialData(credential store.OfflineCredential) map[string]any {
	return map[string]any{
		"password_updated_at": credential.PasswordUpdatedAt,
		"failed_attempts":     credential.FailedAttempts,
		"locked_until":        credential.LockedUntil,
	}
}

func defaultExtensions() []map[string]any {
	return []map[string]any{
		{
			"id":          "authman.identity",
			"name":        "Authman Identity",
			"version":     "builtin",
			"enabled":     true,
			"surface":     []string{"admin", "player"},
			"description": "Built-in identity and account state provider",
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":          map[string]any{"type": "string"},
					"protocol_name": map[string]any{"type": "string"},
					"uuid":          map[string]any{"type": "string"},
				},
			},
		},
	}
}

func extensionRegistryData() []map[string]any {
	return []map[string]any{
		{
			"provider":       "authman.identity",
			"title":          "Authman Identity",
			"visibility":     "player_visible",
			"last_update":    nil,
			"preview_values": map[string]any{"kind": "offline"},
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":          map[string]any{"type": "string", "title": "Account type"},
					"protocol_name": map[string]any{"type": "string", "title": "Protocol name"},
					"uuid":          map[string]any{"type": "string", "title": "UUID"},
				},
			},
		},
	}
}
