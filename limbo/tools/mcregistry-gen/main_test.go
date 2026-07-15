package main

import (
	"encoding/json"
	"testing"
)

func TestNormalizeBiomeMusicForProtocol771(t *testing.T) {
	music := nbtValue{Type: "compound", Value: mustJSON(t, map[string]nbtValue{
		"sound":                 {Type: "string", Value: json.RawMessage(`"minecraft:music.overworld.plains"`)},
		"min_delay":             {Type: "int", Value: json.RawMessage("12000")},
		"max_delay":             {Type: "int", Value: json.RawMessage("24000")},
		"replace_current_music": {Type: "byte", Value: json.RawMessage("0")},
	})}
	biome := nbtValue{Type: "compound", Value: mustJSON(t, map[string]nbtValue{
		"effects": {Type: "compound", Value: mustJSON(t, map[string]nbtValue{"music": music})},
	})}
	registries := []generatedRegistry{{
		ID: "minecraft:worldgen/biome",
		Entries: []generatedEntry{{
			Key:       "minecraft:plains",
			Source:    biome,
			HasSource: true,
		}},
	}}

	if err := normalizeRegistries(771, registries); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}
	var root map[string]nbtValue
	if err := json.Unmarshal(registries[0].Entries[0].Source.Value, &root); err != nil {
		t.Fatalf("decode normalized biome: %v", err)
	}
	var effects map[string]nbtValue
	if err := json.Unmarshal(root["effects"].Value, &effects); err != nil {
		t.Fatalf("decode normalized effects: %v", err)
	}
	if effects["music"].Type != "list" {
		t.Fatalf("music type = %q, want list", effects["music"].Type)
	}
	if effects["music_volume"].Type != "float" {
		t.Fatalf("music_volume type = %q, want float", effects["music_volume"].Type)
	}
	if len(registries[0].Entries[0].Value) == 0 {
		t.Fatal("normalized NBT was not encoded")
	}
}

func TestNormalizeBiomeMusicLeavesProtocol770Unchanged(t *testing.T) {
	source := nbtValue{Type: "compound", Value: json.RawMessage(`{"effects":{"type":"compound","value":{}}}`)}
	registries := []generatedRegistry{{
		ID:      "minecraft:worldgen/biome",
		Entries: []generatedEntry{{Key: "minecraft:plains", Source: source, HasSource: true}},
	}}

	if err := normalizeRegistries(770, registries); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}
	if string(registries[0].Entries[0].Source.Value) != string(source.Value) {
		t.Fatalf("protocol 770 source changed: %s", registries[0].Entries[0].Source.Value)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return raw
}
