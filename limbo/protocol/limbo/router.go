package limbo

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/blockstate"
	protocolpack "github.com/RoselleMC/authman/limbo/protocol/pack"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
	"github.com/RoselleMC/authman/limbo/protocol/registrydata"
	"github.com/google/uuid"
	"go.minekube.com/common/minecraft/component"
)

const (
	stateStatus = 1
	stateLogin  = 2
)

// Router is the main Minecraft limbo protocol router.
//
// Status is protocol-neutral. Play support is intentionally version-adapted:
// legacy adapters cover protocol 47 (Minecraft 1.8.x) and protocol 340
// (Minecraft 1.12.2), while modern adapters are selected from the configured
// ModernProtocols table.
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
	ModernProtocols     *ModernProtocols
	ProtocolPack        *protocolpack.Pack
	ProtocolPackSource  protocolpack.Source
	PacketIDs           *packetid.Table
	BlockStates         *blockstate.Table
	RegistryData        *registrydata.Data
	RegistryDataSource  registrydata.Source
	ProtocolPolicy      limbgo.ProtocolPolicy
	LoginMode           limbgo.LoginMode
	LoginPolicy         limbgo.LoginPolicy
	LoginDecisionPolicy limbgo.LoginPolicyV2
	SessionVerifier     limbgo.SessionVerifier
	YggdrasilVerifier   limbgo.YggdrasilVerifierConfig
	OnlineServerID      string
}

// ServeConn implements limbgo.ProtocolRouter.
func (r Router) ServeConn(ctx context.Context, conn net.Conn, services limbgo.SessionServices) error {
	var err error
	r, err = r.withProtocolPackSnapshot()
	if err != nil {
		return err
	}
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
		return r.serveLogin(ctx, conn, reader, services, info)
	default:
		return fmt.Errorf("unknown handshake next state %d", info.NextState)
	}
}

func (r Router) serveLogin(ctx context.Context, conn net.Conn, reader *bufio.Reader, services limbgo.SessionServices, info handshakeInfo) error {
	loginPacketProtocol := info.ProtocolVersion
	var cfg modernProtocolConfig
	hasModernConfig := false
	if info.ProtocolVersion != protocol47 && info.ProtocolVersion != protocol340 {
		modernProtocols, err := r.modernProtocols()
		if err != nil {
			return err
		}
		if loaded, ok := modernProtocols.configFor(info.ProtocolVersion); ok {
			cfg = loaded
			cfg.packetIDs = r.PacketIDs
			cfg.blockStates = r.BlockStates
			hasModernConfig = true
			loginPacketProtocol = cfg.packetProtocol()
		}
	}

	if err := r.allowProtocol(ctx, conn, loginPacketProtocol, info); err != nil {
		return err
	}

	loginStart, err := wire.ReadPacket(reader, 0)
	if err != nil {
		return err
	}

	loginStartID, ok := r.packetID(loginPacketProtocol, packetid.StateLogin, packetid.ToServer, "login_start")
	if !ok || loginStart.ID != loginStartID {
		return fmt.Errorf("expected login_start packet %d, got %d", loginStartID, loginStart.ID)
	}

	username, claimedUUID, err := readLoginStart(loginStart.Data, loginStartLayoutFor(hasModernConfig, cfg))
	if err != nil {
		return err
	}
	loginRequest := limbgo.LoginRequest{
		Username:        username,
		ClaimedUUID:     claimedUUID,
		ProtocolVersion: int(info.ProtocolVersion),
		RemoteAddr:      conn.RemoteAddr(),
		RequestedHost:   info.Address,
	}
	authLayout := sessionAuthLayoutFor(info.ProtocolVersion, hasModernConfig, cfg)
	loginDecision, err := r.resolveLoginDecision(ctx, loginRequest)
	if err != nil {
		return err
	}
	var player limbgo.Player
	switch loginDecision.Mode {
	case limbgo.LoginModeOffline:
		player = offlineLoginPlayer(loginRequest, loginDecision.Profile)
	case limbgo.LoginModeOnline:
		verifier := r.sessionVerifier()
		var profile limbgo.VerifiedProfile
		authConn, authReader, profile, err := performOnlineSessionAuth(ctx, conn, reader, authLayout, loginPacketProtocol, loginRequest, verifier, r.OnlineServerID, r.PacketIDs)
		if err != nil {
			_ = writeLoginDisconnect(loginDisconnectConn(authConn, conn), loginPacketProtocol, r.PacketIDs, "Session verification failed")
			return err
		}
		conn, reader = authConn, authReader
		player = limbgo.VerifiedLoginPlayer(loginRequest, profile)
	case limbgo.LoginModeHybrid:
		verifier := r.sessionVerifier()
		var profile limbgo.VerifiedProfile
		authConn, authReader, profile, err := performOnlineSessionAuth(ctx, conn, reader, authLayout, loginPacketProtocol, loginRequest, verifier, r.OnlineServerID, r.PacketIDs)
		switch {
		case err == nil:
			conn, reader = authConn, authReader
			player = limbgo.VerifiedLoginPlayer(loginRequest, profile)
		case errors.Is(err, limbgo.ErrInvalidLogin) && authConn != nil && authReader != nil:
			conn, reader = authConn, authReader
			player = limbgo.OfflineLoginPlayer(loginRequest)
		default:
			_ = writeLoginDisconnect(loginDisconnectConn(authConn, conn), loginPacketProtocol, r.PacketIDs, "Session verification failed")
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported login mode %q", limbgo.ErrInvalidLogin, loginDecision.Mode)
	}

	switch info.ProtocolVersion {
	case protocol47:
		return serveProtocol47(ctx, conn, services, player)
	case protocol340:
		return serveProtocol340(ctx, conn, services, player)
	default:
		if cfg, ok := legacyProtocolConfigFor(info.ProtocolVersion); ok {
			return serveLegacyProtocol(ctx, conn, services, player, cfg)
		}
		if cfg, ok := flatProtocolConfigFor(info.ProtocolVersion); ok {
			return serveFlatProtocol(ctx, conn, services, player, cfg)
		}
		if cfg, ok := codecProtocolConfigFor(info.ProtocolVersion); ok {
			registryData, err := r.registryData()
			if err != nil {
				return err
			}
			return serveCodecProtocol(ctx, conn, services, player, cfg, registryData)
		}
		if hasModernConfig {
			if cfg.preConfiguration {
				registryData, err := r.registryData()
				if err != nil {
					return err
				}
				return serveModernPreConfigurationProtocol(ctx, conn, services, player, cfg, registryData)
			}
			return serveModernProtocol(ctx, conn, reader, services, player, cfg, r.registryData)
		}
		return writeLoginDisconnect(conn, info.ProtocolVersion, r.PacketIDs, "limbgo play support currently implements protocols "+r.supportedPlayProtocols())
	}
}

func (r Router) allowProtocol(ctx context.Context, conn net.Conn, packetProtocol int32, info handshakeInfo) error {
	if r.ProtocolPolicy == nil {
		return nil
	}
	request := limbgo.ProtocolRequest{
		ProtocolVersion: int(info.ProtocolVersion),
		RemoteAddr:      conn.RemoteAddr(),
		RequestedHost:   info.Address,
	}
	if r.ProtocolPack != nil {
		descriptor, ok := r.ProtocolPack.Protocol(info.ProtocolVersion)
		request.ProtocolKnown = ok
		request.PortalDialog = ok && descriptor.Layout.PortalDialog
	}
	err := r.ProtocolPolicy.AllowProtocol(ctx, request)
	if err == nil {
		return nil
	}
	if reason, ok := limbgo.ProtocolRejection(err); ok {
		_ = writeLoginDisconnectComponent(conn, packetProtocol, r.PacketIDs, reason)
	}
	return err
}

func loginDisconnectConn(primary net.Conn, fallback net.Conn) net.Conn {
	if primary != nil {
		return primary
	}
	return fallback
}

func (r Router) resolveLoginDecision(ctx context.Context, req limbgo.LoginRequest) (limbgo.LoginDecision, error) {
	if r.LoginDecisionPolicy != nil {
		decision, err := r.LoginDecisionPolicy.ResolveLogin(ctx, req)
		if err != nil {
			return limbgo.LoginDecision{}, err
		}
		return normalizeLoginDecision(decision), nil
	}
	if r.LoginPolicy != nil {
		mode, err := r.LoginPolicy.ResolveLoginMode(ctx, req)
		if err != nil {
			return limbgo.LoginDecision{}, err
		}
		return normalizeLoginDecision(limbgo.LoginDecision{Mode: mode}), nil
	}
	if r.LoginMode == "" {
		return limbgo.LoginDecision{Mode: limbgo.LoginModeOffline}, nil
	}
	return limbgo.LoginDecision{Mode: r.LoginMode}, nil
}

func normalizeLoginDecision(decision limbgo.LoginDecision) limbgo.LoginDecision {
	if decision.Mode == "" {
		decision.Mode = limbgo.LoginModeOffline
	}
	if decision.Mode != limbgo.LoginModeOffline {
		decision.Profile = nil
	}
	return decision
}

func offlineLoginPlayer(req limbgo.LoginRequest, profile *limbgo.LoginProfile) limbgo.Player {
	if profile == nil {
		return limbgo.OfflineLoginPlayer(req)
	}
	return limbgo.OfflineLoginPlayerWithProfile(req, *profile)
}

func (r Router) sessionVerifier() limbgo.SessionVerifier {
	if r.SessionVerifier != nil {
		return r.SessionVerifier
	}
	return limbgo.NewYggdrasilVerifier(r.YggdrasilVerifier)
}

func (r Router) modernProtocols() (*ModernProtocols, error) {
	if r.ModernProtocols != nil {
		return r.ModernProtocols, nil
	}
	return DefaultModernProtocols()
}

func (r Router) withProtocolPackSnapshot() (Router, error) {
	protocolPack := r.ProtocolPack
	if r.ProtocolPackSource != nil {
		loaded, err := r.ProtocolPackSource.ProtocolPack()
		if err != nil {
			return Router{}, err
		}
		protocolPack = loaded
	}
	if protocolPack == nil {
		return r, nil
	}
	modernProtocols, err := ModernProtocolsFromPack(protocolPack)
	if err != nil {
		return Router{}, err
	}
	r.ProtocolPack = protocolPack
	r.ModernProtocols = modernProtocols
	r.PacketIDs = protocolPack.PacketIDs()
	r.BlockStates = protocolPack.BlockStates()
	r.RegistryData = protocolPack.RegistryData()
	r.RegistryDataSource = nil
	return r, nil
}

func (r Router) packetID(protocol int32, state packetid.State, direction packetid.Direction, name string) (int32, bool) {
	if r.PacketIDs != nil {
		return r.PacketIDs.ID(protocol, state, direction, name)
	}
	return packetid.ID(protocol, state, direction, name)
}

func (r Router) registryData() (*registrydata.Data, error) {
	if r.RegistryDataSource != nil {
		return r.RegistryDataSource.RegistryData()
	}
	if r.RegistryData != nil {
		return r.RegistryData, nil
	}
	return registrydata.Default()
}

func (r Router) supportedPlayProtocols() string {
	modernProtocols, err := r.modernProtocols()
	if err != nil {
		return "47 and 340"
	}
	protocols := append([]int32{protocol47, protocol340}, modernProtocols.supportedProtocols()...)
	sort.Slice(protocols, func(i, j int) bool {
		return protocols[i] < protocols[j]
	})
	return formatProtocolRanges(protocols)
}

func formatProtocolRanges(protocols []int32) string {
	if len(protocols) == 0 {
		return ""
	}
	var parts []string
	start := protocols[0]
	prev := protocols[0]
	for _, protocol := range protocols[1:] {
		if protocol == prev || protocol == prev+1 {
			prev = protocol
			continue
		}
		parts = append(parts, formatProtocolRange(start, prev))
		start = protocol
		prev = protocol
	}
	parts = append(parts, formatProtocolRange(start, prev))
	return strings.Join(parts, ", ")
}

func formatProtocolRange(start, end int32) string {
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
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

type loginStartUUIDMode string

const (
	loginStartUUIDNone     loginStartUUIDMode = ""
	loginStartUUIDOptional loginStartUUIDMode = "optional"
	loginStartUUIDRequired loginStartUUIDMode = "required"
)

type loginStartLayout struct {
	signature bool
	uuidMode  loginStartUUIDMode
}

func loginStartLayoutFor(modern bool, cfg modernProtocolConfig) loginStartLayout {
	if !modern {
		return loginStartLayout{}
	}
	return loginStartLayout{
		signature: cfg.loginStartSignature,
		uuidMode:  cfg.loginStartUUID,
	}
}

func readLoginStart(data []byte, layout loginStartLayout) (string, string, error) {
	reader := bytes.NewReader(data)
	username, err := wire.ReadString(reader, 16)
	if err != nil {
		return "", "", err
	}
	if username == "" {
		return "", "", fmt.Errorf("empty username")
	}
	if layout.signature {
		hasSignature, err := readLoginBool(reader)
		if err != nil {
			return "", "", err
		}
		if hasSignature {
			if _, err := wire.ReadLong(reader); err != nil {
				return "", "", err
			}
			if _, err := readLoginByteArray(reader); err != nil {
				return "", "", err
			}
			if _, err := readLoginByteArray(reader); err != nil {
				return "", "", err
			}
		}
	}
	claimedUUID, err := readLoginStartUUID(reader, layout.uuidMode)
	if err != nil {
		return "", "", err
	}
	if reader.Len() != 0 {
		return "", "", fmt.Errorf("login_start has %d trailing bytes", reader.Len())
	}
	return username, claimedUUID, nil
}

func readLoginStartUUID(reader *bytes.Reader, mode loginStartUUIDMode) (string, error) {
	switch mode {
	case loginStartUUIDNone:
		return "", nil
	case loginStartUUIDOptional:
		hasUUID, err := readLoginBool(reader)
		if err != nil {
			return "", err
		}
		if !hasUUID {
			return "", nil
		}
		return readLoginUUID(reader)
	case loginStartUUIDRequired:
		return readLoginUUID(reader)
	default:
		return "", fmt.Errorf("unsupported login_start uuid mode %q", mode)
	}
}

func readLoginBool(reader *bytes.Reader) (bool, error) {
	value, err := reader.ReadByte()
	if err != nil {
		return false, err
	}
	switch value {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("minecraft bool has invalid value %d", value)
	}
}

func readLoginUUID(reader *bytes.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(reader, raw[:]); err != nil {
		return "", err
	}
	id, err := uuid.FromBytes(raw[:])
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func writeLoginDisconnect(conn net.Conn, protocol int32, packetIDs *packetid.Table, message string) error {
	return writeLoginDisconnectComponent(conn, protocol, packetIDs, &component.Text{Content: message})
}

func writeLoginDisconnectComponent(conn net.Conn, protocol int32, packetIDs *packetid.Table, reason component.Component) error {
	var id int32
	var ok bool
	if packetIDs != nil {
		id, ok = packetIDs.ID(protocol, packetid.StateLogin, packetid.ToClient, "disconnect")
	} else {
		id, ok = packetid.ID(protocol, packetid.StateLogin, packetid.ToClient, "disconnect")
	}
	if !ok {
		id = 0
	}
	if reason == nil {
		reason = &component.Text{Content: "Disconnected"}
	}
	payload, err := limbgo.MarshalComponentJSON(protocol, reason)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, string(payload)); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}
