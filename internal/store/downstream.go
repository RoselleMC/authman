package store

import (
	"time"

	"github.com/RoselleMC/authman/internal/extensions"
)

func defaultDownstreamServer(now time.Time) DownstreamServer {
	return DownstreamServer{
		ID:               "default",
		Slug:             "default",
		DisplayName:      "Default Server",
		Status:           "active",
		RegistrationOpen: true,
		PortalTheme: map[string]any{
			"primary_color":  "#16a34a",
			"accent_color":   "#2563eb",
			"portal_message": "Welcome to Authman",
			"display_name":   "Default Server",
			"description":    "Default Authman downstream context",
		},
		PortalConfig: map[string]any{
			"registration_strategy": "open",
			"show_in_global":        true,
		},
		ExtensionProviders: []string{"authman.identity"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func normalizeDownstreamServer(server DownstreamServer) DownstreamServer {
	if server.Slug == "" {
		server.Slug = server.ID
	}
	if server.DisplayName == "" {
		server.DisplayName = server.Slug
	}
	if server.Status == "" {
		server.Status = "active"
	}
	if server.PortalTheme == nil {
		server.PortalTheme = map[string]any{}
	}
	if _, ok := server.PortalTheme["primary_color"]; !ok {
		server.PortalTheme["primary_color"] = "#16a34a"
	}
	if _, ok := server.PortalTheme["accent_color"]; !ok {
		server.PortalTheme["accent_color"] = "#2563eb"
	}
	if _, ok := server.PortalTheme["display_name"]; !ok {
		server.PortalTheme["display_name"] = server.DisplayName
	}
	if server.PortalConfig == nil {
		server.PortalConfig = map[string]any{}
	}
	if _, ok := server.PortalConfig["registration_strategy"]; !ok {
		if server.RegistrationOpen {
			server.PortalConfig["registration_strategy"] = "open"
		} else {
			server.PortalConfig["registration_strategy"] = "closed"
		}
	}
	if _, ok := server.PortalConfig["show_in_global"]; !ok {
		server.PortalConfig["show_in_global"] = true
	}
	if server.ExtensionProviders == nil {
		server.ExtensionProviders = []string{}
	}
	return server
}

func cloneDownstreamServer(server DownstreamServer) DownstreamServer {
	server.PortalTheme = cloneMap(server.PortalTheme)
	server.PortalConfig = cloneMap(server.PortalConfig)
	server.ExtensionProviders = append([]string(nil), server.ExtensionProviders...)
	return server
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func cloneExtensionPlayerData(data ExtensionPlayerData) ExtensionPlayerData {
	data.Values = cloneMap(data.Values)
	data.Schema.Fields = append([]extensions.Field(nil), data.Schema.Fields...)
	return data
}
