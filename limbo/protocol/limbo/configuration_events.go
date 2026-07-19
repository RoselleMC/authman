package limbo

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/RoselleMC/authman/limbo"
	"github.com/RoselleMC/authman/limbo/dialog"
	"github.com/RoselleMC/authman/limbo/internal/protocol/wire"
	"github.com/RoselleMC/authman/limbo/protocol/packetid"
	"go.minekube.com/common/minecraft/component"
)

type configurationSession struct {
	conn     net.Conn
	player   limbgo.Player
	adapter  playAdapter
	mu       sync.Mutex
	terminal bool
}

func (s *configurationSession) Player() limbgo.Player {
	return s.player
}

func (s *configurationSession) Capabilities() limbgo.SessionCapabilities {
	_, storeCookie := s.adapter.packetID(packetid.StateConfiguration, packetid.ToClient, "store_cookie")
	_, transfer := s.adapter.packetID(packetid.StateConfiguration, packetid.ToClient, "transfer")
	_, disconnect := s.adapter.packetID(packetid.StateConfiguration, packetid.ToClient, "disconnect")
	return limbgo.SessionCapabilities{
		StoreCookie: storeCookie,
		Transfer:    transfer,
		Disconnect:  disconnect,
	}
}

func (s *configurationSession) SendMessage(context.Context, component.Component) error {
	return s.unsupported("system message")
}

func (s *configurationSession) SendActionBar(context.Context, component.Component) error {
	return s.unsupported("action bar")
}

func (s *configurationSession) ShowTitle(context.Context, limbgo.Title) error {
	return s.unsupported("title")
}

func (s *configurationSession) ClearTitle(context.Context, bool) error {
	return s.unsupported("title")
}

func (s *configurationSession) ShowDialog(context.Context, dialog.Dialog) error {
	return s.unsupported("dialog")
}

func (s *configurationSession) ClearDialog(context.Context) error {
	return s.unsupported("dialog")
}

func (s *configurationSession) AddResourcePack(context.Context, limbgo.ResourcePack) error {
	return s.unsupported("resource pack")
}

func (s *configurationSession) RemoveResourcePack(context.Context, string) error {
	return s.unsupported("resource pack")
}

func (s *configurationSession) StoreCookie(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return fmt.Errorf("%w: configuration session is already terminal", limbgo.ErrInvalidSessionControl)
	}
	return writeStoreCookieState(s.conn, s.adapter, packetid.StateConfiguration, key, value)
}

func (s *configurationSession) Transfer(_ context.Context, host string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return fmt.Errorf("%w: configuration session is already terminal", limbgo.ErrInvalidSessionControl)
	}
	if err := writeTransferState(s.conn, s.adapter, packetid.StateConfiguration, host, port); err != nil {
		return err
	}
	s.terminal = true
	return nil
}

func (s *configurationSession) Disconnect(_ context.Context, reason component.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return nil
	}
	err := writeConfigurationDisconnect(s.conn, s.adapter, reason)
	s.terminal = true
	_ = s.conn.Close()
	return err
}

func (s *configurationSession) unsupported(name string) error {
	return fmt.Errorf("%w: %s is unavailable during configuration for protocol %d", limbgo.ErrUnsupportedCapability, name, s.adapter.protocol)
}

func (s *configurationSession) isTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal
}

func serveConfigurationEvent(ctx context.Context, conn net.Conn, services limbgo.SessionServices, player limbgo.Player, cfg modernProtocolConfig) (bool, error) {
	handler, ok := services.Events().(limbgo.ConfigurationEventHandler)
	if !ok {
		return false, nil
	}
	session := &configurationSession{conn: conn, player: player, adapter: newModernPlayAdapter(cfg)}
	if err := handler.HandleConfiguration(ctx, session, &limbgo.ConfigurationEvent{
		Player:   player,
		Protocol: int(cfg.protocol),
	}); err != nil {
		return false, err
	}
	return session.isTerminal(), nil
}

func writeConfigurationDisconnect(conn net.Conn, adapter playAdapter, reason component.Component) error {
	id, ok := adapter.packetID(packetid.StateConfiguration, packetid.ToClient, "disconnect")
	if !ok {
		return fmt.Errorf("%w: configuration disconnect protocol %d", limbgo.ErrUnsupportedCapability, adapter.protocol)
	}
	if reason == nil {
		reason = &component.Text{Content: "Disconnected"}
	}
	raw, err := marshalComponentJSON(adapter, reason)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	if adapter.componentPayloadNBT {
		nbt, nbtErr := componentJSONToAnonymousNBT(raw)
		err = nbtErr
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
