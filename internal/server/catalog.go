package server

import (
	"time"

	"github.com/RoselleMC/authman/internal/identity"
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

func defaultPortalTheme() map[string]any {
	return map[string]any{
		"id":                 "authman-default",
		"name":               "Authman Default",
		"color_scheme":       "green",
		"supports_dark_mode": true,
		"tokens": map[string]any{
			"primary": "#16a34a",
			"info":    "#2563eb",
			"warning": "#d97706",
			"danger":  "#dc2626",
		},
	}
}

func defaultDownstreamServers() []map[string]any {
	return []map[string]any{
		{
			"id":           "default",
			"slug":         "default",
			"display_name": "Default Server",
			"public":       true,
			"registration": map[string]any{
				"offline_enabled": true,
				"strategy":        "open",
			},
			"theme": defaultPortalTheme(),
		},
	}
}

func portalServersData() []map[string]any {
	return []map[string]any{
		{
			"slug":              "default",
			"display_name":      "Default Server",
			"description":       "Default Authman downstream context",
			"primary_color":     "#16a34a",
			"accent_color":      "#2563eb",
			"portal_message":    "Welcome to Authman",
			"registration_open": true,
			"prefer_dark":       false,
		},
	}
}

func portalServerData(slug string) (map[string]any, bool) {
	for _, server := range portalServersData() {
		if server["slug"] == slug {
			server["current_context"] = true
			return server, true
		}
	}
	return nil, false
}

func downstreamServerData() []map[string]any {
	return []map[string]any{
		{
			"id":                "default",
			"slug":              "default",
			"display_name":      "Default Server",
			"status":            "active",
			"registration_open": true,
			"portal_theme": map[string]any{
				"primary_color":  "#16a34a",
				"accent_color":   "#2563eb",
				"portal_message": "Welcome to Authman",
				"display_name":   "Default Server",
				"description":    "Default Authman downstream context",
			},
			"portal_config": map[string]any{
				"registration_strategy": "open",
				"show_in_global":        true,
			},
			"extension_providers": []string{"authman.identity"},
		},
	}
}

func downstreamServerByID(id string) (map[string]any, bool) {
	for _, server := range downstreamServerData() {
		if server["id"] == id || server["slug"] == id {
			return server, true
		}
	}
	return nil, false
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
