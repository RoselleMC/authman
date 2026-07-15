package limbgo

import (
	"encoding/json"
	"os"
	"time"

	"go.minekube.com/common/minecraft/component"
)

// FileConfig is the simple deployable configuration format.
type FileConfig struct {
	Listen        string                  `json:"listen"`
	Status        FileStatusConfig        `json:"status"`
	Auth          FileAuthConfig          `json:"auth"`
	ProxyProtocol FileProxyProtocolConfig `json:"proxy_protocol"`
	Protocol      struct {
		ModernProtocols string `json:"modern_protocols"`
		RegistryData    string `json:"registry_data"`
	} `json:"protocol"`
	World struct {
		ID        string          `json:"id"`
		Schematic string          `json:"schematic"`
		Dimension DimensionConfig `json:"dimension"`
	} `json:"world"`
	Spawn struct {
		World string   `json:"world"`
		Pos   Vec3     `json:"pos"`
		Look  Rotation `json:"look"`
		Mode  GameMode `json:"mode"`
	} `json:"spawn"`
}

// FileAuthConfig is the deployable JSON shape for login authentication.
type FileAuthConfig struct {
	Mode             LoginMode `json:"mode"`
	YggdrasilBaseURL string    `json:"yggdrasil_base_url"`
	OnlineServerID   string    `json:"online_server_id"`
}

// FileProxyProtocolConfig is the deployable JSON shape for accepting HAProxy
// PROXY protocol headers from trusted upstream routers.
type FileProxyProtocolConfig struct {
	Enabled                 bool     `json:"enabled"`
	Required                bool     `json:"required"`
	TrustedProxies          []string `json:"trusted_proxies"`
	RestrictTrustedProxies  bool     `json:"restrict_trusted_proxies"`
	ReadHeaderTimeoutMillis int64    `json:"read_header_timeout_millis"`
}

// FileStatusConfig is the deployable JSON shape for server-list status.
type FileStatusConfig struct {
	Description         string                    `json:"description"`
	MOTD                string                    `json:"motd"`
	MOTDMiniMessage     string                    `json:"motd_minimessage"`
	VersionName         string                    `json:"version_name"`
	VersionProtocol     int32                     `json:"version_protocol"`
	MaxPlayers          int                       `json:"max_players"`
	OnlinePlayers       int                       `json:"online_players"`
	SamplePlayers       []StatusSamplePlayer      `json:"sample_players"`
	HidePlayers         bool                      `json:"hide_players"`
	Favicon             string                    `json:"favicon"`
	EnforcesSecureChat  *bool                     `json:"enforces_secure_chat"`
	PreviewsChat        *bool                     `json:"previews_chat"`
	PreventsChatReports *bool                     `json:"prevents_chat_reports"`
	RateLimit           FileStatusRateLimitConfig `json:"rate_limit"`
}

// FileStatusRateLimitConfig is the deployable status ping limiter config.
type FileStatusRateLimitConfig struct {
	Enabled      *bool `json:"enabled"`
	Requests     int   `json:"requests"`
	WindowMillis int64 `json:"window_millis"`
}

// DimensionConfig is the deployable JSON shape for world dimension settings.
type DimensionConfig struct {
	Environment     DimensionEnvironment `json:"environment"`
	Name            string               `json:"name"`
	MinY            *int32               `json:"min_y"`
	Height          *int32               `json:"height"`
	LogicalHeight   *int32               `json:"logical_height"`
	Natural         *bool                `json:"natural"`
	HasSkylight     *bool                `json:"has_skylight"`
	HasCeiling      *bool                `json:"has_ceiling"`
	UltraWarm       *bool                `json:"ultrawarm"`
	AmbientLight    *float32             `json:"ambient_light"`
	FixedTime       *int64               `json:"fixed_time"`
	TimeOfDay       *int64               `json:"time"`
	WorldAge        *int64               `json:"world_age"`
	CoordinateScale *float64             `json:"coordinate_scale"`
	Effects         string               `json:"effects"`
}

// LoadFileConfig reads a JSON deployment config.
func LoadFileConfig(path string) (FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, err
	}
	var cfg FileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return FileConfig{}, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":25565"
	}
	if cfg.Status.Description == "" {
		cfg.Status.Description = "limbgo"
	}
	if cfg.Status.MaxPlayers <= 0 {
		cfg.Status.MaxPlayers = 1
	}
	if cfg.World.ID == "" {
		cfg.World.ID = "default"
	}
	if cfg.Spawn.World == "" {
		cfg.Spawn.World = cfg.World.ID
	}
	if cfg.World.Schematic == "" && cfg.Spawn.Pos == (Vec3{}) && cfg.Spawn.Look == (Rotation{}) && cfg.Spawn.Mode == 0 {
		spawn := DefaultSpawn(cfg.World.ID)
		cfg.Spawn.World = spawn.World
		cfg.Spawn.Pos = spawn.Position
		cfg.Spawn.Look = spawn.Rotation
		cfg.Spawn.Mode = spawn.GameMode
	}
	return cfg, nil
}

// Component returns the configured server-list rich MOTD.
func (cfg FileStatusConfig) Component() (component.Component, error) {
	switch {
	case cfg.MOTDMiniMessage != "":
		return ParseMiniMessage(cfg.MOTDMiniMessage)
	case cfg.MOTD != "":
		return &component.Text{Content: cfg.MOTD}, nil
	case cfg.Description != "":
		return &component.Text{Content: cfg.Description}, nil
	default:
		return &component.Text{Content: "limbgo"}, nil
	}
}

// Options returns status response options for protocol routers.
func (cfg FileStatusConfig) Options(description component.Component) StatusOptions {
	return StatusOptions{
		VersionName:         cfg.VersionName,
		Protocol:            cfg.VersionProtocol,
		MaxPlayers:          cfg.MaxPlayers,
		OnlinePlayers:       cfg.OnlinePlayers,
		SamplePlayers:       cfg.SamplePlayers,
		HidePlayers:         cfg.HidePlayers,
		Description:         description,
		Favicon:             cfg.Favicon,
		EnforcesSecureChat:  cfg.EnforcesSecureChat,
		PreviewsChat:        cfg.PreviewsChat,
		PreventsChatReports: cfg.PreventsChatReports,
	}
}

// RateLimiter returns the configured status ping limiter. It is enabled by
// default for the standalone binary and can be disabled explicitly.
func (cfg FileStatusRateLimitConfig) RateLimiter() *RateLimiter {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return nil
	}
	return NewRateLimiter(RateLimitConfig{
		Requests: cfg.Requests,
		Window:   time.Duration(cfg.WindowMillis) * time.Millisecond,
	})
}

// Config returns the API PROXY protocol configuration described by the file.
func (cfg FileProxyProtocolConfig) Config() ProxyProtocolConfig {
	return ProxyProtocolConfig{
		Enabled:                cfg.Enabled || cfg.Required,
		Required:               cfg.Required,
		TrustedProxies:         append([]string(nil), cfg.TrustedProxies...),
		RestrictTrustedProxies: cfg.RestrictTrustedProxies,
		ReadHeaderTimeout:      time.Duration(cfg.ReadHeaderTimeoutMillis) * time.Millisecond,
	}
}

// SpawnTarget returns the static spawn described by the file.
func (cfg FileConfig) SpawnTarget() SpawnTarget {
	return SpawnTarget{
		World:    cfg.Spawn.World,
		Position: cfg.Spawn.Pos,
		Rotation: cfg.Spawn.Look,
		GameMode: cfg.Spawn.Mode,
	}
}

// Dimension returns the configured world dimension using vanilla-like defaults
// for the selected environment.
func (cfg FileConfig) Dimension(schematicHeight int32) Dimension {
	return cfg.World.Dimension.Dimension(schematicHeight)
}

// Dimension returns this config as an API Dimension.
func (cfg DimensionConfig) Dimension(schematicHeight int32) Dimension {
	env := cfg.Environment
	if env == "" {
		env = inferDimensionEnvironment(cfg.Name)
	}
	d := DimensionPreset(env, schematicHeight)
	if cfg.Name != "" {
		d.Name = cfg.Name
	}
	if cfg.MinY != nil {
		d.MinY = *cfg.MinY
	}
	if cfg.Height != nil {
		d.Height = normalizeDimensionHeight(*cfg.Height)
	}
	if cfg.LogicalHeight != nil {
		d.LogicalHeight = *cfg.LogicalHeight
	}
	if cfg.Natural != nil {
		d.Natural = *cfg.Natural
	}
	if cfg.HasSkylight != nil {
		d.HasSkylight = *cfg.HasSkylight
	}
	if cfg.HasCeiling != nil {
		d.HasCeiling = *cfg.HasCeiling
	}
	if cfg.UltraWarm != nil {
		d.UltraWarm = *cfg.UltraWarm
	}
	if cfg.AmbientLight != nil {
		d.AmbientLight = *cfg.AmbientLight
	}
	if cfg.FixedTime != nil {
		d.FixedTime = cfg.FixedTime
	}
	if cfg.TimeOfDay != nil {
		d.TimeOfDay = cfg.TimeOfDay
	}
	if cfg.WorldAge != nil {
		d.WorldAge = *cfg.WorldAge
	}
	if cfg.CoordinateScale != nil {
		d.CoordinateScale = *cfg.CoordinateScale
	}
	if cfg.Effects != "" {
		d.Effects = cfg.Effects
	}
	return NormalizeDimension(d, schematicHeight)
}

// Empty reports whether no dimension fields were configured.
func (cfg DimensionConfig) Empty() bool {
	return cfg.Environment == "" &&
		cfg.Name == "" &&
		cfg.MinY == nil &&
		cfg.Height == nil &&
		cfg.LogicalHeight == nil &&
		cfg.Natural == nil &&
		cfg.HasSkylight == nil &&
		cfg.HasCeiling == nil &&
		cfg.UltraWarm == nil &&
		cfg.AmbientLight == nil &&
		cfg.FixedTime == nil &&
		cfg.TimeOfDay == nil &&
		cfg.WorldAge == nil &&
		cfg.CoordinateScale == nil &&
		cfg.Effects == ""
}
