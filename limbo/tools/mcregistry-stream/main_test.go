package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseStreamReadsCompleteRegistryCapture(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncompressed", true: "compressed"}[compressed], func(t *testing.T) {
			registries, tags, err := parseStream(testRegistryStream(t, compressed, true, true), 7, 13, 3)
			if err != nil {
				t.Fatalf("parseStream: %v", err)
			}
			if len(registries) != 1 || registries[0].ID != "minecraft:test_registry" {
				t.Fatalf("registries = %#v", registries)
			}
			if len(registries[0].Entries) != 1 || registries[0].Entries[0].Key != "minecraft:test_entry" {
				t.Fatalf("registry entries = %#v", registries[0].Entries)
			}
			nbt, err := base64.StdEncoding.DecodeString(registries[0].Entries[0].Value)
			if err != nil {
				t.Fatalf("decode registry entry: %v", err)
			}
			if len(nbt) == 0 || nbt[0] != 10 {
				t.Fatalf("registry entry is not anonymous compound NBT: %x", nbt)
			}
			if len(tags) != 1 || tags[0].ID != "minecraft:test_registry" || len(tags[0].Tags) != 1 {
				t.Fatalf("tags = %#v", tags)
			}
			if got := tags[0].Tags[0].Values; len(got) != 2 || got[0] != 0 || got[1] != 1 {
				t.Fatalf("tag values = %v", got)
			}
		})
	}
}

func TestParseStreamRejectsIncompleteCapture(t *testing.T) {
	_, _, err := parseStream(testRegistryStream(t, false, false, true), 7, 13, 3)
	if err == nil || !strings.Contains(err.Error(), "before finish_configuration") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseStreamRejectsKnownPackOmission(t *testing.T) {
	_, _, err := parseStream(testRegistryStream(t, false, true, false), 7, 13, 3)
	if err == nil || !strings.Contains(err.Error(), "no inline value") {
		t.Fatalf("error = %v", err)
	}
}

func TestNegativeCountsProduceUsefulErrors(t *testing.T) {
	body := cursor{data: append(testString("minecraft:test"), testVarInt(-1)...)}
	_, err := parseRegistry(&body)
	if err == nil || !strings.Contains(err.Error(), "negative entry count") || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("error = %v", err)
	}

	tagsBody := cursor{data: testVarInt(-1)}
	_, err = parseTags(&tagsBody)
	if err == nil || !strings.Contains(err.Error(), "negative tag registry count") || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("error = %v", err)
	}
}

func testRegistryStream(t *testing.T, compressed, includeFinish, inlineValue bool) []byte {
	t.Helper()
	registryBody := append(testString("minecraft:test_registry"), testVarInt(1)...)
	registryBody = append(registryBody, testString("minecraft:test_entry")...)
	if inlineValue {
		registryBody = append(registryBody, 1)
		registryBody = append(registryBody, testAnonymousCompoundNBT()...)
	} else {
		registryBody = append(registryBody, 0)
	}

	tagsBody := append(testVarInt(1), testString("minecraft:test_registry")...)
	tagsBody = append(tagsBody, testVarInt(1)...)
	tagsBody = append(tagsBody, testString("minecraft:test_tag")...)
	tagsBody = append(tagsBody, testVarInt(2)...)
	tagsBody = append(tagsBody, testVarInt(0)...)
	tagsBody = append(tagsBody, testVarInt(1)...)

	var stream []byte
	if compressed {
		stream = append(stream, testWireFrame(testPacket(3, testVarInt(0)))...)
		stream = append(stream, testCompressedFrame(t, testPacket(2, nil), true)...)
		stream = append(stream, testCompressedFrame(t, testPacket(7, registryBody), false)...)
		stream = append(stream, testCompressedFrame(t, testPacket(13, tagsBody), true)...)
		if includeFinish {
			stream = append(stream, testCompressedFrame(t, testPacket(3, nil), false)...)
		}
		return stream
	}

	stream = append(stream, testWireFrame(testPacket(2, nil))...)
	stream = append(stream, testWireFrame(testPacket(7, registryBody))...)
	stream = append(stream, testWireFrame(testPacket(13, tagsBody))...)
	if includeFinish {
		stream = append(stream, testWireFrame(testPacket(3, nil))...)
	}
	return stream
}

func testPacket(id int32, body []byte) []byte {
	return append(testVarInt(id), body...)
}

func testWireFrame(packet []byte) []byte {
	return append(testVarInt(int32(len(packet))), packet...)
}

func testCompressedFrame(t *testing.T, packet []byte, useCompression bool) []byte {
	t.Helper()
	if !useCompression {
		return testWireFrame(append([]byte{0}, packet...))
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(packet); err != nil {
		t.Fatalf("compress packet: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}
	payload := append(testVarInt(int32(len(packet))), compressed.Bytes()...)
	return testWireFrame(payload)
}

func testString(value string) []byte {
	return append(testVarInt(int32(len(value))), value...)
}

func testVarInt(value int32) []byte {
	remaining := uint32(value)
	var out []byte
	for {
		current := byte(remaining & 0x7f)
		remaining >>= 7
		if remaining != 0 {
			current |= 0x80
		}
		out = append(out, current)
		if remaining == 0 {
			return out
		}
	}
}

func testAnonymousCompoundNBT() []byte {
	var out bytes.Buffer
	out.WriteByte(10)
	testNamedNBTHeader(&out, 1, "byte")
	out.WriteByte(42)
	testNamedNBTHeader(&out, 8, "label")
	_ = binary.Write(&out, binary.BigEndian, uint16(2))
	out.WriteString("ok")
	testNamedNBTHeader(&out, 9, "list")
	out.WriteByte(3)
	_ = binary.Write(&out, binary.BigEndian, int32(2))
	_ = binary.Write(&out, binary.BigEndian, int32(7))
	_ = binary.Write(&out, binary.BigEndian, int32(8))
	testNamedNBTHeader(&out, 11, "ints")
	_ = binary.Write(&out, binary.BigEndian, int32(2))
	_ = binary.Write(&out, binary.BigEndian, int32(9))
	_ = binary.Write(&out, binary.BigEndian, int32(10))
	out.WriteByte(0)
	return out.Bytes()
}

func testNamedNBTHeader(out *bytes.Buffer, tagType byte, name string) {
	out.WriteByte(tagType)
	_ = binary.Write(out, binary.BigEndian, uint16(len(name)))
	out.WriteString(name)
}
