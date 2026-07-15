package limbo

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/RoselleMC/authman/limbo/protocol/pack"
)

//go:embed modern_protocols.json
var rawModernProtocolConfigs []byte

type modernProtocolConfigRecord struct {
	PacketIDProtocol               int32  `json:"packet_id_protocol"`
	DataProtocol                   int32  `json:"data_protocol"`
	RegistryDataProtocol           int32  `json:"registry_data_protocol"`
	BlockStateProtocol             int32  `json:"block_state_protocol"`
	LoginStartSignature            bool   `json:"login_start_signature"`
	LoginStartUUID                 string `json:"login_start_uuid"`
	EncryptionRequestAuthenticate  bool   `json:"encryption_request_authenticate"`
	EncryptionResponseVerifyFlag   bool   `json:"encryption_response_verify_flag"`
	PreConfiguration               bool   `json:"pre_configuration"`
	PreConfigurationDimensionNBT   bool   `json:"pre_configuration_dimension_nbt"`
	PreConfigurationDeath          bool   `json:"pre_configuration_death"`
	PreConfigurationPortalCooldown bool   `json:"pre_configuration_portal_cooldown"`
	PositionDismountVehicle        bool   `json:"position_dismount_vehicle"`
	LoginSuccessNoProperties       bool   `json:"login_success_no_properties"`
	StrictErrorHandling            bool   `json:"strict_error_handling"`
	RegistryCodecNBT               bool   `json:"registry_codec_nbt"`
	LegacyPlayLogin                bool   `json:"legacy_play_login"`
	PositionV2                     bool   `json:"position_v2"`
	SpawnInfoSeaLevel              bool   `json:"spawn_info_sea_level"`
	PositionFlagsU32               bool   `json:"position_flags_u32"`
	ChunkHeightmapArray            bool   `json:"chunk_heightmap_array"`
	ChunkHeightmapFullNBT          bool   `json:"chunk_heightmap_full_nbt"`
	ChunkTrustEdges                bool   `json:"chunk_trust_edges"`
	ChunkSectionFluidCount         bool   `json:"chunk_section_fluid_count"`
	ChunkFixedPalettedStorage      bool   `json:"chunk_fixed_paletted_storage"`
	ComponentPayloadNBT            bool   `json:"component_payload_nbt"`
	ComponentLegacySchema          bool   `json:"component_legacy_schema"`
	ComponentChangePageString      bool   `json:"component_change_page_string"`
	ComponentHoverEntityIDIntArray bool   `json:"component_hover_entity_id_int_array"`
	ComponentHoverEntityTypeKey    bool   `json:"component_hover_entity_type_key"`
	ComponentDefaultItemQuantity   bool   `json:"component_default_item_quantity"`
	SystemChatOverlayVarInt        bool   `json:"system_chat_overlay_varint"`
	LegacyChatSenderUUID           bool   `json:"legacy_chat_sender_uuid"`
	ResourcePackRequiredPrompt     bool   `json:"resource_pack_required_prompt"`
	PortalDialog                   bool   `json:"portal_dialog"`
}

var (
	modernConfigOnce sync.Once
	modernConfigs    *ModernProtocols
	modernConfigErr  error
)

// ModernProtocols contains the version-specific switches for the modern
// login/configuration/play flow.
type ModernProtocols struct {
	configs map[int32]modernProtocolConfig
}

// DefaultModernProtocols returns the embedded generated/configured protocol
// compatibility table.
func DefaultModernProtocols() (*ModernProtocols, error) {
	modernConfigOnce.Do(func() {
		modernConfigs, modernConfigErr = loadModernProtocolConfigs(rawModernProtocolConfigs)
	})
	return modernConfigs, modernConfigErr
}

// LoadModernProtocolsFile reads a modern protocol compatibility table from a
// JSON file.
func LoadModernProtocolsFile(path string) (*ModernProtocols, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadModernProtocolsBytes(raw)
}

// LoadModernProtocolsBytes reads a modern protocol compatibility table from
// JSON bytes.
func LoadModernProtocolsBytes(raw []byte) (*ModernProtocols, error) {
	return loadModernProtocolConfigs(raw)
}

// ModernProtocolsFromPack compiles the compatibility layouts carried by one
// immutable protocol pack snapshot.
func ModernProtocolsFromPack(protocolPack *pack.Pack) (*ModernProtocols, error) {
	if protocolPack == nil {
		return nil, fmt.Errorf("protocol pack is nil")
	}
	manifest := protocolPack.Manifest()
	configs := make(map[int32]modernProtocolConfig, len(manifest.Protocols))
	for _, descriptor := range manifest.Protocols {
		configs[descriptor.Protocol] = modernProtocolConfigFromLayout(descriptor.Protocol, descriptor.PacketIDProtocol, descriptor.DataProtocol, descriptor.RegistryDataProtocol, descriptor.BlockStateProtocol, descriptor.Layout)
	}
	return &ModernProtocols{configs: configs}, nil
}

func (p *ModernProtocols) configFor(protocol int32) (modernProtocolConfig, bool) {
	if p == nil {
		return modernProtocolConfig{}, false
	}
	cfg, ok := p.configs[protocol]
	return cfg, ok
}

func (p *ModernProtocols) supportedProtocols() []int32 {
	if p == nil {
		return nil
	}
	out := make([]int32, 0, len(p.configs))
	for protocol := range p.configs {
		out = append(out, protocol)
	}
	return out
}

func loadModernProtocolConfigs(raw []byte) (*ModernProtocols, error) {
	var records map[string]modernProtocolConfigRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("parse modern protocol configs: %w", err)
	}
	configs := make(map[int32]modernProtocolConfig, len(records))
	for rawProtocol, record := range records {
		protocol, err := strconv.ParseInt(rawProtocol, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("parse protocol %q: %w", rawProtocol, err)
		}
		loginStartUUID := loginStartUUIDMode(record.LoginStartUUID)
		if err := validateLoginStartUUIDMode(loginStartUUID); err != nil {
			return nil, fmt.Errorf("parse protocol %d: %w", protocol, err)
		}
		configs[int32(protocol)] = modernProtocolConfigFromLayout(
			int32(protocol),
			protocolAliasOrSelf(record.PacketIDProtocol, int32(protocol)),
			protocolAliasOrSelf(record.DataProtocol, int32(protocol)),
			protocolAliasOrSelf(record.RegistryDataProtocol, protocolAliasOrSelf(record.DataProtocol, int32(protocol))),
			protocolAliasOrSelf(record.BlockStateProtocol, protocolAliasOrSelf(record.DataProtocol, int32(protocol))),
			pack.Layout{
				LoginStartSignature:            record.LoginStartSignature,
				LoginStartUUID:                 string(loginStartUUID),
				EncryptionRequestAuthenticate:  record.EncryptionRequestAuthenticate,
				EncryptionResponseVerifyFlag:   record.EncryptionResponseVerifyFlag,
				PreConfiguration:               record.PreConfiguration,
				PreConfigurationDimensionNBT:   record.PreConfigurationDimensionNBT,
				PreConfigurationDeath:          record.PreConfigurationDeath,
				PreConfigurationPortalCooldown: record.PreConfigurationPortalCooldown,
				PositionDismountVehicle:        record.PositionDismountVehicle,
				LoginSuccessNoProperties:       record.LoginSuccessNoProperties,
				StrictErrorHandling:            record.StrictErrorHandling,
				RegistryCodecNBT:               record.RegistryCodecNBT,
				LegacyPlayLogin:                record.LegacyPlayLogin,
				PositionV2:                     record.PositionV2,
				SpawnInfoSeaLevel:              record.SpawnInfoSeaLevel,
				PositionFlagsU32:               record.PositionFlagsU32,
				ChunkHeightmapArray:            record.ChunkHeightmapArray,
				ChunkHeightmapFullNBT:          record.ChunkHeightmapFullNBT,
				ChunkTrustEdges:                record.ChunkTrustEdges,
				ChunkSectionFluidCount:         record.ChunkSectionFluidCount,
				ChunkFixedPalettedStorage:      record.ChunkFixedPalettedStorage,
				ComponentPayloadNBT:            record.ComponentPayloadNBT,
				ComponentLegacySchema:          record.ComponentLegacySchema,
				ComponentChangePageString:      record.ComponentChangePageString,
				ComponentHoverEntityIDIntArray: record.ComponentHoverEntityIDIntArray,
				ComponentHoverEntityTypeKey:    record.ComponentHoverEntityTypeKey,
				ComponentDefaultItemQuantity:   record.ComponentDefaultItemQuantity,
				SystemChatOverlayVarInt:        record.SystemChatOverlayVarInt,
				LegacyChatSenderUUID:           record.LegacyChatSenderUUID,
				ResourcePackRequiredPrompt:     record.ResourcePackRequiredPrompt,
				PortalDialog:                   record.PortalDialog,
			},
		)
	}
	return &ModernProtocols{configs: configs}, nil
}

func modernProtocolConfigFromLayout(protocol, packetProtocol, dataProtocol, registryProtocol, blockProtocol int32, layout pack.Layout) modernProtocolConfig {
	return modernProtocolConfig{
		protocol:                       protocol,
		packetIDProtocol:               packetProtocol,
		dataProtocol:                   dataProtocol,
		registryDataProtocol:           registryProtocol,
		blockStateProtocol:             blockProtocol,
		loginStartSignature:            layout.LoginStartSignature,
		loginStartUUID:                 loginStartUUIDMode(layout.LoginStartUUID),
		encryptionRequestAuthenticate:  layout.EncryptionRequestAuthenticate,
		encryptionResponseVerifyFlag:   layout.EncryptionResponseVerifyFlag,
		preConfiguration:               layout.PreConfiguration,
		preConfigurationDimensionNBT:   layout.PreConfigurationDimensionNBT,
		preConfigurationDeath:          layout.PreConfigurationDeath,
		preConfigurationPortalCooldown: layout.PreConfigurationPortalCooldown,
		positionDismountVehicle:        layout.PositionDismountVehicle,
		loginSuccessNoProperties:       layout.LoginSuccessNoProperties,
		strictErrorHandling:            layout.StrictErrorHandling,
		registryCodecNBT:               layout.RegistryCodecNBT,
		legacyPlayLogin:                layout.LegacyPlayLogin,
		positionV2:                     layout.PositionV2,
		spawnInfoSeaLevel:              layout.SpawnInfoSeaLevel,
		positionFlagsU32:               layout.PositionFlagsU32,
		chunkHeightmapArray:            layout.ChunkHeightmapArray,
		chunkHeightmapFullNBT:          layout.ChunkHeightmapFullNBT,
		chunkTrustEdges:                layout.ChunkTrustEdges,
		chunkSectionFluidCount:         layout.ChunkSectionFluidCount,
		chunkFixedPalettedStorage:      layout.ChunkFixedPalettedStorage,
		componentPayloadNBT:            layout.ComponentPayloadNBT,
		componentLegacySchema:          layout.ComponentLegacySchema,
		componentChangePageString:      layout.ComponentChangePageString,
		componentHoverEntityIDIntArray: layout.ComponentHoverEntityIDIntArray,
		componentHoverEntityTypeKey:    layout.ComponentHoverEntityTypeKey,
		componentDefaultItemQuantity:   layout.ComponentDefaultItemQuantity,
		systemChatOverlayVarInt:        layout.SystemChatOverlayVarInt,
		legacyChatSenderUUID:           layout.LegacyChatSenderUUID,
		resourcePackRequiredPrompt:     layout.ResourcePackRequiredPrompt,
		portalDialog:                   layout.PortalDialog,
	}
}

func validateLoginStartUUIDMode(mode loginStartUUIDMode) error {
	switch mode {
	case loginStartUUIDNone, loginStartUUIDOptional, loginStartUUIDRequired:
		return nil
	default:
		return fmt.Errorf("unsupported login_start_uuid mode %q", mode)
	}
}

func protocolAliasOrSelf(alias, self int32) int32 {
	if alias != 0 {
		return alias
	}
	return self
}
