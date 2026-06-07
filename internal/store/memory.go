package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/mojang"
)

type Memory struct {
	mu                  sync.RWMutex
	nextID              int
	nextAuditID         int
	playersByID         map[string]identity.Player
	offlineByNormalized map[string]string
	credentialsByPlayer map[string]OfflineCredential
	sessionsByID        map[string]auth.Session
	portalLinksByToken  map[string]auth.PortalLink
	auditEvents         []audit.Event
	mojangRoutes        map[string]mojang.Route
	downstreamServers   map[string]DownstreamServer
	extensionData       map[string]ExtensionPlayerData
}

func NewMemory() *Memory {
	m := &Memory{
		playersByID:         make(map[string]identity.Player),
		offlineByNormalized: make(map[string]string),
		credentialsByPlayer: make(map[string]OfflineCredential),
		sessionsByID:        make(map[string]auth.Session),
		portalLinksByToken:  make(map[string]auth.PortalLink),
		mojangRoutes:        make(map[string]mojang.Route),
		downstreamServers:   make(map[string]DownstreamServer),
		extensionData:       make(map[string]ExtensionPlayerData),
	}
	server := defaultDownstreamServer(time.Now().UTC())
	m.downstreamServers[server.ID] = server
	return m
}

func (m *Memory) CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.Player{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.offlineByNormalized[name.Normalized]; ok {
		return identity.Player{}, fmt.Errorf("offline player already exists")
	}
	m.nextID++
	player, err := identity.NewOfflinePlayer(fmt.Sprintf("player-%d", m.nextID), rawName)
	if err != nil {
		return identity.Player{}, err
	}
	m.playersByID[player.ID] = player
	m.offlineByNormalized[name.Normalized] = player.ID
	m.credentialsByPlayer[player.ID] = OfflineCredential{
		PlayerID:     player.ID,
		PasswordHash: passwordHash,
	}
	return player, nil
}

func (m *Memory) GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.Player{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.offlineByNormalized[name.Normalized]
	if !ok {
		return identity.Player{}, fmt.Errorf("offline player not found: %w", ErrNotFound)
	}
	return m.playersByID[id], nil
}

func (m *Memory) PremiumNameExists(ctx context.Context, rawName string) bool {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, player := range m.playersByID {
		if player.Kind == identity.PlayerKindPremium && strings.EqualFold(player.ProtocolName, name.Normalized) {
			return true
		}
	}
	return false
}

func (m *Memory) GetOfflineCredential(ctx context.Context, rawName string) (identity.Player, OfflineCredential, error) {
	player, err := m.GetOfflinePlayer(ctx, rawName)
	if err != nil {
		return identity.Player{}, OfflineCredential{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	credential, ok := m.credentialsByPlayer[player.ID]
	if !ok {
		return identity.Player{}, OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	return player, credential, nil
}

func (m *Memory) GetPlayerByID(ctx context.Context, id string) (identity.Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	player, ok := m.playersByID[id]
	if !ok {
		return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
	}
	return player, nil
}

func (m *Memory) ListPlayers(ctx context.Context) []identity.Player {
	m.mu.RLock()
	defer m.mu.RUnlock()
	players := make([]identity.Player, 0, len(m.playersByID))
	for _, player := range m.playersByID {
		players = append(players, player)
	}
	return players
}

func (m *Memory) SetPlayerLocked(ctx context.Context, id string, locked bool) (identity.Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	player, ok := m.playersByID[id]
	if !ok {
		return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
	}
	player.Locked = locked
	m.playersByID[id] = player
	return player, nil
}

func (m *Memory) UpdateOfflinePassword(ctx context.Context, id string, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	player, ok := m.playersByID[id]
	if !ok {
		return fmt.Errorf("player not found: %w", ErrNotFound)
	}
	if player.Kind != identity.PlayerKindOffline {
		return fmt.Errorf("player is not offline")
	}
	m.credentialsByPlayer[id] = OfflineCredential{
		PlayerID:          id,
		PasswordHash:      passwordHash,
		PasswordUpdatedAt: ptrTime(time.Now().UTC()),
	}
	return nil
}

func (m *Memory) RecordOfflineLoginFailure(ctx context.Context, playerID string, now time.Time) (OfflineCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentialsByPlayer[playerID]
	if !ok {
		return OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	credential.FailedAttempts++
	if credential.FailedAttempts >= 5 {
		lockedUntil := now.UTC().Add(15 * time.Minute)
		credential.LockedUntil = &lockedUntil
	}
	m.credentialsByPlayer[playerID] = credential
	return credential, nil
}

func (m *Memory) RecordOfflineLoginSuccess(ctx context.Context, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentialsByPlayer[playerID]
	if !ok {
		return fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	credential.FailedAttempts = 0
	credential.LockedUntil = nil
	m.credentialsByPlayer[playerID] = credential
	return nil
}

func (m *Memory) SaveSession(ctx context.Context, session auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionsByID[session.ID] = session
	return nil
}

func (m *Memory) GetSession(ctx context.Context, id string) (auth.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessionsByID[id]
	if !ok {
		return auth.Session{}, fmt.Errorf("session not found: %w", ErrNotFound)
	}
	return session, nil
}

func (m *Memory) UpdateSession(ctx context.Context, session auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessionsByID[session.ID]; !ok {
		return fmt.Errorf("session not found: %w", ErrNotFound)
	}
	m.sessionsByID[session.ID] = session
	return nil
}

func (m *Memory) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessionsByID, id)
	return nil
}

func (m *Memory) SavePortalLink(ctx context.Context, link auth.PortalLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.portalLinksByToken[link.TokenHash] = link
	return nil
}

func (m *Memory) GetPortalLink(ctx context.Context, tokenHash string) (auth.PortalLink, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	link, ok := m.portalLinksByToken[tokenHash]
	if !ok {
		return auth.PortalLink{}, fmt.Errorf("portal link not found: %w", ErrNotFound)
	}
	return link, nil
}

func (m *Memory) MarkPortalLinkUsed(ctx context.Context, tokenHash string, now time.Time) (auth.PortalLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, ok := m.portalLinksByToken[tokenHash]
	if !ok {
		return auth.PortalLink{}, fmt.Errorf("portal link not found: %w", ErrNotFound)
	}
	now = now.UTC()
	link.Status = auth.PortalLinkUsed
	link.UsedAt = &now
	m.portalLinksByToken[tokenHash] = link
	return link, nil
}

func (m *Memory) AppendAuditEvent(ctx context.Context, event audit.Event) (audit.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextAuditID++
	event.ID = "audit-" + strconv.Itoa(m.nextAuditID)
	m.auditEvents = append(m.auditEvents, event)
	return event, nil
}

func (m *Memory) ListAuditEvents(ctx context.Context, limit int) []audit.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	} else if limit > 5000 {
		limit = 5000
	}
	n := len(m.auditEvents)
	if n < limit {
		limit = n
	}
	events := make([]audit.Event, 0, limit)
	for i := n - 1; i >= 0 && len(events) < limit; i-- {
		events = append(events, m.auditEvents[i])
	}
	return events
}

func (m *Memory) ListMojangRoutes(ctx context.Context) []mojang.Route {
	m.mu.RLock()
	defer m.mu.RUnlock()
	routes := make([]mojang.Route, 0, len(m.mojangRoutes))
	for _, route := range m.mojangRoutes {
		routes = append(routes, route)
	}
	return routes
}

func (m *Memory) UpsertMojangRoute(ctx context.Context, route mojang.Route) (mojang.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mojangRoutes[route.ID] = route
	return route, nil
}

func (m *Memory) DeleteMojangRoute(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mojangRoutes[id]; !ok {
		return fmt.Errorf("mojang route not found: %w", ErrNotFound)
	}
	delete(m.mojangRoutes, id)
	return nil
}

func (m *Memory) ListDownstreamServers(ctx context.Context) []DownstreamServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	servers := make([]DownstreamServer, 0, len(m.downstreamServers))
	for _, server := range m.downstreamServers {
		servers = append(servers, cloneDownstreamServer(server))
	}
	return servers
}

func (m *Memory) GetDownstreamServer(ctx context.Context, idOrSlug string) (DownstreamServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, server := range m.downstreamServers {
		if server.ID == idOrSlug || server.Slug == idOrSlug {
			return cloneDownstreamServer(server), nil
		}
	}
	return DownstreamServer{}, fmt.Errorf("downstream server not found: %w", ErrNotFound)
}

func (m *Memory) UpsertDownstreamServer(ctx context.Context, server DownstreamServer) (DownstreamServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if server.ID == "" {
		m.nextID++
		server.ID = "server-" + strconv.Itoa(m.nextID)
		server.CreatedAt = now
	} else if existing, ok := m.downstreamServers[server.ID]; ok && !existing.CreatedAt.IsZero() {
		server.CreatedAt = existing.CreatedAt
	}
	if server.CreatedAt.IsZero() {
		server.CreatedAt = now
	}
	server.UpdatedAt = now
	server = normalizeDownstreamServer(server)
	m.downstreamServers[server.ID] = server
	return cloneDownstreamServer(server), nil
}

func (m *Memory) DeleteDownstreamServer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.downstreamServers[id]; !ok {
		return fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	delete(m.downstreamServers, id)
	return nil
}

func (m *Memory) ListExtensionPlayerData(ctx context.Context, playerID string, serverSlug string, includePrivate bool) []ExtensionPlayerData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows := make([]ExtensionPlayerData, 0)
	for _, row := range m.extensionData {
		if row.PlayerID != playerID {
			continue
		}
		if serverSlug != "" && row.ServerID != serverSlug {
			continue
		}
		if !includePrivate && row.Visibility == "private" {
			continue
		}
		rows = append(rows, cloneExtensionPlayerData(row))
	}
	return rows
}

func (m *Memory) UpsertExtensionPlayerData(ctx context.Context, data ExtensionPlayerData) (ExtensionPlayerData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if data.ID == "" {
		data.ID = data.ServerID + ":" + data.PlayerID + ":" + data.Provider
	}
	if existing, ok := m.extensionData[data.ID]; ok && !existing.CreatedAt.IsZero() {
		data.CreatedAt = existing.CreatedAt
	}
	if data.CreatedAt.IsZero() {
		data.CreatedAt = now
	}
	data.UpdatedAt = now
	if data.Visibility == "" {
		data.Visibility = "player_visible"
	}
	if data.Source == "" {
		data.Source = "node_api"
	}
	m.extensionData[data.ID] = data
	return cloneExtensionPlayerData(data), nil
}
