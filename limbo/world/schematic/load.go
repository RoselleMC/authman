package schematic

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/RoselleMC/authman/limbo"
	"github.com/Tnze/go-mc/nbt"
)

// Options controls schematic loading.
type Options struct {
	WorldID   string
	Dimension limbgo.Dimension
}

// LoadFile reads a Sponge .schem file and returns a version-neutral MemoryWorld.
func LoadFile(path string, opts Options) (*limbgo.MemoryWorld, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Load(file, opts)
}

// Load reads a Sponge .schem file from r.
func Load(r io.Reader, opts Options) (*limbgo.MemoryWorld, error) {
	reader, err := maybeGzip(r)
	if err != nil {
		return nil, err
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	var data spongeSchematic
	if _, err := nbt.NewDecoder(reader).Decode(&data); err != nil {
		return nil, fmt.Errorf("%w: decode nbt: %v", limbgo.ErrInvalidSchematic, err)
	}
	return buildWorld(data, opts)
}

type spongeSchematic struct {
	Version   int32            `nbt:"Version"`
	Width     int16            `nbt:"Width"`
	Height    int16            `nbt:"Height"`
	Length    int16            `nbt:"Length"`
	Palette   map[string]int32 `nbt:"Palette"`
	BlockData []byte           `nbt:"BlockData"`
}

func buildWorld(data spongeSchematic, opts Options) (*limbgo.MemoryWorld, error) {
	if data.Width <= 0 || data.Height <= 0 || data.Length <= 0 {
		return nil, fmt.Errorf("%w: non-positive dimensions %dx%dx%d", limbgo.ErrInvalidSchematic, data.Width, data.Height, data.Length)
	}
	if len(data.Palette) == 0 {
		return nil, fmt.Errorf("%w: missing palette", limbgo.ErrInvalidSchematic)
	}

	palette, err := decodePalette(data.Palette)
	if err != nil {
		return nil, err
	}
	blockIDs, err := decodeBlockData(data.BlockData, int(data.Width)*int(data.Height)*int(data.Length))
	if err != nil {
		return nil, err
	}

	worldID := opts.WorldID
	if worldID == "" {
		worldID = "default"
	}
	dimension := opts.Dimension
	if dimension.Name == "" && dimension.Environment == "" {
		dimension = defaultDimension(int32(data.Height))
	} else {
		dimension = limbgo.NormalizeDimension(dimension, int32(data.Height))
	}

	chunks := make(map[limbgo.ChunkPos]limbgo.Chunk)
	for y := int32(0); y < int32(data.Height); y++ {
		for z := int32(0); z < int32(data.Length); z++ {
			for x := int32(0); x < int32(data.Width); x++ {
				paletteID := blockIDs[blockIndex(x, y, z, int32(data.Width), int32(data.Length))]
				if int(paletteID) >= len(palette) {
					return nil, fmt.Errorf("%w: block palette id %d outside palette size %d", limbgo.ErrInvalidSchematic, paletteID, len(palette))
				}
				setBlock(chunks, x, y, z, paletteID)
			}
		}
	}

	return &limbgo.MemoryWorld{
		WorldID:        worldID,
		WorldDimension: dimension,
		Palette:        palette,
		Chunks:         chunks,
	}, nil
}

func decodePalette(input map[string]int32) ([]limbgo.BlockState, error) {
	maxID := int32(-1)
	for _, id := range input {
		if id < 0 {
			return nil, fmt.Errorf("%w: negative palette id %d", limbgo.ErrInvalidSchematic, id)
		}
		if id > maxID {
			maxID = id
		}
	}

	palette := make([]limbgo.BlockState, maxID+1)
	for rawState, id := range input {
		state := parseBlockState(rawState)
		palette[id] = state
	}
	for i, state := range palette {
		if state.Name == "" {
			return nil, fmt.Errorf("%w: missing palette entry %d", limbgo.ErrInvalidSchematic, i)
		}
	}
	return palette, nil
}

func parseBlockState(raw string) limbgo.BlockState {
	name := raw
	properties := map[string]string(nil)

	if open := strings.IndexByte(raw, '['); open >= 0 && strings.HasSuffix(raw, "]") {
		name = raw[:open]
		rawProperties := strings.TrimSuffix(raw[open+1:], "]")
		properties = make(map[string]string)
		for _, pair := range strings.Split(rawProperties, ",") {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			properties[key] = value
		}
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return limbgo.BlockState{Name: name, Properties: properties}
}

func decodeBlockData(data []byte, expected int) ([]uint32, error) {
	ids := make([]uint32, 0, expected)
	for offset := 0; offset < len(data); {
		value, next, err := readSchematicVarInt(data, offset)
		if err != nil {
			return nil, err
		}
		ids = append(ids, value)
		offset = next
	}
	if len(ids) != expected {
		return nil, fmt.Errorf("%w: decoded %d blocks, want %d", limbgo.ErrInvalidSchematic, len(ids), expected)
	}
	return ids, nil
}

func readSchematicVarInt(data []byte, offset int) (uint32, int, error) {
	var value uint32
	for shift := uint(0); shift < 35; shift += 7 {
		if offset >= len(data) {
			return 0, offset, fmt.Errorf("%w: truncated block varint", limbgo.ErrInvalidSchematic)
		}
		b := data[offset]
		offset++
		value |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, offset, nil
		}
	}
	return 0, offset, fmt.Errorf("%w: block varint too long", limbgo.ErrInvalidSchematic)
}

func setBlock(chunks map[limbgo.ChunkPos]limbgo.Chunk, x, y, z int32, paletteID uint32) {
	chunkX := floorDiv(x, 16)
	chunkZ := floorDiv(z, 16)
	sectionY := floorDiv(y, 16)
	pos := limbgo.ChunkPos{X: chunkX, Z: chunkZ}

	chunk := chunks[pos]
	if len(chunk.Sections) == 0 {
		chunk = limbgo.Chunk{X: chunkX, Z: chunkZ, MinY: 0}
	}

	sectionIndex := -1
	for i := range chunk.Sections {
		if chunk.Sections[i].Y == sectionY {
			sectionIndex = i
			break
		}
	}
	if sectionIndex == -1 {
		chunk.Sections = append(chunk.Sections, limbgo.ChunkSection{
			Y:             sectionY,
			BlockStateIDs: make([]uint32, 16*16*16),
		})
		sectionIndex = len(chunk.Sections) - 1
		sort.Slice(chunk.Sections, func(i, j int) bool {
			return chunk.Sections[i].Y < chunk.Sections[j].Y
		})
		for i := range chunk.Sections {
			if chunk.Sections[i].Y == sectionY {
				sectionIndex = i
				break
			}
		}
	}

	localX := mod(x, 16)
	localY := mod(y, 16)
	localZ := mod(z, 16)
	chunk.Sections[sectionIndex].BlockStateIDs[sectionIndexInChunk(localX, localY, localZ)] = paletteID
	chunks[pos] = chunk
}

func maybeGzip(r io.Reader) (io.Reader, error) {
	buffered := bufio.NewReader(r)
	header, err := buffered.Peek(2)
	if err != nil {
		return buffered, nil
	}
	if header[0] == 0x1f && header[1] == 0x8b {
		gzipReader, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, err
		}
		return gzipReader, nil
	}
	return buffered, nil
}

func blockIndex(x, y, z, width, length int32) int {
	return int((y*length+z)*width + x)
}

func sectionIndexInChunk(x, y, z int32) int {
	return int((y*16+z)*16 + x)
}

func floorDiv(value, divisor int32) int32 {
	if value >= 0 {
		return value / divisor
	}
	return -((-value + divisor - 1) / divisor)
}

func mod(value, divisor int32) int32 {
	result := value % divisor
	if result < 0 {
		result += divisor
	}
	return result
}

func defaultDimension(height int32) limbgo.Dimension {
	return limbgo.DimensionPreset(limbgo.DimensionOverworld, height)
}
