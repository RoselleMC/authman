package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/RoselleMC/authman/limbo/protocol/blockstate"
	"github.com/RoselleMC/authman/limbo/protocol/pack"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
	"github.com/RoselleMC/authman/limbo/protocol/versions"
)

type sourceRecord struct {
	PacketIDProtocol     int32 `json:"packet_id_protocol"`
	DataProtocol         int32 `json:"data_protocol"`
	RegistryDataProtocol int32 `json:"registry_data_protocol"`
	BlockStateProtocol   int32 `json:"block_state_protocol"`
	pack.Layout
}

func main() {
	modernPath := flag.String("modern", "", "modern protocol layout JSON")
	registryPath := flag.String("registry", "", "registry data ZIP")
	outputPath := flag.String("output", "", "output protocol pack ZIP")
	name := flag.String("name", "authman-default", "protocol pack name")
	version := flag.String("version", "builtin-1", "protocol pack version")
	flag.Parse()
	if *modernPath == "" || *registryPath == "" || *outputPath == "" {
		fatal(fmt.Errorf("-modern, -registry, and -output are required"))
	}

	records, err := loadRecords(*modernPath)
	if err != nil {
		fatal(err)
	}
	manifest, err := buildManifest(*name, *version, records)
	if err != nil {
		fatal(err)
	}
	registryData, err := os.ReadFile(*registryPath)
	if err != nil {
		fatal(fmt.Errorf("read registry data: %w", err))
	}
	files := map[string][]byte{
		"manifest.json":    marshalJSON(manifest),
		"packets.json":     marshalJSON(packetid.Export()),
		"blockstates.json": marshalJSON(blockstate.Export()),
		"registrydata.zip": registryData,
	}
	raw, err := writeZip(files)
	if err != nil {
		fatal(err)
	}
	loaded, err := pack.LoadZip(raw)
	if err != nil {
		fatal(fmt.Errorf("validate generated protocol pack: %w", err))
	}
	if err := os.WriteFile(*outputPath, raw, 0o644); err != nil {
		fatal(fmt.Errorf("write protocol pack: %w", err))
	}
	metadata := loaded.Metadata()
	fmt.Printf("wrote %s (%d bytes, %d protocols, sha256=%s)\n", *outputPath, len(raw), len(metadata.Protocols), metadata.SHA256)
}

func loadRecords(path string) (map[string]sourceRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read modern protocol layouts: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var records map[string]sourceRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("parse modern protocol layouts: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return records, nil
}

func buildManifest(name, version string, records map[string]sourceRecord) (pack.Manifest, error) {
	protocols := make([]int, 0, len(records))
	for rawProtocol := range records {
		protocol, err := strconv.Atoi(rawProtocol)
		if err != nil || protocol <= 0 {
			return pack.Manifest{}, fmt.Errorf("invalid modern protocol key %q", rawProtocol)
		}
		protocols = append(protocols, protocol)
	}
	sort.Ints(protocols)
	manifest := pack.Manifest{FormatVersion: pack.FormatVersion, Name: name, Version: version}
	for _, rawProtocol := range protocols {
		protocol := int32(rawProtocol)
		record := records[strconv.Itoa(rawProtocol)]
		descriptor := pack.ProtocolDescriptor{
			Protocol:             protocol,
			PacketIDProtocol:     alias(record.PacketIDProtocol, protocol),
			DataProtocol:         alias(record.DataProtocol, protocol),
			RegistryDataProtocol: alias(record.RegistryDataProtocol, alias(record.DataProtocol, protocol)),
			BlockStateProtocol:   alias(record.BlockStateProtocol, alias(record.DataProtocol, protocol)),
			Layout:               record.Layout,
		}
		for _, release := range versions.LookupProtocol(protocol) {
			if release.ReleaseType == "" || release.ReleaseType == "release" {
				descriptor.MinecraftVersions = append(descriptor.MinecraftVersions, release.MinecraftVersion)
			}
		}
		if len(descriptor.MinecraftVersions) == 0 {
			return pack.Manifest{}, fmt.Errorf("protocol %d has no release metadata", protocol)
		}
		manifest.Protocols = append(manifest.Protocols, descriptor)
	}
	return manifest, nil
}

func alias(value, fallback int32) int32 {
	if value != 0 {
		return value
	}
	return fallback
}

func marshalJSON(value any) []byte {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err)
	}
	return append(raw, '\n')
}

func writeZip(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", name, err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func requireEOF(decoder *json.Decoder) error {
	var value any
	err := decoder.Decode(&value)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return err
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
