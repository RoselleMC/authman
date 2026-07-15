package wire

import (
	"encoding/binary"
	"io"
	"math"
)

// WriteByte writes one byte.
func WriteByte(w io.Writer, value byte) error {
	_, err := w.Write([]byte{value})
	return err
}

// WriteBool writes a Minecraft boolean.
func WriteBool(w io.Writer, value bool) error {
	if value {
		return WriteByte(w, 1)
	}
	return WriteByte(w, 0)
}

// WriteShort writes a signed 16-bit big-endian integer.
func WriteShort(w io.Writer, value int16) error {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(value))
	_, err := w.Write(buf[:])
	return err
}

// WriteUnsignedShort writes an unsigned 16-bit big-endian integer.
func WriteUnsignedShort(w io.Writer, value uint16) error {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], value)
	_, err := w.Write(buf[:])
	return err
}

// WriteInt writes a signed 32-bit big-endian integer.
func WriteInt(w io.Writer, value int32) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(value))
	_, err := w.Write(buf[:])
	return err
}

// ReadUnsignedShort reads an unsigned 16-bit big-endian integer.
func ReadUnsignedShort(r io.Reader) (uint16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(buf[:]), nil
}

// WriteFloat writes a 32-bit IEEE 754 big-endian float.
func WriteFloat(w io.Writer, value float32) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(value))
	_, err := w.Write(buf[:])
	return err
}

// WriteDouble writes a 64-bit IEEE 754 big-endian float.
func WriteDouble(w io.Writer, value float64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(value))
	_, err := w.Write(buf[:])
	return err
}

// WriteLong writes a signed 64-bit big-endian integer.
func WriteLong(w io.Writer, value int64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(value))
	_, err := w.Write(buf[:])
	return err
}

// ReadLong reads a signed 64-bit big-endian integer.
func ReadLong(r io.Reader) (int64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(buf[:])), nil
}

// WritePosition writes the packed block position format used by modern Java
// protocol lines, including 1.8.
func WritePosition(w io.Writer, x, y, z int32) error {
	packed := (int64(x&0x3ffffff) << 38) | (int64(z&0x3ffffff) << 12) | int64(y&0xfff)
	return WriteLong(w, packed)
}
