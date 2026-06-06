package store

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/identity"
)

type OfflineCredential struct {
	PlayerID     string
	PasswordHash string
}

type Memory struct {
	mu                  sync.RWMutex
	nextID              int
	nextAuditID         int
	playersByID         map[string]identity.Player
	offlineByNormalized map[string]string
	credentialsByPlayer map[string]OfflineCredential
	auditEvents         []audit.Event
}

func NewMemory() *Memory {
	return &Memory{
		playersByID:         make(map[string]identity.Player),
		offlineByNormalized: make(map[string]string),
		credentialsByPlayer: make(map[string]OfflineCredential),
	}
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
		PlayerID:     id,
		PasswordHash: passwordHash,
	}
	return nil
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
