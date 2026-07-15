package schematic

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/RoselleMC/authman/limbo"
	"github.com/Tnze/go-mc/nbt"
)

func TestLoadSpongeSchematic(t *testing.T) {
	input := spongeSchematic{
		Version: 3,
		Width:   2,
		Height:  1,
		Length:  1,
		Palette: map[string]int32{
			"minecraft:air":   0,
			"minecraft:stone": 1,
		},
		BlockData: encodeSchematicVarInts(1, 0),
	}

	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	if err := nbt.NewEncoder(gz).Encode(input, "Schematic"); err != nil {
		t.Fatalf("encode nbt: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	world, err := Load(bytes.NewReader(encoded.Bytes()), Options{WorldID: "spawn"})
	if err != nil {
		t.Fatalf("load schematic: %v", err)
	}
	if world.ID() != "spawn" {
		t.Fatalf("world id = %q, want spawn", world.ID())
	}

	palette := world.BlockPalette()
	if len(palette) != 2 || palette[1].Name != "minecraft:stone" {
		t.Fatalf("palette = %+v", palette)
	}

	chunk, ok := world.Chunk(0, 0)
	if !ok {
		t.Fatal("missing chunk 0,0")
	}
	if len(chunk.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(chunk.Sections))
	}
	if got := chunk.Sections[0].BlockStateIDs[0]; got != 1 {
		t.Fatalf("first block palette id = %d, want 1", got)
	}
	if got := chunk.Sections[0].BlockStateIDs[1]; got != 0 {
		t.Fatalf("second block palette id = %d, want 0", got)
	}
}

func TestStaticWorldProviderReturnsSchematicWorld(t *testing.T) {
	world := &limbgo.MemoryWorld{WorldID: "spawn"}
	provider := limbgo.StaticWorldProvider{"spawn": world}
	got, err := provider.World(nil, "spawn")
	if err != nil {
		t.Fatalf("lookup world: %v", err)
	}
	if got.ID() != "spawn" {
		t.Fatalf("world id = %q, want spawn", got.ID())
	}
}

func encodeSchematicVarInts(values ...uint32) []byte {
	var out []byte
	for _, value := range values {
		for {
			if value&^uint32(0x7f) == 0 {
				out = append(out, byte(value))
				break
			}
			out = append(out, byte(value&0x7f|0x80))
			value >>= 7
		}
	}
	return out
}
