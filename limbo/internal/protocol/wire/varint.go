package wire

import (
	"fmt"
	"io"
)

const (
	MaxVarIntBytes  = 5
	MaxVarLongBytes = 10
)

// ReadVarInt reads a Minecraft VarInt from r.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var value int32
	for shift := uint(0); shift < 35; shift += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= int32(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}
	}
	return 0, fmt.Errorf("minecraft varint exceeds %d bytes", MaxVarIntBytes)
}

// WriteVarInt writes a Minecraft VarInt to w.
func WriteVarInt(w io.ByteWriter, value int32) error {
	u := uint32(value)
	for {
		if u&^uint32(0x7f) == 0 {
			return w.WriteByte(byte(u))
		}
		if err := w.WriteByte(byte(u&0x7f | 0x80)); err != nil {
			return err
		}
		u >>= 7
	}
}

// VarIntLen returns the encoded length of value.
func VarIntLen(value int32) int {
	u := uint32(value)
	for i := 1; i < MaxVarIntBytes; i++ {
		if u&^uint32(0x7f) == 0 {
			return i
		}
		u >>= 7
	}
	return MaxVarIntBytes
}
