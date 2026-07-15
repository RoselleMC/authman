package status

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"go.minekube.com/common/minecraft/component"
)

const (
	stateStatus = 1
	stateLogin  = 2
)

// Router implements the stable handshake and server-list status path.
//
// Login is deliberately rejected until the generated play-state adapters are
// implemented. This makes the standalone binary observable without pretending to
// support chunk rendering yet.
type Router struct {
	Description         string
	MOTD                component.Component
	StatusProvider      limbgo.StatusProvider
	StatusRateLimiter   *limbgo.RateLimiter
	VersionName         string
	VersionProtocol     int32
	MaxPlayers          int
	OnlinePlayers       int
	SamplePlayers       []limbgo.StatusSamplePlayer
	HidePlayers         bool
	Favicon             string
	EnforcesSecureChat  *bool
	PreviewsChat        *bool
	PreventsChatReports *bool
}

// ServeConn implements limbgo.ProtocolRouter.
func (r Router) ServeConn(ctx context.Context, conn net.Conn, _ limbgo.SessionServices) error {
	reader := bufio.NewReader(conn)
	handshake, err := wire.ReadPacket(reader, 0)
	if err != nil {
		return err
	}
	if handshake.ID != 0 {
		return fmt.Errorf("expected handshake packet 0, got %d", handshake.ID)
	}

	info, err := readHandshake(handshake.Data)
	if err != nil {
		return err
	}

	switch info.NextState {
	case stateStatus:
		return r.serveStatus(ctx, conn, reader, info)
	case stateLogin:
		return writeLoginDisconnect(conn, "limbgo play-state protocol adapters are not implemented yet")
	default:
		return fmt.Errorf("unknown handshake next state %d", info.NextState)
	}
}

func (r Router) serveStatus(ctx context.Context, conn net.Conn, reader *bufio.Reader, info handshakeInfo) error {
	req, err := wire.ReadPacket(reader, 0)
	if err != nil {
		return err
	}
	if req.ID != 0 {
		return fmt.Errorf("expected status request packet 0, got %d", req.ID)
	}
	if !r.allowStatus(conn.RemoteAddr()) {
		return nil
	}

	status, err := r.status(ctx, conn.RemoteAddr(), info)
	if err != nil {
		return err
	}
	payload, err := limbgo.MarshalStatusJSON(status, info.ProtocolVersion)
	if err != nil {
		return err
	}

	var data bytes.Buffer
	if err := wire.WriteString(&data, string(payload)); err != nil {
		return err
	}
	if err := wire.WritePacket(conn, wire.Packet{ID: 0, Data: data.Bytes()}); err != nil {
		return err
	}

	ping, err := wire.ReadPacket(reader, 0)
	if err != nil {
		return err
	}
	if ping.ID != 1 {
		return fmt.Errorf("expected ping packet 1, got %d", ping.ID)
	}
	return wire.WritePacket(conn, wire.Packet{ID: 1, Data: ping.Data})
}

func (r Router) allowStatus(remote net.Addr) bool {
	return r.StatusRateLimiter == nil || r.StatusRateLimiter.Allow(remote)
}

func (r Router) status(ctx context.Context, remote net.Addr, info handshakeInfo) (limbgo.Status, error) {
	if r.StatusProvider != nil {
		return r.StatusProvider.Status(ctx, limbgo.StatusRequest{
			Protocol:   info.ProtocolVersion,
			Address:    info.Address,
			Port:       info.Port,
			RemoteAddr: remote,
		})
	}
	description := r.MOTD
	if description == nil && r.Description != "" {
		description = &component.Text{Content: r.Description}
	}
	return limbgo.StatusOptions{
		VersionName:         r.VersionName,
		Protocol:            r.VersionProtocol,
		MaxPlayers:          r.MaxPlayers,
		OnlinePlayers:       r.OnlinePlayers,
		SamplePlayers:       r.SamplePlayers,
		HidePlayers:         r.HidePlayers,
		Description:         description,
		Favicon:             r.Favicon,
		EnforcesSecureChat:  r.EnforcesSecureChat,
		PreviewsChat:        r.PreviewsChat,
		PreventsChatReports: r.PreventsChatReports,
	}.Status(), nil
}

type handshakeInfo struct {
	ProtocolVersion int32
	Address         string
	Port            uint16
	NextState       int32
}

func readHandshake(data []byte) (handshakeInfo, error) {
	body := bytes.NewReader(data)
	protocol, err := wire.ReadVarInt(body)
	if err != nil {
		return handshakeInfo{}, err
	}
	address, err := wire.ReadString(body, 255)
	if err != nil {
		return handshakeInfo{}, err
	}
	port, err := wire.ReadUnsignedShort(body)
	if err != nil {
		return handshakeInfo{}, err
	}
	nextState, err := wire.ReadVarInt(body)
	if err != nil {
		return handshakeInfo{}, err
	}
	return handshakeInfo{
		ProtocolVersion: protocol,
		Address:         address,
		Port:            port,
		NextState:       nextState,
	}, nil
}

func writeLoginDisconnect(conn net.Conn, message string) error {
	payload, err := limbgo.MarshalComponentJSON(0, &component.Text{Content: message})
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, string(payload)); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: 0, Data: data.Bytes()})
}
