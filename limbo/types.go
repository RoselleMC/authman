package limbgo

import (
	"context"
	"net"
)

// Player describes the identity known after the handshake/login stage.
type Player struct {
	Name              string
	UUID              string
	ProtocolVersion   int
	RemoteAddr        net.Addr
	RequestedHost     string
	LoginMode         LoginMode
	AuthSource        string
	Verified          bool
	Properties        map[string]string
	ProfileProperties []ProfileProperty
}

// Vec3 is a Minecraft world position.
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Rotation is the yaw/pitch pair used when spawning a player.
type Rotation struct {
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
}

// SpawnTarget is the resolved destination for a player entering limbo.
type SpawnTarget struct {
	World    string
	Position Vec3
	Rotation Rotation
	GameMode GameMode
}

// JoinTarget is the fully resolved destination for a player entering limbo.
// World is the actual world instance to send. Spawn.World may be left empty; in
// that case the world's ID is used.
type JoinTarget struct {
	World World
	Spawn SpawnTarget
}

// GameMode is the small subset of vanilla game modes relevant to a limbo spawn.
type GameMode int

const (
	GameModeSurvival GameMode = iota
	GameModeCreative
	GameModeAdventure
	GameModeSpectator
)

// Bool returns a pointer to value for optional API fields.
func Bool(value bool) *bool {
	return &value
}

// SpawnResolver decides where a player should enter limbo.
type SpawnResolver interface {
	ResolveSpawn(ctx context.Context, player Player) (SpawnTarget, error)
}

// SpawnResolverFunc adapts a function to SpawnResolver.
type SpawnResolverFunc func(context.Context, Player) (SpawnTarget, error)

// ResolveSpawn implements SpawnResolver.
func (fn SpawnResolverFunc) ResolveSpawn(ctx context.Context, player Player) (SpawnTarget, error) {
	return fn(ctx, player)
}

// StaticSpawn returns the same spawn target for every player.
func StaticSpawn(target SpawnTarget) SpawnResolver {
	return SpawnResolverFunc(func(context.Context, Player) (SpawnTarget, error) {
		return target, nil
	})
}

// JoinResolver decides the exact world instance and spawn for a player. Use it
// when the world is player-specific or produced dynamically from a schematic.
type JoinResolver interface {
	ResolveJoin(ctx context.Context, player Player) (JoinTarget, error)
}

// JoinResolverFunc adapts a function to JoinResolver.
type JoinResolverFunc func(context.Context, Player) (JoinTarget, error)

// ResolveJoin implements JoinResolver.
func (fn JoinResolverFunc) ResolveJoin(ctx context.Context, player Player) (JoinTarget, error) {
	return fn(ctx, player)
}

// JoinReleaser can be implemented by a JoinResolver that owns per-player
// resources and wants to clean them up after the connection closes.
type JoinReleaser interface {
	ReleaseJoin(ctx context.Context, player Player, target JoinTarget) error
}

// StaticJoin returns the same world instance and spawn for every player.
func StaticJoin(world World, spawn SpawnTarget) JoinResolver {
	return JoinResolverFunc(func(context.Context, Player) (JoinTarget, error) {
		return JoinTarget{World: world, Spawn: spawn}, nil
	})
}
