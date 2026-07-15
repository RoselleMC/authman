package limbo

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/blockstate"
	"github.com/RoselleMC/authman/limbo/protocol/registrydata"
)

func dimensionTypeRegistry766(dimension limbgo.Dimension) registrydata.Registry {
	dimension = limbgo.NormalizeDimension(dimension, 256)
	profile := dimensionProtocolProfile(dimension.Environment)
	var n nbtWriter
	n.writeAnonymousCompound(func() {
		if dimension.FixedTime != nil {
			n.writeLong("fixed_time", *dimension.FixedTime)
		}
		n.writeByte("piglin_safe", boolByte(profile.piglinSafe))
		n.writeByte("natural", boolByte(dimension.Natural))
		n.writeFloat("ambient_light", dimension.AmbientLight)
		n.writeInt("monster_spawn_block_light_limit", profile.monsterSpawnBlockLightLimit)
		n.writeString("infiniburn", profile.infiniburn)
		n.writeByte("respawn_anchor_works", boolByte(profile.respawnAnchorWorks))
		n.writeByte("has_skylight", boolByte(dimension.HasSkylight))
		n.writeByte("bed_works", boolByte(profile.bedWorks))
		n.writeString("effects", dimension.Effects)
		n.writeByte("has_raids", boolByte(profile.hasRaids))
		n.writeInt("logical_height", dimension.LogicalHeight)
		n.writeDouble("coordinate_scale", dimension.CoordinateScale)
		writeMonsterSpawnLightLevel(&n, profile.monsterSpawnLightLevel)
		n.writeInt("min_y", dimension.MinY)
		n.writeByte("ultrawarm", boolByte(dimension.UltraWarm))
		n.writeByte("has_ceiling", boolByte(dimension.HasCeiling))
		n.writeInt("height", dimension.Height)
		n.writeByte("has_ender_dragon_fight", boolByte(profile.hasEnderDragonFight))
	})
	return registrydata.Registry{
		ID: "minecraft:dimension_type",
		Entries: []registrydata.Entry{{
			Key:   dimension.Name,
			Value: n.bytes(),
		}},
	}
}

type dimensionProtocolSettings struct {
	piglinSafe                  bool
	respawnAnchorWorks          bool
	bedWorks                    bool
	hasRaids                    bool
	hasEnderDragonFight         bool
	infiniburn                  string
	monsterSpawnBlockLightLimit int32
	monsterSpawnLightLevel      dimensionIntProvider
}

func dimensionProtocolProfile(environment limbgo.DimensionEnvironment) dimensionProtocolSettings {
	switch environment {
	case limbgo.DimensionNether:
		return dimensionProtocolSettings{
			piglinSafe:                  true,
			respawnAnchorWorks:          true,
			bedWorks:                    false,
			hasRaids:                    false,
			infiniburn:                  "#minecraft:infiniburn_nether",
			monsterSpawnBlockLightLimit: 15,
			monsterSpawnLightLevel:      fixedDimensionInt(7),
		}
	case limbgo.DimensionEnd:
		return dimensionProtocolSettings{
			piglinSafe:             false,
			respawnAnchorWorks:     false,
			bedWorks:               false,
			hasRaids:               true,
			hasEnderDragonFight:    true,
			infiniburn:             "#minecraft:infiniburn_end",
			monsterSpawnLightLevel: uniformDimensionInt(0, 7),
		}
	default:
		return dimensionProtocolSettings{
			piglinSafe:             false,
			respawnAnchorWorks:     false,
			bedWorks:               true,
			hasRaids:               true,
			infiniburn:             "#minecraft:infiniburn_overworld",
			monsterSpawnLightLevel: uniformDimensionInt(0, 7),
		}
	}
}

type dimensionIntProvider struct {
	value        *int32
	minInclusive *int32
	maxInclusive *int32
}

func fixedDimensionInt(value int32) dimensionIntProvider {
	return dimensionIntProvider{value: &value}
}

func uniformDimensionInt(minInclusive, maxInclusive int32) dimensionIntProvider {
	return dimensionIntProvider{
		minInclusive: &minInclusive,
		maxInclusive: &maxInclusive,
	}
}

func writeMonsterSpawnLightLevel(n *nbtWriter, provider dimensionIntProvider) {
	if provider.value != nil {
		n.writeInt("monster_spawn_light_level", *provider.value)
		return
	}
	min := int32(0)
	max := int32(7)
	if provider.minInclusive != nil {
		min = *provider.minInclusive
	}
	if provider.maxInclusive != nil {
		max = *provider.maxInclusive
	}
	n.writeCompound("monster_spawn_light_level", func() {
		n.writeInt("min_inclusive", min)
		n.writeInt("max_inclusive", max)
		n.writeString("type", "minecraft:uniform")
	})
}

func heightmapsNBT766() []byte {
	var n nbtWriter
	n.writeAnonymousCompound(func() {
		n.writeLongArray("MOTION_BLOCKING", make([]int64, 37))
	})
	return n.bytes()
}

func writeHeightmapsArrayModern(data *bytes.Buffer) {
	_ = wire.WriteVarInt(data, 1)
	_ = wire.WriteVarInt(data, 4)
	_ = wire.WriteVarInt(data, 37)
	for i := 0; i < 37; i++ {
		_ = wire.WriteLong(data, 0)
	}
}

func encodeChunkDataModern(world limbgo.World, chunk limbgo.Chunk, cfg modernProtocolConfig) []byte {
	dimension := world.Dimension()
	sectionCount := int(dimension.Height / 16)
	if sectionCount <= 0 {
		sectionCount = 16
	}
	var data bytes.Buffer
	for i := 0; i < sectionCount; i++ {
		sectionY := dimension.MinY/16 + int32(i)
		writeSection766(&data, world.BlockPalette(), findSection(chunk, sectionY), cfg)
	}
	return data.Bytes()
}

func writeSection766(data *bytes.Buffer, palette []limbgo.BlockState, section *limbgo.ChunkSection, cfg modernProtocolConfig) {
	protocol := cfg.blockStateProtocolID()
	_ = wire.WriteShort(data, int16(nonAirBlockCountModern(palette, section, protocol, cfg.blockStates)))
	if cfg.chunkSectionFluidCount {
		_ = wire.WriteShort(data, 0)
	}
	writeBlockStates766(data, palette, section, protocol, cfg.chunkFixedPalettedStorage, cfg.blockStates)
	writeBiomes766(data, cfg.chunkFixedPalettedStorage)
}

func writeBlockStates766(data *bytes.Buffer, palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32, fixedPalettedStorage bool, blockStates *blockstate.Table) {
	_ = wire.WriteByte(data, 4)
	localPalette := buildLocalPaletteModern(palette, section, protocol, blockStates)
	_ = wire.WriteVarInt(data, int32(len(localPalette)))
	for _, state := range localPalette {
		_ = wire.WriteVarInt(data, int32(state))
	}
	indexByState := make(map[uint32]uint64, len(localPalette))
	for i, state := range localPalette {
		indexByState[state] = uint64(i)
	}
	longs := packSectionPaletteIndicesModern(palette, section, indexByState, protocol, blockStates)
	if !fixedPalettedStorage {
		_ = wire.WriteVarInt(data, int32(len(longs)))
	}
	for _, value := range longs {
		_ = wire.WriteLong(data, int64(value))
	}
}

func writeBiomes766(data *bytes.Buffer, fixedPalettedStorage bool) {
	_ = wire.WriteByte(data, 0)
	_ = wire.WriteVarInt(data, 0)
	if !fixedPalettedStorage {
		_ = wire.WriteVarInt(data, 0)
	}
}

func buildLocalPaletteModern(palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32, blockStates *blockstate.Table) []uint32 {
	seen := map[uint32]struct{}{0: {}}
	local := []uint32{0}
	if section == nil {
		return local
	}
	for _, paletteID := range section.BlockStateIDs {
		state := blockStateForProtocolTable(blockStates, protocol, palette, paletteID)
		if _, ok := seen[state]; ok {
			continue
		}
		seen[state] = struct{}{}
		local = append(local, state)
	}
	return local
}

func packSectionPaletteIndicesModern(palette []limbgo.BlockState, section *limbgo.ChunkSection, indexByState map[uint32]uint64, protocol int32, blockStates *blockstate.Table) []uint64 {
	const bitsPerBlock = 4
	const valuesPerLong = 64 / bitsPerBlock
	longs := make([]uint64, 16*16*16/valuesPerLong)
	for i := 0; i < 16*16*16; i++ {
		state := uint32(0)
		if section != nil && i < len(section.BlockStateIDs) {
			state = blockStateForProtocolTable(blockStates, protocol, palette, section.BlockStateIDs[i])
		}
		paletteIndex := indexByState[state] & 0xf
		longIndex := i / valuesPerLong
		bitOffset := uint((i % valuesPerLong) * bitsPerBlock)
		longs[longIndex] |= paletteIndex << bitOffset
	}
	return longs
}

func nonAirBlockCountModern(palette []limbgo.BlockState, section *limbgo.ChunkSection, protocol int32, blockStates *blockstate.Table) int {
	if section == nil {
		return 0
	}
	count := 0
	for _, paletteID := range section.BlockStateIDs {
		if blockStateForProtocolTable(blockStates, protocol, palette, paletteID) != 0 {
			count++
		}
	}
	return count
}

func writeLightData766(data *bytes.Buffer) {
	writeBitSet(data, []uint64{0x3ffff})
	writeBitSet(data, []uint64{0})
	writeBitSet(data, []uint64{0})
	writeBitSet(data, []uint64{0x3ffff})
	_ = wire.WriteVarInt(data, 18)
	for i := 0; i < 18; i++ {
		_ = wire.WriteVarInt(data, 2048)
		data.Write(bytes.Repeat([]byte{0xff}, 2048))
	}
	_ = wire.WriteVarInt(data, 0)
}

func writeBitSet(data *bytes.Buffer, values []uint64) {
	_ = wire.WriteVarInt(data, int32(len(values)))
	for _, value := range values {
		_ = wire.WriteLong(data, int64(value))
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

func writeUUID(data *bytes.Buffer, uuid string) error {
	cleaned := strings.ReplaceAll(uuid, "-", "")
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return fmt.Errorf("decode uuid %q: %w", uuid, err)
	}
	if len(raw) != 16 {
		return fmt.Errorf("uuid %q decoded to %d bytes", uuid, len(raw))
	}
	_, err = data.Write(raw)
	return err
}
