package limbo

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/blockstate"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
)

const protocol47 = int32(47)

func serveProtocol47(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player) error {
	join, err := resolveJoin(ctx, services, player)
	if err != nil {
		return err
	}
	spawn := join.Spawn
	world := join.World

	if err := writeLoginSuccess(protocol47, conn, player); err != nil {
		return err
	}
	if err := writeJoinGame47(conn, spawn, world); err != nil {
		return err
	}
	if err := writeSpawnPosition47(conn, spawn); err != nil {
		return err
	}
	if err := writePlayerPosition47(conn, spawn); err != nil {
		return err
	}
	if err := writeUpdateTime(protocol47, conn, world); err != nil {
		return err
	}
	chunk, ok := world.Chunk(chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	if !ok {
		return fmt.Errorf("%w: spawn chunk %d,%d", limbgo.ErrWorldNotFound, chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	}
	if err := writeMapChunk47(conn, world, chunk); err != nil {
		return err
	}
	return servePlayEvents(ctx, conn, bufio.NewReader(conn), services, player, newPlayAdapter(protocol47))
}

func writeLoginSuccess(protocol int32, conn net.Conn, player limbgo.Player) error {
	var data bytes.Buffer
	if err := wire.WriteString(&data, player.UUID); err != nil {
		return err
	}
	if err := wire.WriteString(&data, player.Name); err != nil {
		return err
	}
	return writeNamedPacket(protocol, conn, packetid.StateLogin, "success", data.Bytes())
}

func writeJoinGame47(conn net.Conn, spawn limbgo.SpawnTarget, world limbgo.World) error {
	var data bytes.Buffer
	if err := wire.WriteInt(&data, 1); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, byte(spawn.GameMode)); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, legacyDimensionID(world.Dimension())); err != nil {
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
	return writeNamedPacket(protocol47, conn, packetid.StatePlay, "login", data.Bytes())
}

func writeSpawnPosition47(conn net.Conn, spawn limbgo.SpawnTarget) error {
	return writeSpawnPosition(protocol47, conn, spawn)
}

func writeSpawnPosition(protocol int32, conn net.Conn, spawn limbgo.SpawnTarget) error {
	var data bytes.Buffer
	if err := wire.WritePosition(&data, int32(spawn.Position.X), int32(spawn.Position.Y), int32(spawn.Position.Z)); err != nil {
		return err
	}
	return writeNamedPacket(protocol, conn, packetid.StatePlay, "spawn_position", data.Bytes())
}

func writePlayerPosition47(conn net.Conn, spawn limbgo.SpawnTarget) error {
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
	return writeNamedPacket(protocol47, conn, packetid.StatePlay, "position", data.Bytes())
}

func writeMapChunk47(conn net.Conn, world limbgo.World, chunk limbgo.Chunk) error {
	sectionMask := uint16(0)
	for _, section := range chunk.Sections {
		if section.Y >= 0 && section.Y < 16 {
			sectionMask |= 1 << uint(section.Y)
		}
	}

	chunkData := encodeChunkData47(world.BlockPalette(), chunk, sectionMask, protocol47)
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
	if err := wire.WriteUnsignedShort(&data, sectionMask); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, int32(len(chunkData))); err != nil {
		return err
	}
	if _, err := data.Write(chunkData); err != nil {
		return err
	}
	return writeNamedPacket(protocol47, conn, packetid.StatePlay, "map_chunk", data.Bytes())
}

func encodeChunkData47(palette []limbgo.BlockState, chunk limbgo.Chunk, sectionMask uint16, protocol int32) []byte {
	var data bytes.Buffer
	for y := int32(0); y < 16; y++ {
		if sectionMask&(1<<uint(y)) == 0 {
			continue
		}
		section := findSection(chunk, y)
		for i := 0; i < 16*16*16; i++ {
			state := uint32(0)
			if section != nil && i < len(section.BlockStateIDs) {
				state = blockStateForProtocol(protocol, palette, section.BlockStateIDs[i])
			}
			_ = wire.WriteShort(&data, int16(state))
		}
		data.Write(make([]byte, 16*16*16/2))
		data.Write(make([]byte, 16*16*16/2))
		data.Write(make([]byte, 16*16*16/2))
	}
	data.Write(make([]byte, 16*16))
	return data.Bytes()
}

func findSection(chunk limbgo.Chunk, y int32) *limbgo.ChunkSection {
	for i := range chunk.Sections {
		if chunk.Sections[i].Y == y {
			return &chunk.Sections[i]
		}
	}
	return nil
}

func blockStateForProtocol(protocol int32, palette []limbgo.BlockState, paletteID uint32) uint32 {
	return blockStateForProtocolTable(nil, protocol, palette, paletteID)
}

func blockStateForProtocolTable(table *blockstate.Table, protocol int32, palette []limbgo.BlockState, paletteID uint32) uint32 {
	if int(paletteID) >= len(palette) {
		return 0
	}
	var state uint32
	var ok bool
	if table != nil {
		state, ok = table.DefaultState(protocol, palette[paletteID])
	} else {
		state, ok = blockstate.DefaultState(protocol, palette[paletteID])
	}
	if !ok {
		return 0
	}
	return state
}

func writeNamedPacket(protocol int32, conn net.Conn, state packetid.State, name string, data []byte) error {
	id, ok := packetid.ID(protocol, state, packetid.ToClient, name)
	if !ok {
		return fmt.Errorf("missing packet id for protocol %d state %s packet %s", protocol, state, name)
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data})
}

func chunkCoord(value float64) int32 {
	block := int32(value)
	if value < 0 && float64(block) != value {
		block--
	}
	if block >= 0 {
		return block / 16
	}
	return -((-block + 15) / 16)
}
