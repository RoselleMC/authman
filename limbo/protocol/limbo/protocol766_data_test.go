package limbo

import (
	"bytes"
	"testing"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
)

func TestDimensionTypeRegistryIncludesEnderDragonFightFlag(t *testing.T) {
	registry := dimensionTypeRegistry766(limbgo.DimensionPreset(limbgo.DimensionOverworld, 256))
	if len(registry.Entries) != 1 {
		t.Fatalf("dimension registry entries = %d, want 1", len(registry.Entries))
	}
	if !bytes.Contains(registry.Entries[0].Value, []byte("has_ender_dragon_fight")) {
		t.Fatalf("dimension registry missing has_ender_dragon_fight")
	}
}

func TestEndDimensionProfileEnablesEnderDragonFight(t *testing.T) {
	profile := dimensionProtocolProfile(limbgo.DimensionEnd)
	if !profile.hasEnderDragonFight {
		t.Fatalf("end dimension profile hasEnderDragonFight = false")
	}
}

func TestProtocol775DefaultWorldChunkContainsSpawnBedrock(t *testing.T) {
	protocols, err := DefaultModernProtocols()
	if err != nil {
		t.Fatalf("load modern protocols: %v", err)
	}
	cfg, ok := protocols.configFor(protocol775)
	if !ok {
		t.Fatalf("missing protocol 775 config")
	}
	if !cfg.chunkFixedPalettedStorage {
		t.Fatalf("protocol 775 chunkFixedPalettedStorage = false")
	}
	if cfg.chunkSectionFluidCount {
		t.Fatalf("protocol 775 chunkSectionFluidCount = true")
	}

	world := limbgo.DefaultWorld("default")
	chunk, ok := world.Chunk(0, 0)
	if !ok {
		t.Fatalf("default world missing chunk 0,0")
	}
	var packet bytes.Buffer
	_ = wire.WriteInt(&packet, 0)
	_ = wire.WriteInt(&packet, 0)
	writeHeightmapsArrayModern(&packet)
	chunkData := encodeChunkDataModern(world, chunk, cfg)
	_ = wire.WriteVarInt(&packet, int32(len(chunkData)))
	packet.Write(chunkData)
	_ = wire.WriteVarInt(&packet, 0)

	want := blockStateForProtocol(cfg.dataProtocolID(), world.BlockPalette(), 1)
	assertChunkBlockModern(t, packet.Bytes(), true, false, false, true, int(limbgo.DefaultBedrockY/16), 0, want)
}
