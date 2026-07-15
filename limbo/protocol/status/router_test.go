package status

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"go.minekube.com/common/minecraft/component"
)

func TestRouterStatusPing(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Router{Description: "test limbo"}.ServeConn(context.Background(), serverConn, nil)
	}()

	if err := writeHandshake(clientConn, 767, "localhost", 25565, stateStatus); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	if err := wire.WritePacket(clientConn, wire.Packet{ID: 0}); err != nil {
		t.Fatalf("write status request: %v", err)
	}

	reader := bufio.NewReader(clientConn)
	responsePacket, err := wire.ReadPacket(reader, 0)
	if err != nil {
		t.Fatalf("read status response: %v", err)
	}
	if responsePacket.ID != 0 {
		t.Fatalf("status response packet id = %d, want 0", responsePacket.ID)
	}

	responseText, err := wire.ReadString(bytes.NewReader(responsePacket.Data), 32767)
	if err != nil {
		t.Fatalf("read response json: %v", err)
	}
	var response struct {
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal([]byte(responseText), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Description.Text != "test limbo" {
		t.Fatalf("description = %q, want %q", response.Description.Text, "test limbo")
	}

	var ping bytes.Buffer
	if err := wire.WriteLong(&ping, 42); err != nil {
		t.Fatalf("write ping payload: %v", err)
	}
	if err := wire.WritePacket(clientConn, wire.Packet{ID: 1, Data: ping.Bytes()}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	pong, err := wire.ReadPacket(reader, 0)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	got, err := wire.ReadLong(bytes.NewReader(pong.Data))
	if err != nil {
		t.Fatalf("read pong payload: %v", err)
	}
	if got != 42 {
		t.Fatalf("pong payload = %d, want 42", got)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("router error: %v", err)
	}
}

func TestRouterStatusProviderAndRichMOTD(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	secure := true
	errCh := make(chan error, 1)
	go func() {
		errCh <- Router{
			StatusProvider: limbgo.StatusProviderFunc(func(_ context.Context, request limbgo.StatusRequest) (limbgo.Status, error) {
				if request.Protocol != 771 {
					t.Errorf("request protocol = %d, want 771", request.Protocol)
				}
				if request.Address != "status.example" {
					t.Errorf("request address = %q", request.Address)
				}
				return limbgo.Status{
					VersionName:        "custom",
					Protocol:           999,
					MaxPlayers:         50,
					OnlinePlayers:      2,
					SamplePlayers:      []limbgo.StatusSamplePlayer{{Name: "Score2", ID: "00000000-0000-0000-0000-000000000002"}},
					Description:        &component.Text{Content: "Hello", Extra: []component.Component{&component.Text{Content: " status"}}},
					Favicon:            "data:image/png;base64,AAAA",
					EnforcesSecureChat: &secure,
				}, nil
			}),
		}.ServeConn(context.Background(), serverConn, nil)
	}()

	if err := writeHandshake(clientConn, 771, "status.example", 25565, stateStatus); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	if err := wire.WritePacket(clientConn, wire.Packet{ID: 0}); err != nil {
		t.Fatalf("write status request: %v", err)
	}
	response := readStatusResponse(t, clientConn)
	if response.Version.Name != "custom" {
		t.Fatalf("version name = %q", response.Version.Name)
	}
	if response.Version.Protocol != 999 {
		t.Fatalf("version protocol = %d", response.Version.Protocol)
	}
	if response.Description.Text != "Hello" {
		t.Fatalf("description text = %q", response.Description.Text)
	}
	if len(response.Description.Extra) != 1 || response.Description.Extra[0].Text != " status" {
		t.Fatalf("description extra = %+v", response.Description.Extra)
	}
	if response.Players.Max != 50 || response.Players.Online != 2 {
		t.Fatalf("players = %+v", response.Players)
	}
	if response.Favicon == "" {
		t.Fatalf("favicon missing")
	}
	if !response.EnforcesSecureChat {
		t.Fatalf("enforces secure chat = false")
	}

	var ping bytes.Buffer
	if err := wire.WriteLong(&ping, 100); err != nil {
		t.Fatalf("write ping payload: %v", err)
	}
	if err := wire.WritePacket(clientConn, wire.Packet{ID: 1, Data: ping.Bytes()}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	if _, err := wire.ReadPacket(bufio.NewReader(clientConn), 0); err != nil {
		t.Fatalf("read pong: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("router error: %v", err)
	}
}

func writeHandshake(conn net.Conn, protocol int32, address string, port uint16, nextState int32) error {
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, protocol); err != nil {
		return err
	}
	if err := wire.WriteString(&data, address); err != nil {
		return err
	}
	if err := data.WriteByte(byte(port >> 8)); err != nil {
		return err
	}
	if err := data.WriteByte(byte(port)); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, nextState); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: 0, Data: data.Bytes()})
}

func readStatusResponse(t *testing.T, conn net.Conn) statusResponse {
	t.Helper()
	packet, err := wire.ReadPacket(bufio.NewReader(conn), 0)
	if err != nil {
		t.Fatalf("read status response: %v", err)
	}
	responseText, err := wire.ReadString(bytes.NewReader(packet.Data), 32767)
	if err != nil {
		t.Fatalf("read response json: %v", err)
	}
	var response statusResponse
	if err := json.Unmarshal([]byte(responseText), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return response
}

type statusResponse struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int32  `json:"protocol"`
	} `json:"version"`
	Description struct {
		Text  string `json:"text"`
		Extra []struct {
			Text string `json:"text"`
		} `json:"extra"`
	} `json:"description"`
	Players struct {
		Max    int                         `json:"max"`
		Online int                         `json:"online"`
		Sample []limbgo.StatusSamplePlayer `json:"sample"`
	} `json:"players"`
	Favicon            string `json:"favicon"`
	EnforcesSecureChat bool   `json:"enforcesSecureChat"`
}
