package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	nbtEnd       byte = 0
	nbtByte      byte = 1
	nbtShort     byte = 2
	nbtInt       byte = 3
	nbtLong      byte = 4
	nbtFloat     byte = 5
	nbtDouble    byte = 6
	nbtByteArray byte = 7
	nbtString    byte = 8
	nbtList      byte = 9
	nbtCompound  byte = 10
	nbtIntArray  byte = 11
	nbtLongArray byte = 12
)

var preferredEntries = map[string][]string{
	"minecraft:worldgen/biome": {"minecraft:plains"},
	"minecraft:chat_type":      {"minecraft:chat"},
	"minecraft:damage_type":    {"minecraft:generic"},
	"minecraft:dialog":         {"minecraft:server_links"},
}

var compactRegistries = map[string]bool{}

type versionJSON struct {
	Version          int32  `json:"version"`
	MinecraftVersion string `json:"minecraftVersion"`
	MajorVersion     string `json:"majorVersion"`
	ReleaseType      string `json:"releaseType"`
}

type protocolVersionJSON struct {
	MinecraftVersion string `json:"minecraftVersion"`
	Version          int32  `json:"version"`
	ReleaseType      string `json:"releaseType"`
}

type loginPacketJSON struct {
	DimensionCodec json.RawMessage `json:"dimensionCodec"`
	Dimension      json.RawMessage `json:"dimension"`
}

type registryJSON struct {
	ID      string      `json:"id"`
	Entries []entryJSON `json:"entries"`
}

type entryJSON struct {
	Key   string   `json:"key"`
	Value nbtValue `json:"value"`
}

type nbtValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type generatedRegistry struct {
	ID      string
	Entries []generatedEntry
}

type generatedEntry struct {
	Key       string
	Value     []byte
	Source    nbtValue
	HasSource bool
}

type generatedData struct {
	Registries map[int32][]generatedRegistry
	Tags       map[int32][]generatedTagRegistry
	Codecs     map[int32][]byte
	Dimensions map[int32][]byte
}

type generatedTagRegistry struct {
	ID   string
	Tags []generatedTag
}

type generatedTag struct {
	Key    string
	Values []int32
}

type encodedData struct {
	Registries      map[string][]encodedRegistry    `json:"registries"`
	Tags            map[string][]encodedTagRegistry `json:"tags,omitempty"`
	DimensionCodecs map[string]string               `json:"dimension_codecs"`
	Dimensions      map[string]string               `json:"dimensions"`
}

type encodedProtocolData struct {
	FormatVersion  int                  `json:"format_version,omitempty"`
	Protocol       int32                `json:"protocol,omitempty"`
	Registries     []encodedRegistry    `json:"registries,omitempty"`
	Tags           []encodedTagRegistry `json:"tags,omitempty"`
	DimensionCodec string               `json:"dimension_codec,omitempty"`
	Dimension      string               `json:"dimension,omitempty"`
}

type encodedRegistry struct {
	ID      string         `json:"id"`
	Entries []encodedEntry `json:"entries"`
}

type encodedEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type encodedTagRegistry struct {
	ID   string       `json:"id"`
	Tags []encodedTag `json:"tags"`
}

type encodedTag struct {
	Key    string  `json:"key"`
	Values []int32 `json:"values"`
}

type itemJSON struct {
	ID                int32    `json:"id"`
	Name              string   `json:"name"`
	EnchantCategories []string `json:"enchantCategories"`
}

func main() {
	var pcDataDir string
	var outPath string
	var outDir string
	var zipOut string
	flag.StringVar(&pcDataDir, "pc-data", "", "path to minecraft-data/data/pc")
	flag.StringVar(&outPath, "out", "", "output legacy aggregate JSON file")
	flag.StringVar(&outDir, "out-dir", "", "output directory for per-protocol JSON files")
	flag.StringVar(&zipOut, "zip-out", "", "output zip containing per-protocol JSON files")
	flag.Parse()

	if pcDataDir == "" || (outPath == "" && outDir == "" && zipOut == "") {
		fatalf("-pc-data and one of -out, -out-dir, or -zip-out are required")
	}
	data, err := readProtocolRegistries(pcDataDir)
	if err != nil {
		fatalf("%v", err)
	}
	if outPath != "" {
		source, err := render(data)
		if err != nil {
			fatalf("%v", err)
		}
		if err := os.WriteFile(outPath, source, 0o644); err != nil {
			fatalf("write %s: %v", outPath, err)
		}
	}
	if outDir != "" || zipOut != "" {
		files, err := renderProtocolFiles(data)
		if err != nil {
			fatalf("%v", err)
		}
		if outDir != "" {
			if err := writeProtocolDir(outDir, files); err != nil {
				fatalf("%v", err)
			}
		}
		if zipOut != "" {
			if err := writeProtocolZip(zipOut, files); err != nil {
				fatalf("%v", err)
			}
		}
	}
}

func readProtocolRegistries(pcDataDir string) (generatedData, error) {
	orderPath := filepath.Join(pcDataDir, "common", "versions.json")
	data, err := os.ReadFile(orderPath)
	if err != nil {
		return generatedData{}, fmt.Errorf("read version order: %w", err)
	}
	var orderedDirs []string
	if err := json.Unmarshal(data, &orderedDirs); err != nil {
		return generatedData{}, fmt.Errorf("parse version order: %w", err)
	}
	releaseTypes, err := readReleaseTypes(pcDataDir)
	if err != nil {
		return generatedData{}, err
	}

	out := generatedData{
		Registries: map[int32][]generatedRegistry{},
		Tags:       map[int32][]generatedTagRegistry{},
		Codecs:     map[int32][]byte{},
		Dimensions: map[int32][]byte{},
	}
	var latest []generatedRegistry
	var latestTags []generatedTagRegistry
	var latestCodec []byte
	var latestDimension []byte
	for _, dir := range orderedDirs {
		version, err := readVersion(filepath.Join(pcDataDir, dir, "version.json"))
		if err != nil || version.Version <= 0 || version.MinecraftVersion == "" {
			continue
		}
		version.ReleaseType = releaseTypeFor(version, releaseTypes)
		if version.ReleaseType != "release" || !isModernJava(version.MajorVersion) || !isReleaseName(version.MinecraftVersion) {
			continue
		}
		loginPath := filepath.Join(pcDataDir, dir, "loginPacket.json")
		if registries, tags, codec, dimension, err := readLoginRegistries(loginPath); err == nil {
			if registries != nil {
				latest = registries
			}
			if tags != nil {
				latestTags = tags
			}
			if codec != nil {
				latestCodec = codec
			}
			if dimension != nil {
				latestDimension = dimension
			}
		} else if version.Version >= 735 && !os.IsNotExist(err) {
			return generatedData{}, err
		}
		if latest != nil {
			registries := cloneRegistries(latest)
			if err := normalizeRegistries(version.Version, registries); err != nil {
				return generatedData{}, fmt.Errorf("normalize protocol %d registry data: %w", version.Version, err)
			}
			out.Registries[version.Version] = registries
		}
		if latestTags != nil {
			out.Tags[version.Version] = cloneTags(latestTags)
		}
		if latestCodec != nil && version.Version >= 735 && version.Version < 766 {
			out.Codecs[version.Version] = append([]byte(nil), latestCodec...)
		}
		if latestDimension != nil && version.Version >= 751 && version.Version < 759 {
			out.Dimensions[version.Version] = append([]byte(nil), latestDimension...)
		}
	}
	if len(out.Registries) == 0 && len(out.Codecs) == 0 {
		return generatedData{}, fmt.Errorf("no registry data found in %s", pcDataDir)
	}
	return out, nil
}

func readLoginRegistries(path string) ([]generatedRegistry, []generatedTagRegistry, []byte, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var login loginPacketJSON
	if err := json.Unmarshal(data, &login); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(login.DimensionCodec) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("%s missing dimensionCodec", path)
	}

	var dimension []byte
	if len(login.Dimension) > 0 {
		var dimensionNBT nbtValue
		if err := json.Unmarshal(login.Dimension, &dimensionNBT); err == nil && dimensionNBT.Type != "" {
			encoded, err := encodeAnonymousNBT(dimensionNBT)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("%s encode dimension: %w", path, err)
			}
			dimension = encoded
		}
	}

	var typed nbtValue
	if err := json.Unmarshal(login.DimensionCodec, &typed); err == nil && typed.Type != "" {
		codec, err := encodeAnonymousNBT(typed)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s encode dimensionCodec: %w", path, err)
		}
		return nil, nil, codec, dimension, nil
	}

	var dimensionCodec map[string]registryJSON
	if err := json.Unmarshal(login.DimensionCodec, &dimensionCodec); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("parse %s dimensionCodec: %w", path, err)
	}
	var registryIDs []string
	for registryID := range dimensionCodec {
		if registryID == "minecraft:dimension_type" {
			continue
		}
		registryIDs = append(registryIDs, registryID)
	}
	sort.Strings(registryIDs)

	var registries []generatedRegistry
	tagRefs := map[string]map[string]bool{}
	for _, registryID := range registryIDs {
		registry, ok := dimensionCodec[registryID]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("%s missing registry %s", path, registryID)
		}
		generated := generatedRegistry{ID: registry.ID}
		if generated.ID == "" {
			generated.ID = registryID
		}
		if !compactRegistries[registryID] {
			for _, entry := range registry.Entries {
				collectTagReferences(registryID, entry.Value, tagRefs)
				value, err := encodeAnonymousNBT(entry.Value)
				if err != nil {
					return nil, nil, nil, nil, fmt.Errorf("%s encode %s/%s: %w", path, registryID, entry.Key, err)
				}
				generated.Entries = append(generated.Entries, generatedEntry{
					Key:       entry.Key,
					Value:     value,
					Source:    cloneNBTValue(entry.Value),
					HasSource: true,
				})
			}
			registries = append(registries, generated)
			continue
		}
		entry, err := selectRegistryEntry(registryID, registry.Entries)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s %w", path, err)
		}
		collectTagReferences(registryID, entry.Value, tagRefs)
		value, err := encodeAnonymousNBT(entry.Value)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s encode %s/%s: %w", path, registryID, entry.Key, err)
		}
		generated.Entries = append(generated.Entries, generatedEntry{
			Key:       entry.Key,
			Value:     value,
			Source:    cloneNBTValue(entry.Value),
			HasSource: true,
		})
		registries = append(registries, generated)
	}
	tags, err := generateTags(path, tagRefs, registries)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return registries, tags, nil, dimension, nil
}

func selectRegistryEntry(registryID string, entries []entryJSON) (entryJSON, error) {
	for _, key := range preferredEntries[registryID] {
		if entry, ok := findEntry(entries, key); ok {
			return entry, nil
		}
	}
	if len(entries) == 0 {
		return entryJSON{}, fmt.Errorf("registry %s has no entries", registryID)
	}
	for _, entry := range entries {
		if !hasMinecraftTagReference(entry.Value) {
			return entry, nil
		}
	}
	return entries[0], nil
}

func collectTagReferences(sourceRegistry string, value nbtValue, refs map[string]map[string]bool) {
	var text string
	if value.Type == "string" && json.Unmarshal(value.Value, &text) == nil && strings.HasPrefix(text, "#minecraft:") {
		if targetRegistry, ok := tagTargetRegistry(sourceRegistry, strings.TrimPrefix(text, "#")); ok {
			if refs[targetRegistry] == nil {
				refs[targetRegistry] = map[string]bool{}
			}
			refs[targetRegistry][strings.TrimPrefix(text, "#")] = true
		}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(value.Value, &raw) == nil {
		for _, nested := range raw {
			var child nbtValue
			if json.Unmarshal(nested, &child) == nil && child.Type != "" {
				collectTagReferences(sourceRegistry, child, refs)
			}
		}
	}
	var list struct {
		Type  string            `json:"type"`
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(value.Value, &list) == nil && list.Type != "" {
		for _, rawChild := range list.Value {
			collectTagReferences(sourceRegistry, nbtValue{Type: list.Type, Value: rawChild}, refs)
		}
	}
}

func hasMinecraftTagReference(value nbtValue) bool {
	refs := map[string]map[string]bool{}
	collectTagReferences("minecraft:unknown", value, refs)
	for _, tags := range refs {
		if len(tags) > 0 {
			return true
		}
	}
	return false
}

func tagTargetRegistry(sourceRegistry string, tagName string) (string, bool) {
	switch {
	case sourceRegistry == "minecraft:enchantment" && strings.HasPrefix(tagName, "minecraft:enchantable/"):
		return "minecraft:item", true
	case sourceRegistry == "minecraft:enchantment" && tagName == "minecraft:arrows":
		return "minecraft:entity_type", true
	case sourceRegistry == "minecraft:enchantment" && (strings.HasSuffix(tagName, "_blocks") || strings.HasPrefix(tagName, "minecraft:blocks_") || tagName == "minecraft:lightning_rods"):
		return "minecraft:block", true
	case sourceRegistry == "minecraft:enchantment" && strings.HasPrefix(tagName, "minecraft:sensitive_to_"):
		return "minecraft:entity_type", true
	case sourceRegistry == "minecraft:dialog":
		return "minecraft:dialog", true
	default:
		return sourceRegistry, true
	}
}

func generateTags(loginPath string, refs map[string]map[string]bool, registries []generatedRegistry) ([]generatedTagRegistry, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	registryEntryIDs := generatedRegistryEntryIDs(registries)
	itemIDs, err := readItemIDs(filepath.Join(filepath.Dir(loginPath), "items.json"))
	if err != nil {
		return nil, err
	}
	var registryIDs []string
	for registryID := range refs {
		registryIDs = append(registryIDs, registryID)
	}
	sort.Strings(registryIDs)
	var out []generatedTagRegistry
	for _, registryID := range registryIDs {
		var tagNames []string
		for tagName := range refs[registryID] {
			tagNames = append(tagNames, tagName)
		}
		sort.Strings(tagNames)
		tagRegistry := generatedTagRegistry{ID: registryID}
		for _, tagName := range tagNames {
			entries, err := tagEntries(registryID, tagName, itemIDs, registryEntryIDs)
			if err != nil {
				return nil, err
			}
			tagRegistry.Tags = append(tagRegistry.Tags, generatedTag{Key: tagName, Values: entries})
		}
		out = append(out, tagRegistry)
	}
	return out, nil
}

func tagEntries(registryID string, tagName string, itemIDs map[string]itemJSON, registryEntryIDs map[string]map[string]int32) ([]int32, error) {
	if registryID == "minecraft:item" && strings.HasPrefix(tagName, "minecraft:enchantable/") {
		category := strings.TrimPrefix(tagName, "minecraft:enchantable/")
		var ids []int32
		for _, item := range itemIDs {
			for _, itemCategory := range item.EnchantCategories {
				if itemCategory == category {
					ids = append(ids, item.ID)
					break
				}
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids, nil
	}
	if entries := registryEntryIDs[registryID]; len(entries) > 0 {
		var ids []int32
		for _, id := range entries {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids, nil
	}
	return nil, nil
}

func generatedRegistryEntryIDs(registries []generatedRegistry) map[string]map[string]int32 {
	out := map[string]map[string]int32{}
	for _, registry := range registries {
		out[registry.ID] = map[string]int32{}
		for i, entry := range registry.Entries {
			out[registry.ID][entry.Key] = int32(i)
		}
	}
	return out
}

func readItemIDs(path string) (map[string]itemJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read items: %w", err)
	}
	var items []itemJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse items: %w", err)
	}
	out := make(map[string]itemJSON, len(items))
	for _, item := range items {
		out["minecraft:"+item.Name] = item
	}
	return out, nil
}

func findEntry(entries []entryJSON, key string) (entryJSON, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry, true
		}
	}
	return entryJSON{}, false
}

func encodeAnonymousNBT(value nbtValue) ([]byte, error) {
	tag, err := tagID(value.Type)
	if err != nil {
		return nil, err
	}
	if tag != nbtCompound {
		return nil, fmt.Errorf("root must be compound, got %s", value.Type)
	}
	var buf bytes.Buffer
	_ = buf.WriteByte(tag)
	if err := writePayload(&buf, tag, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeNamedValue(buf *bytes.Buffer, name string, value nbtValue) error {
	tag, err := tagID(value.Type)
	if err != nil {
		return err
	}
	_ = buf.WriteByte(tag)
	if tag == nbtEnd {
		return nil
	}
	writeRawString(buf, name)
	return writePayload(buf, tag, value)
}

func writePayload(buf *bytes.Buffer, tag byte, value nbtValue) error {
	switch tag {
	case nbtEnd:
		return nil
	case nbtByte:
		v, err := intValue(value.Value)
		if err != nil {
			return err
		}
		_ = buf.WriteByte(byte(int8(v)))
	case nbtShort:
		v, err := intValue(value.Value)
		if err != nil {
			return err
		}
		writeRawShort(buf, int16(v))
	case nbtInt:
		v, err := intValue(value.Value)
		if err != nil {
			return err
		}
		writeRawInt(buf, int32(v))
	case nbtLong:
		v, err := intValue(value.Value)
		if err != nil {
			return err
		}
		writeRawLong(buf, int64(v))
	case nbtFloat:
		v, err := floatValue(value.Value)
		if err != nil {
			return err
		}
		writeRawFloat(buf, float32(v))
	case nbtDouble:
		v, err := floatValue(value.Value)
		if err != nil {
			return err
		}
		writeRawDouble(buf, v)
	case nbtString:
		var s string
		if err := json.Unmarshal(value.Value, &s); err != nil {
			return err
		}
		writeRawString(buf, s)
	case nbtList:
		return writeListPayload(buf, value.Value)
	case nbtCompound:
		var fields map[string]nbtValue
		if err := json.Unmarshal(value.Value, &fields); err != nil {
			return err
		}
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := writeNamedValue(buf, name, fields[name]); err != nil {
				return err
			}
		}
		_ = buf.WriteByte(nbtEnd)
	case nbtByteArray:
		values, err := intArrayValue(value.Value)
		if err != nil {
			return err
		}
		writeRawInt(buf, int32(len(values)))
		for _, value := range values {
			_ = buf.WriteByte(byte(int8(value)))
		}
	case nbtIntArray:
		values, err := intArrayValue(value.Value)
		if err != nil {
			return err
		}
		writeRawInt(buf, int32(len(values)))
		for _, value := range values {
			writeRawInt(buf, int32(value))
		}
	case nbtLongArray:
		values, err := intArrayValue(value.Value)
		if err != nil {
			return err
		}
		writeRawInt(buf, int32(len(values)))
		for _, value := range values {
			writeRawLong(buf, int64(value))
		}
	default:
		return fmt.Errorf("unsupported tag id %d", tag)
	}
	return nil
}

func writeListPayload(buf *bytes.Buffer, raw json.RawMessage) error {
	var list struct {
		Type  string            `json:"type"`
		Value []json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	tag, err := tagID(list.Type)
	if err != nil {
		return err
	}
	_ = buf.WriteByte(tag)
	writeRawInt(buf, int32(len(list.Value)))
	for _, rawValue := range list.Value {
		if err := writePayload(buf, tag, nbtValue{Type: list.Type, Value: rawValue}); err != nil {
			return err
		}
	}
	return nil
}

func tagID(name string) (byte, error) {
	switch name {
	case "end":
		return nbtEnd, nil
	case "byte":
		return nbtByte, nil
	case "short":
		return nbtShort, nil
	case "int":
		return nbtInt, nil
	case "long":
		return nbtLong, nil
	case "float":
		return nbtFloat, nil
	case "double":
		return nbtDouble, nil
	case "byteArray":
		return nbtByteArray, nil
	case "string":
		return nbtString, nil
	case "list":
		return nbtList, nil
	case "compound":
		return nbtCompound, nil
	case "intArray":
		return nbtIntArray, nil
	case "longArray":
		return nbtLongArray, nil
	default:
		return 0, fmt.Errorf("unsupported nbt type %q", name)
	}
}

func intValue(raw json.RawMessage) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err == nil {
		return value.Int64()
	}

	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var pair []json.Number
	if err := decoder.Decode(&pair); err != nil {
		return 0, err
	}
	if len(pair) != 2 {
		return 0, fmt.Errorf("integer array has %d elements, want 2", len(pair))
	}
	hi, err := pair[0].Int64()
	if err != nil {
		return 0, err
	}
	lo, err := pair[1].Int64()
	if err != nil {
		return 0, err
	}
	return int64(uint64(uint32(hi))<<32 | uint64(uint32(lo))), nil
}

func floatValue(raw json.RawMessage) (float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	return value.Float64()
}

func intArrayValue(raw json.RawMessage) ([]int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var rawValues []json.Number
	if err := decoder.Decode(&rawValues); err != nil {
		return nil, err
	}
	values := make([]int64, 0, len(rawValues))
	for _, rawValue := range rawValues {
		value, err := rawValue.Int64()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func writeRawShort(buf *bytes.Buffer, value int16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(value))
	_, _ = buf.Write(b[:])
}

func writeRawInt(buf *bytes.Buffer, value int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(value))
	_, _ = buf.Write(b[:])
}

func writeRawLong(buf *bytes.Buffer, value int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(value))
	_, _ = buf.Write(b[:])
}

func writeRawFloat(buf *bytes.Buffer, value float32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], math.Float32bits(value))
	_, _ = buf.Write(b[:])
}

func writeRawDouble(buf *bytes.Buffer, value float64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(value))
	_, _ = buf.Write(b[:])
}

func writeRawString(buf *bytes.Buffer, value string) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(len(value)))
	_, _ = buf.Write(b[:])
	_, _ = io.WriteString(buf, value)
}

func readVersion(path string) (versionJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return versionJSON{}, err
	}
	var version versionJSON
	if err := json.Unmarshal(data, &version); err != nil {
		return versionJSON{}, err
	}
	return version, nil
}

func readReleaseTypes(pcDataDir string) (map[string]string, error) {
	path := filepath.Join(pcDataDir, "common", "protocolVersions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read protocol versions: %w", err)
	}
	var entries []protocolVersionJSON
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse protocol versions: %w", err)
	}
	releaseTypes := make(map[string]string, len(entries)*2)
	for _, entry := range entries {
		releaseType := releaseType(entry.ReleaseType)
		releaseTypes[entry.MinecraftVersion] = releaseType
		releaseTypes[entry.MinecraftVersion+"/"+strconv.Itoa(int(entry.Version))] = releaseType
	}
	return releaseTypes, nil
}

func render(data generatedData) ([]byte, error) {
	out := encodedData{
		Registries:      map[string][]encodedRegistry{},
		Tags:            map[string][]encodedTagRegistry{},
		DimensionCodecs: map[string]string{},
		Dimensions:      map[string]string{},
	}
	ids := make([]int, 0, len(data.Registries))
	for protocol := range data.Registries {
		ids = append(ids, int(protocol))
	}
	sort.Ints(ids)
	for _, id := range ids {
		var registries []encodedRegistry
		for _, registry := range data.Registries[int32(id)] {
			encoded := encodedRegistry{ID: registry.ID}
			for _, entry := range registry.Entries {
				encoded.Entries = append(encoded.Entries, encodedEntry{
					Key:   entry.Key,
					Value: base64.StdEncoding.EncodeToString(entry.Value),
				})
			}
			registries = append(registries, encoded)
		}
		out.Registries[strconv.Itoa(id)] = registries
	}
	tagIDs := make([]int, 0, len(data.Tags))
	for protocol := range data.Tags {
		tagIDs = append(tagIDs, int(protocol))
	}
	sort.Ints(tagIDs)
	for _, id := range tagIDs {
		var registries []encodedTagRegistry
		for _, registry := range data.Tags[int32(id)] {
			encoded := encodedTagRegistry{ID: registry.ID}
			for _, tag := range registry.Tags {
				encoded.Tags = append(encoded.Tags, encodedTag{
					Key:    tag.Key,
					Values: append([]int32(nil), tag.Values...),
				})
			}
			registries = append(registries, encoded)
		}
		out.Tags[strconv.Itoa(id)] = registries
	}
	codecIDs := make([]int, 0, len(data.Codecs))
	for protocol := range data.Codecs {
		codecIDs = append(codecIDs, int(protocol))
	}
	sort.Ints(codecIDs)
	for _, id := range codecIDs {
		out.DimensionCodecs[strconv.Itoa(id)] = base64.StdEncoding.EncodeToString(data.Codecs[int32(id)])
	}
	dimensionIDs := make([]int, 0, len(data.Dimensions))
	for protocol := range data.Dimensions {
		dimensionIDs = append(dimensionIDs, int(protocol))
	}
	sort.Ints(dimensionIDs)
	for _, id := range dimensionIDs {
		out.Dimensions[strconv.Itoa(id)] = base64.StdEncoding.EncodeToString(data.Dimensions[int32(id)])
	}
	source, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode registry data: %w", err)
	}
	return append(source, '\n'), nil
}

func renderProtocolFiles(data generatedData) (map[string][]byte, error) {
	files := map[string][]byte{}
	idSet := make(map[int]struct{})
	for protocol := range data.Registries {
		idSet[int(protocol)] = struct{}{}
	}
	for protocol := range data.Tags {
		idSet[int(protocol)] = struct{}{}
	}
	for protocol := range data.Codecs {
		idSet[int(protocol)] = struct{}{}
	}
	for protocol := range data.Dimensions {
		idSet[int(protocol)] = struct{}{}
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		protocol := int32(id)
		encoded := encodedProtocolData{FormatVersion: 1, Protocol: protocol}
		for _, registry := range data.Registries[protocol] {
			outRegistry := encodedRegistry{ID: registry.ID}
			for _, entry := range registry.Entries {
				outRegistry.Entries = append(outRegistry.Entries, encodedEntry{
					Key:   entry.Key,
					Value: base64.StdEncoding.EncodeToString(entry.Value),
				})
			}
			encoded.Registries = append(encoded.Registries, outRegistry)
		}
		for _, registry := range data.Tags[protocol] {
			outRegistry := encodedTagRegistry{ID: registry.ID}
			for _, tag := range registry.Tags {
				outRegistry.Tags = append(outRegistry.Tags, encodedTag{
					Key:    tag.Key,
					Values: append([]int32(nil), tag.Values...),
				})
			}
			encoded.Tags = append(encoded.Tags, outRegistry)
		}
		if codec := data.Codecs[protocol]; len(codec) > 0 {
			encoded.DimensionCodec = base64.StdEncoding.EncodeToString(codec)
		}
		if dimension := data.Dimensions[protocol]; len(dimension) > 0 {
			encoded.Dimension = base64.StdEncoding.EncodeToString(dimension)
		}
		source, err := json.MarshalIndent(encoded, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode protocol %d registry data: %w", protocol, err)
		}
		files[strconv.Itoa(id)+".json"] = append(source, '\n')
	}
	return files, nil
}

func writeProtocolDir(outDir string, files map[string][]byte) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}
	maxGeneratedProtocol := 0
	for name := range files {
		protocol, err := strconv.Atoi(strings.TrimSuffix(name, filepath.Ext(name)))
		if err == nil && protocol > maxGeneratedProtocol {
			maxGeneratedProtocol = protocol
		}
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", outDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		protocol, parseErr := strconv.Atoi(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if parseErr == nil && protocol > maxGeneratedProtocol {
			continue
		}
		if err := os.Remove(filepath.Join(outDir, entry.Name())); err != nil {
			return fmt.Errorf("remove stale protocol file %s: %w", entry.Name(), err)
		}
	}
	names := sortedFileNames(files)
	for _, name := range names {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func writeProtocolZip(zipOut string, files map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(zipOut), 0o755); err != nil {
		return fmt.Errorf("create zip dir: %w", err)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	modTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range sortedFileNames(files) {
		header := &zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: modTime,
		}
		header.SetMode(0o644)
		file, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return fmt.Errorf("create zip entry %s: %w", name, err)
		}
		if _, err := file.Write(files[name]); err != nil {
			_ = writer.Close()
			return fmt.Errorf("write zip entry %s: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}
	if err := os.WriteFile(zipOut, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", zipOut, err)
	}
	return nil
}

func sortedFileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func byteList(values []byte) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(int(value)))
	}
	return strings.Join(parts, ", ")
}

func byteStringLiteral(values []byte) string {
	if len(values) == 0 {
		return "nil"
	}
	var buf strings.Builder
	buf.WriteString("[]byte(")
	for start := 0; start < len(values); start += 128 {
		if start > 0 {
			buf.WriteString(" + ")
		}
		end := start + 128
		if end > len(values) {
			end = len(values)
		}
		buf.WriteString(fmt.Sprintf("%q", string(values[start:end])))
	}
	buf.WriteString(")")
	return buf.String()
}

func cloneRegistries(in []generatedRegistry) []generatedRegistry {
	out := make([]generatedRegistry, 0, len(in))
	for _, registry := range in {
		entries := make([]generatedEntry, 0, len(registry.Entries))
		for _, entry := range registry.Entries {
			entries = append(entries, generatedEntry{
				Key:       entry.Key,
				Value:     append([]byte(nil), entry.Value...),
				Source:    cloneNBTValue(entry.Source),
				HasSource: entry.HasSource,
			})
		}
		out = append(out, generatedRegistry{
			ID:      registry.ID,
			Entries: entries,
		})
	}
	return out
}

func normalizeRegistries(protocol int32, registries []generatedRegistry) error {
	if protocol < 771 || protocol > 773 {
		return nil
	}
	for registryIndex := range registries {
		registry := &registries[registryIndex]
		if registry.ID != "minecraft:worldgen/biome" {
			continue
		}
		for entryIndex := range registry.Entries {
			entry := &registry.Entries[entryIndex]
			if !entry.HasSource {
				return fmt.Errorf("biome %s has no structured NBT source", entry.Key)
			}
			normalized, err := normalizeBiomeMusic(entry.Source)
			if err != nil {
				return fmt.Errorf("biome %s: %w", entry.Key, err)
			}
			encoded, err := encodeAnonymousNBT(normalized)
			if err != nil {
				return fmt.Errorf("encode biome %s: %w", entry.Key, err)
			}
			entry.Source = normalized
			entry.Value = encoded
		}
	}
	return nil
}

func normalizeBiomeMusic(value nbtValue) (nbtValue, error) {
	if value.Type != "compound" {
		return nbtValue{}, fmt.Errorf("root is %q, want compound", value.Type)
	}
	var root map[string]nbtValue
	if err := json.Unmarshal(value.Value, &root); err != nil {
		return nbtValue{}, fmt.Errorf("decode root compound: %w", err)
	}
	effects, ok := root["effects"]
	if !ok || effects.Type != "compound" {
		return nbtValue{}, fmt.Errorf("effects compound is missing")
	}
	var effectFields map[string]nbtValue
	if err := json.Unmarshal(effects.Value, &effectFields); err != nil {
		return nbtValue{}, fmt.Errorf("decode effects compound: %w", err)
	}

	if music, ok := effectFields["music"]; ok && music.Type == "compound" {
		weightedMusic, err := weightedMusicList(music)
		if err != nil {
			return nbtValue{}, err
		}
		effectFields["music"] = weightedMusic
	}
	if _, ok := effectFields["music_volume"]; !ok {
		effectFields["music_volume"] = nbtValue{Type: "float", Value: json.RawMessage("1.0")}
	}

	rawEffects, err := json.Marshal(effectFields)
	if err != nil {
		return nbtValue{}, fmt.Errorf("encode effects compound: %w", err)
	}
	root["effects"] = nbtValue{Type: "compound", Value: rawEffects}
	rawRoot, err := json.Marshal(root)
	if err != nil {
		return nbtValue{}, fmt.Errorf("encode root compound: %w", err)
	}
	return nbtValue{Type: "compound", Value: rawRoot}, nil
}

func weightedMusicList(music nbtValue) (nbtValue, error) {
	element, err := json.Marshal(map[string]nbtValue{
		"data":   music,
		"weight": {Type: "int", Value: json.RawMessage("1")},
	})
	if err != nil {
		return nbtValue{}, fmt.Errorf("encode weighted music entry: %w", err)
	}
	payload, err := json.Marshal(struct {
		Type  string            `json:"type"`
		Value []json.RawMessage `json:"value"`
	}{
		Type:  "compound",
		Value: []json.RawMessage{element},
	})
	if err != nil {
		return nbtValue{}, fmt.Errorf("encode weighted music list: %w", err)
	}
	return nbtValue{Type: "list", Value: payload}, nil
}

func cloneNBTValue(value nbtValue) nbtValue {
	return nbtValue{Type: value.Type, Value: append(json.RawMessage(nil), value.Value...)}
}

func cloneTags(in []generatedTagRegistry) []generatedTagRegistry {
	out := make([]generatedTagRegistry, 0, len(in))
	for _, registry := range in {
		tags := make([]generatedTag, 0, len(registry.Tags))
		for _, tag := range registry.Tags {
			tags = append(tags, generatedTag{
				Key:    tag.Key,
				Values: append([]int32(nil), tag.Values...),
			})
		}
		out = append(out, generatedTagRegistry{
			ID:   registry.ID,
			Tags: tags,
		})
	}
	return out
}

func releaseType(value string) string {
	if value == "" {
		return "release"
	}
	return strings.ToLower(value)
}

func releaseTypeFor(version versionJSON, releaseTypes map[string]string) string {
	if releaseType, ok := releaseTypes[version.MinecraftVersion+"/"+strconv.Itoa(int(version.Version))]; ok {
		return releaseType
	}
	if releaseType, ok := releaseTypes[version.MinecraftVersion]; ok {
		return releaseType
	}
	return releaseType(version.ReleaseType)
}

func isModernJava(majorVersion string) bool {
	return strings.HasPrefix(majorVersion, "1.") || strings.HasPrefix(majorVersion, "26.")
}

func isReleaseName(minecraftVersion string) bool {
	if strings.Contains(minecraftVersion, "-pre") || strings.Contains(minecraftVersion, "-rc") {
		return false
	}
	for _, part := range strings.Split(minecraftVersion, ".") {
		if strings.Contains(part, "w") {
			return false
		}
	}
	return true
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "mcregistry-gen: "+format+"\n", args...)
	os.Exit(1)
}
