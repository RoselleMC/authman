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

const protocol340 = int32(340)

func serveProtocol340(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player) error {
	join, err := resolveJoin(ctx, services, player)
	if err != nil {
		return err
	}
	spawn := join.Spawn
	world := join.World

	if err := writeLoginSuccess(protocol340, conn, player); err != nil {
		return err
	}
	if err := writeJoinGame340(conn, spawn, world); err != nil {
		return err
	}
	if err := writeSpawnPosition(protocol340, conn, spawn); err != nil {
		return err
	}
	if err := writePlayerPosition340(conn, spawn); err != nil {
		return err
	}
	if err := writeUpdateTime(protocol340, conn, world); err != nil {
		return err
	}
	chunk, ok := world.Chunk(chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	if !ok {
		return fmt.Errorf("%w: spawn chunk %d,%d", limbgo.ErrWorldNotFound, chunkCoord(spawn.Position.X), chunkCoord(spawn.Position.Z))
	}
	if err := writeMapChunk340(conn, world, chunk); err != nil {
		return err
	}
	return servePlayEvents(ctx, conn, bufio.NewReader(conn), services, player, newPlayAdapter(protocol340))
}

func writeJoinGame340(conn net.Conn, spawn limbgo.SpawnTarget, world limbgo.World) error {
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
	return writeNamedPacket(protocol340, conn, packetid.StatePlay, "login", data.Bytes())
}

func writePlayerPosition340(conn net.Conn, spawn limbgo.SpawnTarget) error {
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
	return writeNamedPacket(protocol340, conn, packetid.StatePlay, "position", data.Bytes())
}

func writeMapChunk340(conn net.Conn, world limbgo.World, chunk limbgo.Chunk) error {
	sectionMask := int32(0)
	for _, section := range chunk.Sections {
		if section.Y >= 0 && section.Y < 16 {
			sectionMask |= 1 << uint(section.Y)
		}
	}

	chunkData := encodeChunkData340(world.BlockPalette(), chunk, uint32(sectionMask), protocol340)
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
	if err := wire.WriteVarInt(&data, 0); err != nil {
		return err
	}
	return writeNamedPacket(protocol340, conn, packetid.StatePlay, "map_chunk", data.Bytes())
}

func encodeChunkData340(palette []limbgo.BlockState, chunk limbgo.Chunk, sectionMask uint32, protocol int32) []byte {
	var data bytes.Buffer
	for y := int32(0); y < 16; y++ {
		if sectionMask&(1<<uint(y)) == 0 {
			continue
		}
		section := findSection(chunk, y)
		writeSection340(&data, palette, section, protocol)
	}
	data.Write(make([]byte, 16*16))
	return data.Bytes()
}

func writeSection340(data *bytes.Buffer, palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32) {
	_ = wire.WriteByte(data, 4)
	localPalette := buildLocalPalette340(palette, section, protocol)
	_ = wire.WriteVarInt(data, int32(len(localPalette)))
	for _, legacyState := range localPalette {
		_ = wire.WriteVarInt(data, int32(legacyState))
	}

	indexByState := make(map[uint32]uint64, len(localPalette))
	for i, legacyState := range localPalette {
		indexByState[legacyState] = uint64(i)
	}
	longs := packSectionPaletteIndices340(palette, section, indexByState, protocol)
	_ = wire.WriteVarInt(data, int32(len(longs)))
	for _, value := range longs {
		_ = wire.WriteLong(data, int64(value))
	}
	data.Write(make([]byte, 16*16*16/2))
	data.Write(make([]byte, 16*16*16/2))
}

func buildLocalPalette340(palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32) []uint32 {
	seen := map[uint32]struct{}{0: {}}
	local := []uint32{0}
	if section == nil {
		return local
	}
	for _, paletteID := range section.BlockStateIDs {
		legacyState := blockStateForProtocol(protocol, palette, paletteID)
		if _, ok := seen[legacyState]; ok {
			continue
		}
		seen[legacyState] = struct{}{}
		local = append(local, legacyState)
	}
	return local
}

func packSectionPaletteIndices340(palette []limbgo.BlockState, section *limbgo.ChunkSection, indexByState map[uint32]uint64, protocol int32) []uint64 {
	const bitsPerBlock = 4
	const valuesPerLong = 64 / bitsPerBlock
	longs := make([]uint64, 16*16*16/valuesPerLong)
	for i := 0; i < 16*16*16; i++ {
		legacyState := uint32(0)
		if section != nil && i < len(section.BlockStateIDs) {
			legacyState = blockStateForProtocol(protocol, palette, section.BlockStateIDs[i])
		}
		paletteIndex := indexByState[legacyState] & 0xf
		longIndex := i / valuesPerLong
		bitOffset := uint((i % valuesPerLong) * bitsPerBlock)
		longs[longIndex] |= paletteIndex << bitOffset
	}
	return longs
}
