package limbgo

import "testing"

func TestDefaultWorldHasBedrockAtDefaultSpawn(t *testing.T) {
	world := DefaultWorld("spawn")
	if world.ID() != "spawn" {
		t.Fatalf("world id = %q", world.ID())
	}
	spawn := DefaultSpawn("spawn")
	if spawn.World != "spawn" {
		t.Fatalf("spawn world = %q", spawn.World)
	}
	if spawn.Position != (Vec3{X: 0, Y: 65, Z: 0}) {
		t.Fatalf("spawn position = %+v", spawn.Position)
	}
	chunk, ok := world.Chunk(0, 0)
	if !ok {
		t.Fatalf("missing default chunk")
	}
	if len(chunk.Sections) != 1 {
		t.Fatalf("sections = %d", len(chunk.Sections))
	}
	section := chunk.Sections[0]
	if section.Y != 4 {
		t.Fatalf("section y = %d, want 4", section.Y)
	}
	if section.BlockStateIDs[0] != 1 {
		t.Fatalf("default block palette id = %d, want bedrock palette id 1", section.BlockStateIDs[0])
	}
	palette := world.BlockPalette()
	if len(palette) != 2 || palette[1].Name != "minecraft:bedrock" {
		t.Fatalf("palette = %+v", palette)
	}
}
