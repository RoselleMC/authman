package limbgo

import "context"

// WorldProvider resolves worlds by ID. Deployments can use one schematic-backed
// world, while API users can route players to different preloaded worlds.
type WorldProvider interface {
	World(ctx context.Context, id string) (World, error)
}

// World is the protocol-facing view of a precomputed limbo map.
type World interface {
	ID() string
	Dimension() Dimension
	BlockPalette() []BlockState
	Chunk(x int32, z int32) (Chunk, bool)
}

// Dimension contains only the properties that matter to a limbo client's
// login, rendering, and chunk view. Protocol adapters fill dimension_type
// gameplay-only fields from internal vanilla-like presets.
type Dimension struct {
	Name            string
	Environment     DimensionEnvironment
	MinY            int32
	Height          int32
	LogicalHeight   int32
	Natural         bool
	HasSkylight     bool
	HasCeiling      bool
	UltraWarm       bool
	AmbientLight    float32
	FixedTime       *int64
	TimeOfDay       *int64
	WorldAge        int64
	CoordinateScale float64
	Effects         string
}

// DimensionEnvironment selects the vanilla visual/behavior preset for a limbo
// world.
type DimensionEnvironment string

const (
	DimensionOverworld DimensionEnvironment = "overworld"
	DimensionNether    DimensionEnvironment = "nether"
	DimensionEnd       DimensionEnvironment = "end"
)

// DimensionPreset returns vanilla-like dimension defaults for a limbo world.
// If height is positive, it overrides the preset height and logical height.
func DimensionPreset(environment DimensionEnvironment, height int32) Dimension {
	height = normalizeDimensionHeight(height)
	switch environment {
	case DimensionNether:
		fixedTime := int64(18000)
		return Dimension{
			Name:            "minecraft:the_nether",
			Environment:     DimensionNether,
			MinY:            0,
			Height:          fallbackHeight(height, 256),
			LogicalHeight:   128,
			Natural:         false,
			HasSkylight:     false,
			HasCeiling:      true,
			UltraWarm:       true,
			AmbientLight:    0.1,
			FixedTime:       &fixedTime,
			CoordinateScale: 8,
			Effects:         "minecraft:the_nether",
		}
	case DimensionEnd:
		fixedTime := int64(6000)
		resolvedHeight := fallbackHeight(height, 256)
		return Dimension{
			Name:            "minecraft:the_end",
			Environment:     DimensionEnd,
			MinY:            0,
			Height:          resolvedHeight,
			LogicalHeight:   resolvedHeight,
			Natural:         false,
			HasSkylight:     false,
			HasCeiling:      false,
			UltraWarm:       false,
			AmbientLight:    0,
			FixedTime:       &fixedTime,
			CoordinateScale: 1,
			Effects:         "minecraft:the_end",
		}
	default:
		resolvedHeight := fallbackHeight(height, 256)
		return Dimension{
			Name:            "minecraft:overworld",
			Environment:     DimensionOverworld,
			MinY:            0,
			Height:          resolvedHeight,
			LogicalHeight:   resolvedHeight,
			Natural:         true,
			HasSkylight:     true,
			HasCeiling:      false,
			UltraWarm:       false,
			AmbientLight:    0,
			CoordinateScale: 1,
			Effects:         "minecraft:overworld",
		}
	}
}

// NormalizeDimension fills protocol-required zero-value fields while preserving
// explicit values that have already been set by API callers.
func NormalizeDimension(d Dimension, schematicHeight int32) Dimension {
	if d.Environment == "" {
		d.Environment = inferDimensionEnvironment(d.Name)
	}
	if d.Name == "" {
		return DimensionPreset(d.Environment, schematicHeight)
	}
	if d.Height == 0 {
		d.Height = normalizeDimensionHeight(schematicHeight)
	}
	if d.Height == 0 {
		d.Height = 256
	}
	if d.LogicalHeight == 0 {
		d.LogicalHeight = d.Height
	}
	if d.CoordinateScale == 0 {
		d.CoordinateScale = 1
	}
	if d.Effects == "" {
		preset := DimensionPreset(d.Environment, d.Height)
		d.Effects = preset.Effects
	}
	return d
}

func inferDimensionEnvironment(name string) DimensionEnvironment {
	switch name {
	case "minecraft:the_nether":
		return DimensionNether
	case "minecraft:the_end":
		return DimensionEnd
	default:
		return DimensionOverworld
	}
}

func normalizeDimensionHeight(height int32) int32 {
	if height <= 0 {
		return 0
	}
	if remainder := height % 16; remainder != 0 {
		height += 16 - remainder
	}
	return height
}

func fallbackHeight(height, fallback int32) int32 {
	if height > 0 {
		return height
	}
	return fallback
}

// Chunk is intentionally version-neutral. Protocol adapters translate it into
// the client-specific chunk packet shape.
type Chunk struct {
	X        int32
	Z        int32
	MinY     int32
	Sections []ChunkSection
}

// ChunkSection stores palette IDs in a compact, version-neutral form.
type ChunkSection struct {
	Y             int32
	BlockStateIDs []uint32
	BiomeIDs      []uint32
}

// BlockState is a version-neutral Minecraft block state.
type BlockState struct {
	Name       string
	Properties map[string]string
}

const (
	DefaultWorldID  = "default"
	DefaultBedrockY = int32(64)
)

// DefaultWorld returns a minimal one-block bedrock world for limbo deployments
// that do not provide a schematic.
func DefaultWorld(id string) *MemoryWorld {
	return DefaultWorldWithDimension(id, Dimension{})
}

// DefaultWorldWithDimension returns the built-in one-block bedrock world using
// the provided dimension settings.
func DefaultWorldWithDimension(id string, dimension Dimension) *MemoryWorld {
	if id == "" {
		id = DefaultWorldID
	}
	dimension = NormalizeDimension(dimension, 256)
	blocks := make([]uint32, 16*16*16)
	blocks[0] = 1
	sectionY := DefaultBedrockY / 16
	return &MemoryWorld{
		WorldID:        id,
		WorldDimension: dimension,
		Palette: []BlockState{
			{Name: "minecraft:air"},
			{Name: "minecraft:bedrock"},
		},
		Chunks: map[ChunkPos]Chunk{
			{X: 0, Z: 0}: {
				X:    0,
				Z:    0,
				MinY: dimension.MinY,
				Sections: []ChunkSection{
					{Y: sectionY, BlockStateIDs: blocks},
				},
			},
		},
	}
}

// DefaultSpawn returns the spawn target that stands on the built-in bedrock
// block.
func DefaultSpawn(worldID string) SpawnTarget {
	if worldID == "" {
		worldID = DefaultWorldID
	}
	return SpawnTarget{
		World:    worldID,
		Position: Vec3{X: 0, Y: float64(DefaultBedrockY + 1), Z: 0},
		GameMode: GameModeAdventure,
	}
}

// MemoryWorld is a small immutable World implementation useful for embedded API users.
type MemoryWorld struct {
	WorldID        string
	WorldDimension Dimension
	Palette        []BlockState
	Chunks         map[ChunkPos]Chunk
}

// ChunkPos identifies a chunk in a world.
type ChunkPos struct {
	X int32
	Z int32
}

// ID returns the world ID.
func (w *MemoryWorld) ID() string {
	return w.WorldID
}

// Dimension returns the world dimension.
func (w *MemoryWorld) Dimension() Dimension {
	return w.WorldDimension
}

// BlockPalette returns the world's block palette.
func (w *MemoryWorld) BlockPalette() []BlockState {
	out := make([]BlockState, len(w.Palette))
	copy(out, w.Palette)
	return out
}

// Chunk returns a chunk by position.
func (w *MemoryWorld) Chunk(x int32, z int32) (Chunk, bool) {
	chunk, ok := w.Chunks[ChunkPos{X: x, Z: z}]
	return chunk, ok
}

// StaticWorldProvider serves a fixed set of worlds from memory.
type StaticWorldProvider map[string]World

// World resolves a world by ID.
func (p StaticWorldProvider) World(_ context.Context, id string) (World, error) {
	world, ok := p[id]
	if !ok {
		return nil, ErrWorldNotFound
	}
	return world, nil
}
