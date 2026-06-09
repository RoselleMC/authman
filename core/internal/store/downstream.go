package store

import (
	"strconv"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/extensions"
)

const (
	DefaultTransferGrantTTLSeconds = 45
	DefaultDownstreamPort          = 25565
)

type DownstreamTarget struct {
	ServerID             string
	Slug                 string
	DisplayName          string
	Status               string
	Host                 string
	Port                 int
	TransferHost         string
	TransferPort         int
	MOTD                 string
	GateEnabled          bool
	GrantTTLSeconds      int
	AllowedPortalSources []string
	RegistrationOpen     bool
	ExtensionProviders   []string
}

func defaultDownstreamServer(now time.Time) DownstreamServer {
	return DownstreamServer{
		ID:               "default",
		Slug:             "default",
		DisplayName:      "Default Server",
		Status:           "active",
		RegistrationOpen: true,
		PortalTheme:      map[string]any{},
		PortalConfig: map[string]any{
			"registration_strategy":  "open",
			"show_in_global":         true,
			"host":                   "127.0.0.1",
			"port":                   DefaultDownstreamPort,
			"transfer_host":          "127.0.0.1",
			"transfer_port":          DefaultDownstreamPort,
			"motd":                   "Welcome to Authman",
			"gate_enabled":           true,
			"grant_ttl_seconds":      DefaultTransferGrantTTLSeconds,
			"allowed_portal_sources": []string{},
			"portal_hosts":           []string{},
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
	server.PortalTheme = map[string]any{}
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
	if _, ok := server.PortalConfig["host"]; !ok {
		server.PortalConfig["host"] = "127.0.0.1"
	}
	if _, ok := server.PortalConfig["port"]; !ok {
		server.PortalConfig["port"] = DefaultDownstreamPort
	}
	if _, ok := server.PortalConfig["transfer_host"]; !ok {
		server.PortalConfig["transfer_host"] = stringFromAny(server.PortalConfig["host"], "127.0.0.1")
	}
	if _, ok := server.PortalConfig["transfer_port"]; !ok {
		server.PortalConfig["transfer_port"] = intFromAny(server.PortalConfig["port"], DefaultDownstreamPort)
	}
	if _, ok := server.PortalConfig["motd"]; !ok {
		server.PortalConfig["motd"] = server.DisplayName
	}
	if _, ok := server.PortalConfig["gate_enabled"]; !ok {
		if _, ok := server.PortalConfig["grant_required"]; ok {
			server.PortalConfig["gate_enabled"] = boolFromAny(server.PortalConfig["grant_required"], true)
		} else {
			server.PortalConfig["gate_enabled"] = true
		}
	}
	if _, ok := server.PortalConfig["grant_required"]; !ok {
		server.PortalConfig["grant_required"] = boolFromAny(server.PortalConfig["gate_enabled"], true)
	}
	if _, ok := server.PortalConfig["grant_ttl_seconds"]; !ok {
		server.PortalConfig["grant_ttl_seconds"] = DefaultTransferGrantTTLSeconds
	}
	if _, ok := server.PortalConfig["allowed_portal_sources"]; !ok {
		server.PortalConfig["allowed_portal_sources"] = []string{}
	}
	if _, ok := server.PortalConfig["portal_hosts"]; !ok {
		server.PortalConfig["portal_hosts"] = []string{}
	}
	if _, ok := server.PortalConfig["limbo_blueprint_id"]; !ok {
		server.PortalConfig["limbo_blueprint_id"] = ""
	}
	if server.ExtensionProviders == nil {
		server.ExtensionProviders = []string{}
	}
	return server
}

func DownstreamTargetFromServer(server DownstreamServer) DownstreamTarget {
	server = normalizeDownstreamServer(server)
	host := strings.TrimSpace(stringFromAny(server.PortalConfig["host"], "127.0.0.1"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := intFromAny(server.PortalConfig["port"], DefaultDownstreamPort)
	if port <= 0 || port > 65535 {
		port = DefaultDownstreamPort
	}
	transferHost := strings.TrimSpace(stringFromAny(server.PortalConfig["transfer_host"], host))
	if transferHost == "" {
		transferHost = host
	}
	transferPort := intFromAny(server.PortalConfig["transfer_port"], port)
	if transferPort <= 0 || transferPort > 65535 {
		transferPort = port
	}
	ttl := intFromAny(server.PortalConfig["grant_ttl_seconds"], DefaultTransferGrantTTLSeconds)
	if ttl < 5 {
		ttl = 5
	}
	if ttl > 300 {
		ttl = 300
	}
	motd := strings.TrimSpace(stringFromAny(server.PortalConfig["motd"], server.DisplayName))
	if motd == "" {
		motd = server.DisplayName
	}
	return DownstreamTarget{
		ServerID:             server.ID,
		Slug:                 server.Slug,
		DisplayName:          server.DisplayName,
		Status:               server.Status,
		Host:                 host,
		Port:                 port,
		TransferHost:         transferHost,
		TransferPort:         transferPort,
		MOTD:                 motd,
		GateEnabled:          boolFromAny(server.PortalConfig["grant_required"], boolFromAny(server.PortalConfig["gate_enabled"], true)),
		GrantTTLSeconds:      ttl,
		AllowedPortalSources: stringSliceFromAny(server.PortalConfig["allowed_portal_sources"]),
		RegistrationOpen:     server.RegistrationOpen,
		ExtensionProviders:   append([]string(nil), server.ExtensionProviders...),
	}
}

func DownstreamTargetData(target DownstreamTarget) map[string]any {
	return map[string]any{
		"server_id":              target.ServerID,
		"slug":                   target.Slug,
		"display_name":           target.DisplayName,
		"status":                 target.Status,
		"host":                   target.Host,
		"port":                   target.Port,
		"transfer_host":          target.TransferHost,
		"transfer_port":          target.TransferPort,
		"motd":                   target.MOTD,
		"grant_required":         target.GateEnabled,
		"gate_enabled":           target.GateEnabled,
		"grant_ttl_seconds":      target.GrantTTLSeconds,
		"allowed_portal_sources": target.AllowedPortalSources,
		"registration_open":      target.RegistrationOpen,
		"extension_providers":    target.ExtensionProviders,
	}
}

func cloneLimboBlueprint(blueprint LimboBlueprint) LimboBlueprint {
	blueprint.Schematic = append([]byte(nil), blueprint.Schematic...)
	blueprint.Preview = cloneMap(blueprint.Preview)
	blueprint.Config = cloneMap(blueprint.Config)
	return blueprint
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

func stringFromAny(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	default:
		return fallback
	}
}

func intFromAny(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func boolFromAny(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1":
			return true
		case "false", "no", "0":
			return false
		}
	}
	return fallback
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return []string{}
		}
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return []string{}
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func cloneExtensionPlayerData(data ExtensionPlayerData) ExtensionPlayerData {
	data.Values = cloneMap(data.Values)
	data.Schema.Fields = append([]extensions.Field(nil), data.Schema.Fields...)
	return data
}
