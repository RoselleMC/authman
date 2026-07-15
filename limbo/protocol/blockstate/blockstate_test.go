package blockstate

import (
	"testing"

	"github.com/RoselleMC/authman/limbo"
)

func TestDefaultState(t *testing.T) {
	cases := []struct {
		protocol int32
		name     string
		want     uint32
	}{
		{protocol: 47, name: "minecraft:stone", want: 16},
		{protocol: 340, name: "minecraft:stone", want: 16},
		{protocol: 766, name: "minecraft:stone", want: 1},
		{protocol: 774, name: "minecraft:stone", want: 1},
		{protocol: 775, name: "minecraft:stone", want: 1},
	}

	for _, tt := range cases {
		got, ok := DefaultState(tt.protocol, limbgo.BlockState{Name: tt.name})
		if !ok {
			t.Fatalf("missing default state for protocol=%d block=%s", tt.protocol, tt.name)
		}
		if got != tt.want {
			t.Fatalf("default state for protocol=%d block=%s = %d, want %d", tt.protocol, tt.name, got, tt.want)
		}
	}
}

func TestUnknownBlock(t *testing.T) {
	if _, ok := DefaultState(47, limbgo.BlockState{Name: "minecraft:not_a_block"}); ok {
		t.Fatal("unexpected default state for unknown block")
	}
}
