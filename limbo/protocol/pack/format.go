// Package pack loads immutable, hot-swappable Minecraft protocol packs.
package pack

const FormatVersion = 1

// Manifest describes every protocol layout available in one pack.
type Manifest struct {
	FormatVersion int                  `json:"format_version"`
	Name          string               `json:"name"`
	Version       string               `json:"version"`
	Protocols     []ProtocolDescriptor `json:"protocols"`
}

// ProtocolDescriptor binds a Minecraft protocol number to generated data and
// one of the codec layouts implemented by the Limbo runtime.
type ProtocolDescriptor struct {
	Protocol             int32    `json:"protocol"`
	MinecraftVersions    []string `json:"minecraft_versions"`
	PacketIDProtocol     int32    `json:"packet_id_protocol,omitempty"`
	DataProtocol         int32    `json:"data_protocol,omitempty"`
	RegistryDataProtocol int32    `json:"registry_data_protocol,omitempty"`
	BlockStateProtocol   int32    `json:"block_state_protocol,omitempty"`
	Layout               Layout   `json:"layout"`
}

// Layout is the declarative protocol codec instruction set currently
// understood by Limbo. The loader validates combinations before compiling
// them into the existing allocation-free Go codec paths.
type Layout struct {
	LoginStartSignature            bool   `json:"login_start_signature,omitempty"`
	LoginStartUUID                 string `json:"login_start_uuid,omitempty"`
	EncryptionRequestAuthenticate  bool   `json:"encryption_request_authenticate,omitempty"`
	EncryptionResponseVerifyFlag   bool   `json:"encryption_response_verify_flag,omitempty"`
	PreConfiguration               bool   `json:"pre_configuration,omitempty"`
	PreConfigurationDimensionNBT   bool   `json:"pre_configuration_dimension_nbt,omitempty"`
	PreConfigurationDeath          bool   `json:"pre_configuration_death,omitempty"`
	PreConfigurationPortalCooldown bool   `json:"pre_configuration_portal_cooldown,omitempty"`
	PositionDismountVehicle        bool   `json:"position_dismount_vehicle,omitempty"`
	LoginSuccessNoProperties       bool   `json:"login_success_no_properties,omitempty"`
	StrictErrorHandling            bool   `json:"strict_error_handling,omitempty"`
	RegistryCodecNBT               bool   `json:"registry_codec_nbt,omitempty"`
	LegacyPlayLogin                bool   `json:"legacy_play_login,omitempty"`
	PositionV2                     bool   `json:"position_v2,omitempty"`
	SpawnInfoSeaLevel              bool   `json:"spawn_info_sea_level,omitempty"`
	PositionFlagsU32               bool   `json:"position_flags_u32,omitempty"`
	ChunkHeightmapArray            bool   `json:"chunk_heightmap_array,omitempty"`
	ChunkHeightmapFullNBT          bool   `json:"chunk_heightmap_full_nbt,omitempty"`
	ChunkTrustEdges                bool   `json:"chunk_trust_edges,omitempty"`
	ChunkSectionFluidCount         bool   `json:"chunk_section_fluid_count,omitempty"`
	ChunkFixedPalettedStorage      bool   `json:"chunk_fixed_paletted_storage,omitempty"`
	ComponentPayloadNBT            bool   `json:"component_payload_nbt,omitempty"`
	ComponentLegacySchema          bool   `json:"component_legacy_schema,omitempty"`
	ComponentChangePageString      bool   `json:"component_change_page_string,omitempty"`
	ComponentHoverEntityIDIntArray bool   `json:"component_hover_entity_id_int_array,omitempty"`
	ComponentHoverEntityTypeKey    bool   `json:"component_hover_entity_type_key,omitempty"`
	ComponentDefaultItemQuantity   bool   `json:"component_default_item_quantity,omitempty"`
	SystemChatOverlayVarInt        bool   `json:"system_chat_overlay_varint,omitempty"`
	LegacyChatSenderUUID           bool   `json:"legacy_chat_sender_uuid,omitempty"`
	ResourcePackRequiredPrompt     bool   `json:"resource_pack_required_prompt,omitempty"`
	PortalDialog                   bool   `json:"portal_dialog,omitempty"`
}

// Metadata is safe to expose through Core and node heartbeat APIs.
type Metadata struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	SHA256            string   `json:"sha256"`
	Protocols         []int32  `json:"protocols"`
	MinecraftVersions []string `json:"minecraft_versions"`
}
