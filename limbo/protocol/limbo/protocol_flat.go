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

type flatProtocolConfig struct {
	protocol             int32
	loginDifficulty      bool
	loginViewDistance    bool
	loginHashedSeed      bool
	loginRespawnScreen   bool
	chunkHeightmaps      bool
	chunkSectionSolid    bool
	chunkBiomesInData    bool
	chunkBiomesOuter1024 bool
	chunkUpdateLight     bool
}

func flatProtocolConfigFor(protocol int32) (flatProtocolConfig, bool) {
	switch protocol {
	case 393, 401, 404:
		return flatProtocolConfig{
			protocol:          protocol,
			loginDifficulty:   true,
			chunkBiomesInData: true,
		}, true
	case 477, 480, 490, 498:
		return flatProtocolConfig{
			protocol:          protocol,
			loginViewDistance: true,
			chunkHeightmaps:   true,
			chunkSectionSolid: true,
			chunkBiomesInData: true,
			chunkUpdateLight:  true,
		}, true
	case 573, 575, 578:
		return flatProtocolConfig{
			protocol:             protocol,
			loginViewDistance:    true,
			loginHashedSeed:      true,
			loginRespawnScreen:   true,
			chunkHeightmaps:      true,
			chunkSectionSolid:    true,
			chunkBiomesOuter1024: true,
			chunkUpdateLight:     true,
		}, true
	default:
		return flatProtocolConfig{}, false
	}
}

func serveFlatProtocol(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player, cfg flatProtocolConfig) error {
	join, err := resolveJoin(ctx, services, player)
	if err != nil {
		return err
	}
	spawn := join.Spawn
	world := join.World

	if err := writeLoginSuccess(cfg.protocol, conn, player); err != nil {
		return err
	}
	if err := writeJoinGameFlat(conn, spawn, world, cfg); err != nil {
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
	if err := writeMapChunkFlat(conn, world, chunk, cfg); err != nil {
		return err
	}
	if cfg.chunkUpdateLight {
		if err := writeUpdateLightFlat(conn, chunk.X, chunk.Z, cfg.protocol); err != nil {
			return err
		}
	}
	return servePlayEvents(ctx, conn, bufio.NewReader(conn), services, player, newPlayAdapter(cfg.protocol))
}

func writeJoinGameFlat(conn net.Conn, spawn limbgo.SpawnTarget, world limbgo.World, cfg flatProtocolConfig) error {
	var data bytes.Buffer
	if err := wire.WriteInt(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, byte(spawn.GameMode)); err != nil {
		return err
	}
	if err := wire.WriteInt(&data, legacyDimensionInt(world.Dimension())); err != nil {
		return err
	}
	if cfg.loginDifficulty {
		if err := wire.WriteByte(&data, 0); err != nil {
			return err
		}
	}
	if cfg.loginHashedSeed {
		if err := wire.WriteLong(&data, 0); err != nil {
			return err
		}
	}
	if err := wire.WriteByte(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteString(&data, "default"); err != nil {
		return err
	}
	if cfg.loginViewDistance {
		if err := wire.WriteVarInt(&data, 2); err != nil {
			return err
		}
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	if cfg.loginRespawnScreen {
		if err := wire.WriteBool(&data, false); err != nil {
			return err
		}
	}
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "login", data.Bytes())
}

func writeMapChunkFlat(conn net.Conn, world limbgo.World, chunk limbgo.Chunk, cfg flatProtocolConfig) error {
	sectionMask := int32(0)
	for _, section := range chunk.Sections {
		if section.Y >= 0 && section.Y < 16 {
			sectionMask |= 1 << uint(section.Y)
		}
	}

	chunkData := encodeChunkDataFlat(world, chunk, uint32(sectionMask), cfg)
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
	if cfg.chunkHeightmaps {
		if _, err := data.Write(fullNBT(heightmapsNBT766())); err != nil {
			return err
		}
	}
	if cfg.chunkBiomesOuter1024 {
		for i := 0; i < 1024; i++ {
			if err := wire.WriteInt(&data, 1); err != nil {
				return err
			}
		}
	}
	if err := wire.WriteVarInt(&data, int32(len(chunkData))); err != nil {
		return err
	}
	if _, err := data.Write(chunkData); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 0); err != nil {
		return err
	}
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "map_chunk", data.Bytes())
}

func encodeChunkDataFlat(world limbgo.World, chunk limbgo.Chunk, sectionMask uint32, cfg flatProtocolConfig) []byte {
	var data bytes.Buffer
	for y := int32(0); y < 16; y++ {
		if sectionMask&(1<<uint(y)) == 0 {
			continue
		}
		section := findSection(chunk, y)
		if cfg.chunkSectionSolid {
			writeSectionFlatSolid(&data, world.BlockPalette(), section, cfg.protocol)
		} else {
			writeSection340(&data, world.BlockPalette(), section, cfg.protocol)
		}
	}
	if cfg.chunkBiomesInData {
		for i := 0; i < 16*16; i++ {
			_ = wire.WriteInt(&data, 1)
		}
	}
	return data.Bytes()
}

func writeSectionFlatSolid(data *bytes.Buffer, palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32) {
	_ = wire.WriteShort(data, int16(nonAirBlockCountModern(palette, section, protocol, nil)))
	writeBlockStates766(data, palette, section, protocol, false, nil)
}

func writeUpdateLightFlat(conn net.Conn, chunkX int32, chunkZ int32, protocol int32) error {
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, chunkX); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, chunkZ); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 0x3ffff); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 0); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 0); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 0x3ffff); err != nil {
		return err
	}
	for i := 0; i < 18; i++ {
		if err := wire.WriteVarInt(&data, 2048); err != nil {
			return err
		}
		if _, err := data.Write(bytes.Repeat([]byte{0xff}, 2048)); err != nil {
			return err
		}
	}
	return writeNamedPacket(protocol, conn, packetid.StatePlay, "update_light", data.Bytes())
}
