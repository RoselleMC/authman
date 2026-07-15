package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	stateLogin = iota
	stateConfiguration
)

type outputData struct {
	FormatVersion int           `json:"format_version"`
	Protocol      int32         `json:"protocol"`
	Registries    []registry    `json:"registries"`
	Tags          []tagRegistry `json:"tags,omitempty"`
}

type registry struct {
	ID      string  `json:"id"`
	Entries []entry `json:"entries"`
}

type entry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type tagRegistry struct {
	ID   string `json:"id"`
	Tags []tag  `json:"tags"`
}

type tag struct {
	Key    string  `json:"key"`
	Values []int32 `json:"values"`
}

func main() {
	var streamPath string
	var outputPath string
	var protocol int
	var registryPacketID int
	var tagsPacketID int
	var finishPacketID int
	flag.StringVar(&streamPath, "stream", "", "raw server-to-client stream captured from connection start")
	flag.StringVar(&outputPath, "out", "", "output per-protocol registry JSON")
	flag.IntVar(&protocol, "protocol", 0, "Minecraft protocol number")
	flag.IntVar(&registryPacketID, "registry-packet", -1, "clientbound configuration registry_data packet ID")
	flag.IntVar(&tagsPacketID, "tags-packet", -1, "clientbound configuration tags packet ID")
	flag.IntVar(&finishPacketID, "finish-packet", -1, "clientbound finish_configuration packet ID")
	flag.Parse()

	if streamPath == "" || outputPath == "" || protocol <= 0 || registryPacketID < 0 || tagsPacketID < 0 || finishPacketID < 0 {
		fatalf("-stream, -out, -protocol, -registry-packet, -tags-packet, and -finish-packet are required")
	}
	raw, err := os.ReadFile(streamPath)
	if err != nil {
		fatalf("read stream: %v", err)
	}
	registries, tags, err := parseStream(raw, int32(registryPacketID), int32(tagsPacketID), int32(finishPacketID))
	if err != nil {
		fatalf("parse stream: %v", err)
	}
	if len(registries) == 0 {
		fatalf("stream contains no registry_data packets")
	}
	encoded, err := json.MarshalIndent(outputData{
		FormatVersion: 1,
		Protocol:      int32(protocol),
		Registries:    registries,
		Tags:          tags,
	}, "", "  ")
	if err != nil {
		fatalf("encode output: %v", err)
	}
	if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o644); err != nil {
		fatalf("write output: %v", err)
	}
}

func parseStream(raw []byte, registryPacketID, tagsPacketID, finishPacketID int32) ([]registry, []tagRegistry, error) {
	stream := cursor{data: raw}
	state := stateLogin
	compressed := false
	var registries []registry
	var tags []tagRegistry
	for stream.remaining() > 0 {
		frameLength, err := stream.varInt()
		if err != nil {
			return nil, nil, fmt.Errorf("read frame length at %d: %w", stream.offset, err)
		}
		if frameLength < 0 {
			return nil, nil, fmt.Errorf("negative frame length %d", frameLength)
		}
		frame, err := stream.take(int(frameLength))
		if err != nil {
			return nil, nil, fmt.Errorf("read frame at %d: %w", stream.offset, err)
		}
		packet, err := decodeFrame(frame, compressed)
		if err != nil {
			return nil, nil, err
		}
		body := cursor{data: packet}
		packetID, err := body.varInt()
		if err != nil {
			return nil, nil, fmt.Errorf("read packet ID: %w", err)
		}

		if state == stateLogin {
			switch packetID {
			case 2:
				state = stateConfiguration
			case 3:
				if compressed {
					return nil, nil, errors.New("received duplicate set_compression packet")
				}
				if _, err := body.varInt(); err != nil {
					return nil, nil, fmt.Errorf("read compression threshold: %w", err)
				}
				compressed = true
			}
			continue
		}

		switch packetID {
		case registryPacketID:
			value, err := parseRegistry(&body)
			if err != nil {
				return nil, nil, fmt.Errorf("parse registry_data packet: %w", err)
			}
			if value.ID != "minecraft:dimension_type" {
				registries = append(registries, value)
			}
		case tagsPacketID:
			value, err := parseTags(&body)
			if err != nil {
				return nil, nil, fmt.Errorf("parse tags packet: %w", err)
			}
			tags = value
		case finishPacketID:
			if body.remaining() != 0 {
				return nil, nil, fmt.Errorf("finish_configuration packet has %d trailing bytes", body.remaining())
			}
			if len(registries) == 0 {
				return nil, nil, errors.New("finish_configuration arrived before any registry_data packet")
			}
			if tags == nil {
				return nil, nil, errors.New("finish_configuration arrived before the tags packet")
			}
			return registries, tags, nil
		}
	}
	return nil, nil, errors.New("stream ended before finish_configuration")
}

func decodeFrame(frame []byte, compressed bool) ([]byte, error) {
	if !compressed {
		return frame, nil
	}
	body := cursor{data: frame}
	uncompressedLength, err := body.varInt()
	if err != nil {
		return nil, fmt.Errorf("read compressed frame length: %w", err)
	}
	if uncompressedLength == 0 {
		return body.rest(), nil
	}
	if uncompressedLength < 0 || uncompressedLength > 128<<20 {
		return nil, fmt.Errorf("invalid uncompressed frame length %d", uncompressedLength)
	}
	reader, err := zlib.NewReader(bytes.NewReader(body.rest()))
	if err != nil {
		return nil, fmt.Errorf("open compressed frame: %w", err)
	}
	decompressed, readErr := io.ReadAll(io.LimitReader(reader, int64(uncompressedLength)+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("decompress frame: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close compressed frame: %w", closeErr)
	}
	if len(decompressed) != int(uncompressedLength) {
		return nil, fmt.Errorf("decompressed frame is %d bytes, want %d", len(decompressed), uncompressedLength)
	}
	return decompressed, nil
}

func parseRegistry(body *cursor) (registry, error) {
	id, err := body.string()
	if err != nil {
		return registry{}, fmt.Errorf("read registry ID: %w", err)
	}
	count, err := body.varInt()
	if err != nil {
		return registry{}, fmt.Errorf("read registry %s entry count: %w", id, err)
	}
	if count < 0 {
		return registry{}, fmt.Errorf("registry %s has negative entry count %d", id, count)
	}
	value := registry{ID: id, Entries: make([]entry, 0, count)}
	for index := int32(0); index < count; index++ {
		key, err := body.string()
		if err != nil {
			return registry{}, fmt.Errorf("read entry %d key: %w", index, err)
		}
		present, err := body.byte()
		if err != nil {
			return registry{}, fmt.Errorf("read entry %s presence: %w", key, err)
		}
		if present == 0 {
			return registry{}, fmt.Errorf("entry %s has no inline value; capture with an empty known-pack response", key)
		}
		start := body.offset
		if err := body.skipAnonymousNBT(); err != nil {
			return registry{}, fmt.Errorf("read entry %s NBT: %w", key, err)
		}
		value.Entries = append(value.Entries, entry{
			Key:   key,
			Value: base64.StdEncoding.EncodeToString(body.data[start:body.offset]),
		})
	}
	if body.remaining() != 0 {
		return registry{}, fmt.Errorf("registry %s has %d trailing bytes", id, body.remaining())
	}
	return value, nil
}

func parseTags(body *cursor) ([]tagRegistry, error) {
	registryCount, err := body.varInt()
	if err != nil {
		return nil, fmt.Errorf("read tag registry count: %w", err)
	}
	if registryCount < 0 {
		return nil, fmt.Errorf("negative tag registry count %d", registryCount)
	}
	registries := make([]tagRegistry, 0, registryCount)
	for registryIndex := int32(0); registryIndex < registryCount; registryIndex++ {
		id, err := body.string()
		if err != nil {
			return nil, fmt.Errorf("read tag registry %d ID: %w", registryIndex, err)
		}
		tagCount, err := body.varInt()
		if err != nil {
			return nil, fmt.Errorf("read registry %s tag count: %w", id, err)
		}
		if tagCount < 0 {
			return nil, fmt.Errorf("registry %s has negative tag count %d", id, tagCount)
		}
		registryValue := tagRegistry{ID: id, Tags: make([]tag, 0, tagCount)}
		for tagIndex := int32(0); tagIndex < tagCount; tagIndex++ {
			key, err := body.string()
			if err != nil {
				return nil, fmt.Errorf("read tag %d key: %w", tagIndex, err)
			}
			valueCount, err := body.varInt()
			if err != nil {
				return nil, fmt.Errorf("read tag %s value count: %w", key, err)
			}
			if valueCount < 0 {
				return nil, fmt.Errorf("tag %s has negative value count %d", key, valueCount)
			}
			values := make([]int32, 0, valueCount)
			for valueIndex := int32(0); valueIndex < valueCount; valueIndex++ {
				item, err := body.varInt()
				if err != nil {
					return nil, fmt.Errorf("read tag %s value %d: %w", key, valueIndex, err)
				}
				values = append(values, item)
			}
			registryValue.Tags = append(registryValue.Tags, tag{Key: key, Values: values})
		}
		registries = append(registries, registryValue)
	}
	if body.remaining() != 0 {
		return nil, fmt.Errorf("tags packet has %d trailing bytes", body.remaining())
	}
	return registries, nil
}

type cursor struct {
	data   []byte
	offset int
}

func (c *cursor) remaining() int { return len(c.data) - c.offset }

func (c *cursor) rest() []byte { return c.data[c.offset:] }

func (c *cursor) byte() (byte, error) {
	value, err := c.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (c *cursor) take(length int) ([]byte, error) {
	if length < 0 || length > c.remaining() {
		return nil, io.ErrUnexpectedEOF
	}
	value := c.data[c.offset : c.offset+length]
	c.offset += length
	return value, nil
}

func (c *cursor) varInt() (int32, error) {
	var value uint32
	for index := 0; index < 5; index++ {
		current, err := c.byte()
		if err != nil {
			return 0, err
		}
		value |= uint32(current&0x7f) << (7 * index)
		if current&0x80 == 0 {
			return int32(value), nil
		}
	}
	return 0, errors.New("VarInt exceeds 5 bytes")
}

func (c *cursor) string() (string, error) {
	length, err := c.varInt()
	if err != nil {
		return "", fmt.Errorf("read string length: %w", err)
	}
	if length < 0 {
		return "", fmt.Errorf("negative string length %d", length)
	}
	raw, err := c.take(int(length))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (c *cursor) unsignedShort() (uint16, error) {
	raw, err := c.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(raw), nil
}

func (c *cursor) int32() (int32, error) {
	raw, err := c.take(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(raw)), nil
}

func (c *cursor) skipAnonymousNBT() error {
	tagType, err := c.byte()
	if err != nil {
		return err
	}
	if tagType == 0 {
		return errors.New("anonymous NBT root is TAG_End")
	}
	return c.skipNBTPayload(tagType)
}

func (c *cursor) skipNBTPayload(tagType byte) error {
	switch tagType {
	case 0:
		return nil
	case 1:
		_, err := c.take(1)
		return err
	case 2:
		_, err := c.take(2)
		return err
	case 3, 5:
		_, err := c.take(4)
		return err
	case 4, 6:
		_, err := c.take(8)
		return err
	case 7, 11, 12:
		length, err := c.int32()
		if err != nil {
			return fmt.Errorf("read NBT array length: %w", err)
		}
		if length < 0 {
			return fmt.Errorf("negative NBT array length %d", length)
		}
		width := 1
		if tagType == 11 {
			width = 4
		} else if tagType == 12 {
			width = 8
		}
		_, err = c.take(int(length) * width)
		return err
	case 8:
		length, err := c.unsignedShort()
		if err != nil {
			return err
		}
		_, err = c.take(int(length))
		return err
	case 9:
		elementType, err := c.byte()
		if err != nil {
			return err
		}
		length, err := c.int32()
		if err != nil {
			return fmt.Errorf("read NBT list length: %w", err)
		}
		if length < 0 {
			return fmt.Errorf("negative NBT list length %d", length)
		}
		if elementType == 0 && length != 0 {
			return fmt.Errorf("NBT TAG_End list has non-zero length %d", length)
		}
		for index := int32(0); index < length; index++ {
			if err := c.skipNBTPayload(elementType); err != nil {
				return fmt.Errorf("read NBT list element %d: %w", index, err)
			}
		}
		return nil
	case 10:
		for {
			childType, err := c.byte()
			if err != nil {
				return err
			}
			if childType == 0 {
				return nil
			}
			nameLength, err := c.unsignedShort()
			if err != nil {
				return err
			}
			if _, err := c.take(int(nameLength)); err != nil {
				return err
			}
			if err := c.skipNBTPayload(childType); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown NBT tag type %d", tagType)
	}
}

func fatalf(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "mcregistry-stream: "+format+"\n", values...)
	os.Exit(1)
}
