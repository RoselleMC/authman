package wire

import (
	"bufio"
	"bytes"
	"testing"
)

func TestVarIntRoundTrip(t *testing.T) {
	values := []int32{0, 1, 2, 127, 128, 255, 25565, 2097151, 2147483647, -1, -2147483648}
	for _, value := range values {
		var buf bytes.Buffer
		if err := WriteVarInt(&buf, value); err != nil {
			t.Fatalf("write %d: %v", value, err)
		}
		got, err := ReadVarInt(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("read %d: %v", value, err)
		}
		if got != value {
			t.Fatalf("round trip mismatch: got %d, want %d", got, value)
		}
	}
}

func TestPacketRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := Packet{ID: 0x23, Data: []byte{1, 2, 3, 4}}
	if err := WritePacket(&buf, want); err != nil {
		t.Fatalf("write packet: %v", err)
	}

	got, err := ReadPacket(bufio.NewReader(&buf), 0)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if got.ID != want.ID || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("packet mismatch: got %+v, want %+v", got, want)
	}
}
