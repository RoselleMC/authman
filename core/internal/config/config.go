package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/mojang"
)

type Config struct {
	HTTPAddr                  string
	ExternalHTTPAddr          string
	DatabaseURL               string
	PublicBaseURL             string
	HTTPBasePath              string
	WebRoot                   string
	DefaultLocale             string
	AdminUsername             string
	AdminEmail                string
	AdminPassword             string
	AdminPasswordHash         string
	NodeAccessToken           string
	CORSAllowedOrigins        []string
	MojangSessionURL          string
	MojangProfileURL          string
	MojangRoutes              []mojang.Route
	MojangTimeout             time.Duration
	MojangCooldown            time.Duration
	MojangCacheFresh          time.Duration
	MojangCacheStale          time.Duration
	LogLevel                  slog.Level
	ShutdownTimeout           time.Duration
	PresenceReconcileInterval time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                  envString("AUTHMAN_HTTP_ADDR", ":8080"),
		ExternalHTTPAddr:          envString("AUTHMAN_EXTERNAL_HTTP_ADDR", ""),
		DatabaseURL:               envString("AUTHMAN_DATABASE_URL", ""),
		PublicBaseURL:             envString("AUTHMAN_PUBLIC_BASE_URL", "http://localhost:8080"),
		HTTPBasePath:              httpBasePath(),
		WebRoot:                   envString("AUTHMAN_CORE_WEB_ROOT", ""),
		DefaultLocale:             envString("AUTHMAN_DEFAULT_LOCALE", "zh"),
		AdminUsername:             envString("AUTHMAN_ADMIN_USERNAME", "admin"),
		AdminEmail:                envString("AUTHMAN_ADMIN_EMAIL", ""),
		AdminPassword:             envString("AUTHMAN_ADMIN_PASSWORD", ""),
		AdminPasswordHash:         envString("AUTHMAN_ADMIN_PASSWORD_HASH", ""),
		NodeAccessToken:           envString("AUTHMAN_NODE_ACCESS_TOKEN", ""),
		CORSAllowedOrigins:        envCSV("AUTHMAN_CORS_ALLOWED_ORIGINS"),
		MojangSessionURL:          envString("AUTHMAN_MOJANG_SESSION_URL", mojang.DefaultSessionServerURL),
		MojangProfileURL:          envString("AUTHMAN_MOJANG_PROFILE_URL", mojang.DefaultProfileAPIURL),
		MojangRoutes:              mojangRoutes(),
		MojangTimeout:             envDuration("AUTHMAN_MOJANG_TIMEOUT", 5*time.Second),
		MojangCooldown:            envDuration("AUTHMAN_MOJANG_FAILURE_COOLDOWN", 30*time.Second),
		MojangCacheFresh:          envDuration("AUTHMAN_MOJANG_CACHE_FRESH_TTL", 30*time.Second),
		MojangCacheStale:          envDuration("AUTHMAN_MOJANG_CACHE_STALE_TTL", 5*time.Minute),
		LogLevel:                  parseLogLevel(envString("AUTHMAN_LOG_LEVEL", "info")),
		ShutdownTimeout:           envDuration("AUTHMAN_SHUTDOWN_TIMEOUT", 10*time.Second),
		PresenceReconcileInterval: envDuration("AUTHMAN_PRESENCE_RECONCILE_INTERVAL", 30*time.Second),
	}
	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("AUTHMAN_HTTP_ADDR must not be empty")
	}
	if cfg.PublicBaseURL == "" {
		return Config{}, fmt.Errorf("AUTHMAN_PUBLIC_BASE_URL must not be empty")
	}
	return cfg, nil
}

func httpBasePath() string {
	if value, ok := os.LookupEnv("AUTHMAN_HTTP_BASE_PATH"); ok {
		return normalizeBasePath(value)
	}
	publicURL := envString("AUTHMAN_PUBLIC_BASE_URL", "")
	if publicURL == "" {
		return ""
	}
	parsed, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	return normalizeBasePath(parsed.EscapedPath())
}

func normalizeBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	value = strings.TrimRight(value, "/")
	if value == "/" {
		return ""
	}
	return value
}

func mojangRoutes() []mojang.Route {
	routes := make([]mojang.Route, 0, 4)
	if envBool("AUTHMAN_MOJANG_DIRECT_ENABLED", true) {
		routes = append(routes, mojang.Route{ID: "direct", Kind: mojang.RouteDirect, Weight: 1})
	}
	for index, proxyURL := range envCSV("AUTHMAN_MOJANG_HTTP_PROXIES") {
		routes = append(routes, mojang.Route{ID: fmt.Sprintf("http-%d", index+1), Kind: mojang.RouteHTTP, URL: proxyURL, Weight: 1})
	}
	for index, proxyURL := range envCSV("AUTHMAN_MOJANG_SOCKS5_PROXIES") {
		routes = append(routes, mojang.Route{ID: fmt.Sprintf("socks5-%d", index+1), Kind: mojang.RouteSOCKS5, URL: proxyURL, Weight: 1})
	}
	return routes
}

func envString(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}

func envCSV(key string) []string {
	value := envString(key, "")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(envString(key, ""))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := envString(key, "")
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
