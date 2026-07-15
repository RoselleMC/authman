package limbo

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"

	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
)

const (
	nbtEnd       byte = 0
	nbtByte      byte = 1
	nbtShort     byte = 2
	nbtInt       byte = 3
	nbtLong      byte = 4
	nbtFloat     byte = 5
	nbtDouble    byte = 6
	nbtLongArray byte = 12
	nbtString    byte = 8
	nbtList      byte = 9
	nbtCompound  byte = 10
)

type nbtWriter struct {
	buf bytes.Buffer
}

func (w *nbtWriter) bytes() []byte {
	return w.buf.Bytes()
}

func (w *nbtWriter) writeAnonymousCompound(fn func()) {
	_ = w.buf.WriteByte(nbtCompound)
	fn()
	_ = w.buf.WriteByte(nbtEnd)
}

func (w *nbtWriter) writeByte(name string, value byte) {
	w.writeNamedHeader(nbtByte, name)
	_ = w.buf.WriteByte(value)
}

func (w *nbtWriter) writeInt(name string, value int32) {
	w.writeNamedHeader(nbtInt, name)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(value))
	_, _ = w.buf.Write(b[:])
}

func (w *nbtWriter) writeLong(name string, value int64) {
	w.writeNamedHeader(nbtLong, name)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(value))
	_, _ = w.buf.Write(b[:])
}

func (w *nbtWriter) writeFloat(name string, value float32) {
	w.writeNamedHeader(nbtFloat, name)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], math.Float32bits(value))
	_, _ = w.buf.Write(b[:])
}

func (w *nbtWriter) writeDouble(name string, value float64) {
	w.writeNamedHeader(nbtDouble, name)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(value))
	_, _ = w.buf.Write(b[:])
}

func (w *nbtWriter) writeString(name, value string) {
	w.writeNamedHeader(nbtString, name)
	w.writeRawString(value)
}

func (w *nbtWriter) writeStringList(name string, values []string) {
	w.writeNamedHeader(nbtList, name)
	_ = w.buf.WriteByte(nbtString)
	w.writeRawInt(int32(len(values)))
	for _, value := range values {
		w.writeRawString(value)
	}
}

func (w *nbtWriter) writeLongArray(name string, values []int64) {
	w.writeNamedHeader(nbtLongArray, name)
	w.writeRawInt(int32(len(values)))
	for _, value := range values {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(value))
		_, _ = w.buf.Write(b[:])
	}
}

func (w *nbtWriter) writeCompound(name string, fn func()) {
	w.writeNamedHeader(nbtCompound, name)
	fn()
	_ = w.buf.WriteByte(nbtEnd)
}

func (w *nbtWriter) writeNamedHeader(tag byte, name string) {
	_ = w.buf.WriteByte(tag)
	w.writeRawString(name)
}

func (w *nbtWriter) writeRawString(value string) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(len(value)))
	_, _ = w.buf.Write(b[:])
	_, _ = io.WriteString(&w.buf, value)
}

func (w *nbtWriter) writeRawInt(value int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(value))
	_, _ = w.buf.Write(b[:])
}

func writeOptionAnonymousNBT(out *bytes.Buffer, present bool, data []byte) error {
	if err := wire.WriteBool(out, present); err != nil {
		return err
	}
	if present {
		_, err := out.Write(data)
		return err
	}
	return nil
}

func fullNBT(anonymous []byte) []byte {
	if len(anonymous) == 0 {
		return nil
	}
	out := make([]byte, 0, len(anonymous)+2)
	out = append(out, anonymous[0], 0, 0)
	out = append(out, anonymous[1:]...)
	return out
}
