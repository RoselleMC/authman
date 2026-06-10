package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RoselleMC/limbgo"
	"github.com/RoselleMC/limbgo/dialog"
	"github.com/RoselleMC/limbgo/protocol/limbo"
	"github.com/RoselleMC/limbgo/world/schematic"
	"go.minekube.com/common/minecraft/component"
)

const version = "0.1.0-dev"

const minDialogProtocol = 771 // Minecraft 1.21.6

var miniMessageLineBreakRE = regexp.MustCompile(`(?i)\r\n|\r|\n|<\s*(?:newline|br)\s*/?>`)

type config struct {
	Listen         string
	CoreURL        string
	NodeToken      string
	NodeName       string
	Schematic      string
	WorldID        string
	SourceID       string
	DefaultHost    string
	LoginMode      string
	OnlineServerID string
	LogLevel       string
}

type portal struct {
	cfg           config
	client        *coreClient
	nodeID        string
	locked        bool
	runtime       map[string]any
	logger        *slog.Logger
	fallbackWorld limbgo.World
	fallbackSpawn limbgo.SpawnTarget
	worldsMu      sync.RWMutex
	worlds        map[string]cachedWorld
}

type cachedWorld struct {
	sha256 string
	world  limbgo.World
	spawn  limbgo.SpawnTarget
}

func main() {
	var cfg config
	flag.StringVar(&cfg.Listen, "listen", getenv("AUTHMAN_LIMBO_LISTEN", ":25565"), "listen address")
	flag.StringVar(&cfg.CoreURL, "core-url", getenv("AUTHMAN_CORE_URL", "http://127.0.0.1:8080"), "Authman Core URL")
	flag.StringVar(&cfg.NodeToken, "node-token", getenv("AUTHMAN_NODE_TOKEN", ""), "Authman node token")
	flag.StringVar(&cfg.NodeName, "node-name", getenv("AUTHMAN_NODE_NAME", "limbo-portal"), "node display name")
	flag.StringVar(&cfg.Schematic, "schematic", getenv("AUTHMAN_LIMBO_SCHEMATIC", ""), "optional limbo schematic file")
	flag.StringVar(&cfg.WorldID, "world-id", getenv("AUTHMAN_LIMBO_WORLD_ID", "authman"), "limbo world id")
	flag.StringVar(&cfg.SourceID, "source-id", getenv("AUTHMAN_LIMBO_SOURCE_ID", ""), "portal source id")
	flag.StringVar(&cfg.DefaultHost, "default-host", getenv("AUTHMAN_LIMBO_DEFAULT_HOST", ""), "default requested host")
	flag.StringVar(&cfg.LoginMode, "login-mode", getenv("AUTHMAN_LIMBO_LOGIN_MODE", "hybrid"), "limbo login mode: hybrid, offline, or online")
	flag.StringVar(&cfg.OnlineServerID, "online-server-id", getenv("AUTHMAN_LIMBO_ONLINE_SERVER_ID", "authman-limbo"), "serverId challenge used for online-mode session verification")
	flag.StringVar(&cfg.LogLevel, "log-level", getenv("AUTHMAN_LIMBO_LOG_LEVEL", "info"), "log level: debug, info, warn, or error")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)
	world, spawn, err := loadWorld(cfg)
	if err != nil {
		fatal(err)
	}
	p := &portal{
		cfg:           cfg,
		client:        newCoreClient(cfg),
		runtime:       map[string]any{},
		logger:        logger,
		fallbackWorld: world,
		fallbackSpawn: spawn,
		worlds:        map[string]cachedWorld{},
	}
	if err := p.heartbeat(context.Background()); err != nil {
		logger.Warn("initial Authman heartbeat failed; portal starts locked", "err", err)
		p.locked = true
	}

	motd, err := limbgo.ParseMiniMessage("<green>Authman</green> <gray>login portal</gray>")
	if err != nil {
		fatal(err)
	}
	router := limbo.Router{
		MOTD:              motd,
		VersionName:       "Authman Limbo",
		MaxPlayers:        1000,
		StatusProvider:    limbgo.StatusProviderFunc(p.status),
		StatusRateLimiter: limbgo.NewRateLimiter(limbgo.RateLimitConfig{Requests: 60, Window: time.Second}),
		LoginMode:         p.loginMode(),
		LoginPolicy:       limbgo.LoginPolicyFunc(p.resolveLoginMode),
		SessionVerifier:   limbgo.SessionVerifierFunc(p.verifySession),
		OnlineServerID:    cfg.OnlineServerID,
		ProtocolPolicy: limbgo.ProtocolPolicyFunc(func(_ context.Context, req limbgo.ProtocolRequest) error {
			if req.ProtocolVersion >= minDialogProtocol {
				return nil
			}
			return limbgo.RejectProtocolText("Authman Limbo requires Minecraft 1.21.6 or newer.")
		}),
	}
	server, err := limbgo.NewServer(limbgo.Config{
		Addr:           cfg.Listen,
		ProtocolRouter: router,
		JoinResolver:   limbgo.JoinResolverFunc(p.resolveJoin),
		Events: limbgo.PlayerEventHandlerFuncs{
			Join:        p.handleJoin,
			DialogClick: p.handleDialogClick,
		},
		Logger: logger,
	})
	if err != nil {
		fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go p.heartbeatLoop(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fatal(err)
		}
	}
}

func loadWorld(cfg config) (limbgo.World, limbgo.SpawnTarget, error) {
	if strings.TrimSpace(cfg.Schematic) == "" {
		world := limbgo.DefaultWorld(cfg.WorldID)
		spawn := limbgo.DefaultSpawn(world.ID())
		spawn.GameMode = limbgo.GameModeAdventure
		return world, spawn, nil
	}
	world, err := schematic.LoadFile(cfg.Schematic, schematic.Options{WorldID: cfg.WorldID})
	if err != nil {
		return nil, limbgo.SpawnTarget{}, fmt.Errorf("load schematic: %w", err)
	}
	return world, limbgo.SpawnTarget{
		World:    world.ID(),
		Position: limbgo.Vec3{X: 0, Y: 65, Z: 0},
		GameMode: limbgo.GameModeAdventure,
	}, nil
}

func (p *portal) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.heartbeat(ctx); err != nil {
				p.logger.Warn("Authman heartbeat failed", "err", err)
			}
		}
	}
}

func (p *portal) heartbeat(ctx context.Context) error {
	res, err := p.client.heartbeat(ctx)
	if err != nil {
		return err
	}
	p.nodeID = res.Node.ID
	p.runtime = res.RuntimeConfig
	p.locked = false
	return nil
}

func (p *portal) loginMode() limbgo.LoginMode {
	switch strings.ToLower(strings.TrimSpace(p.cfg.LoginMode)) {
	case string(limbgo.LoginModeOnline):
		return limbgo.LoginModeOnline
	case string(limbgo.LoginModeHybrid):
		return limbgo.LoginModeHybrid
	case string(limbgo.LoginModeOffline):
		return limbgo.LoginModeOffline
	default:
		return limbgo.LoginModeHybrid
	}
}

func (p *portal) resolveLoginMode(ctx context.Context, req limbgo.LoginRequest) (limbgo.LoginMode, error) {
	configured := p.loginMode()
	if configured != limbgo.LoginModeHybrid {
		return configured, nil
	}
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}
	policy, err := p.client.resolveLoginPolicy(ctx, req)
	if err == nil {
		mode := limbgo.LoginMode(policy.LoginMode)
		switch mode {
		case limbgo.LoginModeOnline, limbgo.LoginModeOffline, limbgo.LoginModeHybrid:
			logger.Info("limbo login policy selected mode", "player", req.Username, "claimed_uuid", req.ClaimedUUID, "mode", mode, "reason", policy.Reason)
			return mode, nil
		default:
			return "", fmt.Errorf("core returned unsupported login policy mode %q", policy.LoginMode)
		}
	}
	return "", err
}

func (p *portal) verifySession(ctx context.Context, proof limbgo.SessionProof) (limbgo.VerifiedProfile, error) {
	profile, err := p.client.verifySession(ctx, proof)
	if err != nil {
		var coreErr coreAPIError
		if errors.As(err, &coreErr) && coreErr.Status == http.StatusUnauthorized && coreErr.Code == "session.verify_failed" {
			return limbgo.VerifiedProfile{}, fmt.Errorf("%w: %s", limbgo.ErrInvalidLogin, coreErr.Message)
		}
		return limbgo.VerifiedProfile{}, err
	}
	if profile.UUID == "" || profile.Name == "" {
		return limbgo.VerifiedProfile{}, fmt.Errorf("%w: Authman returned an incomplete verified profile", limbgo.ErrInvalidLogin)
	}
	return profile, nil
}

func (p *portal) resolveJoin(ctx context.Context, player limbgo.Player) (limbgo.JoinTarget, error) {
	host := requestedHost(player.RequestedHost, p.cfg.DefaultHost)
	resolved, err := p.client.resolveTarget(ctx, host)
	if err != nil {
		p.logger.Warn("using fallback limbo world; target resolve failed", "host", host, "err", err)
		return limbgo.JoinTarget{World: p.fallbackWorld, Spawn: p.fallbackSpawn}, nil
	}
	if resolved.LimboBlueprint == nil || strings.TrimSpace(resolved.LimboBlueprint.ID) == "" || resolved.LimboBlueprint.Missing {
		return limbgo.JoinTarget{World: p.fallbackWorld, Spawn: p.fallbackSpawn}, nil
	}
	world, spawn, err := p.worldForBlueprint(ctx, *resolved.LimboBlueprint)
	if err != nil {
		p.logger.Warn("using fallback limbo world; blueprint load failed", "blueprint", resolved.LimboBlueprint.ID, "err", err)
		return limbgo.JoinTarget{World: p.fallbackWorld, Spawn: p.fallbackSpawn}, nil
	}
	return limbgo.JoinTarget{World: world, Spawn: spawn}, nil
}

func (p *portal) worldForBlueprint(ctx context.Context, blueprint limboBlueprintData) (limbgo.World, limbgo.SpawnTarget, error) {
	p.worldsMu.RLock()
	cached, ok := p.worlds[blueprint.ID]
	p.worldsMu.RUnlock()
	if ok && cached.sha256 == strings.TrimSpace(blueprint.SHA256) {
		return cached.world, cached.spawn, nil
	}
	full, err := p.client.fetchBlueprint(ctx, blueprint.ID)
	if err != nil {
		return nil, limbgo.SpawnTarget{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(full.SchematicBase64)
	if err != nil {
		return nil, limbgo.SpawnTarget{}, fmt.Errorf("decode blueprint schematic: %w", err)
	}
	worldID := stringFromMap(full.Config, "world_id")
	if worldID == "" {
		worldID = "authman-" + full.ID
	}
	world, err := schematic.Load(bytes.NewReader(raw), schematic.Options{
		WorldID:   worldID,
		Dimension: dimensionFromMap(full.Config),
	})
	if err != nil {
		return nil, limbgo.SpawnTarget{}, err
	}
	spawn := spawnFromMap(full.Config, world.ID())
	p.worldsMu.Lock()
	p.worlds[full.ID] = cachedWorld{sha256: strings.TrimSpace(full.SHA256), world: world, spawn: spawn}
	p.worldsMu.Unlock()
	return world, spawn, nil
}

func (p *portal) status(ctx context.Context, req limbgo.StatusRequest) (limbgo.Status, error) {
	host := requestedHost(req.Address, p.cfg.DefaultHost)
	resolved, err := p.client.resolveTarget(ctx, host)
	var description component.Component = &component.Text{Content: "Authman login portal"}
	if err == nil && resolved.Target.MOTD != "" {
		motd := limitMiniMessageLines(resolved.Target.MOTD, 2)
		parsed, parseErr := limbgo.ParseMiniMessage(motd)
		if parseErr == nil && parsed != nil {
			description = parsed
		} else {
			p.logger.Warn("failed to parse downstream motd minimessage", "host", host, "err", parseErr)
			description = &component.Text{Content: motd}
		}
	}
	favicon := ""
	if err == nil {
		favicon = strings.TrimSpace(resolved.Target.ServerIcon)
	}
	return limbgo.Status{
		VersionName:         "Authman Limbo",
		Protocol:            req.Protocol,
		Description:         description,
		Favicon:             favicon,
		MaxPlayers:          1000,
		OnlinePlayers:       0,
		PreventsChatReports: limbgo.Bool(true),
	}, nil
}

func limitMiniMessageLines(value string, maxLines int) string {
	if value == "" || maxLines <= 0 {
		return ""
	}
	matches := miniMessageLineBreakRE.FindAllStringIndex(value, -1)
	if len(matches) < maxLines {
		return value
	}
	return value[:matches[maxLines-1][0]]
}

func (p *portal) handleJoin(ctx context.Context, session limbgo.PlayerSession, event *limbgo.JoinEvent) error {
	p.logger.Info("limbo join event", "player", event.Player.Name, "protocol", event.Protocol)
	if p.locked {
		return session.SendMessage(ctx, &component.Text{Content: "Authman portal is locked."})
	}
	return p.showLoginDialog(ctx, session)
}

func (p *portal) handleDialogClick(ctx context.Context, session limbgo.PlayerSession, event *limbgo.DialogClickEvent) error {
	p.logger.Info("limbo dialog event", "player", event.Player.Name, "protocol", event.Protocol, "id", event.ID, "payload_bytes", len(event.Payload))
	switch event.ID {
	case "authman:login_submit", "authman:register_submit":
	default:
		return p.showLoginDialog(ctx, session)
	}
	values := map[string]string{}
	if len(event.Payload) > 0 {
		parsed, err := parseDialogStringPayload(event.Payload)
		if err != nil {
			p.logger.Warn("failed to parse dialog payload", "player", event.Player.Name, "err", err)
			return session.SendMessage(ctx, &component.Text{Content: "Authman could not read the dialog response. Please try again."})
		}
		values = parsed
	}
	switch event.ID {
	case "authman:register_submit":
		return p.registerAndTransfer(ctx, session, strings.TrimSpace(values["password"]), strings.TrimSpace(values["confirm_password"]))
	default:
		return p.authenticateAndTransfer(ctx, session, strings.TrimSpace(values["password"]))
	}
}

func (p *portal) showLoginDialog(ctx context.Context, session limbgo.PlayerSession) error {
	return p.showLoginDialogMessage(ctx, session, "")
}

func (p *portal) showLoginDialogMessage(ctx context.Context, session limbgo.PlayerSession, message string) error {
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, &component.Text{Content: "Authman Limbo requires Minecraft 1.21.6 or newer."})
	}
	resolved, err := p.client.resolvePlayer(ctx, session.Player())
	if err != nil {
		var coreErr coreAPIError
		if errors.As(err, &coreErr) && coreErr.Status == http.StatusNotFound && !session.Player().Verified {
			return p.showRegisterDialog(ctx, session)
		}
		return session.SendMessage(ctx, &component.Text{Content: "Authman could not resolve this passport."})
	}
	body := []dialog.Raw{
		dialog.PlainMessage(dialog.Text("Authenticate with Authman, then transfer to the downstream server."), 240),
	}
	if strings.TrimSpace(message) != "" {
		body = append(body, dialog.PlainMessage(dialog.Text("Error: "+message), 240))
	}
	inputs := []dialog.Raw(nil)
	if resolved.Auth.Required {
		if !session.Player().Verified {
			body = append(body, dialog.PlainMessage(dialog.Text("Your premium session is not verified. Authman is using offline password authentication for this login."), 240))
		}
		inputs = append(inputs, dialog.TextInput("password", dialog.Text("Password"), dialog.TextInputOptions{
			MaxLength: 128,
			Width:     240,
		}))
	} else {
		body = append(body, dialog.PlainMessage(dialog.Text("This premium passport can continue without a password."), 240))
	}
	return session.ShowDialog(ctx, dialog.Notice(dialog.Common{
		Title:              dialog.Text("Authman"),
		Body:               body,
		Inputs:             inputs,
		CanCloseWithEscape: dialog.Bool(false),
		Pause:              dialog.Bool(false),
		AfterAction:        dialog.AfterActionWaitForResponse,
	}, dialog.Button(
		dialog.Text("Login"),
		dialog.DynamicCustom("authman:login_submit", dialog.Raw{"screen": "login"}),
	)))
}

func (p *portal) showRegisterDialog(ctx context.Context, session limbgo.PlayerSession) error {
	return p.showRegisterDialogMessage(ctx, session, "")
}

func (p *portal) showRegisterDialogMessage(ctx context.Context, session limbgo.PlayerSession, message string) error {
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, &component.Text{Content: "Authman Limbo requires Minecraft 1.21.6 or newer."})
	}
	body := []dialog.Raw{
		dialog.PlainMessage(dialog.Text("Create an Authman offline passport for this name. Use at least 8 characters."), 240),
	}
	if strings.TrimSpace(message) != "" {
		body = append(body, dialog.PlainMessage(dialog.Text("Error: "+message), 240))
	}
	inputs := []dialog.Raw{
		dialog.TextInput("password", dialog.Text("Password"), dialog.TextInputOptions{
			MaxLength: 128,
			Width:     240,
		}),
		dialog.TextInput("confirm_password", dialog.Text("Confirm password"), dialog.TextInputOptions{
			MaxLength: 128,
			Width:     240,
		}),
	}
	return session.ShowDialog(ctx, dialog.Notice(dialog.Common{
		Title:              dialog.Text("Register Authman"),
		Body:               body,
		Inputs:             inputs,
		CanCloseWithEscape: dialog.Bool(false),
		Pause:              dialog.Bool(false),
		AfterAction:        dialog.AfterActionWaitForResponse,
	}, dialog.Button(
		dialog.Text("Register"),
		dialog.DynamicCustom("authman:register_submit", dialog.Raw{"screen": "register"}),
	)))
}

func (p *portal) authenticateAndTransfer(ctx context.Context, session limbgo.PlayerSession, password string) error {
	player := session.Player()
	resolved, err := p.client.resolvePlayer(ctx, player)
	if err != nil {
		return session.SendMessage(ctx, &component.Text{Content: "Authman could not resolve this passport."})
	}
	if resolved.Auth.Required {
		if strings.TrimSpace(password) == "" {
			return p.showLoginDialogMessage(ctx, session, "Password is required.")
		}
		if err := p.client.authenticate(ctx, resolved.Auth.Username, password); err != nil {
			return p.showLoginDialogMessage(ctx, session, "Invalid Authman password.")
		}
	}
	return p.transferResolved(ctx, session, resolved)
}

func (p *portal) registerAndTransfer(ctx context.Context, session limbgo.PlayerSession, password string, confirmPassword string) error {
	if strings.TrimSpace(password) == "" || password != confirmPassword {
		return p.showRegisterDialogMessage(ctx, session, "Passwords do not match.")
	}
	resolved, err := p.client.registerOffline(ctx, session.Player().Name, password, session.Player().RequestedHost)
	if err != nil {
		p.logger.Warn("offline registration failed", "player", session.Player().Name, "err", err)
		return p.showRegisterDialogMessage(ctx, session, "Authman could not register this offline passport.")
	}
	return p.transferResolved(ctx, session, resolved)
}

func (p *portal) transferResolved(ctx context.Context, session limbgo.PlayerSession, resolved resolveResponse) error {
	player := session.Player()
	host := requestedHost(player.RequestedHost, p.cfg.DefaultHost)
	resolvedTarget, err := p.client.resolveTarget(ctx, host)
	if err != nil {
		return session.SendMessage(ctx, &component.Text{Content: "Authman could not resolve a downstream target."})
	}
	target := resolvedTarget.Target
	grant, err := p.client.createGrant(ctx, resolved.Player.ProtocolName, target.ServerID, host, p.sourceID())
	if err != nil {
		return session.SendMessage(ctx, &component.Text{Content: "Authman could not create a transfer grant."})
	}
	caps := session.Capabilities()
	if !caps.StoreCookie || !caps.Transfer {
		if caps.Disconnect {
			return session.Disconnect(ctx, &component.Text{Content: "Authman transfer requires Minecraft 1.20.5+ with vanilla transfer-cookie support."})
		}
		return session.SendMessage(ctx, &component.Text{Content: "Authman transfer requires Minecraft 1.20.5+ with vanilla transfer-cookie support."})
	}
	_ = session.ClearDialog(ctx)
	if caps.ActionBar {
		_ = session.SendActionBar(ctx, &component.Text{Content: "Authman login accepted"})
	}
	if caps.Title {
		_ = session.ShowTitle(ctx, limbgo.Title{
			Title:    &component.Text{Content: "Welcome"},
			Subtitle: &component.Text{Content: "Preparing transfer"},
			Times:    limbgo.TitleTimesTicks(5, 30, 5),
		})
	}
	if err := session.StoreCookie(ctx, p.transferCookieKey(), []byte(grant.Token)); err != nil {
		return err
	}
	if err := session.Transfer(ctx, grant.Target.TransferHost, grant.Target.TransferPort); err != nil {
		return err
	}
	return nil
}

func (p *portal) sourceID() string {
	if strings.TrimSpace(p.cfg.SourceID) != "" {
		return strings.TrimSpace(p.cfg.SourceID)
	}
	if strings.TrimSpace(p.nodeID) != "" {
		return strings.TrimSpace(p.nodeID)
	}
	return strings.TrimSpace(p.cfg.NodeName)
}

func (p *portal) transferCookieKey() string {
	if key := strings.TrimSpace(stringFromMap(p.runtime, "transfer_cookie_key")); key != "" {
		return key
	}
	return "authman:transfer_grant"
}

func requestedHost(value, fallback string) string {
	if host := strings.TrimSpace(value); host != "" {
		return host
	}
	return strings.TrimSpace(fallback)
}

func dimensionFromMap(config map[string]any) limbgo.Dimension {
	switch strings.ToLower(strings.TrimSpace(stringFromMap(config, "dimension"))) {
	case "nether":
		return limbgo.DimensionPreset(limbgo.DimensionNether, 0)
	case "end":
		return limbgo.DimensionPreset(limbgo.DimensionEnd, 0)
	default:
		return limbgo.DimensionPreset(limbgo.DimensionOverworld, 0)
	}
}

func spawnFromMap(config map[string]any, worldID string) limbgo.SpawnTarget {
	spawn := map[string]any{}
	if raw, ok := config["spawn"].(map[string]any); ok {
		spawn = raw
	}
	return limbgo.SpawnTarget{
		World:    worldID,
		Position: limbgo.Vec3{X: floatFromMap(spawn, "x", 0), Y: floatFromMap(spawn, "y", 65), Z: floatFromMap(spawn, "z", 0)},
		Rotation: limbgo.Rotation{Yaw: float32(floatFromMap(spawn, "yaw", 0)), Pitch: float32(floatFromMap(spawn, "pitch", 0))},
		GameMode: limbgo.GameModeAdventure,
	}
}

func floatFromMap(input map[string]any, key string, fallback float64) float64 {
	switch typed := input[key].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func validateConfig(cfg config) error {
	if strings.TrimSpace(cfg.CoreURL) == "" {
		return errors.New("AUTHMAN_CORE_URL is required")
	}
	if strings.TrimSpace(cfg.NodeToken) == "" {
		return errors.New("AUTHMAN_NODE_TOKEN is required")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.LoginMode)) {
	case "", string(limbgo.LoginModeHybrid), string(limbgo.LoginModeOffline), string(limbgo.LoginModeOnline):
	default:
		return fmt.Errorf("unsupported AUTHMAN_LIMBO_LOGIN_MODE %q", cfg.LoginMode)
	}
	return nil
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return value
	default:
		return ""
	}
}

func parseDialogStringPayload(payload []byte) (map[string]string, error) {
	reader := bytes.NewReader(payload)
	tag, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := readNBTAnonymousPayload(reader, tag, "", values); err != nil {
		return nil, err
	}
	return values, nil
}

func readNBTAnonymousPayload(r *bytes.Reader, tag byte, name string, values map[string]string) error {
	switch tag {
	case 0:
		return nil
	case 1:
		_, err := r.ReadByte()
		return err
	case 2:
		_, err := readN(r, 2)
		return err
	case 3, 5:
		_, err := readN(r, 4)
		return err
	case 4, 6:
		_, err := readN(r, 8)
		return err
	case 7:
		n, err := readNBTInt(r)
		if err != nil {
			return err
		}
		_, err = readN(r, n)
		return err
	case 8:
		value, err := readNBTString(r)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = value
		}
		return nil
	case 9:
		childTag, err := r.ReadByte()
		if err != nil {
			return err
		}
		n, err := readNBTInt(r)
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := readNBTAnonymousPayload(r, childTag, name, values); err != nil {
				return err
			}
		}
		return nil
	case 10:
		for {
			childTag, err := r.ReadByte()
			if err != nil {
				return err
			}
			if childTag == 0 {
				return nil
			}
			childName, err := readNBTString(r)
			if err != nil {
				return err
			}
			if err := readNBTAnonymousPayload(r, childTag, childName, values); err != nil {
				return err
			}
		}
	case 11:
		n, err := readNBTInt(r)
		if err != nil {
			return err
		}
		_, err = readN(r, n*4)
		return err
	case 12:
		n, err := readNBTInt(r)
		if err != nil {
			return err
		}
		_, err = readN(r, n*8)
		return err
	default:
		return fmt.Errorf("unsupported nbt tag %d", tag)
	}
}

func readNBTString(r *bytes.Reader) (string, error) {
	sizeRaw, err := readN(r, 2)
	if err != nil {
		return "", err
	}
	size := int(binary.BigEndian.Uint16(sizeRaw))
	raw, err := readN(r, size)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func readNBTInt(r *bytes.Reader) (int, error) {
	raw, err := readN(r, 4)
	if err != nil {
		return 0, err
	}
	value := int(int32(binary.BigEndian.Uint32(raw)))
	if value < 0 {
		return 0, fmt.Errorf("negative nbt length %d", value)
	}
	return value, nil
}

func readN(r *bytes.Reader, n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative read length %d", n)
	}
	if r.Len() < n {
		return nil, io.ErrUnexpectedEOF
	}
	out := make([]byte, n)
	_, err := io.ReadFull(r, out)
	return out, err
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "authman-limbo: %v\n", err)
	os.Exit(1)
}

type coreClient struct {
	cfg    config
	client *http.Client
}

func newCoreClient(cfg config) *coreClient {
	return &coreClient{cfg: cfg, client: &http.Client{Timeout: 8 * time.Second}}
}

type heartbeatResponse struct {
	Node struct {
		ID string `json:"id"`
	} `json:"node"`
	RuntimeConfig map[string]any `json:"runtime_config"`
}

type resolveResponse struct {
	Player struct {
		UUID         string `json:"uuid"`
		Kind         string `json:"kind"`
		ProtocolName string `json:"protocol_name"`
	} `json:"player"`
	Auth struct {
		Required bool   `json:"required"`
		Username string `json:"username"`
	} `json:"auth"`
}

type verifySessionResponse struct {
	Profile verifiedProfileData `json:"profile"`
}

type verifiedProfileData struct {
	UUID       string                `json:"uuid"`
	Name       string                `json:"name"`
	Properties []profilePropertyData `json:"properties"`
	Source     string                `json:"source"`
	Verified   bool                  `json:"verified"`
}

type profilePropertyData struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type targetResponse struct {
	Target         downstreamTarget    `json:"target"`
	LimboBlueprint *limboBlueprintData `json:"limbo_blueprint"`
}

type loginPolicyResponse struct {
	LoginMode      string               `json:"login_mode"`
	Reason         string               `json:"reason"`
	PremiumProfile *verifiedProfileData `json:"premium_profile,omitempty"`
}

type limboBlueprintData struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	SHA256          string         `json:"sha256"`
	Missing         bool           `json:"missing"`
	Config          map[string]any `json:"config"`
	SchematicBase64 string         `json:"schematic_base64"`
}

type grantResponse struct {
	Token  string           `json:"token"`
	Target downstreamTarget `json:"target"`
}

type downstreamTarget struct {
	ServerID     string `json:"server_id"`
	TransferHost string `json:"transfer_host"`
	TransferPort int    `json:"transfer_port"`
	MOTD         string `json:"motd"`
	ServerIcon   string `json:"server_icon"`
}

func (c *coreClient) heartbeat(ctx context.Context) (heartbeatResponse, error) {
	return postJSON[heartbeatResponse](ctx, c, "/api/node/heartbeat", map[string]any{
		"kind":                 "limbo_portal",
		"name":                 c.cfg.NodeName,
		"instance_fingerprint": instanceFingerprint(c.cfg),
		"plugin_version":       version,
	})
}

func (c *coreClient) verifySession(ctx context.Context, proof limbgo.SessionProof) (limbgo.VerifiedProfile, error) {
	res, err := postJSON[verifySessionResponse](ctx, c, "/api/node/limbo/sessions/verify", map[string]any{
		"username":         proof.Username,
		"server_id":        proof.ServerID,
		"remote_ip":        proof.RemoteIP,
		"protocol_version": proof.ProtocolVersion,
		"requested_host":   proof.RequestedHost,
	})
	if err != nil {
		return limbgo.VerifiedProfile{}, err
	}
	return limbgo.VerifiedProfile{
		UUID:       res.Profile.UUID,
		Name:       res.Profile.Name,
		Properties: limbgoProfileProperties(res.Profile.Properties),
		Source:     res.Profile.Source,
		Verified:   res.Profile.Verified,
	}, nil
}

func (c *coreClient) resolvePlayer(ctx context.Context, player limbgo.Player) (resolveResponse, error) {
	return postJSON[resolveResponse](ctx, c, "/api/node/players/resolve", map[string]any{
		"username":           player.Name,
		"login_mode":         string(player.LoginMode),
		"auth_source":        player.AuthSource,
		"verified":           player.Verified,
		"verified_uuid":      player.UUID,
		"profile_properties": coreProfileProperties(player.ProfileProperties),
	})
}

func (c *coreClient) resolveLoginPolicy(ctx context.Context, req limbgo.LoginRequest) (loginPolicyResponse, error) {
	return postJSON[loginPolicyResponse](ctx, c, "/api/node/limbo/login-policy", map[string]any{
		"username":         req.Username,
		"claimed_uuid":     req.ClaimedUUID,
		"protocol_version": req.ProtocolVersion,
		"requested_host":   req.RequestedHost,
	})
}

func (c *coreClient) authenticate(ctx context.Context, username, password string) error {
	_, err := postJSON[map[string]any](ctx, c, "/api/node/players/authenticate", map[string]any{"username": username, "password": password})
	return err
}

func (c *coreClient) registerOffline(ctx context.Context, username string, password string, requestedHost string) (resolveResponse, error) {
	return postJSON[resolveResponse](ctx, c, "/api/node/players/register-offline", map[string]any{
		"username":       username,
		"password":       password,
		"requested_host": requestedHost,
	})
}

func (c *coreClient) resolveTarget(ctx context.Context, requestedHost string) (targetResponse, error) {
	res, err := postJSON[targetResponse](ctx, c, "/api/node/limbo/targets/resolve", map[string]any{"requested_host": requestedHost})
	return res, err
}

func (c *coreClient) fetchBlueprint(ctx context.Context, id string) (limboBlueprintData, error) {
	return getJSON[limboBlueprintData](ctx, c, "/api/node/limbo/blueprints/"+url.PathEscape(strings.TrimSpace(id)))
}

func (c *coreClient) createGrant(ctx context.Context, username, serverID, requestedHost, source string) (grantResponse, error) {
	return postJSON[grantResponse](ctx, c, "/api/node/limbo/transfer-grants", map[string]any{
		"username":       username,
		"server_id":      serverID,
		"requested_host": requestedHost,
		"source":         source,
	})
}

type coreAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e coreAPIError) Error() string {
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return fmt.Sprintf("Authman returned HTTP %d", e.Status)
}

func postJSON[T any](ctx context.Context, c *coreClient, path string, body map[string]any) (T, error) {
	var out T
	payload, err := json.Marshal(body)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.CoreURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.NodeToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Authman-Instance", instanceFingerprint(c.cfg))
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	var envelope struct {
		Data  T `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if envelope.Error != nil {
			return out, coreAPIError{Status: resp.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		return out, coreAPIError{Status: resp.StatusCode}
	}
	return envelope.Data, nil
}

func getJSON[T any](ctx context.Context, c *coreClient, path string) (T, error) {
	var out T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.CoreURL, "/")+path, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.NodeToken)
	req.Header.Set("X-Authman-Instance", instanceFingerprint(c.cfg))
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	var envelope struct {
		Data  T `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return out, err
	}
	if resp.StatusCode >= 400 || envelope.Error != nil {
		if envelope.Error != nil {
			return out, coreAPIError{Status: resp.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		return out, coreAPIError{Status: resp.StatusCode}
	}
	return envelope.Data, nil
}

func instanceFingerprint(cfg config) string {
	seed := cfg.NodeName + "|" + cfg.Listen + "|" + cfg.CoreURL
	return "limbo-" + strconv.FormatUint(fnv64(seed), 16)
}

func fnv64(text string) uint64 {
	var h uint64 = 1469598103934665603
	for _, b := range []byte(text) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

func coreProfileProperties(properties []limbgo.ProfileProperty) []profilePropertyData {
	out := make([]profilePropertyData, 0, len(properties))
	for _, property := range properties {
		name := strings.TrimSpace(property.Name)
		if name == "" {
			continue
		}
		out = append(out, profilePropertyData{
			Name:      name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	return out
}

func limbgoProfileProperties(properties []profilePropertyData) []limbgo.ProfileProperty {
	out := make([]limbgo.ProfileProperty, 0, len(properties))
	for _, property := range properties {
		name := strings.TrimSpace(property.Name)
		if name == "" {
			continue
		}
		out = append(out, limbgo.ProfileProperty{
			Name:      name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	return out
}
