# Authman Limbo

This directory contains Authman's Go-native Minecraft Java Edition Limbo
runtime. It accepts players, places them in a schematic-backed or default world,
drives Authman's dialog authentication flow, and transfers authenticated players
to a downstream server.

The code has two supported integration surfaces:

- the reusable Go packages under `limbo/`;
- the Authman-managed `limbo/cmd/authman-limbo` process, which receives runtime
  configuration and protocol-pack updates from Core.

## Compatibility Strategy

The protocol layer must not be a hand-maintained list of packet IDs and field
layouts. The intended implementation is:

- keep the runtime server and API in Go;
- generate protocol adapters from external Minecraft protocol/version data, such
  as PrismarineJS `minecraft-data`;
- keep world data version-neutral internally, then translate block palettes,
  registry data, login packets, and chunk packets at the protocol edge;
- use the client-sent protocol number from the handshake to select the adapter;
- for unknown future versions, try the newest compatible adapter only when the
  generated data marks the packet shapes as unchanged.

This keeps manual maintenance concentrated in generator rules and compatibility
tests, not in per-version packet tables.

Packet, version, and block-state artifacts are produced from
`minecraft-data/data/pc`:

- `protocol/versions/versions_gen.go`
- `protocol/packetid/packetid_gen.go`
- `protocol/blockstate/blockstate_gen.go`

Modern registry payloads are kept under
`protocol/registrydata/protocols/<protocol>.json` and bundled into
`protocol/registrydata/registrydata.zip`. Treat a real vanilla server capture
as authoritative for these files. `minecraft-data` can lag registry schema
changes or carry an older registry set forward while still exposing the new
protocol number; that is sufficient for packet-table generation, but not proof
that a real client accepts the configuration stream.

Version-specific compatibility switches for the modern login, authentication,
configuration, component, dialog, and play flows live in
`protocol/limbo/modern_protocols.json`. The distributable
`protocol/pack/default_protocols.zip` contains a strict `manifest.json`, packet
IDs, block states, and registry data. Core validates a replacement ZIP before
storing it; Limbo compiles it into immutable lookup tables and atomically swaps
the active snapshot. Existing connections keep their original snapshot while
new connections use the replacement.

When a new Minecraft release keeps an already expressible packet layout, its
descriptor can alias `packet_id_protocol`, `data_protocol`,
`registry_data_protocol`, and `block_state_protocol` independently. A protocol
change that needs semantics outside the current manifest DSL still requires a
normal Authman Limbo binary update. Arbitrary JavaScript or other executable
code is intentionally not accepted in protocol packs.

Regenerate packet IDs, versions, and block-state tables with:

```sh
MINECRAFT_DATA_PC_DIR=/path/to/minecraft-data/data/pc go generate ./limbo/protocol/versions ./limbo/protocol/packetid ./limbo/protocol/blockstate
```

`mcregistry-gen` can bootstrap legacy registry files from `minecraft-data`:

```sh
go run ./limbo/tools/mcregistry-gen -pc-data /path/to/minecraft-data/data/pc -out-dir limbo/protocol/registrydata/protocols
```

For a modern release, capture a complete configuration stream from the matching
official vanilla server while forcing the client's `select_known_packs`
response to an empty list. Then extract the authoritative registry and tag
payloads:

```sh
go run ./limbo/tools/mcregistry-stream \
  -stream /path/to/server-to-client.bin \
  -out limbo/protocol/registrydata/protocols/PROTOCOL.json \
  -protocol PROTOCOL \
  -registry-packet REGISTRY_DATA_ID \
  -tags-packet UPDATE_TAGS_ID \
  -finish-packet FINISH_CONFIGURATION_ID
```

The extractor requires inline values and a complete stream ending in
`finish_configuration`; it rejects captures where known-pack negotiation caused
the server to omit registry values. Validate the resulting bundle with the
unit tests and a real unmodified client before publishing it.

If you only changed files under `protocol/registrydata/protocols`, rebuild the
embedded zip with:

```sh
go generate ./limbo/protocol/registrydata
go generate ./limbo/protocol/pack
```

## Verification

Run the Go unit tests with:

```sh
go test ./limbo/...
```

For client-facing smoke coverage, `tools/js-smoke/chunk-check.mjs` starts a
temporary limbgo server, joins with `minecraft-protocol` clients, reads each
received `map_chunk`, and verifies the first block state decodes to stone:

```sh
node limbo/tools/js-smoke/chunk-check.mjs
```

Dialog API smoke coverage starts a temporary server that uses the join-ready
event, `session.ShowDialog`, `session.ClearDialog`, and `DialogClick`, then
drives it with raw JavaScript fake clients across dialog-capable protocol lines
without a chat trigger. After the click, the server writes `store_cookie` and
`transfer` to mirror a login portal handoff:

```sh
node limbo/tools/js-smoke/dialog-check.mjs
```

## API

See [API.md](API.md) for the embeddable Go API.

## Library File Config

The JSON file configuration below is the reusable library configuration. The
production `authman-limbo` command is configured by Core and environment
variables instead.

The standalone command expects a small JSON config:

```json
{
  "listen": ":25565",
  "status": {
    "description": "limbgo",
    "motd_minimessage": "<gradient:#55ff55:#55ffff><bold>limbgo</bold></gradient>",
    "version_name": "limbgo",
    "max_players": 100,
    "online_players": 0,
    "sample_players": [
      { "name": "limbgo", "id": "00000000-0000-0000-0000-000000000000" }
    ],
    "enforces_secure_chat": false,
    "prevents_chat_reports": true,
    "rate_limit": {
      "requests": 60,
      "window_millis": 1000
    }
  },
  "auth": {
    "mode": "offline",
    "yggdrasil_base_url": "",
    "online_server_id": ""
  },
  "proxy_protocol": {
    "enabled": false,
    "required": false,
    "trusted_proxies": ["192.0.2.0/24", "127.0.0.1"],
    "read_header_timeout_millis": 5000
  },
  "protocol": {
    "modern_protocols": "protocol/limbo/modern_protocols.json",
    "registry_data": "protocol/registrydata/registrydata.zip"
  },
  "world": {
    "id": "spawn",
    "schematic": "spawn.schem",
    "dimension": {
      "environment": "overworld",
      "time": 6000,
      "world_age": 0,
      "fixed_time": 6000,
      "ambient_light": 0,
      "has_skylight": true,
      "has_ceiling": false,
      "ultrawarm": false,
      "natural": true,
      "coordinate_scale": 1,
      "logical_height": 256,
      "effects": "minecraft:overworld"
    }
  },
  "spawn": {
    "world": "spawn",
    "pos": { "x": 0, "y": 65, "z": 0 },
    "look": { "yaw": 0, "pitch": 0 },
    "mode": 2
  }
}
```

The `protocol` paths are optional. When omitted, the binary uses the embedded
generated defaults. `registry_data` can point at the generated zip bundle or a
legacy aggregate JSON file. Supplying these paths is useful when testing a new
protocol line or regenerated registry data without changing Go source.

`proxy_protocol` is optional and disabled by default. Enable it when limbgo is
behind a trusted TCP router that prepends HAProxy PROXY protocol headers, for
example Gate lite route `proxyProtocol: true`. Set `required` when every inbound
connection must come through that router, and keep `trusted_proxies` limited to
the Gate/container/host network so public clients cannot spoof source
addresses.

`auth` is optional. The default mode is `offline`, which accepts the claimed
username and marks `Player.Verified` false. Set `auth.mode` to `online` to run
the vanilla encryption/session proof flow. In online mode, empty
`yggdrasil_base_url` uses Mojang's official sessionserver; a non-empty value is
used as a custom Yggdrasil/sessionserver root. Application callbacks and hybrid
per-connection policy are available through the Go API rather than the static
JSON config. A `LoginPolicy` receives the client-declared `ClaimedUUID` from
`login_start` when that protocol carries one, allowing applications to choose
offline or online mode before vanilla session proof starts. Use
`LoginDecisionPolicy` when forced-offline users also need an application-chosen
runtime name, UUID, or profile properties before Login Success is sent.

The Go session API can also offer resource packs after join and receive client
status events. Modern clients support stable pack IDs and removal; older clients
can still receive a pack but may not support removing a specific pack by ID.

`world.schematic` is optional. When it is set, `world/schematic` loads the
Sponge `.schem` file into a version-neutral world palette. Protocol adapters
translate that palette to client-specific block state IDs at chunk serialization
time. When it is omitted, the standalone binary uses `DefaultWorld`: a
minimal air world with one `minecraft:bedrock` block directly below the spawn
position. If the config also omits `spawn.pos`, the default spawn is
`{ "x": 0, "y": 65, "z": 0 }`, so the bedrock block is at `0,64,0`.

The `world.dimension` block is optional. `environment` accepts `overworld`,
`nether`, or `end` and fills vanilla-like defaults for client-visible limbo
state. Individual fields can then override the preset. File config intentionally
exposes only the parts that affect the login/chunk experience: dimension name,
height/min-y/logical-height, sky/ceiling/warm/natural flags, ambient light,
time/world age, coordinate scale, and visual effects. Modern clients still
receive the full required `dimension_type` registry internally; protocol-only
gameplay flags such as piglin safety, bed/respawn-anchor behavior, raids,
infiniburn, and monster-spawn settings are filled from presets instead of being
deployment config fields. Legacy clients receive the matching dimension id for
overworld, nether, or end.

## Current Protocol State

The current play-state adapters support Minecraft Java protocol `47`
(`1.8.8`) through protocol `775` (`26.1.2`) for the release protocol lines
present in the generated version index:

- offline-mode login success;
- join game;
- spawn position;
- player position;
- one spawn chunk using the generated packet ID table and a small legacy
  block-state translator. Protocol 47 uses the pre-palette chunk format;
  protocols 107-340 use the legacy 4-bit section palette; protocols 393-578 use
  flattened block states with the 1.13/1.14/1.15 chunk breaks; protocols 735-756
  use play-login dimension codecs; protocols 757-763 use the pre-configuration
  modern login packet; protocols 764-765 use the modern configuration phase with
  a generated legacy dimension codec; protocol 766 and the compatible 1.21
  protocol lines use generated registry data, a runtime dimension_type,
  heightmaps, light data, and modern paletted chunk sections. Protocol 770+ uses
  the newer heightmap array packet shape. Protocol 775 uses a packet ID overlay
  derived from the 26.1.2 client protocol details and dedicated full
  registry/tag data for configuration validation. Its chunk section storage uses
  the newer fixed paletted long-array shape, while block-state IDs still reuse
  the protocol 774 translator until a newer block state table is available.

Newer protocol lines are intentionally still rejected during login until their
generated serializers are implemented. Server-list status/ping works through the
shared router independently of play support.
