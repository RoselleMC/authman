# Authman Limbo API

This document covers the embeddable Go API used by Authman's managed Limbo
process and by other Go integrations.

## Server Setup

Create a server with `limbgo.NewServer` and provide a protocol router, world
source, spawn resolver, status policy, and optional player event handler:

```go
motd, err := limbgo.ParseMiniMessage("<gold>limbgo</gold>")
if err != nil {
	return err
}
srv, err := limbgo.NewServer(limbgo.Config{
	Addr: ":25565",
	ProtocolRouter: limbo.Router{
		MOTD:              motd,
		StatusRateLimiter: limbgo.NewRateLimiter(limbgo.RateLimitConfig{}),
	},
	Worlds:        worlds,
	SpawnResolver: limbgo.StaticSpawn(spawn),
	Events:        events,
})
if err != nil {
	return err
}
return srv.ListenAndServe(ctx)
```

When limbgo is behind a trusted TCP router that sends HAProxy PROXY protocol
headers, such as Gate lite routes with `proxyProtocol: true`, enable PROXY
protocol handling on the server:

```go
srv, err := limbgo.NewServer(limbgo.Config{
	Addr:           ":25565",
	ProtocolRouter: limbo.Router{},
	Worlds:         worlds,
	SpawnResolver:  limbgo.StaticSpawn(spawn),
	ProxyProtocol: limbgo.ProxyProtocolConfig{
		Enabled:                true,
		Required:               true,
		TrustedProxies:         []string{"192.0.2.0/24", "127.0.0.1"},
		RestrictTrustedProxies: true,
	},
})
```

With this enabled, `RemoteAddr` in `StatusRequest`, `ProtocolRequest`,
`LoginRequest`, and `Player` is the client address from the trusted PROXY
header. Keep `TrustedProxies` scoped to the Gate/container/host network; do not
trust public client addresses. If `RestrictTrustedProxies` is false and
`TrustedProxies` is empty, limbgo accepts PROXY headers from any upstream.

If an embedding application owns its listener or connection lifecycle, it can
wrap individual connections before passing them to a protocol router:

```go
wrapped, err := limbgo.WrapProxyProtocolConn(conn, limbgo.ProxyProtocolConfig{
	Enabled:  true,
	Required: true,
})
if err != nil {
	_ = conn.Close()
	return err
}
err = router.ServeConn(ctx, wrapped, services)
```

`Config.Worlds` is used by the default world resolver. For applications that
need to select a different world instance per player, prefer `JoinResolver`.

```go
srv, err := limbgo.NewServer(limbgo.Config{
	Addr:           ":25565",
	ProtocolRouter: limbo.Router{},
	JoinResolver: limbgo.JoinResolverFunc(func(ctx context.Context, player limbgo.Player) (limbgo.JoinTarget, error) {
		world := pickWorldFor(player)
		return limbgo.JoinTarget{
			World: world,
			Spawn: limbgo.SpawnTarget{
				Position: limbgo.Vec3{X: 0, Y: 65, Z: 0},
				GameMode: limbgo.GameModeAdventure,
			},
		}, nil
	}),
})
```

`JoinTarget.World` is a full `World` object. The schematic is only one possible
data source for that object; world time, dimension/environment, height, logical
height, skylight, ambient light, coordinate scale, visual effects, and spawn
metadata travel with the world instance. If a resolver owns temporary per-player
worlds, it may also implement `JoinReleaser` to clean them up when the
connection closes.

`Dimension` intentionally exposes only the subset that matters for a limbo
login/chunk view. Modern clients still receive complete `dimension_type`
registry data, but gameplay-only protocol fields such as bed behavior, raids,
piglin safety, infiniburn, and monster spawn settings are derived internally
from vanilla-like presets instead of being API or file-config inputs.

For a zero-asset limbo world, use `DefaultWorld` and `DefaultSpawn`:

```go
spawn := limbgo.DefaultSpawn("default")
world := limbgo.DefaultWorld("default")
```

The default world is air plus one `minecraft:bedrock` block directly below the
spawn position. The standalone binary uses this same world when
`world.schematic` is omitted. If both `world.schematic` and `spawn.pos` are
omitted in the file config, `spawn.pos` defaults to `{X: 0, Y: 65, Z: 0}` and
the bedrock block is placed at `0,64,0`.

Use `DefaultWorldWithDimension` when you want the same no-schematic world but
with custom dimension properties:

```go
world := limbgo.DefaultWorldWithDimension("nether-login", limbgo.DimensionPreset(limbgo.DimensionNether, 256))
```

## Login Authentication

limbgo defaults to offline-mode login: the client claims a username, limbgo
derives the vanilla offline UUID, and `Player.Verified` is false. Applications
must treat this as an unverified claim.

For Mojang or custom Yggdrasil online-mode verification, configure the protocol
router:

```go
router := limbo.Router{
	LoginMode: limbgo.LoginModeOnline,
	YggdrasilVerifier: limbgo.YggdrasilVerifierConfig{
		BaseURL: "", // empty uses https://sessionserver.mojang.com
	},
}
```

Set `YggdrasilVerifierConfig.BaseURL` to a custom sessionserver root when using
a third-party Yggdrasil ecosystem:

```go
router := limbo.Router{
	LoginMode: limbgo.LoginModeOnline,
	YggdrasilVerifier: limbgo.YggdrasilVerifierConfig{
		BaseURL: "https://session.example.net",
	},
}
```

Applications that need their own cache, proxy pool, audit, or trust policy can
provide a callback verifier. limbgo still performs the vanilla encryption
challenge and supplies the sessionserver-compatible proof.

```go
router := limbo.Router{
	LoginMode: limbgo.LoginModeOnline,
	SessionVerifier: limbgo.SessionVerifierFunc(func(ctx context.Context, proof limbgo.SessionProof) (limbgo.VerifiedProfile, error) {
		return verifyWithApplication(ctx, proof)
	}),
}
```

Hybrid deployments can attempt online-mode session proof first and continue as
an unverified offline-mode player when the verifier reports an invalid session:

```go
router := limbo.Router{
	LoginMode:       limbgo.LoginModeHybrid,
	SessionVerifier: appVerifier,
}
```

Only errors wrapping `limbgo.ErrInvalidLogin` fall back to offline mode. Verifier
outages such as rate limits, proxy failures, or upstream 5xx errors should wrap
`limbgo.ErrSessionUnavailable` or another non-invalid error so the connection is
rejected instead of silently downgrading identity. Hybrid fallback is best
effort: after limbgo sends an Encryption Request, an invalid-session client may
disconnect before sending an Encryption Response. If an application needs a
deterministic online/offline split, make that decision before encryption with
`LoginPolicy` or `LoginDecisionPolicy`.

Deployments can also choose offline or online mode per connection before any
session proof is requested:

```go
router := limbo.Router{
	LoginPolicy: limbgo.LoginPolicyFunc(func(ctx context.Context, req limbgo.LoginRequest) (limbgo.LoginMode, error) {
		if shouldRequireOnline(req.Username, req.ClaimedUUID) {
			return limbgo.LoginModeOnline, nil
		}
		return limbgo.LoginModeOffline, nil
	}),
	SessionVerifier: appVerifier,
}
```

Use `LoginDecisionPolicy` when the application also needs to replace the
runtime identity for a forced-offline connection:

```go
router := limbo.Router{
	LoginDecisionPolicy: limbgo.LoginPolicyV2Func(func(ctx context.Context, req limbgo.LoginRequest) (limbgo.LoginDecision, error) {
		if claimedUUIDMatchesPremiumProfile(req.Username, req.ClaimedUUID) {
			return limbgo.LoginDecision{Mode: limbgo.LoginModeOnline}, nil
		}
		return limbgo.LoginDecision{
			Mode: limbgo.LoginModeOffline,
			Profile: &limbgo.LoginProfile{
				Name: "offline:" + req.Username,
				UUID: allocateOfflineRuntimeUUID(req.Username),
				Properties: []limbgo.ProfileProperty{{
					Name:  "textures",
					Value: runtimeTextureValue(req.Username),
				}},
			},
		}, nil
	}),
	SessionVerifier: appVerifier,
}
```

`ModeOffline` with a nil `Profile` keeps the default
`OfflineLoginPlayer(req)` behavior. `ModeOffline` with a `Profile` uses that
runtime name, UUID, and profile properties in Login Success and in
`PlayerSession.Player()`. `ModeOnline` and successfully verified hybrid logins
ignore the override and use the `VerifiedProfile` returned by
`SessionVerifier`. When both `LoginDecisionPolicy` and the older `LoginPolicy`
are set, `LoginDecisionPolicy` runs.

`LoginRequest.ClaimedUUID` is the UUID declared by the client in `login_start`,
formatted with dashes when the protocol version carries one. It is empty on
older protocols or when an optional UUID field is absent. Treat it as a routing
hint only; online-mode identity is still established exclusively by
`SessionVerifier`.

`Player` exposes both the selected mode and the resulting trust metadata:
`LoginMode`, `AuthSource`, `Verified`, `Name`, `UUID`, `Properties`, and
`ProfileProperties`. Offline sessions use `AuthSourceOffline` and remain
unverified. Verified sessions carry the UUID/name/properties returned by the
configured verifier.

## Protocol Admission

Use `ProtocolPolicy` when an application wants to allow only specific client
protocol ranges. The policy runs immediately after the Minecraft handshake,
before the username is parsed and before session authentication starts.

```go
router := limbo.Router{
	ProtocolPolicy: limbgo.ProtocolRangePolicy(
		770,
		774,
		&component.Text{Content: "Please use Minecraft 1.21.5-1.21.11"},
	),
}
```

For custom logic, implement `ProtocolPolicy` or use `ProtocolPolicyFunc`:

```go
router := limbo.Router{
	ProtocolPolicy: limbgo.ProtocolPolicyFunc(func(ctx context.Context, req limbgo.ProtocolRequest) error {
		if req.ProtocolVersion < 770 || req.ProtocolVersion > 774 {
			return limbgo.RejectProtocolText("Unsupported Minecraft version")
		}
		return nil
	}),
}
```

`ProtocolRequest` includes `ProtocolVersion`, `RequestedHost`, `RemoteAddr`,
`ProtocolKnown`, and `PortalDialog`. The capability fields come from the same
immutable protocol-pack snapshot that will encode the rest of this connection,
so a hot update cannot make admission and packet handling observe different
packs. Return `RejectProtocol` or `RejectProtocolText` to send a client-facing
login disconnect reason. Returning any other error aborts the connection
without treating it as a normal protocol denial.

## Registry Data Bundles

Modern Minecraft clients validate dynamic registries during the configuration
phase. limbgo keeps those registries as versioned data assets instead of
hard-coding them in protocol code. The repository stores one full JSON file per
protocol under `protocol/registrydata/protocols/<protocol>.json`, and embeds
`protocol/registrydata/registrydata.zip` in the binary.

For API users that want future protocol registry fixes without restarting the
server instance, use a hot-swappable store as the router's registry source:

```go
registryStore, err := registrydata.NewDefaultStore()
if err != nil {
	return err
}

router := limbo.Router{
	RegistryDataSource: registryStore,
}

srv, err := limbgo.NewServer(limbgo.Config{
	Addr:           ":25565",
	ProtocolRouter: router,
	JoinResolver:   joins,
})
```

When a new registry bundle is available, update the store. Existing player
connections keep the registry snapshot they joined with; later connections use
the new bundle.

```go
if err := registryStore.UpdateZipFile("/srv/limbgo/registrydata-776.zip"); err != nil {
	return err
}
```

The zip contains protocol JSON files named by protocol number, for example
`775.json`. Each file declares `format_version`, `protocol`, complete
`registries`, `tags`, and optional legacy `dimension_codec` / `dimension`
payloads. Updating registry data can fix configuration-phase registry changes;
packet IDs, packet field layouts, chunk data shape, and block-state ID tables
remain protocol adapter concerns.

## Status And MOTD

For static server-list data, set fields directly on `limbo.Router`:

```go
motd, err := limbgo.ParseMiniMessage("<gradient:#55ff55:#55ffff>limbgo</gradient>")
if err != nil {
	return err
}
router := limbo.Router{
	MOTD:                motd,
	VersionName:         "limbgo",
	MaxPlayers:          100,
	OnlinePlayers:       3,
	SamplePlayers:       []limbgo.StatusSamplePlayer{{Name: "Score2", ID: "00000000-0000-0000-0000-000000000002"}},
	EnforcesSecureChat:  limbgo.Bool(false),
	PreventsChatReports: limbgo.Bool(true),
	StatusRateLimiter:   limbgo.NewRateLimiter(limbgo.RateLimitConfig{}),
}
```

For dynamic MOTD, player samples, favicon, or protocol-specific status, provide
a `StatusProvider`:

```go
router := limbo.Router{
	StatusProvider: limbgo.StatusProviderFunc(func(ctx context.Context, req limbgo.StatusRequest) (limbgo.Status, error) {
		return limbgo.Status{
			VersionName: "limbgo",
			Protocol:    req.Protocol,
			Description: &component.Text{Content: "Welcome " + req.Address},
			MaxPlayers:  100,
		}, nil
	}),
	StatusRateLimiter: limbgo.NewRateLimiter(limbgo.RateLimitConfig{
		Requests: 60,
		Window:   time.Second,
	}),
}
```

`StatusRequest` includes the handshake protocol, requested address/port, and
remote address. This keeps MOTD logic in API code rather than in protocol
adapters.

## Player Events

Attach `Config.Events` to observe player input after the limbo join sequence has
completed. If `Events` is nil, limbgo keeps the old minimal behavior and returns
after writing the join/chunk sequence.

```go
events := limbgo.PlayerEventHandlerFuncs{
	Join: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.JoinEvent) error {
		return session.SendMessage(ctx, &component.Text{Content: "ready"})
	},
	Chat: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.ChatEvent) error {
		return session.SendMessage(ctx, &component.Text{Content: "chat accepted"})
	},
	Command: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.CommandEvent) error {
		return session.SendMessage(ctx, &component.Text{Content: "command accepted"})
	},
	DialogClick: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.DialogClickEvent) error {
		return session.SendMessage(ctx, &component.Text{Content: event.ID})
	},
}
```

The handler interface is:

```go
type PlayerEventHandler interface {
	HandleJoin(ctx context.Context, session PlayerSession, event *JoinEvent) error
	HandleChat(ctx context.Context, session PlayerSession, event *ChatEvent) error
	HandleCommand(ctx context.Context, session PlayerSession, event *CommandEvent) error
	HandleDialogClick(ctx context.Context, session PlayerSession, event *DialogClickEvent) error
}

type ResourcePackResponseHandler interface {
	HandleResourcePackResponse(ctx context.Context, session PlayerSession, event *ResourcePackResponseEvent) error
}
```

`PlayerEventHandlerFuncs` is a convenience adapter. For stateful applications,
define your own type implementing `PlayerEventHandler`. Implement
`ResourcePackResponseHandler` as well when the handler needs resource-pack
status events.

## Player Session

Event handlers receive a `PlayerSession`:

```go
type PlayerSession interface {
	Player() Player
	Capabilities() SessionCapabilities
	SendMessage(ctx context.Context, message component.Component) error
	SendActionBar(ctx context.Context, message component.Component) error
	ShowTitle(ctx context.Context, title Title) error
	ClearTitle(ctx context.Context, reset bool) error
	ShowDialog(ctx context.Context, dialog dialog.Dialog) error
	ClearDialog(ctx context.Context) error
	AddResourcePack(ctx context.Context, pack ResourcePack) error
	RemoveResourcePack(ctx context.Context, id string) error
	StoreCookie(ctx context.Context, key string, value []byte) error
	Transfer(ctx context.Context, host string, port int) error
	Disconnect(ctx context.Context, reason component.Component) error
}

type SessionCapabilities struct {
	SystemMessage      bool
	ActionBar          bool
	Title              bool
	Dialog             bool
	ResourcePack       bool
	RemoveResourcePack bool
	StoreCookie        bool
	Transfer           bool
	Disconnect         bool
}
```

`Player()` returns the connected player metadata. `SendMessage` writes a system
message. `SendActionBar`, `ShowTitle`, and `ClearTitle` write the vanilla
message overlay packets. `ShowDialog` and `ClearDialog` are available for
clients whose protocol contains the official dialog packets.

`Capabilities()` lets portal code branch on vanilla feature support without
touching protocol numbers. If a session method is called when unsupported, it
returns `ErrUnsupportedCapability`.

Auth portals can complete a vanilla transfer flow without protocol-specific
code:

```go
events := limbgo.PlayerEventHandlerFuncs{
	Command: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.CommandEvent) error {
		if event.Command != "login-ok" {
			return nil
		}
		caps := session.Capabilities()
		if !caps.StoreCookie || !caps.Transfer {
			return session.Disconnect(ctx, &component.Text{Content: "Please use Minecraft 1.20.5+"})
		}
		if err := session.StoreCookie(ctx, "authman:transfer", grantToken); err != nil {
			return err
		}
		return session.Transfer(ctx, "play.example.net", 25565)
	},
}
```

`StoreCookie` writes a vanilla store-cookie packet, `Transfer` writes the
vanilla transfer packet, and `Disconnect` writes a rich-text kick reason before
closing the connection.

## Resource Packs

Resource pack delivery follows the same shape as Velocity's resource-pack
offer API: the application creates a pack description, sends it through the
player session, and listens for status changes.

```go
type ResourcePack struct {
	ID       string
	URL      string
	Hash     string
	Required bool
	Prompt   component.Component
}

type ResourcePackResponseEvent struct {
	Player     Player
	ID         string
	Pack       ResourcePack
	Status     ResourcePackStatus
	StatusCode int32
	Protocol   int
}
```

```go
pack := limbgo.ResourcePack{
	ID:       "lobby-pack",
	URL:      "https://cdn.example.net/lobby.zip",
	Hash:     "0123456789abcdef0123456789abcdef01234567",
	Required: true,
	Prompt:   &component.Text{Content: "This server requires its resource pack."},
}

events := limbgo.PlayerEventHandlerFuncs{
	Join: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.JoinEvent) error {
		if !session.Capabilities().ResourcePack {
			return session.Disconnect(ctx, &component.Text{Content: "Resource packs are not supported by this client."})
		}
		return session.AddResourcePack(ctx, pack)
	},
	ResourcePackResponse: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.ResourcePackResponseEvent) error {
		if event.ID == "lobby-pack" && event.Status == limbgo.ResourcePackDeclined && event.Pack.Required {
			return session.Disconnect(ctx, &component.Text{Content: "Please accept the resource pack."})
		}
		return nil
	},
}
```

`ResourcePack.ID` is an application-stable identifier. On modern clients,
limbgo derives the protocol UUID from it unless it is already a dashed UUID,
then maps client responses back to the original ID. On legacy protocols that do
not include a pack ID in responses, limbgo reports the last pack sent through
that session.

`RemoveResourcePack` is available only on protocols with the vanilla
`remove_resource_pack` packet, currently 1.20.3+ in the supported adapter set.
Older clients can receive a pack but cannot remove a specific pack by ID.
Status values include `ResourcePackAccepted`, `ResourcePackDeclined`,
`ResourcePackDownloaded`, `ResourcePackInvalidURL`,
`ResourcePackFailedDownload`, `ResourcePackFailedReload`,
`ResourcePackSuccessfullyLoaded`, and `ResourcePackDiscarded`. `StatusCode`
preserves the raw protocol value for future client statuses.

For custom handler types, implement `ResourcePackResponseHandler` in addition
to `PlayerEventHandler` to receive status events. `PlayerEventHandlerFuncs`
already wires this optional hook.

## Rich Text

limbgo uses Minekube rich text components instead of defining its own text
model:

```go
import "go.minekube.com/common/minecraft/component"
```

Any API field that takes `component.Component` supports rich text. That includes
chat/system messages, actionbar messages, title/subtitle overlays, and dialog
title, external title, body text, button labels, button tooltips, input labels,
option display labels, and resource-pack prompts.

Protocol adapters serialize rich text as JSON for older clients and anonymous
NBT for modern clients that require it.

MiniMessage can be parsed with:

```go
message, err := limbgo.ParseMiniMessage("<red><bold>Hello</bold></red>")
```

The parser is lenient: malformed or currently unrepresentable tags remain
literal text instead of crashing the connection.

## Actionbar And Titles

Actionbar and title APIs use the same Minekube component model as chat and
dialogs:

```go
events := limbgo.PlayerEventHandlerFuncs{
	Command: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.CommandEvent) error {
		if event.Command != "notice" {
			return nil
		}
		action, err := limbgo.ParseMiniMessage("<green>Login accepted</green>")
		if err != nil {
			return err
		}
		title, err := limbgo.ParseMiniMessage("<gold><bold>Welcome</bold></gold>")
		if err != nil {
			return err
		}
		subtitle := &component.Text{Content: "Preparing transfer"}
		if session.Capabilities().ActionBar {
			if err := session.SendActionBar(ctx, action); err != nil {
				return err
			}
		}
		if session.Capabilities().Title {
			return session.ShowTitle(ctx, limbgo.Title{
				Title:    title,
				Subtitle: subtitle,
				Times:    limbgo.TitleTimesTicks(10, 40, 10),
			})
		}
		return nil
	},
}
```

`ClearTitle(ctx, reset)` clears the current title. When `reset` is true, vanilla
clients also reset title timings to their defaults. Protocol adapters send
legacy title action packets for older clients and split actionbar/title packets
for modern clients.

## Join Ready

`JoinEvent` is emitted after limbgo has sent the login, position, time, and
spawn chunk packets for the selected world. This is the first callback where
session methods are safe to call without waiting for player chat or commands.

```go
events := limbgo.PlayerEventHandlerFuncs{
	Join: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.JoinEvent) error {
		if !session.Capabilities().Dialog {
			return session.SendMessage(ctx, &component.Text{Content: "Please use Minecraft 1.21.6+"})
		}
		return session.ShowDialog(ctx, loginDialog)
	},
}
```

The callback is intended for login portal flows: show a dialog immediately,
receive `DialogClickEvent`, then call `StoreCookie` and `Transfer` when the
player is authenticated.

## Chat And Commands

`JoinEvent` is emitted when the session is ready for server-initiated packets.
`ChatEvent` is emitted when the player sends chat text. `CommandEvent` is
emitted when the player sends a command or, on legacy protocols, when a chat
message starts with `/`.

```go
type JoinEvent struct {
	Player   Player
	Protocol int
}

type ChatEvent struct {
	Player   Player
	Message  string
	Protocol int
	Canceled bool
}

type CommandEvent struct {
	Player   Player
	Command  string
	Protocol int
	Canceled bool
}
```

The `Command` field does not include the leading `/`.

## Dialog UI

The official dialog UI appears in the generated protocol data after Minecraft
1.21.5. In limbgo terms, use dialog APIs when the client protocol has
`show_dialog`, `clear_dialog`, and `custom_click_action`; current generated data
starts at protocol `771`.

Import the helper package:

```go
import "github.com/RoselleMC/authman/limbo/dialog"
```

A minimal notice dialog:

```go
err := session.ShowDialog(ctx, dialog.Notice(dialog.Common{
	Title: dialog.Text("Welcome"),
	Body: []dialog.Raw{
		dialog.PlainMessage(dialog.Text("Pick a destination"), 220),
	},
	Pause:       dialog.Bool(false),
	AfterAction: dialog.AfterActionWaitForResponse,
}, dialog.Button(
	dialog.Text("Continue"),
	dialog.DynamicCustom("limbgo:continue", dialog.Raw{"screen": "spawn"}),
)))
```

A richer dialog with Minekube components:

```go
err := session.ShowDialog(ctx, dialog.Notice(dialog.Common{
	Title: &component.Text{
		Content: "Welcome",
		Extra: []component.Component{
			&component.Text{Content: " player"},
		},
	},
	Body: []dialog.Raw{
		dialog.PlainMessage(&component.Text{Content: "Choose an action"}, 220),
	},
	Inputs: []dialog.Raw{
		dialog.TextInput("name", &component.Text{Content: "Name"}, dialog.TextInputOptions{
			Initial:   event.Player.Name,
			MaxLength: 32,
		}),
		dialog.NumberRangeInput("level", &component.Text{Content: "Level"}, dialog.NumberRangeOptions{
			Start:       1,
			End:         10,
			Initial:     dialog.Float(4.5),
			Step:        dialog.Float(0.5),
			LabelFormat: "options.generic_value",
		}),
	},
	CanCloseWithEscape: dialog.Bool(true),
	Pause:              dialog.Bool(false),
	AfterAction:        dialog.AfterActionWaitForResponse,
}, dialog.ActionButton{
	Label:   &component.Text{Content: "Submit"},
	Tooltip: &component.Text{Content: "Send rich payload"},
	Action:  dialog.DynamicCustom("limbgo:submit", dialog.Raw{"source": "spawn"}),
}))
```

To close any currently open dialog:

```go
err := session.ClearDialog(ctx)
```

### Dialog Constructors

Dialog types:

- `dialog.Notice`
- `dialog.Confirmation`
- `dialog.MultiAction`
- `dialog.MultiActionWithExit`
- `dialog.DialogList`
- `dialog.ServerLinks`
- `dialog.ServerLinksWithOptions`

Body helpers:

- `dialog.PlainMessage`
- `dialog.Item`
- `dialog.ItemDescription`
- `dialog.ItemWithDescription`

Input helpers:

- `dialog.TextInput`
- `dialog.BooleanInput`
- `dialog.SingleOptionInput`
- `dialog.SingleOptionInputWithOptions`
- `dialog.NumberRangeInput`

Button and action helpers:

- `dialog.Button`
- `dialog.RunCommand`
- `dialog.SuggestCommand`
- `dialog.OpenURL`
- `dialog.CopyToClipboard`
- `dialog.ChangePage`
- `dialog.ShowDialog`
- `dialog.Custom`
- `dialog.DynamicRunCommand`
- `dialog.DynamicCustom`

Common optional pointer helpers:

- `dialog.Bool`
- `dialog.Float`

### Custom Dialog Actions

Use `dialog.Custom` or `dialog.DynamicCustom` to receive a serverbound
`DialogClickEvent`:

```go
events := limbgo.PlayerEventHandlerFuncs{
	DialogClick: func(ctx context.Context, session limbgo.PlayerSession, event *limbgo.DialogClickEvent) error {
		switch event.ID {
		case "limbgo:submit":
			// event.Payload is the raw anonymous NBT body when the client sends one.
			return session.SendMessage(ctx, dialog.Text("submitted"))
		default:
			return nil
		}
	},
}
```

`DialogClickEvent` contains:

```go
type DialogClickEvent struct {
	Player   Player
	ID       string
	Payload  []byte
	Protocol int
	Canceled bool
}
```

`Payload` is kept as raw anonymous NBT. This avoids baking a long-lived NBT
schema into limbgo while still exposing the full client response to applications
that want to decode it.

## Raw Data And Future Versions

The dialog package intentionally exposes `dialog.Raw`:

```go
raw := dialog.Raw{
	"type":  "minecraft:notice",
	"title": dialog.Text("Config loaded"),
	"body": []dialog.Raw{
		{"type": "minecraft:plain_message", "contents": dialog.Text("Hello")},
	},
}
err := session.ShowDialog(ctx, raw)
```

Use `Raw` for config-loaded dialogs, exact vanilla fields not covered by helper
functions, or future fields added by Minecraft before limbgo grows a typed
wrapper. Nested Minekube `component.Component` values inside `Raw` are still
serialized as rich text.
