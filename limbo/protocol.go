package limbgo

import (
	"context"
	"net"
)

// ProtocolRouter owns Minecraft protocol negotiation and packet IO.
//
// The long-term implementation should be generated from external version data
// instead of hand-maintained packet maps. It receives high-level world/session
// services and translates them into the concrete packets required by each
// protocol version.
type ProtocolRouter interface {
	ServeConn(ctx context.Context, conn net.Conn, session SessionServices) error
}

// SessionServices is the API surface exposed to protocol adapters.
type SessionServices interface {
	ResolveSpawn(ctx context.Context, player Player) (SpawnTarget, error)
	World(ctx context.Context, id string) (World, error)
	Events() PlayerEventHandler
}
