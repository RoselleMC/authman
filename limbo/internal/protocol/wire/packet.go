package wire

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

const DefaultMaxPacketSize = 2 << 20

// Packet is one uncompressed Minecraft packet frame.
type Packet struct {
	ID   int32
	Data []byte
}

// ReadPacket reads one length-prefixed packet.
func ReadPacket(r *bufio.Reader, maxSize int32) (Packet, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxPacketSize
	}
	length, err := ReadVarInt(r)
	if err != nil {
		return Packet{}, err
	}
	if length < 0 || length > maxSize {
		return Packet{}, fmt.Errorf("minecraft packet length %d outside limit %d", length, maxSize)
	}

	frame := make([]byte, length)
	if _, err := io.ReadFull(r, frame); err != nil {
		return Packet{}, err
	}

	body := bytes.NewReader(frame)
	id, err := ReadVarInt(body)
	if err != nil {
		return Packet{}, err
	}
	data := make([]byte, body.Len())
	_, _ = body.Read(data)

	return Packet{ID: id, Data: data}, nil
}

// WritePacket writes one length-prefixed packet.
func WritePacket(w io.Writer, packet Packet) error {
	var body bytes.Buffer
	if err := WriteVarInt(&body, packet.ID); err != nil {
		return err
	}
	if _, err := body.Write(packet.Data); err != nil {
		return err
	}

	var frame bytes.Buffer
	if err := WriteVarInt(&frame, int32(body.Len())); err != nil {
		return err
	}
	if _, err := frame.Write(body.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(frame.Bytes())
	return err
}
