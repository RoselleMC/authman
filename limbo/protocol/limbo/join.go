package limbo

import (
	"context"

	"github.com/RoselleMC/authman/limbo"
)

type joinSessionServices interface {
	ResolveJoin(ctx context.Context, player limbgo.Player) (limbgo.JoinTarget, error)
}

func resolveJoin(ctx context.Context, services limbgo.SessionServices, player limbgo.Player) (limbgo.JoinTarget, error) {
	if resolver, ok := services.(joinSessionServices); ok {
		return resolver.ResolveJoin(ctx, player)
	}
	spawn, err := services.ResolveSpawn(ctx, player)
	if err != nil {
		return limbgo.JoinTarget{}, err
	}
	world, err := services.World(ctx, spawn.World)
	if err != nil {
		return limbgo.JoinTarget{}, err
	}
	if spawn.World == "" {
		spawn.World = world.ID()
	}
	return limbgo.JoinTarget{World: world, Spawn: spawn}, nil
}
