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
		"connected_servers":         []map[string]any{},
	}
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
		"last_seen_at":           nil,
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
