package limbo

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/dialog"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
	"github.com/google/uuid"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec"
)

const keepAliveInterval = 10 * time.Second

type playAdapter struct {
	protocol                       int32
	packetProtocol                 int32
	packetIDs                      *packetid.Table
	componentPayloadNBT            bool
	componentLegacySchema          bool
	componentChangePageString      bool
	componentHoverEntityIDIntArray bool
	componentHoverEntityTypeKey    bool
	componentDefaultItemQuantity   bool
	systemChatOverlayVarInt        bool
	legacyChatSenderUUID           bool
	resourcePackRequiredPrompt     bool
	portalDialog                   bool
}

func newPlayAdapter(protocol int32) playAdapter {
	return playAdapter{
		protocol:                       protocol,
		packetProtocol:                 protocol,
		componentPayloadNBT:            protocol >= protocol765,
		componentLegacySchema:          protocol < protocol770,
		componentChangePageString:      protocol < protocol771,
		componentHoverEntityIDIntArray: protocol >= protocol764,
		componentHoverEntityTypeKey:    protocol < protocol770,
		componentDefaultItemQuantity:   protocol >= protocol766,
		systemChatOverlayVarInt:        protocol == protocol759,
		legacyChatSenderUUID:           protocol >= protocol757,
		resourcePackRequiredPrompt:     protocol >= protocol757,
		portalDialog:                   protocol >= protocol771,
	}
}

func newModernPlayAdapter(cfg modernProtocolConfig) playAdapter {
	return playAdapter{
		protocol:                       cfg.protocol,
		packetProtocol:                 cfg.packetProtocol(),
		packetIDs:                      cfg.packetIDs,
		componentPayloadNBT:            cfg.componentPayloadNBT,
		componentLegacySchema:          cfg.componentLegacySchema,
		componentChangePageString:      cfg.componentChangePageString,
		componentHoverEntityIDIntArray: cfg.componentHoverEntityIDIntArray,
		componentHoverEntityTypeKey:    cfg.componentHoverEntityTypeKey,
		componentDefaultItemQuantity:   cfg.componentDefaultItemQuantity,
		systemChatOverlayVarInt:        cfg.systemChatOverlayVarInt,
		legacyChatSenderUUID:           cfg.legacyChatSenderUUID,
		resourcePackRequiredPrompt:     cfg.resourcePackRequiredPrompt,
		portalDialog:                   cfg.portalDialog,
	}
}

func (a playAdapter) packetID(state packetid.State, direction packetid.Direction, name string) (int32, bool) {
	if a.packetIDs != nil {
		return a.packetIDs.ID(a.packetProtocol, state, direction, name)
	}
	return packetid.ID(a.packetProtocol, state, direction, name)
}

type playSession struct {
	conn                    net.Conn
	player                  limbgo.Player
	adapter                 playAdapter
	mu                      sync.Mutex
	closed                  bool
	resourcePacks           map[string]limbgo.ResourcePack
	resourcePackProtocolIDs map[string]string
	lastResourcePackID      string
}

func (s *playSession) Player() limbgo.Player {
	return s.player
}

func (s *playSession) Capabilities() limbgo.SessionCapabilities {
	return sessionCapabilities(s.adapter)
}

func (s *playSession) SendMessage(_ context.Context, message component.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeSystemMessage(s.conn, s.adapter, message)
}

func (s *playSession) SendActionBar(_ context.Context, message component.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeActionBar(s.conn, s.adapter, message)
}

func (s *playSession) ShowTitle(_ context.Context, title limbgo.Title) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeTitle(s.conn, s.adapter, title)
}

func (s *playSession) ClearTitle(_ context.Context, reset bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeClearTitle(s.conn, s.adapter, reset)
}

func (s *playSession) ShowDialog(_ context.Context, dialog dialog.Dialog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeShowDialog(s.conn, s.adapter, dialog)
}

func (s *playSession) ClearDialog(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeClearDialog(s.conn, s.adapter)
}

func (s *playSession) AddResourcePack(_ context.Context, pack limbgo.ResourcePack) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeResourcePack(s.conn, s.adapter, pack); err != nil {
		return err
	}
	s.rememberResourcePack(pack)
	return nil
}

func (s *playSession) RemoveResourcePack(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeRemoveResourcePack(s.conn, s.adapter, id); err != nil {
		return err
	}
	s.forgetResourcePack(id)
	return nil
}

func (s *playSession) StoreCookie(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeStoreCookie(s.conn, s.adapter, key, value)
}

func (s *playSession) Transfer(_ context.Context, host string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeTransfer(s.conn, s.adapter, host, port)
}

func (s *playSession) Disconnect(_ context.Context, reason component.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := writeKickDisconnect(s.conn, s.adapter, reason)
	s.closed = true
	_ = s.conn.Close()
	return err
}

func (s *playSession) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *playSession) rememberResourcePack(pack limbgo.ResourcePack) {
	if s.resourcePacks == nil {
		s.resourcePacks = map[string]limbgo.ResourcePack{}
	}
	if s.resourcePackProtocolIDs == nil {
		s.resourcePackProtocolIDs = map[string]string{}
	}
	s.resourcePacks[pack.ID] = pack
	s.resourcePackProtocolIDs[resourcePackProtocolUUID(pack.ID)] = pack.ID
	if pack.Hash != "" {
		s.resourcePackProtocolIDs[pack.Hash] = pack.ID
	}
	s.lastResourcePackID = pack.ID
}

func (s *playSession) forgetResourcePack(id string) {
	pack, ok := s.resourcePacks[id]
	delete(s.resourcePacks, id)
	delete(s.resourcePackProtocolIDs, resourcePackProtocolUUID(id))
	if ok && pack.Hash != "" {
		delete(s.resourcePackProtocolIDs, pack.Hash)
	}
	s.lastResourcePackID = ""
	for remaining := range s.resourcePacks {
		s.lastResourcePackID = remaining
		break
	}
}

func (s *playSession) readResourcePackResponse(data []byte) (*limbgo.ResourcePackResponseEvent, error) {
	reader := bytes.NewReader(data)
	protocolID := ""
	if s.adapter.protocol == protocol47 {
		hash, err := wire.ReadString(reader, 40)
		if err != nil {
			return nil, err
		}
		protocolID = hash
	} else if hasModernResourcePackID(s.adapter) {
		id, err := readPacketUUID(reader)
		if err != nil {
			return nil, err
		}
		protocolID = id
	}
	rawStatus, err := wire.ReadVarInt(reader)
	if err != nil {
		return nil, err
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("resource_pack_receive has %d trailing bytes", reader.Len())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.lastResourcePackID
	if protocolID != "" {
		if mapped, ok := s.resourcePackProtocolIDs[protocolID]; ok {
			id = mapped
		} else {
			id = protocolID
		}
	}
	return &limbgo.ResourcePackResponseEvent{
		Player:     s.player,
		ID:         id,
		Pack:       s.resourcePacks[id],
		Status:     resourcePackStatus(rawStatus),
		StatusCode: rawStatus,
		Protocol:   int(s.adapter.protocol),
	}, nil
}

func sessionCapabilities(adapter playAdapter) limbgo.SessionCapabilities {
	_, hasDialog := adapter.packetID(packetid.StatePlay, packetid.ToClient, "show_dialog")
	hasDialog = hasDialog && adapter.portalDialog
	hasResourcePack := hasClientboundPlayPacket(adapter, "add_resource_pack") || hasClientboundPlayPacket(adapter, "resource_pack_send")
	hasRemoveResourcePack := hasClientboundPlayPacket(adapter, "remove_resource_pack")
	_, hasStoreCookie := adapter.packetID(packetid.StatePlay, packetid.ToClient, "store_cookie")
	_, hasTransfer := adapter.packetID(packetid.StatePlay, packetid.ToClient, "transfer")
	_, hasDisconnect := adapter.packetID(packetid.StatePlay, packetid.ToClient, "kick_disconnect")
	_, hasSystemMessage := adapter.packetID(packetid.StatePlay, packetid.ToClient, "system_chat")
	if !hasSystemMessage {
		_, hasSystemMessage = adapter.packetID(packetid.StatePlay, packetid.ToClient, "chat")
	}
	_, hasActionBar := adapter.packetID(packetid.StatePlay, packetid.ToClient, "action_bar")
	if !hasActionBar && adapter.protocol >= protocol340 {
		_, hasActionBar = adapter.packetID(packetid.StatePlay, packetid.ToClient, "title")
	}
	_, hasTitle := adapter.packetID(packetid.StatePlay, packetid.ToClient, "set_title_text")
	if !hasTitle {
		_, hasTitle = adapter.packetID(packetid.StatePlay, packetid.ToClient, "title")
	}
	return limbgo.SessionCapabilities{
		SystemMessage:      hasSystemMessage,
		ActionBar:          hasActionBar,
		Title:              hasTitle,
		Dialog:             hasDialog,
		ResourcePack:       hasResourcePack,
		RemoveResourcePack: hasRemoveResourcePack,
		StoreCookie:        hasStoreCookie,
		Transfer:           hasTransfer,
		Disconnect:         hasDisconnect,
	}
}

func servePlayEvents(ctx context.Context, conn net.Conn, reader *bufio.Reader, services limbgo.SessionServices, player limbgo.Player, adapter playAdapter) error {
	handler := services.Events()
	if handler == nil {
		return nil
	}
	session := &playSession{conn: conn, player: player, adapter: adapter}
	keepAliveCtx, stopKeepAlive := context.WithCancel(ctx)
	defer stopKeepAlive()
	go keepAliveLoop(keepAliveCtx, session)
	if err := handler.HandleJoin(ctx, session, &limbgo.JoinEvent{
		Player:   player,
		Protocol: int(adapter.protocol),
	}); err != nil {
		return err
	}
	if session.Closed() {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		packet, err := wire.ReadPacket(reader, 0)
		if err != nil {
			if isClosedPlayConnError(err) {
				return nil
			}
			return err
		}
		if err := handlePlayPacket(ctx, handler, session, packet); err != nil {
			return err
		}
		if session.Closed() {
			return nil
		}
	}
}

func keepAliveLoop(ctx context.Context, session *playSession) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if session.Closed() {
				return
			}
			if err := session.sendKeepAlive(now.UnixMilli()); err != nil {
				return
			}
		}
	}
}

func (s *playSession) sendKeepAlive(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := writeKeepAlive(s.conn, s.adapter, id); err != nil {
		s.closed = true
		_ = s.conn.Close()
		return err
	}
	return nil
}

func isClosedPlayConnError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed)
}

func handlePlayPacket(ctx context.Context, handler limbgo.PlayerEventHandler, session *playSession, packet wire.Packet) error {
	adapter := session.adapter
	switch {
	case isServerboundPlayPacket(adapter, packet.ID, "chat"):
		message, err := readFirstPacketString(packet.Data)
		if err != nil {
			return err
		}
		if strings.HasPrefix(message, "/") {
			return handler.HandleCommand(ctx, session, &limbgo.CommandEvent{
				Player:   session.player,
				Command:  strings.TrimPrefix(message, "/"),
				Protocol: int(adapter.protocol),
			})
		}
		return handler.HandleChat(ctx, session, &limbgo.ChatEvent{
			Player:   session.player,
			Message:  message,
			Protocol: int(adapter.protocol),
		})
	case isServerboundPlayPacket(adapter, packet.ID, "chat_message"):
		message, err := readFirstPacketString(packet.Data)
		if err != nil {
			return err
		}
		return handler.HandleChat(ctx, session, &limbgo.ChatEvent{
			Player:   session.player,
			Message:  message,
			Protocol: int(adapter.protocol),
		})
	case isServerboundPlayPacket(adapter, packet.ID, "chat_command"), isServerboundPlayPacket(adapter, packet.ID, "chat_command_signed"):
		command, err := readFirstPacketString(packet.Data)
		if err != nil {
			return err
		}
		return handler.HandleCommand(ctx, session, &limbgo.CommandEvent{
			Player:   session.player,
			Command:  strings.TrimPrefix(command, "/"),
			Protocol: int(adapter.protocol),
		})
	case isServerboundPlayPacket(adapter, packet.ID, "custom_click_action"):
		event, err := readCustomClickAction(adapter, session.player, packet.Data)
		if err != nil {
			return err
		}
		return handler.HandleDialogClick(ctx, session, event)
	case isServerboundPlayPacket(adapter, packet.ID, "resource_pack_receive"):
		event, err := session.readResourcePackResponse(packet.Data)
		if err != nil {
			return err
		}
		if resourceHandler, ok := handler.(limbgo.ResourcePackResponseHandler); ok {
			return resourceHandler.HandleResourcePackResponse(ctx, session, event)
		}
		return nil
	default:
		return nil
	}
}

func isServerboundPlayPacket(adapter playAdapter, got int32, name string) bool {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToServer, name)
	return ok && got == id
}

func hasClientboundPlayPacket(adapter playAdapter, name string) bool {
	_, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, name)
	return ok
}

func hasModernResourcePackID(adapter playAdapter) bool {
	return hasClientboundPlayPacket(adapter, "add_resource_pack")
}

func readPacketUUID(reader *bytes.Reader) (string, error) {
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

func resourcePackProtocolUUID(id string) string {
	if parsed, err := uuid.Parse(id); err == nil {
		return parsed.String()
	}
	sum := sha1.Sum([]byte("limbgo:resource-pack:" + id))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	var out uuid.UUID
	copy(out[:], sum[:])
	return out.String()
}

func resourcePackStatus(status int32) limbgo.ResourcePackStatus {
	switch status {
	case 0:
		return limbgo.ResourcePackSuccessfullyLoaded
	case 1:
		return limbgo.ResourcePackDeclined
	case 2:
		return limbgo.ResourcePackFailedDownload
	case 3:
		return limbgo.ResourcePackAccepted
	case 4:
		return limbgo.ResourcePackDownloaded
	case 5:
		return limbgo.ResourcePackInvalidURL
	case 6:
		return limbgo.ResourcePackFailedReload
	case 7:
		return limbgo.ResourcePackDiscarded
	default:
		return limbgo.ResourcePackStatus(fmt.Sprintf("unknown_%d", status))
	}
}

func writeKeepAlive(conn net.Conn, adapter playAdapter, id int64) error {
	packetID, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "keep_alive")
	if !ok {
		return nil
	}
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(id))
	return wire.WritePacket(conn, wire.Packet{ID: packetID, Data: data[:]})
}

func readFirstPacketString(data []byte) (string, error) {
	return wire.ReadString(bytes.NewReader(data), 256)
}

func readCustomClickAction(adapter playAdapter, player limbgo.Player, data []byte) (*limbgo.DialogClickEvent, error) {
	reader := bytes.NewReader(data)
	id, err := wire.ReadString(reader, 32767)
	if err != nil {
		return nil, err
	}
	payload, err := readOptionalAnonymousNBT(reader)
	if err != nil {
		return nil, err
	}
	return &limbgo.DialogClickEvent{
		Player:   player,
		ID:       id,
		Payload:  payload,
		Protocol: int(adapter.protocol),
	}, nil
}

func readOptionalAnonymousNBT(reader *bytes.Reader) ([]byte, error) {
	if reader.Len() == 0 {
		return nil, nil
	}
	present, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	payload := make([]byte, reader.Len())
	_, _ = reader.Read(payload)
	return payload, nil
}

func writeSystemMessage(conn net.Conn, adapter playAdapter, message component.Component) error {
	if message == nil {
		message = &component.Text{}
	}
	if adapter.componentPayloadNBT {
		return writeSystemMessageNBT(conn, adapter, message)
	}
	raw, err := marshalComponentJSON(adapter, message)
	if err != nil {
		return err
	}
	if id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "system_chat"); ok {
		var data bytes.Buffer
		if err := wire.WriteString(&data, string(raw)); err != nil {
			return err
		}
		if adapter.systemChatOverlayVarInt {
			if err := wire.WriteVarInt(&data, 1); err != nil {
				return err
			}
		} else {
			if err := wire.WriteBool(&data, false); err != nil {
				return err
			}
		}
		return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
	}
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "chat")
	if !ok {
		return fmt.Errorf("missing chat/system_chat packet id for protocol %d", adapter.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, string(raw)); err != nil {
		return err
	}
	if err := wire.WriteByte(&data, 0); err != nil {
		return err
	}
	if adapter.legacyChatSenderUUID {
		data.Write(make([]byte, 16))
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeSystemMessageNBT(conn net.Conn, adapter playAdapter, message component.Component) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "system_chat")
	if !ok {
		return fmt.Errorf("missing system_chat packet id for protocol %d", adapter.protocol)
	}
	raw, err := marshalComponentJSON(adapter, message)
	if err != nil {
		return err
	}
	nbt, err := componentJSONToAnonymousNBT(raw)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if _, err := data.Write(nbt); err != nil {
		return err
	}
	if err := wire.WriteBool(&data, false); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeActionBar(conn net.Conn, adapter playAdapter, message component.Component) error {
	if _, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "action_bar"); ok {
		return writeComponentPacket(conn, adapter, "action_bar", message)
	}
	if adapter.protocol >= protocol340 {
		return writeLegacyTitleText(conn, adapter, 2, message)
	}
	return fmt.Errorf("%w: actionbar protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
}

func writeTitle(conn net.Conn, adapter playAdapter, title limbgo.Title) error {
	if title.Title == nil && title.Subtitle == nil && title.Times == nil {
		return fmt.Errorf("%w: title requires text or timings", limbgo.ErrInvalidSessionControl)
	}
	if _, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "set_title_text"); ok {
		if title.Times != nil {
			if err := writeTitleTimesPacket(conn, adapter, "set_title_time", title.Times); err != nil {
				return err
			}
		}
		if title.Title != nil {
			if err := writeComponentPacket(conn, adapter, "set_title_text", title.Title); err != nil {
				return err
			}
		}
		if title.Subtitle != nil {
			if err := writeComponentPacket(conn, adapter, "set_title_subtitle", title.Subtitle); err != nil {
				return err
			}
		}
		return nil
	}
	if _, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "title"); !ok {
		return fmt.Errorf("%w: title protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	if title.Times != nil {
		action := int32(2)
		if adapter.protocol >= protocol340 {
			action = 3
		}
		if err := writeLegacyTitleTimes(conn, adapter, action, title.Times); err != nil {
			return err
		}
	}
	if title.Title != nil {
		if err := writeLegacyTitleText(conn, adapter, 0, title.Title); err != nil {
			return err
		}
	}
	if title.Subtitle != nil {
		if err := writeLegacyTitleText(conn, adapter, 1, title.Subtitle); err != nil {
			return err
		}
	}
	return nil
}

func writeClearTitle(conn net.Conn, adapter playAdapter, reset bool) error {
	if id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "clear_titles"); ok {
		var data bytes.Buffer
		if err := wire.WriteBool(&data, reset); err != nil {
			return err
		}
		return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
	}
	if _, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "title"); !ok {
		return fmt.Errorf("%w: clear title protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	action := int32(3)
	if reset {
		action = 4
	}
	if adapter.protocol >= protocol340 {
		action = 4
		if reset {
			action = 5
		}
	}
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, action); err != nil {
		return err
	}
	id, _ := adapter.packetID(packetid.StatePlay, packetid.ToClient, "title")
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeComponentPacket(conn net.Conn, adapter playAdapter, name string, message component.Component) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, name)
	if !ok {
		return fmt.Errorf("%w: %s protocol %d", limbgo.ErrUnsupportedCapability, name, adapter.protocol)
	}
	var data bytes.Buffer
	if err := writeComponentPayload(&data, adapter, message); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeComponentPayload(data *bytes.Buffer, adapter playAdapter, message component.Component) error {
	if message == nil {
		message = &component.Text{}
	}
	raw, err := marshalComponentJSON(adapter, message)
	if err != nil {
		return err
	}
	if adapter.componentPayloadNBT {
		nbt, err := componentJSONToAnonymousNBT(raw)
		if err != nil {
			return err
		}
		_, err = data.Write(nbt)
		return err
	}
	return wire.WriteString(data, string(raw))
}

func writeTitleTimesPacket(conn net.Conn, adapter playAdapter, name string, times *limbgo.TitleTimes) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, name)
	if !ok {
		return fmt.Errorf("%w: %s protocol %d", limbgo.ErrUnsupportedCapability, name, adapter.protocol)
	}
	var data bytes.Buffer
	if err := writeTitleTimesPayload(&data, times); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeLegacyTitleText(conn net.Conn, adapter playAdapter, action int32, message component.Component) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "title")
	if !ok {
		return fmt.Errorf("%w: title protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, action); err != nil {
		return err
	}
	if err := writeComponentPayload(&data, adapter, message); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeLegacyTitleTimes(conn net.Conn, adapter playAdapter, action int32, times *limbgo.TitleTimes) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "title")
	if !ok {
		return fmt.Errorf("%w: title protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, action); err != nil {
		return err
	}
	if err := writeTitleTimesPayload(&data, times); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeTitleTimesPayload(data *bytes.Buffer, times *limbgo.TitleTimes) error {
	if times == nil {
		times = &limbgo.TitleTimes{}
	}
	if err := wire.WriteInt(data, times.FadeInTicks); err != nil {
		return err
	}
	if err := wire.WriteInt(data, times.StayTicks); err != nil {
		return err
	}
	return wire.WriteInt(data, times.FadeOutTicks)
}

func writeShowDialog(conn net.Conn, adapter playAdapter, value dialog.Dialog) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "show_dialog")
	if !ok {
		return fmt.Errorf("show_dialog is not available for protocol %d", adapter.protocol)
	}
	if value == nil {
		return fmt.Errorf("show_dialog requires a dialog")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	nbt, err := componentJSONToAnonymousNBT(raw)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if err := wire.WriteVarInt(&data, 0); err != nil {
		return err
	}
	if _, err := data.Write(nbt); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeClearDialog(conn net.Conn, adapter playAdapter) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "clear_dialog")
	if !ok {
		return fmt.Errorf("clear_dialog is not available for protocol %d", adapter.protocol)
	}
	return wire.WritePacket(conn, wire.Packet{ID: id})
}

func writeResourcePack(conn net.Conn, adapter playAdapter, pack limbgo.ResourcePack) error {
	if pack.ID == "" {
		return fmt.Errorf("%w: resource pack id is required", limbgo.ErrInvalidSessionControl)
	}
	if pack.URL == "" {
		return fmt.Errorf("%w: resource pack url is required", limbgo.ErrInvalidSessionControl)
	}
	if id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "add_resource_pack"); ok {
		var data bytes.Buffer
		if err := writeUUID(&data, resourcePackProtocolUUID(pack.ID)); err != nil {
			return err
		}
		if err := wire.WriteString(&data, pack.URL); err != nil {
			return err
		}
		if err := wire.WriteString(&data, pack.Hash); err != nil {
			return err
		}
		if err := wire.WriteBool(&data, pack.Required); err != nil {
			return err
		}
		if err := writeOptionalResourcePackPrompt(&data, adapter, pack.Prompt); err != nil {
			return err
		}
		return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
	}
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "resource_pack_send")
	if !ok {
		return fmt.Errorf("%w: resource pack protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, pack.URL); err != nil {
		return err
	}
	if err := wire.WriteString(&data, pack.Hash); err != nil {
		return err
	}
	if adapter.resourcePackRequiredPrompt {
		if err := wire.WriteBool(&data, pack.Required); err != nil {
			return err
		}
		if err := writeOptionalResourcePackPromptJSON(&data, adapter, pack.Prompt); err != nil {
			return err
		}
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeRemoveResourcePack(conn net.Conn, adapter playAdapter, id string) error {
	if id == "" {
		return fmt.Errorf("%w: resource pack id is required", limbgo.ErrInvalidSessionControl)
	}
	packetID, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "remove_resource_pack")
	if !ok {
		return fmt.Errorf("%w: remove_resource_pack protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	var data bytes.Buffer
	if err := wire.WriteBool(&data, true); err != nil {
		return err
	}
	if err := writeUUID(&data, resourcePackProtocolUUID(id)); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: packetID, Data: data.Bytes()})
}

func writeOptionalResourcePackPrompt(data *bytes.Buffer, adapter playAdapter, prompt component.Component) error {
	if prompt == nil {
		return wire.WriteBool(data, false)
	}
	if err := wire.WriteBool(data, true); err != nil {
		return err
	}
	raw, err := marshalComponentJSON(adapter, prompt)
	if err != nil {
		return err
	}
	nbt, err := componentJSONToAnonymousNBT(raw)
	if err != nil {
		return err
	}
	_, err = data.Write(nbt)
	return err
}

func writeOptionalResourcePackPromptJSON(data *bytes.Buffer, adapter playAdapter, prompt component.Component) error {
	if prompt == nil {
		return wire.WriteBool(data, false)
	}
	if err := wire.WriteBool(data, true); err != nil {
		return err
	}
	raw, err := marshalComponentJSON(adapter, prompt)
	if err != nil {
		return err
	}
	return wire.WriteString(data, string(raw))
}

func writeStoreCookie(conn net.Conn, adapter playAdapter, key string, value []byte) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "store_cookie")
	if !ok {
		return fmt.Errorf("%w: store_cookie protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	if key == "" {
		return fmt.Errorf("%w: cookie key is required", limbgo.ErrInvalidSessionControl)
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, key); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, int32(len(value))); err != nil {
		return err
	}
	if _, err := data.Write(value); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeTransfer(conn net.Conn, adapter playAdapter, host string, port int) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "transfer")
	if !ok {
		return fmt.Errorf("%w: transfer protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	if host == "" {
		return fmt.Errorf("%w: transfer host is required", limbgo.ErrInvalidSessionControl)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%w: transfer port %d outside 1-65535", limbgo.ErrInvalidSessionControl, port)
	}
	var data bytes.Buffer
	if err := wire.WriteString(&data, host); err != nil {
		return err
	}
	if err := wire.WriteVarInt(&data, int32(port)); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func writeKickDisconnect(conn net.Conn, adapter playAdapter, reason component.Component) error {
	id, ok := adapter.packetID(packetid.StatePlay, packetid.ToClient, "kick_disconnect")
	if !ok {
		return fmt.Errorf("%w: disconnect protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	if reason == nil {
		reason = &component.Text{}
	}
	raw, err := marshalComponentJSON(adapter, reason)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if adapter.componentPayloadNBT {
		nbt, err := componentJSONToAnonymousNBT(raw)
		if err != nil {
			return err
		}
		if _, err := data.Write(nbt); err != nil {
			return err
		}
	} else if err := wire.WriteString(&data, string(raw)); err != nil {
		return err
	}
	return wire.WritePacket(conn, wire.Packet{ID: id, Data: data.Bytes()})
}

func marshalComponentJSON(adapter playAdapter, message component.Component) ([]byte, error) {
	var out bytes.Buffer
	encoder := codec.Json{
		UseLegacyFieldNames:                     adapter.componentLegacySchema,
		UseLegacyClickEventStructure:            adapter.componentLegacySchema,
		UseLegacyHoverEventStructure:            adapter.componentLegacySchema,
		EmitChangePageClickEventPageAsString:    adapter.componentChangePageString,
		EmitCompactTextComponent:                false,
		EmitHoverShowEntityIdAsIntArray:         adapter.componentHoverEntityIDIntArray,
		EmitHoverShowEntityKeyAsTypeAndUuidAsId: adapter.componentHoverEntityTypeKey,
		EmitDefaultItemHoverQuantity:            adapter.componentDefaultItemQuantity,
		NoDownsampleColor:                       true,
		StdJson:                                 true,
	}
	if err := encoder.Marshal(&out, message); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func componentJSONToAnonymousNBT(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writeAnonymousJSONNBT(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeAnonymousJSONNBT(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		if err := out.WriteByte(nbtCompound); err != nil {
			return err
		}
		for key, child := range typed {
			if err := writeNamedJSONNBT(out, key, child); err != nil {
				return err
			}
		}
		return out.WriteByte(nbtEnd)
	case []any:
		return writeAnonymousJSONListNBT(out, typed)
	case string:
		if err := out.WriteByte(nbtString); err != nil {
			return err
		}
		writeRawNBTString(out, typed)
		return nil
	case bool:
		if err := out.WriteByte(nbtByte); err != nil {
			return err
		}
		if typed {
			return out.WriteByte(1)
		}
		return out.WriteByte(0)
	case json.Number:
		tag, err := jsonNumberNBTTag(typed)
		if err != nil {
			return err
		}
		if err := out.WriteByte(tag); err != nil {
			return err
		}
		return writeJSONNumberNBTPayload(out, tag, typed)
	default:
		if err := out.WriteByte(nbtCompound); err != nil {
			return err
		}
		return out.WriteByte(nbtEnd)
	}
}

func writeNamedJSONNBT(out *bytes.Buffer, name string, value any) error {
	tag := jsonNBTTag(value)
	if err := out.WriteByte(tag); err != nil {
		return err
	}
	writeRawNBTString(out, name)
	return writeJSONNBTPayload(out, tag, value)
}

func writeJSONNBTPayload(out *bytes.Buffer, tag byte, value any) error {
	switch tag {
	case nbtCompound:
		object, _ := value.(map[string]any)
		for key, child := range object {
			if err := writeNamedJSONNBT(out, key, child); err != nil {
				return err
			}
		}
		return out.WriteByte(nbtEnd)
	case nbtList:
		values, _ := value.([]any)
		return writeJSONListPayload(out, values)
	case nbtString:
		text, _ := value.(string)
		writeRawNBTString(out, text)
	case nbtByte:
		boolean, _ := value.(bool)
		if boolean {
			return out.WriteByte(1)
		}
		return out.WriteByte(0)
	case nbtInt:
		number, _ := value.(json.Number)
		integer, err := number.Int64()
		if err != nil {
			return err
		}
		writeRawNBTInt(out, int32(integer))
	case nbtFloat:
		number, _ := value.(json.Number)
		floatValue, err := number.Float64()
		if err != nil {
			return err
		}
		writeRawNBTFloat(out, float32(floatValue))
	}
	return nil
}

func writeAnonymousJSONListNBT(out *bytes.Buffer, values []any) error {
	if err := out.WriteByte(nbtList); err != nil {
		return err
	}
	return writeJSONListPayload(out, values)
}

func writeJSONListPayload(out *bytes.Buffer, values []any) error {
	childTag := byte(nbtEnd)
	if len(values) > 0 {
		childTag = jsonNBTTag(values[0])
	}
	if err := out.WriteByte(childTag); err != nil {
		return err
	}
	writeRawNBTInt(out, int32(len(values)))
	for _, value := range values {
		if err := writeJSONNBTPayload(out, childTag, value); err != nil {
			return err
		}
	}
	return nil
}

func jsonNBTTag(value any) byte {
	switch value.(type) {
	case map[string]any:
		return nbtCompound
	case []any:
		return nbtList
	case bool:
		return nbtByte
	case json.Number:
		tag, err := jsonNumberNBTTag(value.(json.Number))
		if err == nil {
			return tag
		}
		return nbtString
	default:
		return nbtString
	}
}

func jsonNumberNBTTag(value json.Number) (byte, error) {
	text := value.String()
	if strings.ContainsAny(text, ".eE") {
		if _, err := value.Float64(); err != nil {
			return 0, err
		}
		return nbtFloat, nil
	}
	if _, err := value.Int64(); err != nil {
		return 0, err
	}
	return nbtInt, nil
}

func writeJSONNumberNBTPayload(out *bytes.Buffer, tag byte, value json.Number) error {
	switch tag {
	case nbtInt:
		integer, err := value.Int64()
		if err != nil {
			return err
		}
		writeRawNBTInt(out, int32(integer))
	case nbtFloat:
		floatValue, err := value.Float64()
		if err != nil {
			return err
		}
		writeRawNBTFloat(out, float32(floatValue))
	default:
		return fmt.Errorf("unsupported json number nbt tag %d", tag)
	}
	return nil
}

func writeRawNBTString(out *bytes.Buffer, value string) {
	_ = wire.WriteUnsignedShort(out, uint16(len(value)))
	_, _ = out.WriteString(value)
}

func writeRawNBTInt(out *bytes.Buffer, value int32) {
	_ = wire.WriteInt(out, value)
}

func writeRawNBTFloat(out *bytes.Buffer, value float32) {
	_ = wire.WriteInt(out, int32(math.Float32bits(value)))
}
