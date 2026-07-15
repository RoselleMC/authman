package limbo

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
)

type legacyProtocolConfig struct {
	protocol           int32
	loginDimensionInt  bool
	chunkBlockEntities bool
}

func legacyProtocolConfigFor(protocol int32) (legacyProtocolConfig, bool) {
	switch protocol {
	case 107:
		return legacyProtocolConfig{protocol: protocol}, true
	case 109, 110, 210, 315, 316, 335, 338:
		return legacyProtocolConfig{protocol: protocol, loginDimensionInt: true, chunkBlockEntities: true}, true
	default:
		return legacyProtocolConfig{}, false
	}
}

func serveLegacyProtocol(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player, cfg legacyProtocolConfig) error {
	join, err := resolveJoin(ctx, services, player)
	if err != nil {
		return err
	}
	spawn := join.Spawn
	world := join.World

	if err := writeLoginSuccess(cfg.protocol, conn, player); err != nil {
		return err
	}
	if err := writeJoinGameLegacy(conn, spawn, world, cfg); err != nil {
		return err
	}
	if err := writeSpawnPosition(cfg.protocol, conn, spawn); err != nil {
		return err
	}
	if err := writePlayerPositionLegacy(conn, spawn, cfg.protocol); err != nil {
		return err
	}
	if err := writeUpdateTime(cfg.protocol, conn, world); err != nil {
		return err
	}
	chunk, ok := world.Chunk(chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	if !ok {
		return fmt.Errorf("%w: spawn chunk %d,%d", limbgo.ErrWorldNotFound, chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	}
	if err := writeMapChunkLegacy(conn, world, chunk, cfg); err != nil {
		return err
	}
	return servePlayEvents(ctx, conn, bufio.NewReader(conn), services, player, newPlayAdapter(cfg.protocol))
}

func writeJoinGameLegacy(conn net.Conn, spawn limbgo.SpawnTarget, world limbgo.World, cfg legacyProtocolConfig) error {
	var data bytes.Buffer
	if err := wire.WriteInt(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, byte(spawn.GameMode)); err != nil {
		return err
	}
	if cfg.loginDimensionInt {
		if err := wire.WriteInt(&data, legacyDimensionInt(world.Dimension())); err != nil {
			return err
		}
	} else if err := wire.WriteByte(&data, legacyDimensionID(world.Dimension())); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, 0); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteString(&data, "default"); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "login", data.Bytes())
}

func writePlayerPositionLegacy(conn net.Conn, spawn limbgo.SpawnTarget, protocol int32) error {
	var data bytes.Buffer
	if err := wire.WriteDouble(&data, spawn.Position.X); err != nil {
		return err
	}
	if err := wire.WriteDouble(&data, spawn.Position.Y); err != nil {
		return err
	}
	if err := wire.WriteDouble(&data, spawn.Position.Z); err != nil {
		return err
	}
	if err := wire.WriteFloat(&data, spawn.Rotation.Yaw); err != nil {
		return err
	}
	if err := wire.WriteFloat(&data, spawn.Rotation.Pitch); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, 0); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 1); err != nil {
		return err
	}
	return writeNamedPacket(protocol, conn, packetid.StatePlay, "position", data.Bytes())
}

func writeMapChunkLegacy(conn net.Conn, world limbgo.World, chunk limbgo.Chunk, cfg legacyProtocolConfig) error {
	sectionMask := int32(0)
	for _, section := range chunk.Sections {
		if section.Y >= 0 && section.Y < 16 {
			sectionMask |= 1 << uint(section.Y)
		}
	}

	chunkData := encodeChunkData340(world.BlockPalette(), chunk, uint32(sectionMask), cfg.protocol)
	var data bytes.Buffer
	if err := wire.WriteInt(&data, chunk.X); err != nil {
		return err
	}
	if err := wire.WriteInt(&data, chunk.Z); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, true); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, sectionMask); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, int32(len(chunkData))); err != nil {
		return err
	}
	if _, err := data.Write(chunkData); err != nil {
		return err
	}
	if cfg.chunkBlockEntities {
		if err := wire.WriteVarInt(&data, 0); err != nil {
			return err
		}
	}
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "map_chunk", data.Bytes())
}
