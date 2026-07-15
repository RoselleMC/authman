package pack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/RoselleMC/authman/limbo/protocol/blockstate"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
	"github.com/RoselleMC/authman/limbo/protocol/registrydata"
)

const (
	MaxArchiveBytes      = 32 << 20
	maxArchiveFiles      = 8
	maxArchiveEntryBytes = 64 << 20
	maxExpandedBytes     = 128 << 20
)

var requiredFiles = map[string]struct{}{
	"manifest.json":    {},
	"packets.json":     {},
	"blockstates.json": {},
	"registrydata.zip": {},
}

// Pack is a fully validated immutable protocol snapshot.
type Pack struct {
	manifest    Manifest
	protocols   map[int32]ProtocolDescriptor
	packetIDs   *packetid.Table
	blockStates *blockstate.Table
	registries  *registrydata.Data
	sha256      string
}

// LoadZip validates and compiles a protocol pack. No state is changed when
// validation fails.
func LoadZip(raw []byte) (*Pack, error) {
	files, err := readArchive(raw)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := decodeStrict(files["manifest.json"], &manifest); err != nil {
		return nil, fmt.Errorf("parse protocol pack manifest: %w", err)
	}
	var packets []packetid.VersionPackets
	if err := decodeStrict(files["packets.json"], &packets); err != nil {
		return nil, fmt.Errorf("parse protocol packet table: %w", err)
	}
	var blocks map[int32]map[string]uint32
	if err := decodeStrict(files["blockstates.json"], &blocks); err != nil {
		return nil, fmt.Errorf("parse protocol block-state table: %w", err)
	}
	packetIDs, err := packetid.NewTable(packets)
	if err != nil {
		return nil, err
	}
	blockStates, err := blockstate.NewTable(blocks)
	if err != nil {
		return nil, err
	}
	registries, err := registrydata.LoadZipBytes(files["registrydata.zip"])
	if err != nil {
		return nil, fmt.Errorf("parse protocol registry data: %w", err)
	}
	protocols, err := validateManifest(&manifest, packetIDs, blockStates, registries)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &Pack{
		manifest:    manifest,
		protocols:   protocols,
		packetIDs:   packetIDs,
		blockStates: blockStates,
		registries:  registries,
		sha256:      hex.EncodeToString(sum[:]),
	}, nil
}

func readArchive(raw []byte) (map[string][]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("protocol pack is empty")
	}
	if len(raw) > MaxArchiveBytes {
		return nil, fmt.Errorf("protocol pack is %d bytes; maximum is %d", len(raw), MaxArchiveBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open protocol pack zip: %w", err)
	}
	if len(reader.File) > maxArchiveFiles {
		return nil, fmt.Errorf("protocol pack has %d entries; maximum is %d", len(reader.File), maxArchiveFiles)
	}
	files := make(map[string][]byte, len(requiredFiles))
	var expanded uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("protocol pack contains unsafe entry %q", file.Name)
		}
		if _, allowed := requiredFiles[name]; !allowed {
			return nil, fmt.Errorf("protocol pack contains unsupported entry %q", name)
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("protocol pack contains duplicate entry %q", name)
		}
		if file.UncompressedSize64 > maxArchiveEntryBytes {
			return nil, fmt.Errorf("protocol pack entry %q expands to %d bytes; maximum is %d", name, file.UncompressedSize64, maxArchiveEntryBytes)
		}
		expanded += file.UncompressedSize64
		if expanded > maxExpandedBytes {
			return nil, fmt.Errorf("protocol pack expands to more than %d bytes", maxExpandedBytes)
		}
		handle, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open protocol pack entry %q: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(handle, maxArchiveEntryBytes+1))
		closeErr := handle.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read protocol pack entry %q: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close protocol pack entry %q: %w", name, closeErr)
		}
		if len(data) > maxArchiveEntryBytes {
			return nil, fmt.Errorf("protocol pack entry %q exceeds %d bytes", name, maxArchiveEntryBytes)
		}
		files[name] = data
	}
	for name := range requiredFiles {
		if _, ok := files[name]; !ok {
			return nil, fmt.Errorf("protocol pack is missing %q", name)
		}
	}
	return files, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing JSON value")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func validateManifest(manifest *Manifest, packets *packetid.Table, blocks *blockstate.Table, registries *registrydata.Data) (map[int32]ProtocolDescriptor, error) {
	if manifest.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported protocol pack format %d", manifest.FormatVersion)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Name == "" || len(manifest.Name) > 80 {
		return nil, fmt.Errorf("protocol pack name must contain 1-80 characters")
	}
	if manifest.Version == "" || len(manifest.Version) > 80 {
		return nil, fmt.Errorf("protocol pack version must contain 1-80 characters")
	}
	if len(manifest.Protocols) == 0 {
		return nil, fmt.Errorf("protocol pack has no protocol descriptors")
	}
	blockProtocols := make(map[int32]struct{})
	for _, protocol := range blocks.Protocols() {
		blockProtocols[protocol] = struct{}{}
	}
	protocols := make(map[int32]ProtocolDescriptor, len(manifest.Protocols))
	versions := make(map[string]int32)
	for i := range manifest.Protocols {
		descriptor := &manifest.Protocols[i]
		if descriptor.Protocol <= 0 {
			return nil, fmt.Errorf("protocol descriptor has invalid protocol %d", descriptor.Protocol)
		}
		if _, exists := protocols[descriptor.Protocol]; exists {
			return nil, fmt.Errorf("protocol pack has duplicate descriptor %d", descriptor.Protocol)
		}
		normalizeAliases(descriptor)
		if len(descriptor.MinecraftVersions) == 0 {
			return nil, fmt.Errorf("protocol %d has no Minecraft release names", descriptor.Protocol)
		}
		seenNames := make(map[string]struct{}, len(descriptor.MinecraftVersions))
		for versionIndex, version := range descriptor.MinecraftVersions {
			version = strings.TrimSpace(version)
			if !validVersionName(version) {
				return nil, fmt.Errorf("protocol %d has invalid Minecraft version %q", descriptor.Protocol, version)
			}
			if _, duplicate := seenNames[version]; duplicate {
				return nil, fmt.Errorf("protocol %d repeats Minecraft version %q", descriptor.Protocol, version)
			}
			if other, duplicate := versions[version]; duplicate {
				return nil, fmt.Errorf("Minecraft version %q is assigned to protocols %d and %d", version, other, descriptor.Protocol)
			}
			descriptor.MinecraftVersions[versionIndex] = version
			seenNames[version] = struct{}{}
			versions[version] = descriptor.Protocol
		}
		if _, ok := packets.Lookup(descriptor.PacketIDProtocol); !ok {
			return nil, fmt.Errorf("protocol %d references missing packet table %d", descriptor.Protocol, descriptor.PacketIDProtocol)
		}
		if _, ok := blockProtocols[descriptor.BlockStateProtocol]; !ok {
			return nil, fmt.Errorf("protocol %d references missing block-state table %d", descriptor.Protocol, descriptor.BlockStateProtocol)
		}
		if descriptor.Layout.PreConfiguration || descriptor.Layout.RegistryCodecNBT {
			if codec, ok := registries.DimensionCodec(descriptor.RegistryDataProtocol); !ok || len(codec) == 0 {
				return nil, fmt.Errorf("protocol %d references missing dimension codec %d", descriptor.Protocol, descriptor.RegistryDataProtocol)
			}
			if descriptor.Layout.PreConfigurationDimensionNBT {
				if dimension, ok := registries.Dimension(descriptor.RegistryDataProtocol); !ok || len(dimension) == 0 {
					return nil, fmt.Errorf("protocol %d references missing dimension data %d", descriptor.Protocol, descriptor.RegistryDataProtocol)
				}
			}
		} else {
			if registryList, ok := registries.Registries(descriptor.RegistryDataProtocol); !ok || len(registryList) == 0 {
				return nil, fmt.Errorf("protocol %d references missing registry data %d", descriptor.Protocol, descriptor.RegistryDataProtocol)
			}
			if tagList, ok := registries.Tags(descriptor.RegistryDataProtocol); !ok || len(tagList) == 0 {
				return nil, fmt.Errorf("protocol %d references missing registry tags %d", descriptor.Protocol, descriptor.RegistryDataProtocol)
			}
		}
		if err := validateLayout(descriptor.Protocol, descriptor.Layout); err != nil {
			return nil, err
		}
		if err := validateRequiredPackets(*descriptor, packets); err != nil {
			return nil, err
		}
		protocols[descriptor.Protocol] = *descriptor
	}
	sort.Slice(manifest.Protocols, func(i, j int) bool {
		return manifest.Protocols[i].Protocol < manifest.Protocols[j].Protocol
	})
	return protocols, nil
}

func normalizeAliases(descriptor *ProtocolDescriptor) {
	if descriptor.PacketIDProtocol == 0 {
		descriptor.PacketIDProtocol = descriptor.Protocol
	}
	if descriptor.DataProtocol == 0 {
		descriptor.DataProtocol = descriptor.Protocol
	}
	if descriptor.RegistryDataProtocol == 0 {
		descriptor.RegistryDataProtocol = descriptor.DataProtocol
	}
	if descriptor.BlockStateProtocol == 0 {
		descriptor.BlockStateProtocol = descriptor.DataProtocol
	}
}

func validVersionName(value string) bool {
	if value == "" || len(value) > 40 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '-', '_', '+':
			continue
		default:
			return false
		}
	}
	return true
}

func validateLayout(protocol int32, layout Layout) error {
	switch layout.LoginStartUUID {
	case "", "optional", "required":
	default:
		return fmt.Errorf("protocol %d has invalid login_start_uuid layout %q", protocol, layout.LoginStartUUID)
	}
	if !layout.PreConfiguration && (layout.PreConfigurationDimensionNBT || layout.PreConfigurationDeath || layout.PreConfigurationPortalCooldown) {
		return fmt.Errorf("protocol %d enables a pre-configuration field without pre_configuration", protocol)
	}
	if layout.PreConfiguration && layout.LegacyPlayLogin {
		return fmt.Errorf("protocol %d cannot combine pre_configuration and legacy_play_login", protocol)
	}
	if layout.ChunkHeightmapArray && layout.ChunkHeightmapFullNBT {
		return fmt.Errorf("protocol %d selects two chunk heightmap layouts", protocol)
	}
	if layout.PositionFlagsU32 && !layout.PositionV2 {
		return fmt.Errorf("protocol %d requires position_v2 when position_flags_u32 is enabled", protocol)
	}
	if layout.ChunkFixedPalettedStorage && !layout.ChunkHeightmapArray {
		return fmt.Errorf("protocol %d fixed paletted storage requires the array heightmap layout", protocol)
	}
	if layout.EncryptionResponseVerifyFlag && !layout.LoginStartSignature {
		return fmt.Errorf("protocol %d encryption response verify flag requires login_start_signature", protocol)
	}
	if layout.PortalDialog {
		if !layout.ComponentPayloadNBT {
			return fmt.Errorf("protocol %d portal dialog requires NBT component payloads", protocol)
		}
		if layout.PreConfiguration || layout.LegacyPlayLogin {
			return fmt.Errorf("protocol %d portal dialog requires the modern configuration and play layouts", protocol)
		}
	}
	return nil
}

type requiredPacket struct {
	state     packetid.State
	direction packetid.Direction
	name      string
}

func validateRequiredPackets(descriptor ProtocolDescriptor, packets *packetid.Table) error {
	required := []requiredPacket{
		{packetid.StateLogin, packetid.ToServer, "login_start"},
		{packetid.StateLogin, packetid.ToServer, "encryption_begin"},
		{packetid.StateLogin, packetid.ToClient, "disconnect"},
		{packetid.StateLogin, packetid.ToClient, "encryption_begin"},
		{packetid.StateLogin, packetid.ToClient, "success"},
		{packetid.StatePlay, packetid.ToClient, "login"},
		{packetid.StatePlay, packetid.ToClient, "position"},
		{packetid.StatePlay, packetid.ToClient, "map_chunk"},
		{packetid.StatePlay, packetid.ToClient, "keep_alive"},
		{packetid.StatePlay, packetid.ToServer, "keep_alive"},
		{packetid.StatePlay, packetid.ToClient, "kick_disconnect"},
	}
	if !descriptor.Layout.PreConfiguration {
		required = append(required,
			requiredPacket{packetid.StateLogin, packetid.ToServer, "login_acknowledged"},
			requiredPacket{packetid.StateConfiguration, packetid.ToClient, "disconnect"},
			requiredPacket{packetid.StateConfiguration, packetid.ToClient, "finish_configuration"},
			requiredPacket{packetid.StateConfiguration, packetid.ToClient, "registry_data"},
			requiredPacket{packetid.StateConfiguration, packetid.ToClient, "tags"},
			requiredPacket{packetid.StateConfiguration, packetid.ToServer, "finish_configuration"},
		)
	}
	if descriptor.Layout.PortalDialog {
		required = append(required,
			requiredPacket{packetid.StatePlay, packetid.ToClient, "show_dialog"},
			requiredPacket{packetid.StatePlay, packetid.ToClient, "clear_dialog"},
			requiredPacket{packetid.StatePlay, packetid.ToClient, "store_cookie"},
			requiredPacket{packetid.StatePlay, packetid.ToClient, "transfer"},
			requiredPacket{packetid.StatePlay, packetid.ToServer, "custom_click_action"},
		)
	}
	for _, packet := range required {
		if _, ok := packets.ID(descriptor.PacketIDProtocol, packet.state, packet.direction, packet.name); !ok {
			return fmt.Errorf("protocol %d packet table %d is missing required %s/%s packet %q", descriptor.Protocol, descriptor.PacketIDProtocol, packet.state, packet.direction, packet.name)
		}
	}
	return nil
}

// Manifest returns a copy of the pack manifest.
func (p *Pack) Manifest() Manifest {
	if p == nil {
		return Manifest{}
	}
	out := p.manifest
	out.Protocols = append([]ProtocolDescriptor(nil), p.manifest.Protocols...)
	for i := range out.Protocols {
		out.Protocols[i].MinecraftVersions = append([]string(nil), out.Protocols[i].MinecraftVersions...)
	}
	return out
}

// Protocol returns one compiled descriptor.
func (p *Pack) Protocol(protocol int32) (ProtocolDescriptor, bool) {
	if p == nil {
		return ProtocolDescriptor{}, false
	}
	descriptor, ok := p.protocols[protocol]
	if !ok {
		return ProtocolDescriptor{}, false
	}
	descriptor.MinecraftVersions = append([]string(nil), descriptor.MinecraftVersions...)
	return descriptor, true
}

func (p *Pack) PacketIDs() *packetid.Table { return p.packetIDs }

func (p *Pack) BlockStates() *blockstate.Table { return p.blockStates }

func (p *Pack) RegistryData() *registrydata.Data { return p.registries }

// Metadata summarizes the active immutable snapshot.
func (p *Pack) Metadata() Metadata {
	if p == nil {
		return Metadata{}
	}
	metadata := Metadata{Name: p.manifest.Name, Version: p.manifest.Version, SHA256: p.sha256}
	for _, descriptor := range p.manifest.Protocols {
		metadata.Protocols = append(metadata.Protocols, descriptor.Protocol)
		metadata.MinecraftVersions = append(metadata.MinecraftVersions, descriptor.MinecraftVersions...)
	}
	return metadata
}
