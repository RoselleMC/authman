package registrydata

import (
	"bytes"
	"testing"
)

func TestRegistriesIncludeModernBaselineEntries(t *testing.T) {
	for _, protocol := range []int32{766, 767, 768, 769, 770, 771, 772, 773, 774, 775} {
		registries, ok := Registries(protocol)
		if !ok {
			t.Fatalf("protocol %d has no generated registries", protocol)
		}
		entries := map[string]bool{}
		for _, registry := range registries {
			for _, entry := range registry.Entries {
				if len(entry.Value) == 0 || entry.Value[0] != 10 {
					t.Fatalf("protocol %d entry %s/%s is not anonymous compound NBT", protocol, registry.ID, entry.Key)
				}
				entries[registry.ID+"/"+entry.Key] = true
			}
		}
		for _, key := range []string{
			"minecraft:worldgen/biome/minecraft:plains",
			"minecraft:chat_type/minecraft:chat",
			"minecraft:damage_type/minecraft:generic",
		} {
			if !entries[key] {
				t.Fatalf("protocol %d missing generated entry %s", protocol, key)
			}
		}
	}
}

func TestProtocols771To773IncludeCompleteVanillaConfigurationData(t *testing.T) {
	requiredRegistries := []string{
		"minecraft:worldgen/biome",
		"minecraft:chat_type",
		"minecraft:trim_pattern",
		"minecraft:trim_material",
		"minecraft:wolf_variant",
		"minecraft:wolf_sound_variant",
		"minecraft:pig_variant",
		"minecraft:frog_variant",
		"minecraft:cat_variant",
		"minecraft:cow_variant",
		"minecraft:chicken_variant",
		"minecraft:painting_variant",
		"minecraft:damage_type",
		"minecraft:banner_pattern",
		"minecraft:enchantment",
		"minecraft:jukebox_song",
		"minecraft:instrument",
		"minecraft:test_environment",
		"minecraft:test_instance",
		"minecraft:dialog",
	}
	for _, protocol := range []int32{771, 772, 773} {
		registries, ok := Registries(protocol)
		if !ok {
			t.Fatalf("protocol %d has no generated registries", protocol)
		}
		registrySizes := make(map[string]int, len(registries))
		for _, registry := range registries {
			registrySizes[registry.ID] = len(registry.Entries)
		}
		for _, registryID := range requiredRegistries {
			if registrySizes[registryID] == 0 {
				t.Fatalf("protocol %d missing non-empty vanilla registry %s", protocol, registryID)
			}
		}

		tags, ok := Tags(protocol)
		if !ok {
			t.Fatalf("protocol %d has no generated tags", protocol)
		}
		tagCount := 0
		for _, registry := range tags {
			tagCount += len(registry.Tags)
		}
		if len(tags) < 13 || tagCount < 500 {
			t.Fatalf("protocol %d has incomplete vanilla tags: %d registries, %d tags", protocol, len(tags), tagCount)
		}
	}
}

func TestEmbeddedZipLoadsProtocolRegistryFiles(t *testing.T) {
	data, err := LoadZipBytes(embeddedZip)
	if err != nil {
		t.Fatalf("load embedded registry zip: %v", err)
	}
	if codec, ok := data.DimensionCodec(735); !ok || len(codec) == 0 {
		t.Fatalf("embedded registry zip missing protocol 735 dimension codec")
	}
	if codec, ok := data.DimensionCodec(756); !ok || len(codec) == 0 {
		t.Fatalf("embedded registry zip missing protocol 756 dimension codec")
	}
	if dimension, ok := data.Dimension(756); !ok || len(dimension) == 0 {
		t.Fatalf("embedded registry zip missing protocol 756 dimension")
	}
	if _, ok := data.Registries(775); !ok {
		t.Fatalf("embedded registry zip missing protocol 775 registries")
	}
	if _, ok := data.Tags(775); !ok {
		t.Fatalf("embedded registry zip missing protocol 775 tags")
	}
}

func TestLoadFileAcceptsRegistryZip(t *testing.T) {
	data, err := LoadFile("registrydata.zip")
	if err != nil {
		t.Fatalf("load registrydata.zip: %v", err)
	}
	if _, ok := data.Registries(774); !ok {
		t.Fatalf("registrydata.zip missing protocol 774 registries")
	}
}

func TestStoreCanHotUpdateFromZip(t *testing.T) {
	store, err := NewStoreFromZip(embeddedZip)
	if err != nil {
		t.Fatalf("new store from zip: %v", err)
	}
	if err := store.UpdateZip(embeddedZip); err != nil {
		t.Fatalf("hot update store from zip: %v", err)
	}
	data, err := store.RegistryData()
	if err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	if _, ok := data.Registries(775); !ok {
		t.Fatalf("hot-updated store missing protocol 775 registries")
	}
}

func TestProtocol775WolfVariantsIncludeBabyAssets(t *testing.T) {
	registries, ok := Registries(775)
	if !ok {
		t.Fatalf("protocol 775 has no generated registries")
	}
	for _, registry := range registries {
		if registry.ID != "minecraft:wolf_variant" {
			continue
		}
		if len(registry.Entries) == 0 {
			t.Fatalf("protocol 775 wolf_variant has no entries")
		}
		for _, entry := range registry.Entries {
			if !bytes.Contains(entry.Value, []byte("baby_assets")) {
				t.Fatalf("protocol 775 wolf_variant %s missing baby_assets", entry.Key)
			}
		}
		return
	}
	t.Fatalf("protocol 775 missing wolf_variant registry")
}

func TestProtocol775WolfSoundVariantsIncludeAgeSplitSounds(t *testing.T) {
	expectedEntries := map[string][]string{
		"minecraft:cat_sound_variant":     {"minecraft:classic", "minecraft:royal"},
		"minecraft:chicken_sound_variant": {"minecraft:classic", "minecraft:picky"},
		"minecraft:pig_sound_variant":     {"minecraft:big", "minecraft:classic", "minecraft:mini"},
		"minecraft:wolf_sound_variant":    {"minecraft:angry", "minecraft:big", "minecraft:classic", "minecraft:cute", "minecraft:grumpy", "minecraft:puglin", "minecraft:sad"},
	}
	for _, registryID := range []string{
		"minecraft:cat_sound_variant",
		"minecraft:chicken_sound_variant",
		"minecraft:pig_sound_variant",
		"minecraft:wolf_sound_variant",
	} {
		assertRegistryEntries(t, 775, registryID, expectedEntries[registryID])
		for _, entry := range entriesForRegistry(t, 775, registryID) {
			if !bytes.Contains(entry.Value, []byte("adult_sounds")) {
				t.Fatalf("protocol 775 %s/%s missing adult_sounds", registryID, entry.Key)
			}
			if !bytes.Contains(entry.Value, []byte("baby_sounds")) {
				t.Fatalf("protocol 775 %s/%s missing baby_sounds", registryID, entry.Key)
			}
		}
	}
	assertRegistryEntries(t, 775, "minecraft:cow_sound_variant", []string{"minecraft:classic", "minecraft:moody"})
	for _, entry := range entriesForRegistry(t, 775, "minecraft:cow_sound_variant") {
		if !bytes.Contains(entry.Value, []byte("ambient_sound")) {
			t.Fatalf("protocol 775 minecraft:cow_sound_variant/%s missing ambient_sound", entry.Key)
		}
	}
}

func TestProtocol775FarmVariantsIncludeBabyAssets(t *testing.T) {
	expectedEntries := map[string][]string{
		"minecraft:cat_variant":     {"minecraft:all_black", "minecraft:black", "minecraft:british_shorthair", "minecraft:calico", "minecraft:jellie", "minecraft:persian", "minecraft:ragdoll", "minecraft:red", "minecraft:siamese", "minecraft:tabby", "minecraft:white"},
		"minecraft:chicken_variant": {"minecraft:cold", "minecraft:temperate", "minecraft:warm"},
		"minecraft:cow_variant":     {"minecraft:cold", "minecraft:temperate", "minecraft:warm"},
		"minecraft:pig_variant":     {"minecraft:cold", "minecraft:temperate", "minecraft:warm"},
	}
	for _, registryID := range []string{
		"minecraft:cat_variant",
		"minecraft:chicken_variant",
		"minecraft:cow_variant",
		"minecraft:pig_variant",
	} {
		assertRegistryEntries(t, 775, registryID, expectedEntries[registryID])
		for _, entry := range entriesForRegistry(t, 775, registryID) {
			if !bytes.Contains(entry.Value, []byte("baby_asset_id")) {
				t.Fatalf("protocol 775 %s/%s missing baby_asset_id", registryID, entry.Key)
			}
		}
	}
}

func TestProtocol775DamageTypeTagsIncludeClientRequiredTags(t *testing.T) {
	isFire := tagValuesForRegistry(t, 775, "minecraft:damage_type", "minecraft:is_fire")
	if len(isFire) == 0 {
		t.Fatalf("protocol 775 minecraft:damage_type/minecraft:is_fire tag is empty")
	}
	bypassesShield := tagValuesForRegistry(t, 775, "minecraft:damage_type", "minecraft:bypasses_shield")
	if len(bypassesShield) <= len(isFire) {
		t.Fatalf("protocol 775 minecraft:damage_type/minecraft:bypasses_shield tag did not expand referenced tags")
	}
	coldFarmBiomes := tagValuesForRegistry(t, 775, "minecraft:worldgen/biome", "minecraft:spawns_cold_variant_farm_animals")
	if len(coldFarmBiomes) == 0 {
		t.Fatalf("protocol 775 minecraft:worldgen/biome/minecraft:spawns_cold_variant_farm_animals tag is empty")
	}
	flowerBannerPattern := tagValuesForRegistry(t, 775, "minecraft:banner_pattern", "minecraft:pattern_item/flower")
	if len(flowerBannerPattern) == 0 {
		t.Fatalf("protocol 775 minecraft:banner_pattern/minecraft:pattern_item/flower tag is empty")
	}
	goatHorns := tagValuesForRegistry(t, 775, "minecraft:instrument", "minecraft:goat_horns")
	if len(goatHorns) == 0 {
		t.Fatalf("protocol 775 minecraft:instrument/minecraft:goat_horns tag is empty")
	}
}

func TestProtocol774IncludesVanillaRequiredVariantRegistries(t *testing.T) {
	registries, ok := Registries(774)
	if !ok {
		t.Fatalf("protocol 774 has no generated registries")
	}
	entries := map[string]int{}
	for _, registry := range registries {
		entries[registry.ID] = len(registry.Entries)
	}
	for _, registryID := range []string{
		"minecraft:cat_variant",
		"minecraft:chicken_variant",
		"minecraft:cow_variant",
		"minecraft:frog_variant",
		"minecraft:painting_variant",
		"minecraft:pig_variant",
		"minecraft:wolf_sound_variant",
		"minecraft:wolf_variant",
		"minecraft:zombie_nautilus_variant",
	} {
		if entries[registryID] == 0 {
			t.Fatalf("protocol 774 missing non-empty registry %s", registryID)
		}
	}
}

func entriesForRegistry(t *testing.T, protocol int32, registryID string) []Entry {
	t.Helper()
	registries, ok := Registries(protocol)
	if !ok {
		t.Fatalf("protocol %d has no generated registries", protocol)
	}
	for _, registry := range registries {
		if registry.ID == registryID {
			if len(registry.Entries) == 0 {
				t.Fatalf("protocol %d registry %s has no entries", protocol, registryID)
			}
			return registry.Entries
		}
	}
	t.Fatalf("protocol %d missing registry %s", protocol, registryID)
	return nil
}

func assertRegistryEntries(t *testing.T, protocol int32, registryID string, expected []string) {
	t.Helper()
	present := map[string]bool{}
	for _, entry := range entriesForRegistry(t, protocol, registryID) {
		present[entry.Key] = true
	}
	for _, key := range expected {
		if !present[key] {
			t.Fatalf("protocol %d missing registry entry %s/%s", protocol, registryID, key)
		}
	}
}

func tagValuesForRegistry(t *testing.T, protocol int32, registryID string, tagKey string) []int32 {
	t.Helper()
	tags, ok := Tags(protocol)
	if !ok {
		t.Fatalf("protocol %d has no generated tags", protocol)
	}
	for _, registry := range tags {
		if registry.ID != registryID {
			continue
		}
		for _, tag := range registry.Tags {
			if tag.Key == tagKey {
				return tag.Values
			}
		}
	}
	t.Fatalf("protocol %d missing tag %s/%s", protocol, registryID, tagKey)
	return nil
}

func TestProtocol774IncludesRequiredDamageTypes(t *testing.T) {
	registries, ok := Registries(774)
	if !ok {
		t.Fatalf("protocol 774 has no generated registries")
	}
	damageTypes := map[string]bool{}
	for _, registry := range registries {
		if registry.ID != "minecraft:damage_type" {
			continue
		}
		for _, entry := range registry.Entries {
			damageTypes[entry.Key] = true
		}
	}
	for _, key := range []string{
		"minecraft:generic",
		"minecraft:in_fire",
		"minecraft:on_fire",
		"minecraft:out_of_world",
	} {
		if !damageTypes[key] {
			t.Fatalf("protocol 774 missing damage type %s", key)
		}
	}
}

func TestProtocol774IncludesWorldRuntimeRegistryEntries(t *testing.T) {
	registries, ok := Registries(774)
	if !ok {
		t.Fatalf("protocol 774 has no generated registries")
	}
	entries := map[string]map[string]bool{}
	for _, registry := range registries {
		entries[registry.ID] = map[string]bool{}
		for _, entry := range registry.Entries {
			entries[registry.ID][entry.Key] = true
		}
	}
	cases := map[string][]string{
		"minecraft:worldgen/biome": {"minecraft:plains", "minecraft:forest", "minecraft:dark_forest"},
		"minecraft:banner_pattern": {"minecraft:base", "minecraft:border", "minecraft:stripe_bottom"},
		"minecraft:dialog":         {"minecraft:server_links", "minecraft:quick_actions"},
	}
	for registryID, keys := range cases {
		for _, key := range keys {
			if !entries[registryID][key] {
				t.Fatalf("protocol 774 missing %s/%s", registryID, key)
			}
		}
	}
}

func TestGeneratedRegistrySetsAreNonEmpty(t *testing.T) {
	data, err := Default()
	if err != nil {
		t.Fatalf("load default registry data: %v", err)
	}
	for protocol, registries := range data.registries {
		for _, registry := range registries {
			if len(registry.Entries) == 0 {
				t.Fatalf("protocol %d registry %s has no entries", protocol, registry.ID)
			}
		}
	}
}

func TestProtocol774IncludesRequiredGeneratedTags(t *testing.T) {
	tags, ok := Tags(774)
	if !ok {
		t.Fatalf("protocol 774 has no generated tags")
	}
	for _, registry := range tags {
		if registry.ID != "minecraft:item" {
			continue
		}
		for _, tag := range registry.Tags {
			if tag.Key == "minecraft:enchantable/head_armor" && len(tag.Values) > 0 {
				return
			}
		}
	}
	t.Fatalf("protocol 774 missing non-empty minecraft:item/minecraft:enchantable/head_armor tag")
}

func TestProtocol774BindsEnchantmentReferencedTags(t *testing.T) {
	tags, ok := Tags(774)
	if !ok {
		t.Fatalf("protocol 774 has no generated tags")
	}
	present := map[string]bool{}
	for _, registry := range tags {
		for _, tag := range registry.Tags {
			present[registry.ID+"/"+tag.Key] = true
		}
	}
	for _, key := range []string{
		"minecraft:block/minecraft:soul_speed_blocks",
		"minecraft:block/minecraft:blocks_wind_charge_explosions",
		"minecraft:entity_type/minecraft:sensitive_to_smite",
		"minecraft:entity_type/minecraft:arrows",
	} {
		if !present[key] {
			t.Fatalf("protocol 774 missing generated tag %s", key)
		}
	}
}

func TestDimensionCodecIncludesConfigurationBaseline(t *testing.T) {
	for _, protocol := range []int32{757, 758, 759, 760, 761, 762, 763, 764, 765} {
		codec, ok := DimensionCodec(protocol)
		if !ok {
			t.Fatalf("protocol %d has no generated dimension codec", protocol)
		}
		if len(codec) == 0 || codec[0] != 10 {
			t.Fatalf("protocol %d dimension codec is not anonymous compound NBT", protocol)
		}
	}
}

func TestDimensionIncludesNBTBaseline(t *testing.T) {
	data, err := Default()
	if err != nil {
		t.Fatalf("load default registry data: %v", err)
	}
	for _, protocol := range []int32{757, 758} {
		dimension, ok := data.Dimension(protocol)
		if !ok {
			t.Fatalf("protocol %d has no generated dimension", protocol)
		}
		if len(dimension) == 0 || dimension[0] != 10 {
			t.Fatalf("protocol %d dimension is not anonymous compound NBT", protocol)
		}
	}
}
