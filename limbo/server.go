package limbgo

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Config controls a limbo server instance.
type Config struct {
	Addr           string
	ProtocolRouter ProtocolRouter
	JoinResolver   JoinResolver
	Worlds         WorldProvider
	SpawnResolver  SpawnResolver
	Events         PlayerEventHandler
	ProxyProtocol  ProxyProtocolConfig
	Logger         *slog.Logger
}

// Server is an embeddable limbo server.
type Server struct {
	cfg           Config
	proxyProtocol proxyProtocolRuntime

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	joins    map[string]activeJoin
	wg       sync.WaitGroup
	closed   bool
}

type activeJoin struct {
	player Player
	target JoinTarget
}

// NewServer validates config and creates a server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":25565"
	}
	if cfg.ProtocolRouter == nil {
		return nil, ErrMissingProtocolRouter
	}
	if cfg.JoinResolver == nil && cfg.Worlds == nil {
		return nil, ErrMissingWorldProvider
	}
	if cfg.JoinResolver == nil && cfg.SpawnResolver == nil {
		return nil, ErrMissingSpawnResolver
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	proxyProtocol, err := newProxyProtocolRuntime(cfg.ProxyProtocol)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:           cfg,
		conns:         make(map[net.Conn]struct{}),
		joins:         make(map[string]activeJoin),
		proxyProtocol: proxyProtocol,
	}, nil
}

// ListenAndServe listens on Config.Addr and serves until the context is done or
// the listener fails.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve accepts connections from an existing listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return net.ErrClosed
	}
	s.listener = ln
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		rawConn := conn
		conn, err = s.proxyProtocol.wrap(rawConn)
		if err != nil {
			s.cfg.Logger.Debug("connection rejected", "remote", rawConn.RemoteAddr(), "error", err)
			_ = rawConn.Close()
			continue
		}

		s.trackConn(conn)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrackConn(conn)
			if err := s.cfg.ProtocolRouter.ServeConn(ctx, conn, s); err != nil && ctx.Err() == nil {
				s.cfg.Logger.Debug("connection closed", "remote", conn.RemoteAddr(), "error", err)
			}
		}()
	}
}

// Shutdown closes the listener and active connections, then waits for handlers.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ResolveSpawn implements SessionServices.
func (s *Server) ResolveSpawn(ctx context.Context, player Player) (SpawnTarget, error) {
	if s.cfg.JoinResolver != nil {
		target, err := s.ResolveJoin(ctx, player)
		if err != nil {
			return SpawnTarget{}, err
		}
		return target.Spawn, nil
	}
	return s.cfg.SpawnResolver.ResolveSpawn(ctx, player)
}

// World implements SessionServices.
func (s *Server) World(ctx context.Context, id string) (World, error) {
	return s.cfg.Worlds.World(ctx, id)
}

// ResolveJoin resolves the exact world instance and spawn for a player.
func (s *Server) ResolveJoin(ctx context.Context, player Player) (JoinTarget, error) {
	if s.cfg.JoinResolver == nil {
		spawn, err := s.cfg.SpawnResolver.ResolveSpawn(ctx, player)
		if err != nil {
			return JoinTarget{}, err
		}
		world, err := s.cfg.Worlds.World(ctx, spawn.World)
		if err != nil {
			return JoinTarget{}, err
		}
		return normalizeJoinTarget(JoinTarget{World: world, Spawn: spawn})
	}
	target, err := s.cfg.JoinResolver.ResolveJoin(ctx, player)
	if err != nil {
		return JoinTarget{}, err
	}
	target, err = normalizeJoinTarget(target)
	if err != nil {
		return JoinTarget{}, err
	}
	if _, ok := s.cfg.JoinResolver.(JoinReleaser); ok {
		s.mu.Lock()
		s.joins[remoteAddrKey(player.RemoteAddr)] = activeJoin{player: player, target: target}
		s.mu.Unlock()
	}
	return target, nil
}

// Events implements SessionServices.
func (s *Server) Events() PlayerEventHandler {
	return s.cfg.Events
}

func (s *Server) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return
	}
	s.conns[conn] = struct{}{}
}

func (s *Server) untrackConn(conn net.Conn) {
	_ = conn.Close()
	s.releaseJoin(conn.RemoteAddr())
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Server) releaseJoin(remote net.Addr) {
	releaser, ok := s.cfg.JoinResolver.(JoinReleaser)
	if !ok {
		return
	}
	key := remoteAddrKey(remote)
	s.mu.Lock()
	join, ok := s.joins[key]
	if ok {
		delete(s.joins, key)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	if err := releaser.ReleaseJoin(context.Background(), join.player, join.target); err != nil {
		s.cfg.Logger.Debug("release join", "remote", key, "player", join.player.Name, "error", err)
	}
}

func normalizeJoinTarget(target JoinTarget) (JoinTarget, error) {
	if target.World == nil {
		return JoinTarget{}, ErrMissingWorld
	}
	if target.Spawn.World == "" {
		target.Spawn.World = target.World.ID()
	}
	return target, nil
}

func remoteAddrKey(remote net.Addr) string {
	if remote == nil {
		return ""
	}
	return remote.String()
}

// ShutdownTimeout is a convenience wrapper around Shutdown.
func (s *Server) ShutdownTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Shutdown(ctx)
}
