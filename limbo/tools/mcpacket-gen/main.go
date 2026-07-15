package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type versionJSON struct {
	Version          int32  `json:"version"`
	MinecraftVersion string `json:"minecraftVersion"`
	MajorVersion     string `json:"majorVersion"`
	ReleaseType      string `json:"releaseType"`
}

type versionPackets struct {
	MinecraftVersion string
	Protocol         int32
	Entries          []packetEntry
}

type packetEntry struct {
	State     string
	Direction string
	Name      string
	ID        int32
}

func main() {
	var pcDataDir string
	var outPath string
	flag.StringVar(&pcDataDir, "pc-data", "", "path to minecraft-data/data/pc")
	flag.StringVar(&outPath, "out", "", "output Go file")
	flag.Parse()

	if pcDataDir == "" || outPath == "" {
		fatalf("-pc-data and -out are required")
	}

	versions, err := readPacketVersions(pcDataDir)
	if err != nil {
		fatalf("%v", err)
	}
	source, err := render(versions)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(outPath, source, 0o644); err != nil {
		fatalf("write %s: %v", outPath, err)
	}
}

func readPacketVersions(pcDataDir string) ([]versionPackets, error) {
	dirs, err := os.ReadDir(pcDataDir)
	if err != nil {
		return nil, fmt.Errorf("read pc data dir: %w", err)
	}

	var versions []versionPackets
	seenProtocol := map[int32]struct{}{}
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Name() == "common" || dir.Name() == "latest" {
			continue
		}
		version, err := readVersion(filepath.Join(pcDataDir, dir.Name(), "version.json"))
		if err != nil {
			continue
		}
		if version.Version <= 0 || version.MinecraftVersion == "" || !isModernJava(version.MajorVersion) || !isReleaseName(version.MinecraftVersion) {
			continue
		}
		if _, ok := seenProtocol[version.Version]; ok {
			continue
		}
		protocolPath := filepath.Join(pcDataDir, dir.Name(), "protocol.json")
		if _, err := os.Stat(protocolPath); err != nil {
			continue
		}

		entries, err := readPacketEntries(protocolPath)
		if err != nil {
			return nil, err
		}
		versions = append(versions, versionPackets{
			MinecraftVersion: version.MinecraftVersion,
			Protocol:         version.Version,
			Entries:          entries,
		})
		seenProtocol[version.Version] = struct{}{}
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Protocol < versions[j].Protocol
	})
	if len(versions) == 0 {
		return nil, fmt.Errorf("no protocol mappings found in %s", pcDataDir)
	}
	return versions, nil
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

func readPacketEntries(path string) ([]packetEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var protocol map[string]any
	if err := json.Unmarshal(data, &protocol); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var entries []packetEntry
	for _, state := range []string{"login", "configuration", "play"} {
		stateValue, ok := protocol[state].(map[string]any)
		if !ok {
			continue
		}
		for _, direction := range []string{"toClient", "toServer"} {
			directionValue, ok := stateValue[direction].(map[string]any)
			if !ok {
				continue
			}
			mappings, ok := packetMappings(directionValue)
			if !ok {
				continue
			}
			for rawID, nameValue := range mappings {
				name, ok := nameValue.(string)
				if !ok {
					continue
				}
				id, err := strconv.ParseInt(strings.TrimPrefix(rawID, "0x"), 16, 32)
				if err != nil {
					return nil, fmt.Errorf("parse packet id %q in %s: %w", rawID, path, err)
				}
				entries = append(entries, packetEntry{
					State:     state,
					Direction: direction,
					Name:      name,
					ID:        int32(id),
				})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].State != entries[j].State {
			return entries[i].State < entries[j].State
		}
		if entries[i].Direction != entries[j].Direction {
			return entries[i].Direction < entries[j].Direction
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func packetMappings(direction map[string]any) (map[string]any, bool) {
	types, ok := direction["types"].(map[string]any)
	if !ok {
		return nil, false
	}
	packet, ok := types["packet"].([]any)
	if !ok || len(packet) < 2 {
		return nil, false
	}
	fields, ok := packet[1].([]any)
	if !ok || len(fields) == 0 {
		return nil, false
	}
	nameField, ok := fields[0].(map[string]any)
	if !ok {
		return nil, false
	}
	fieldType, ok := nameField["type"].([]any)
	if !ok || len(fieldType) < 2 {
		return nil, false
	}
	mapper, ok := fieldType[1].(map[string]any)
	if !ok {
		return nil, false
	}
	mappings, ok := mapper["mappings"].(map[string]any)
	return mappings, ok
}

func render(versions []versionPackets) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by tools/mcpacket-gen; DO NOT EDIT.\n")
	buf.WriteString("package packetid\n\n")
	buf.WriteString("var byProtocol = map[int32]VersionPackets{\n")
	for _, version := range versions {
		fmt.Fprintf(&buf, "\t%d: {MinecraftVersion: %q, Protocol: %d, Entries: []Entry{\n", version.Protocol, version.MinecraftVersion, version.Protocol)
		for _, entry := range version.Entries {
			fmt.Fprintf(&buf, "\t\t{State: %q, Direction: %q, Name: %q, ID: %d},\n", entry.State, entry.Direction, entry.Name, entry.ID)
		}
		buf.WriteString("\t}},\n")
	}
	buf.WriteString("}\n")

	source, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return source, nil
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
	_, _ = fmt.Fprintf(os.Stderr, "mcpacket-gen: "+format+"\n", args...)
	os.Exit(1)
}
