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
	"github.com/RoselleMC/authman/limbo/protocol/registrydata"
)

type codecProtocolConfig struct {
	protocol            int32
	dimensionNBT        bool
	chunkIgnoreOldData  bool
	chunkMaskLongArray  bool
	chunkBiomeFixed1024 bool
	chunkBiomeVarInts   bool
	lightMaskLongArrays bool
}

func codecProtocolConfigFor(protocol int32) (codecProtocolConfig, bool) {
	switch protocol {
	case 735, 736:
		return codecProtocolConfig{
			protocol:            protocol,
			chunkIgnoreOldData:  true,
			chunkBiomeFixed1024: true,
		}, true
	case 751, 753, 754:
		return codecProtocolConfig{
			protocol:          protocol,
			dimensionNBT:      true,
			chunkBiomeVarInts: true,
		}, true
	case 755, 756:
		return codecProtocolConfig{
			protocol:            protocol,
			dimensionNBT:        true,
			chunkMaskLongArray:  true,
			chunkBiomeVarInts:   true,
			lightMaskLongArrays: true,
		}, true
	default:
		return codecProtocolConfig{}, false
	}
}

func serveCodecProtocol(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player, cfg codecProtocolConfig, registryData *registrydata.Data) error {
	join, err := resolveJoin(ctx, services, player)
	if err != nil {
		return err
	}
	spawn := join.Spawn
	world := join.World

	if err := writeLoginSuccessUUIDNoProperties(cfg.protocol, conn, player); err != nil {
		return err
	}
	if err := writeJoinGameCodec(conn, spawn, world, cfg, registryData); err != nil {
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
	if err := writeMapChunkCodec(conn, world, chunk, cfg); err != nil {
		return err
	}
	if err := writeUpdateLightCodec(conn, chunk.X, chunk.Z, cfg); err != nil {
		return err
	}
	return servePlayEvents(ctx, conn, bufio.NewReader(conn), services, player, newPlayAdapter(cfg.protocol))
}

func writeLoginSuccessUUIDNoProperties(protocol int32, conn net.Conn, player limbgo.Player) error {
	var data bytes.Buffer
	if err := writeUUID(&data, player.UUID); err != nil {
		return err
	}
	if err := wire.WriteString(&data, player.Name); err != nil {
		return err
	}
	return writeNamedPacket(protocol, conn, packetid.StateLogin, "success", data.Bytes())
}

func writeJoinGameCodec(conn net.Conn, spawn limbgo.SpawnTarget, world limbgo.World, cfg codecProtocolConfig, registryData *registrydata.Data) error {
	dimensionName := limbgo.NormalizeDimension(world.Dimension(), 256).Name
	codec, ok := registryData.DimensionCodec(cfg.protocol)
	if !ok {
		return fmt.Errorf("no generated dimension codec for protocol %d", cfg.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteInt(&data, 1); err != nil {
		return err
	}
	if cfg.dimensionNBT {
		if err := wire.WriteBool(&data, false); err != nil {
			return err
		}
	}
	if err := wire.WriteByte(&data, byte(spawn.GameMode)); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, 255); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteString(&data, dimensionName); err != nil {
		return err
	}
	if _, err := data.Write(fullNBT(codec)); err != nil {
		return err
	}
	if cfg.dimensionNBT {
		dimension, ok := registryData.Dimension(cfg.protocol)
		if !ok {
			return fmt.Errorf("no generated dimension for protocol %d", cfg.protocol)
		}
		if _, err := data.Write(fullNBT(dimension)); err != nil {
			return err
		}
	} else {
		if err := wire.WriteString(&data, dimensionName); err != nil {
			return err
		}
	}
	if err := wire.WriteString(&data, dimensionName); err != nil {
		return err
	}
	if err := wire.WriteLong(&data, 0); err != nil {
		return err
	}
	if cfg.dimensionNBT {
		if err := wire.WriteVarInt(&data, 1); err != nil {
			return err
		}
	} else if err := wire.WriteByte(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, 2); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "login", data.Bytes())
}

func writeMapChunkCodec(conn net.Conn, world limbgo.World, chunk limbgo.Chunk, cfg codecProtocolConfig) error {
	sectionMask := int32(0)
	for _, section := range chunk.Sections {
		if section.Y >= 0 && section.Y < 16 {
			sectionMask |= 1 << uint(section.Y)
		}
	}

	chunkData := encodeChunkDataCodec(world, chunk, uint32(sectionMask), cfg)
	var data bytes.Buffer
	if err := wire.WriteInt(&data, chunk.X); err != nil {
		return err
	}
	if err := wire.WriteInt(&data, chunk.Z); err != nil {
		return err
	}
	if !cfg.chunkMaskLongArray {
		if err := wire.WriteBool(&data, true); err != nil {
			return err
		}
		if cfg.chunkIgnoreOldData {
			if err := wire.WriteBool(&data, false); err != nil {
				return err
			}
		}
		if err := wire.WriteVarInt(&data, sectionMask); err != nil {
			return err
		}
	} else {
		writeBitSet(&data, []uint64{uint64(sectionMask)})
	}
	if _, err := data.Write(fullNBT(heightmapsNBT766())); err != nil {
		return err
	}
	if cfg.chunkBiomeFixed1024 {
		for i := 0; i < 1024; i++ {
			if err := wire.WriteInt(&data, 1); err != nil {
				return err
			}
		}
	}
	if cfg.chunkBiomeVarInts {
		if err := wire.WriteVarInt(&data, 1024); err != nil {
			return err
		}
		for i := 0; i < 1024; i++ {
			if err := wire.WriteVarInt(&data, 1); err != nil {
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

func encodeChunkDataCodec(world limbgo.World, chunk limbgo.Chunk, sectionMask uint32, cfg codecProtocolConfig) []byte {
	var data bytes.Buffer
	for y := int32(0); y < 16; y++ {
		if sectionMask&(1<<uint(y)) == 0 {
			continue
		}
		writeSectionFlatSolid(&data, world.BlockPalette(), findSection(chunk, y), cfg.protocol)
	}
	return data.Bytes()
}

func writeUpdateLightCodec(conn net.Conn, chunkX int32, chunkZ int32, cfg codecProtocolConfig) error {
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, chunkX); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, chunkZ); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	if cfg.lightMaskLongArrays {
		writeBitSet(&data, []uint64{0x3ffff})
		writeBitSet(&data, []uint64{0})
		writeBitSet(&data, []uint64{0})
		writeBitSet(&data, []uint64{0x3ffff})
		if err := wire.WriteVarInt(&data, 18); err != nil {
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
		if err := wire.WriteVarInt(&data, 0); err != nil {
			return err
		}
		return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "update_light", data.Bytes())
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
	return writeNamedPacket(cfg.protocol, conn, packetid.StatePlay, "update_light", data.Bytes())
}
