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
	"math"
	"net"
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

	"github.com/RoselleMC/authman/internal/playermsg"
	"github.com/RoselleMC/limbgo"
	"github.com/RoselleMC/limbgo/dialog"
	"github.com/RoselleMC/limbgo/protocol/limbo"
	"github.com/RoselleMC/limbgo/world/schematic"
	"go.minekube.com/common/minecraft/component"
	"golang.org/x/net/websocket"
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
	msgMu         sync.RWMutex
	messages      map[string]string
	dialogs       map[string]playermsg.DialogDoc
	targetsMu     sync.RWMutex
	targetNames   map[string]string
	authMu        sync.Mutex
	authed        map[limbgo.PlayerSession]authedSession
}

// authedSession remembers that THIS connection already proved its passport so
// the profile dialogs can act without re-authenticating. It is keyed by the
// live PlayerSession instance (one per connection) rather than by a derivable
// identity, so a second connection — even one claiming the same offline name,
// whose offline UUID is deterministic — can never inherit this grant. The
// resolved passport id is stored so downstream profile operations bind to the
// exact passport that authenticated, not a re-resolved login name.
type authedSession struct {
	passportID string
	username   string
	expires    time.Time
}

func (p *portal) markAuthed(session limbgo.PlayerSession, passportID, username string) {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	now := time.Now()
	for key, st := range p.authed {
		if now.After(st.expires) {
			delete(p.authed, key)
		}
	}
	p.authed[session] = authedSession{passportID: passportID, username: username, expires: now.Add(10 * time.Minute)}
}

func (p *portal) authedState(session limbgo.PlayerSession) (authedSession, bool) {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	st, ok := p.authed[session]
	if !ok || time.Now().After(st.expires) {
		delete(p.authed, session)
		return authedSession{}, false
	}
	return st, true
}

func (p *portal) clearAuthed(session limbgo.PlayerSession) {
	p.authMu.Lock()
	defer p.authMu.Unlock()
	delete(p.authed, session)
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
		authed:        map[limbgo.PlayerSession]authedSession{},
		messages:      map[string]string{},
		dialogs:       map[string]playermsg.DialogDoc{},
		targetNames:   map[string]string{},
	}
	if err := p.heartbeat(context.Background()); err != nil {
		logger.Warn("initial Authman heartbeat failed; portal starts locked", "err", err)
		p.setLocked(true)
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
			// No player name exists at handshake time; strip the token.
			return limbgo.RejectProtocol(p.message("limbo.kick.client_too_old", map[string]string{"player": ""}))
		}),
	}
	server, err := limbgo.NewServer(limbgo.Config{
		Addr:           cfg.Listen,
		ProtocolRouter: router,
		JoinResolver:   limbgo.JoinResolverFunc(p.resolveJoin),
		Events: limbgo.PlayerEventHandlerFuncs{
			Join:        p.handleJoin,
			DialogClick: p.handleDialogClick,
			Chat:        p.handleChat,
		},
		Logger: logger,
	})
	if err != nil {
		fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go p.heartbeatLoop(ctx)
	go p.eventLoop(ctx)

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
	for {
		if err := p.heartbeat(ctx); err != nil {
			p.logger.Warn("Authman heartbeat failed", "err", err)
		}
		if !sleepContext(ctx, p.heartbeatInterval()) {
			return
		}
	}
}

func (p *portal) heartbeat(ctx context.Context) error {
	res, err := p.client.heartbeat(ctx)
	if err != nil {
		return err
	}
	p.applyHeartbeat(res)
	return nil
}

func (p *portal) applyHeartbeat(res heartbeatResponse) {
	p.msgMu.Lock()
	p.nodeID = res.Node.ID
	p.runtime = res.RuntimeConfig
	p.locked = false
	p.messages = res.PlayerMessages.Messages
	p.dialogs = res.PlayerMessages.Dialogs
	p.msgMu.Unlock()
}

func (p *portal) eventLoop(ctx context.Context) {
	for {
		if !p.websocketEnabled() {
			if !sleepContext(ctx, p.heartbeatInterval()) {
				return
			}
			continue
		}
		if err := p.runEventStream(ctx); err != nil && !errors.Is(err, context.Canceled) {
			p.logger.Warn("Authman node event stream disconnected", "err", err)
		}
		wait := p.websocketReconnectMin()
		for {
			if !sleepContext(ctx, wait) {
				return
			}
			if !p.websocketEnabled() {
				break
			}
			if err := p.runEventStream(ctx); err != nil && !errors.Is(err, context.Canceled) {
				p.logger.Warn("Authman node event stream disconnected", "err", err)
				wait = minDuration(wait*2, p.websocketReconnectMax())
				continue
			}
			wait = p.websocketReconnectMin()
			break
		}
	}
}

func (p *portal) runEventStream(ctx context.Context) error {
	p.logger.Info("connecting Authman node event stream")
	ws, err := p.client.eventStream(ctx)
	if err != nil {
		return err
	}
	defer ws.Close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var event nodeEvent
		if err := websocket.JSON.Receive(ws, &event); err != nil {
			return err
		}
		switch event.Type {
		case "sync":
			p.applyHeartbeat(event.Data)
		case "revoked":
			p.logger.Warn("Authman node token revoked; portal locked")
			p.setLocked(true)
			return errors.New("node token revoked")
		case "ping":
			continue
		case "error":
			return fmt.Errorf("Authman node event error: %s", event.Error.Message)
		default:
			p.logger.Debug("ignored Authman node event", "type", event.Type)
		}
	}
}

func (p *portal) heartbeatInterval() time.Duration {
	p.msgMu.RLock()
	defer p.msgMu.RUnlock()
	seconds := clampInt(intFromMap(p.runtime, "heartbeat_interval_seconds"), 5, 3600)
	if seconds == 0 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func (p *portal) websocketEnabled() bool {
	p.msgMu.RLock()
	defer p.msgMu.RUnlock()
	return boolFromMap(p.runtime, "websocket_enabled", true)
}

func (p *portal) websocketReconnectMin() time.Duration {
	p.msgMu.RLock()
	defer p.msgMu.RUnlock()
	seconds := clampInt(intFromMap(p.runtime, "websocket_reconnect_min_seconds"), 1, 600)
	if seconds == 0 {
		seconds = 2
	}
	return time.Duration(seconds) * time.Second
}

func (p *portal) websocketReconnectMax() time.Duration {
	p.msgMu.RLock()
	defer p.msgMu.RUnlock()
	seconds := clampInt(intFromMap(p.runtime, "websocket_reconnect_max_seconds"), 1, 3600)
	if seconds == 0 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func (p *portal) isLocked() bool {
	p.msgMu.RLock()
	defer p.msgMu.RUnlock()
	return p.locked
}

func (p *portal) setLocked(locked bool) {
	p.msgMu.Lock()
	p.locked = locked
	p.msgMu.Unlock()
}

// rawMessage returns the effective MiniMessage source for a message key,
// preferring the Core-delivered value and falling back to built-in defaults.
func (p *portal) rawMessage(key string) string {
	p.msgMu.RLock()
	value := strings.TrimSpace(p.messages[key])
	p.msgMu.RUnlock()
	if value != "" {
		return value
	}
	return playermsg.Defaults("")[key]
}

// message renders a configured message into a component with sanitized vars.
func (p *portal) message(key string, vars map[string]string) component.Component {
	return playermsg.RenderComponent(p.rawMessage(key), vars)
}

// dialogDoc returns the effective dialog document for a screen.
func (p *portal) dialogDoc(screen string) playermsg.DialogDoc {
	p.msgMu.RLock()
	doc, ok := p.dialogs[screen]
	p.msgMu.RUnlock()
	if ok && strings.TrimSpace(doc.Title) != "" {
		return doc
	}
	return playermsg.DefaultDialog(screen)
}

// rememberTarget caches the downstream display name per requested host so
// message placeholders can use {server} without extra Core round-trips.
func (p *portal) rememberTarget(host string, target downstreamTarget) {
	name := strings.TrimSpace(target.DisplayName)
	if name == "" {
		name = strings.TrimSpace(target.ServerID)
	}
	if name == "" {
		return
	}
	p.targetsMu.Lock()
	p.targetNames[strings.ToLower(strings.TrimSpace(host))] = name
	p.targetsMu.Unlock()
}

func (p *portal) targetName(host string) string {
	p.targetsMu.RLock()
	defer p.targetsMu.RUnlock()
	return p.targetNames[strings.ToLower(strings.TrimSpace(host))]
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
	resolved, err := p.client.resolveTarget(ctx, host, 0, remoteIP(player.RemoteAddr))
	if err != nil {
		p.logger.Warn("using fallback limbo world; target resolve failed", "host", host, "err", err)
		return limbgo.JoinTarget{World: p.fallbackWorld, Spawn: p.fallbackSpawn}, nil
	}
	p.rememberTarget(host, resolved.Target)
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
	resolved, err := p.client.resolveTarget(ctx, host, 0, remoteIP(req.RemoteAddr))
	var description component.Component = &component.Text{Content: "Authman login portal"}
	if err == nil {
		p.rememberTarget(host, resolved.Target)
	}
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
	if p.isLocked() {
		return session.SendMessage(ctx, p.message("limbo.error.portal_locked", p.sessionVars(session)))
	}
	return p.showLoginDialog(ctx, session)
}

// handleChat is the universal recovery path: a player who dismissed the dialog
// (ESC) or got stranded on the waiting screen can type anything in chat to
// bring the auth dialog back.
func (p *portal) handleChat(ctx context.Context, session limbgo.PlayerSession, event *limbgo.ChatEvent) error {
	if p.isLocked() {
		return session.SendMessage(ctx, p.message("limbo.error.portal_locked", p.sessionVars(session)))
	}
	if _, ok := p.authedState(session); ok {
		resolved, err := p.client.resolvePlayer(ctx, session.Player())
		if err == nil {
			return p.routeProfiles(ctx, session, resolved)
		}
	}
	return p.showLoginDialog(ctx, session)
}

func (p *portal) handleDialogClick(ctx context.Context, session limbgo.PlayerSession, event *limbgo.DialogClickEvent) error {
	p.logger.Info("limbo dialog event", "player", event.Player.Name, "protocol", event.Protocol, "id", event.ID, "payload_bytes", len(event.Payload))
	switch event.ID {
	case "authman:login_submit", "authman:register_submit", "authman:profile_create_submit", "authman:profile_select_submit", "authman:open_screen":
	default:
		return p.showLoginDialog(ctx, session)
	}
	values := map[string]string{}
	if len(event.Payload) > 0 {
		parsed, err := parseDialogStringPayload(event.Payload)
		if err != nil {
			p.logger.Warn("failed to parse dialog payload", "player", event.Player.Name, "err", err)
			errorText := p.rawMessage("limbo.error.dialog_payload")
			if event.ID == "authman:register_submit" {
				return p.showRegisterDialogMessage(ctx, session, errorText)
			}
			return p.showLoginDialogMessage(ctx, session, errorText)
		}
		values = parsed
	}
	switch event.ID {
	case "authman:register_submit":
		doc := p.dialogDoc(playermsg.ScreenRegister)
		return p.registerAndTransfer(ctx, session,
			strings.TrimSpace(values[roleKeyOr(doc, playermsg.RolePassword, "password")]),
			strings.TrimSpace(values[roleKeyOr(doc, playermsg.RoleConfirm, "confirm_password")]))
	case "authman:profile_create_submit":
		doc := p.dialogDoc(playermsg.ScreenProfileCreate)
		return p.handleProfileCreateSubmit(ctx, session, strings.TrimSpace(values[roleKeyOr(doc, playermsg.RoleProfileName, "profile_name")]))
	case "authman:profile_select_submit":
		doc := p.dialogDoc(playermsg.ScreenProfileSelect)
		return p.handleProfileSelectSubmit(ctx, session, strings.TrimSpace(values[roleKeyOr(doc, playermsg.RoleProfileChoice, "profile_choice")]))
	case "authman:open_screen":
		return p.handleOpenScreen(ctx, session, strings.TrimSpace(values["screen"]))
	default:
		doc := p.dialogDoc(playermsg.ScreenLogin)
		return p.authenticateAndTransfer(ctx, session, strings.TrimSpace(values[roleKeyOr(doc, playermsg.RolePassword, "password")]))
	}
}

func roleKeyOr(doc playermsg.DialogDoc, role string, fallback string) string {
	if key := doc.RoleKey(role); key != "" {
		return key
	}
	return fallback
}

// sessionVars builds the sanitized placeholder values shared by all messages
// rendered for one player session.
func (p *portal) sessionVars(session limbgo.PlayerSession) map[string]string {
	player := session.Player()
	host := requestedHost(player.RequestedHost, p.cfg.DefaultHost)
	return map[string]string{
		"player": player.Name,
		"server": p.targetName(host),
	}
}

// dialogExtras carries runtime data for the profile dialog screens: the
// passport's profiles (for the profile_choice option input), a prefill for the
// profile name input, and whether profile creation is still allowed.
type dialogExtras struct {
	profiles    []profileSummary
	prefillName string
	canCreate   bool
}

// buildAuthDialog renders the configured free-form dialog document for one
// screen and runtime state into a limbgo dialog payload. Every element class
// (body, inputs, buttons) honours the same visibility conditions; functional
// behaviour comes from role bindings rather than fixed slots.
func (p *portal) buildAuthDialog(screen string, st playermsg.DialogState, errorText string, vars map[string]string, extras *dialogExtras) dialog.Raw {
	doc := p.dialogDoc(screen)
	render := func(text string) component.Component {
		if strings.Contains(text, "{error}") {
			text = playermsg.SubstituteRaw(text, map[string]string{"error": errorText})
		}
		return playermsg.RenderComponent(text, vars)
	}

	body := []dialog.Raw(nil)
	for _, el := range doc.Body {
		if !playermsg.VisibleWhen(el.When, st) {
			continue
		}
		switch el.Kind {
		case playermsg.BodyItem:
			item := dialog.Raw{"id": strings.TrimSpace(el.Item)}
			if el.Count > 1 {
				item["count"] = el.Count
			}
			var description any
			if strings.TrimSpace(el.Description) != "" {
				description = render(el.Description)
			}
			showTooltip := el.ShowTooltip
			if showTooltip == nil {
				showTooltip = dialog.Bool(true) // vanilla default
			}
			// Vanilla's ItemBody codec only accepts 1..256; clamp stored
			// out-of-range values instead of producing an undecodable packet.
			body = append(body, dialog.ItemWithDescription(item, description, dialog.Bool(el.ShowDecorations), showTooltip, clampDimension(el.Width, 256), clampDimension(el.Height, 256)))
		default:
			body = append(body, dialog.PlainMessage(render(el.Text), el.Width))
		}
	}

	inputs := []dialog.Raw(nil)
	for _, in := range doc.Inputs {
		if !playermsg.VisibleWhen(in.When, st) {
			continue
		}
		label := render(in.Label)
		if in.Role == playermsg.RoleProfileChoice && extras != nil {
			options := make([]dialog.Option, 0, len(extras.profiles))
			for _, profile := range extras.profiles {
				options = append(options, dialog.Option{ID: profile.ID, Display: &component.Text{Content: profile.ProtocolName}, Initial: profile.Primary})
			}
			inputs = append(inputs, dialog.SingleOptionInputWithOptions(in.Key, label, options, dialog.SingleOptionInputOptions{
				Width:        in.Width,
				LabelVisible: in.LabelVisible,
			}))
			continue
		}
		if in.Role == playermsg.RoleProfileName && extras != nil && strings.TrimSpace(in.Initial) == "" {
			in.Initial = extras.prefillName
		}
		switch in.Kind {
		case playermsg.InputBoolean:
			inputs = append(inputs, dialog.BooleanInput(in.Key, label, in.InitialBool, in.OnTrue, in.OnFalse))
		case playermsg.InputOption:
			options := make([]dialog.Option, 0, len(in.Options))
			for _, opt := range in.Options {
				options = append(options, dialog.Option{ID: opt.ID, Display: render(opt.Display), Initial: opt.Initial})
			}
			inputs = append(inputs, dialog.SingleOptionInputWithOptions(in.Key, label, options, dialog.SingleOptionInputOptions{
				Width:        in.Width,
				LabelVisible: in.LabelVisible,
			}))
		case playermsg.InputRange:
			inputs = append(inputs, dialog.NumberRangeInput(in.Key, label, dialog.NumberRangeOptions{
				Start:       in.Start,
				End:         in.End,
				Initial:     in.InitialNum,
				Step:        in.Step,
				Width:       in.Width,
				LabelFormat: in.LabelFormat,
			}))
		default:
			inputs = append(inputs, dialog.TextInput(in.Key, label, dialog.TextInputOptions{
				Initial:        in.Initial,
				MaxLength:      in.MaxLength,
				Width:          in.Width,
				LabelVisible:   in.LabelVisible,
				Multiline:      in.Multiline,
				MultilineLines: in.MultilineLines,
			}))
		}
	}

	buttons := []dialog.ActionButton(nil)
	for _, btn := range doc.Buttons {
		if !playermsg.VisibleWhen(btn.When, st) {
			continue
		}
		var action dialog.Raw
		switch btn.Action.Kind {
		case playermsg.ActionOpenURL:
			action = dialog.OpenURL(strings.TrimSpace(btn.Action.URL))
		case playermsg.ActionCopyToClipboard:
			action = dialog.CopyToClipboard(btn.Action.Value)
		case playermsg.ActionOpenScreen:
			if btn.Action.Screen == playermsg.ScreenProfileCreate && (extras == nil || !extras.canCreate) {
				continue
			}
			action = dialog.DynamicCustom("authman:open_screen", dialog.Raw{"screen": btn.Action.Screen})
		default:
			action = dialog.DynamicCustom("authman:"+screen+"_submit", dialog.Raw{"screen": screen})
		}
		button := dialog.ActionButton{Label: render(btn.Label), Width: btn.Width, Action: action}
		if strings.TrimSpace(btn.Tooltip) != "" {
			button.Tooltip = render(btn.Tooltip)
		}
		buttons = append(buttons, button)
	}

	if len(buttons) == 0 {
		// Defensive: a stored doc whose buttons are all condition-hidden must
		// never produce an empty multi_action (undecodable client-side).
		buttons = append(buttons, dialog.ActionButton{
			Label:  &component.Text{Content: "Continue"},
			Action: dialog.DynamicCustom("authman:"+screen+"_submit", dialog.Raw{"screen": screen}),
		})
	}

	afterAction := dialog.AfterActionWaitForResponse
	pause := doc.Pause
	if doc.AfterAction == playermsg.AfterNone {
		afterAction = dialog.AfterActionNone
		// Vanilla rejects pause=true with a non-unpausing after_action.
		pause = false
	}
	common := dialog.Common{
		Title:              render(doc.Title),
		Body:               body,
		Inputs:             inputs,
		CanCloseWithEscape: dialog.Bool(doc.CanCloseWithEscape),
		Pause:              dialog.Bool(pause),
		AfterAction:        afterAction,
	}
	if strings.TrimSpace(doc.ExternalTitle) != "" {
		common.ExternalTitle = render(doc.ExternalTitle)
	}
	if len(buttons) == 1 {
		return dialog.Notice(common, buttons[0])
	}
	return dialog.MultiAction(common, buttons, doc.Columns)
}

func clampDimension(value, max int) int {
	if value > max {
		return max
	}
	if value < 0 {
		return 0
	}
	return value
}

func (p *portal) showLoginDialog(ctx context.Context, session limbgo.PlayerSession) error {
	return p.showLoginDialogMessage(ctx, session, "")
}

func (p *portal) showLoginDialogMessage(ctx context.Context, session limbgo.PlayerSession, errorText string) error {
	vars := p.sessionVars(session)
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, p.message("limbo.kick.client_too_old", vars))
	}
	resolved, err := p.client.resolvePlayer(ctx, session.Player())
	if err != nil {
		var coreErr coreAPIError
		if errors.As(err, &coreErr) && coreErr.Status == http.StatusNotFound && !session.Player().Verified {
			return p.showRegisterDialog(ctx, session)
		}
		return session.SendMessage(ctx, p.message("limbo.error.resolve_failed", vars))
	}
	return session.ShowDialog(ctx, p.buildAuthDialog(playermsg.ScreenLogin, playermsg.DialogState{
		AuthRequired: resolved.Auth.Required,
		Verified:     session.Player().Verified,
		HasError:     strings.TrimSpace(errorText) != "",
	}, errorText, vars, nil))
}

func (p *portal) showRegisterDialog(ctx context.Context, session limbgo.PlayerSession) error {
	return p.showRegisterDialogMessage(ctx, session, "")
}

func (p *portal) showRegisterDialogMessage(ctx context.Context, session limbgo.PlayerSession, errorText string) error {
	vars := p.sessionVars(session)
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, p.message("limbo.kick.client_too_old", vars))
	}
	return session.ShowDialog(ctx, p.buildAuthDialog(playermsg.ScreenRegister, playermsg.DialogState{
		AuthRequired: true,
		HasError:     strings.TrimSpace(errorText) != "",
	}, errorText, vars, nil))
}

func (p *portal) authenticateAndTransfer(ctx context.Context, session limbgo.PlayerSession, password string) error {
	player := session.Player()
	resolved, err := p.client.resolvePlayer(ctx, player)
	if err != nil {
		// Re-present the dialog so wait_for_response clients are not stranded
		// on the waiting screen; showLoginDialogMessage degrades to chat if
		// resolve keeps failing.
		return p.showLoginDialogMessage(ctx, session, p.rawMessage("limbo.error.resolve_failed"))
	}
	if resolved.Auth.Required {
		if strings.TrimSpace(password) == "" {
			return p.showLoginDialogMessage(ctx, session, p.rawMessage("limbo.error.password_required"))
		}
		if err := p.client.authenticate(ctx, resolved.Auth.Username, password, remoteIP(player.RemoteAddr)); err != nil {
			return p.showLoginDialogMessage(ctx, session, p.rawMessage("limbo.error.invalid_password"))
		}
	}
	p.markAuthed(session, resolved.Passport.ID, resolved.Auth.Username)
	return p.routeProfiles(ctx, session, resolved)
}

func (p *portal) registerAndTransfer(ctx context.Context, session limbgo.PlayerSession, password string, confirmPassword string) error {
	if strings.TrimSpace(password) == "" || password != confirmPassword {
		return p.showRegisterDialogMessage(ctx, session, p.rawMessage("limbo.error.passwords_mismatch"))
	}
	player := session.Player()
	resolved, err := p.client.registerOffline(ctx, player.Name, password, player.RequestedHost, remoteIP(player.RemoteAddr))
	if err != nil {
		p.logger.Warn("offline registration failed", "player", session.Player().Name, "err", err)
		return p.showRegisterDialogMessage(ctx, session, p.rawMessage("limbo.error.register_failed"))
	}
	p.markAuthed(session, resolved.Passport.ID, resolved.Auth.Username)
	return p.routeProfiles(ctx, session, resolved)
}

// showAuthDialogError re-presents the dialog for the given screen with an
// error line so the player can retry instead of being left without a dialog.
func (p *portal) showAuthDialogError(ctx context.Context, session limbgo.PlayerSession, screen string, errorText string) error {
	switch screen {
	case playermsg.ScreenRegister:
		return p.showRegisterDialogMessage(ctx, session, errorText)
	case playermsg.ScreenProfileCreate, playermsg.ScreenProfileSelect:
		resolved, err := p.client.resolvePlayer(ctx, session.Player())
		if err != nil {
			return p.showLoginDialogMessage(ctx, session, p.rawMessage("limbo.error.resolve_failed"))
		}
		if screen == playermsg.ScreenProfileCreate {
			return p.showProfileCreateDialog(ctx, session, resolved, errorText)
		}
		return p.showProfileSelectDialog(ctx, session, resolved, errorText)
	default:
		return p.showLoginDialogMessage(ctx, session, errorText)
	}
}

// routeProfiles is the post-auth hub: with no profiles the player first
// creates one, otherwise the profile manager (selection dialog) is always
// shown so the player explicitly picks the identity to join with. The
// auto-join shortcut for single-profile passports is an opt-in setting.
func (p *portal) routeProfiles(ctx context.Context, session limbgo.PlayerSession, resolved resolveResponse) error {
	switch {
	case len(resolved.Profiles) == 0:
		return p.showProfileCreateDialog(ctx, session, resolved, "")
	case len(resolved.Profiles) == 1 && resolved.ProfilePolicy.AutoJoinSingle:
		return p.transferProfile(ctx, session, playermsg.ScreenProfileSelect, resolved.Profiles[0])
	default:
		return p.showProfileSelectDialog(ctx, session, resolved, "")
	}
}

// profileVars builds placeholder values for the profile dialog screens.
func (p *portal) profileVars(session limbgo.PlayerSession, resolved resolveResponse) map[string]string {
	vars := p.sessionVars(session)
	vars["count"] = strconv.Itoa(len(resolved.Profiles))
	vars["max"] = strconv.Itoa(resolved.ProfilePolicy.MaxProfiles)
	if name := strings.TrimSpace(resolved.Passport.Username); name != "" {
		vars["player"] = name
	}
	return vars
}

func (p *portal) showProfileCreateDialog(ctx context.Context, session limbgo.PlayerSession, resolved resolveResponse, errorText string) error {
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, p.message("limbo.kick.client_too_old", p.sessionVars(session)))
	}
	return session.ShowDialog(ctx, p.buildAuthDialog(playermsg.ScreenProfileCreate, playermsg.DialogState{
		AuthRequired: true,
		HasError:     strings.TrimSpace(errorText) != "",
	}, errorText, p.profileVars(session, resolved), &dialogExtras{
		prefillName: strings.TrimSpace(resolved.Passport.Username),
		canCreate:   resolved.ProfilePolicy.CanCreate,
		profiles:    resolved.Profiles,
	}))
}

func (p *portal) showProfileSelectDialog(ctx context.Context, session limbgo.PlayerSession, resolved resolveResponse, errorText string) error {
	if !session.Capabilities().Dialog {
		return session.Disconnect(ctx, p.message("limbo.kick.client_too_old", p.sessionVars(session)))
	}
	return session.ShowDialog(ctx, p.buildAuthDialog(playermsg.ScreenProfileSelect, playermsg.DialogState{
		AuthRequired: true,
		HasError:     strings.TrimSpace(errorText) != "",
	}, errorText, p.profileVars(session, resolved), &dialogExtras{
		canCreate: resolved.ProfilePolicy.CanCreate,
		profiles:  resolved.Profiles,
	}))
}

// requireAuthedResolve guards the profile dialog submits: the session must
// have completed authentication in this connection.
func (p *portal) requireAuthedResolve(ctx context.Context, session limbgo.PlayerSession) (resolveResponse, authedSession, bool, error) {
	st, ok := p.authedState(session)
	if !ok {
		p.logger.Warn("profile dialog submit without authed session", "player", session.Player().Name)
		err := p.showLoginDialog(ctx, session)
		return resolveResponse{}, authedSession{}, false, err
	}
	resolved, err := p.client.resolvePlayer(ctx, session.Player())
	if err != nil {
		p.logger.Warn("profile dialog resolve failed", "player", session.Player().Name, "err", err)
		return resolveResponse{}, authedSession{}, false, p.showLoginDialogMessage(ctx, session, p.rawMessage("limbo.error.resolve_failed"))
	}
	// Defend against any drift between the authenticated passport and the one
	// a re-resolve by login name would return (offline/premium name collision).
	if st.passportID != "" && resolved.Passport.ID != "" && st.passportID != resolved.Passport.ID {
		p.logger.Warn("profile dialog passport drift", "player", session.Player().Name, "authed", st.passportID, "resolved", resolved.Passport.ID)
		p.clearAuthed(session)
		return resolveResponse{}, authedSession{}, false, p.showLoginDialog(ctx, session)
	}
	return resolved, st, true, nil
}

func (p *portal) handleProfileCreateSubmit(ctx context.Context, session limbgo.PlayerSession, profileName string) error {
	resolved, st, ok, err := p.requireAuthedResolve(ctx, session)
	if !ok {
		return err
	}
	created, err := p.client.createProfile(ctx, st.passportID, st.username, profileName, remoteIP(session.Player().RemoteAddr))
	if err != nil {
		var coreErr coreAPIError
		errorKey := "limbo.error.profile_create_failed"
		if errors.As(err, &coreErr) {
			switch coreErr.Code {
			case "profile.invalid_name":
				errorKey = "limbo.error.profile_name_invalid"
			case "profile.name_taken":
				errorKey = "limbo.error.profile_name_taken"
			case "profile.limit_reached":
				errorKey = "limbo.error.profile_limit_reached"
			}
		}
		errorText := playermsg.Substitute(p.rawMessage(errorKey), p.profileVars(session, resolved))
		return p.showProfileCreateDialog(ctx, session, resolved, errorText)
	}
	if created.Player == nil {
		return p.showProfileCreateDialog(ctx, session, created, p.rawMessage("limbo.error.profile_create_failed"))
	}
	// Return to the profile manager so the player sees the new profile in the
	// list and explicitly joins with it.
	return p.showProfileSelectDialog(ctx, session, created, "")
}

func (p *portal) handleProfileSelectSubmit(ctx context.Context, session limbgo.PlayerSession, choice string) error {
	resolved, _, ok, err := p.requireAuthedResolve(ctx, session)
	if !ok {
		return err
	}
	for _, profile := range resolved.Profiles {
		if profile.ID == choice {
			return p.transferProfile(ctx, session, playermsg.ScreenProfileSelect, profile)
		}
	}
	return p.showProfileSelectDialog(ctx, session, resolved, p.rawMessage("limbo.error.profile_selection_invalid"))
}

func (p *portal) handleOpenScreen(ctx context.Context, session limbgo.PlayerSession, screen string) error {
	resolved, _, ok, err := p.requireAuthedResolve(ctx, session)
	if !ok {
		return err
	}
	switch screen {
	case playermsg.ScreenProfileCreate:
		return p.showProfileCreateDialog(ctx, session, resolved, "")
	case playermsg.ScreenProfileSelect:
		return p.showProfileSelectDialog(ctx, session, resolved, "")
	default:
		return p.routeProfiles(ctx, session, resolved)
	}
}

// transferProfile issues a transfer grant for one explicit profile and sends
// the player to the resolved downstream target.
func (p *portal) transferProfile(ctx context.Context, session limbgo.PlayerSession, screen string, profile profileSummary) error {
	player := session.Player()
	host := requestedHost(player.RequestedHost, p.cfg.DefaultHost)
	vars := p.sessionVars(session)
	playerIP := remoteIP(player.RemoteAddr)
	resolvedTarget, err := p.client.resolveTarget(ctx, host, player.ProtocolVersion, playerIP)
	if err != nil {
		return p.showAuthDialogError(ctx, session, screen, p.rawMessage("limbo.error.target_failed"))
	}
	target := resolvedTarget.Target
	p.rememberTarget(host, target)
	if name := strings.TrimSpace(target.DisplayName); name != "" {
		vars["server"] = name
	} else if target.ServerID != "" {
		vars["server"] = target.ServerID
	}
	grant, err := p.client.createGrant(ctx, profile.ID, profile.ProtocolName, target.ServerID, host, p.sourceID(), playerIP, player.ProtocolVersion)
	if err != nil {
		return p.showAuthDialogError(ctx, session, screen, p.rawMessage("limbo.error.grant_failed"))
	}
	caps := session.Capabilities()
	if !caps.StoreCookie || !caps.Transfer {
		if caps.Disconnect {
			return session.Disconnect(ctx, p.message("limbo.kick.transfer_unsupported", vars))
		}
		return session.SendMessage(ctx, p.message("limbo.kick.transfer_unsupported", vars))
	}
	_ = session.ClearDialog(ctx)
	if caps.ActionBar {
		_ = session.SendActionBar(ctx, p.message("limbo.success.actionbar", vars))
	}
	if caps.Title {
		_ = session.ShowTitle(ctx, limbgo.Title{
			Title:    p.message("limbo.success.title", vars),
			Subtitle: p.message("limbo.success.subtitle", vars),
			Times:    limbgo.TitleTimesTicks(5, 30, 5),
		})
	}
	if err := session.StoreCookie(ctx, p.transferCookieKey(), []byte(grant.Token)); err != nil {
		return err
	}
	if err := session.Transfer(ctx, grant.Target.TransferHost, grant.Target.TransferPort); err != nil {
		return err
	}
	p.clearAuthed(session)
	return nil
}

func (p *portal) sourceID() string {
	if strings.TrimSpace(p.cfg.SourceID) != "" {
		return strings.TrimSpace(p.cfg.SourceID)
	}
	p.msgMu.RLock()
	nodeID := strings.TrimSpace(p.nodeID)
	p.msgMu.RUnlock()
	if nodeID != "" {
		return nodeID
	}
	return strings.TrimSpace(p.cfg.NodeName)
}

func (p *portal) transferCookieKey() string {
	p.msgMu.RLock()
	key := strings.TrimSpace(stringFromMap(p.runtime, "transfer_cookie_key"))
	p.msgMu.RUnlock()
	if key != "" {
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
		value, err := r.ReadByte()
		if err != nil {
			return err
		}
		if name != "" {
			if value != 0 {
				values[name] = "true"
			} else {
				values[name] = "false"
			}
		}
		return nil
	case 2:
		raw, err := readN(r, 2)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(raw))), 10)
		}
		return nil
	case 3:
		raw, err := readN(r, 4)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(raw))), 10)
		}
		return nil
	case 4:
		raw, err := readN(r, 8)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = strconv.FormatInt(int64(binary.BigEndian.Uint64(raw)), 10)
		}
		return nil
	case 5:
		raw, err := readN(r, 4)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = strconv.FormatFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(raw))), 'f', -1, 32)
		}
		return nil
	case 6:
		raw, err := readN(r, 8)
		if err != nil {
			return err
		}
		if name != "" {
			values[name] = strconv.FormatFloat(math.Float64frombits(binary.BigEndian.Uint64(raw)), 'f', -1, 64)
		}
		return nil
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
	RuntimeConfig  map[string]any `json:"runtime_config"`
	PlayerMessages struct {
		Messages map[string]string              `json:"messages"`
		Dialogs  map[string]playermsg.DialogDoc `json:"dialogs"`
	} `json:"player_messages"`
}

type nodeEvent struct {
	Type  string            `json:"type"`
	Data  heartbeatResponse `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type profileSummary struct {
	ID           string `json:"id"`
	UUID         string `json:"uuid"`
	ProtocolName string `json:"protocol_name"`
	Primary      bool   `json:"primary"`
}

type profilePolicy struct {
	MaxProfiles    int  `json:"max_profiles"`
	CanCreate      bool `json:"can_create"`
	AutoJoinSingle bool `json:"auto_join_single_profile"`
}

type resolveResponse struct {
	Player *struct {
		UUID         string `json:"uuid"`
		Kind         string `json:"kind"`
		ProtocolName string `json:"protocol_name"`
	} `json:"player"`
	Passport struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Kind     string `json:"kind"`
		Locked   bool   `json:"locked"`
	} `json:"passport"`
	Profiles      []profileSummary `json:"profiles"`
	ProfilePolicy profilePolicy    `json:"profile_policy"`
	Auth          struct {
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
	DisplayName  string `json:"display_name"`
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

func (c *coreClient) eventStream(ctx context.Context) (*websocket.Conn, error) {
	config, err := websocket.NewConfig(coreWebSocketURL(c.cfg.CoreURL, "/api/node/events"), strings.TrimRight(strings.TrimSpace(c.cfg.CoreURL), "/"))
	if err != nil {
		return nil, err
	}
	config.Header.Set("Authorization", "Bearer "+c.cfg.NodeToken)
	config.Header.Set("X-Authman-Instance", instanceFingerprint(c.cfg))
	return config.DialContext(ctx)
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
		"remote_ip":          remoteIP(player.RemoteAddr),
		"profile_properties": coreProfileProperties(player.ProfileProperties),
	})
}

func (c *coreClient) resolveLoginPolicy(ctx context.Context, req limbgo.LoginRequest) (loginPolicyResponse, error) {
	return postJSON[loginPolicyResponse](ctx, c, "/api/node/limbo/login-policy", map[string]any{
		"username":         req.Username,
		"claimed_uuid":     req.ClaimedUUID,
		"remote_ip":        remoteIP(req.RemoteAddr),
		"protocol_version": req.ProtocolVersion,
		"requested_host":   req.RequestedHost,
	})
}

func (c *coreClient) authenticate(ctx context.Context, username, password string, remoteIP string) error {
	_, err := postJSON[map[string]any](ctx, c, "/api/node/players/authenticate", map[string]any{"username": username, "password": password, "remote_ip": remoteIP})
	return err
}

func (c *coreClient) registerOffline(ctx context.Context, username string, password string, requestedHost string, remoteIP string) (resolveResponse, error) {
	return postJSON[resolveResponse](ctx, c, "/api/node/players/register-offline", map[string]any{
		"username":       username,
		"password":       password,
		"requested_host": requestedHost,
		"remote_ip":      remoteIP,
	})
}

func (c *coreClient) resolveTarget(ctx context.Context, requestedHost string, protocolVersion int, remoteIP string) (targetResponse, error) {
	res, err := postJSON[targetResponse](ctx, c, "/api/node/limbo/targets/resolve", map[string]any{
		"requested_host":   requestedHost,
		"remote_ip":        remoteIP,
		"protocol_version": protocolVersion,
	})
	return res, err
}

func (c *coreClient) fetchBlueprint(ctx context.Context, id string) (limboBlueprintData, error) {
	return getJSON[limboBlueprintData](ctx, c, "/api/node/limbo/blueprints/"+url.PathEscape(strings.TrimSpace(id)))
}

func (c *coreClient) createGrant(ctx context.Context, playerID, username, serverID, requestedHost, source string, remoteIP string, protocolVersion int) (grantResponse, error) {
	return postJSON[grantResponse](ctx, c, "/api/node/limbo/transfer-grants", map[string]any{
		"player_id":        playerID,
		"username":         username,
		"server_id":        serverID,
		"requested_host":   requestedHost,
		"source":           source,
		"remote_ip":        remoteIP,
		"protocol_version": protocolVersion,
	})
}

func (c *coreClient) createProfile(ctx context.Context, passportID, username, protocolName string, remoteIP string) (resolveResponse, error) {
	return postJSON[resolveResponse](ctx, c, "/api/node/profiles/create", map[string]any{
		"passport_id":   passportID,
		"username":      username,
		"protocol_name": protocolName,
		"remote_ip":     remoteIP,
	})
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return strings.Trim(strings.TrimSpace(addr.String()), "[]")
	}
	return strings.Trim(strings.TrimSpace(host), "[]")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, coreURL(c.cfg.CoreURL, path), bytes.NewReader(payload))
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL(c.cfg.CoreURL, path), nil)
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

func coreURL(base string, path string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/" + strings.TrimLeft(path, "/")
}

func coreWebSocketURL(base string, path string) string {
	raw := coreURL(base, path)
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http", "":
		parsed.Scheme = "ws"
	}
	return parsed.String()
}

func instanceFingerprint(cfg config) string {
	seed := cfg.NodeName + "|" + cfg.Listen + "|" + cfg.CoreURL
	return "limbo-" + strconv.FormatUint(fnv64(seed), 16)
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func intFromMap(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		return 0
	}
}

func boolFromMap(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
		return fallback
	default:
		return fallback
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
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
