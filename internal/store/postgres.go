package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/node"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	p.pool.Close()
}

func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, postgresSchema)
	return err
}

func (p *Postgres) CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.Player{}, err
	}
	id, err := randomID("player")
	if err != nil {
		return identity.Player{}, err
	}
	player, err := identity.NewOfflinePlayer(id, rawName)
	if err != nil {
		return identity.Player{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.Player{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO players (id, kind, uuid, raw_offline_name, normalized_name, protocol_name, locked)
		VALUES ($1, 'offline', $2, $3, $4, $5, false)
	`, player.ID, player.UUID.String(), player.RawOfflineName, name.Normalized, player.ProtocolName)
	if err != nil {
		return identity.Player{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO offline_credentials (player_id, password_hash)
		VALUES ($1, $2)
	`, player.ID, passwordHash)
	if err != nil {
		return identity.Player{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Player{}, err
	}
	return player, nil
}

func (p *Postgres) GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.Player{}, err
	}
	return p.scanPlayer(p.pool.QueryRow(ctx, `
		SELECT id, kind, uuid, raw_offline_name, normalized_name, protocol_name, locked
		FROM players
		WHERE kind = 'offline' AND normalized_name = $1
	`, name.Normalized))
}

func (p *Postgres) GetPlayerByID(ctx context.Context, id string) (identity.Player, error) {
	return p.scanPlayer(p.pool.QueryRow(ctx, `
		SELECT id, kind, uuid, raw_offline_name, normalized_name, protocol_name, locked
		FROM players
		WHERE id = $1
	`, id))
}

func (p *Postgres) GetOfflineCredential(ctx context.Context, rawName string) (identity.Player, OfflineCredential, error) {
	player, err := p.GetOfflinePlayer(ctx, rawName)
	if err != nil {
		return identity.Player{}, OfflineCredential{}, err
	}
	var credential OfflineCredential
	err = p.pool.QueryRow(ctx, `
		SELECT player_id, password_hash
		FROM offline_credentials
		WHERE player_id = $1
	`, player.ID).Scan(&credential.PlayerID, &credential.PasswordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.Player{}, OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
		}
		return identity.Player{}, OfflineCredential{}, err
	}
	return player, credential, nil
}

func (p *Postgres) ListPlayers(ctx context.Context) []identity.Player {
	rows, err := p.pool.Query(ctx, `
		SELECT id, kind, uuid, raw_offline_name, normalized_name, protocol_name, locked
		FROM players
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	players := make([]identity.Player, 0)
	for rows.Next() {
		player, err := scanPlayerRow(rows)
		if err == nil {
			players = append(players, player)
		}
	}
	return players
}

func (p *Postgres) SetPlayerLocked(ctx context.Context, id string, locked bool) (identity.Player, error) {
	return p.scanPlayer(p.pool.QueryRow(ctx, `
		UPDATE players
		SET locked = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, kind, uuid, raw_offline_name, normalized_name, protocol_name, locked
	`, id, locked))
}

func (p *Postgres) UpdateOfflinePassword(ctx context.Context, id string, passwordHash string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE offline_credentials
		SET password_hash = $2, updated_at = now()
		WHERE player_id = $1
	`, id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("offline credential not found")
	}
	return nil
}

func (p *Postgres) AppendAuditEvent(ctx context.Context, event audit.Event) (audit.Event, error) {
	details, err := json.Marshal(event.Details)
	if err != nil {
		return audit.Event{}, err
	}
	var id int64
	err = p.pool.QueryRow(ctx, `
		INSERT INTO audit_events (occurred_at, actor_type, actor_id, target_type, target_id, event_type, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, event.Occurred, event.ActorType, nullString(event.ActorID), event.Target, nullString(event.TargetID), event.Type, details).Scan(&id)
	if err != nil {
		return audit.Event{}, err
	}
	event.ID = strconv.FormatInt(id, 10)
	return event, nil
}

func (p *Postgres) ListAuditEvents(ctx context.Context, limit int) []audit.Event {
	if limit <= 0 {
		limit = 100
	} else if limit > 5000 {
		limit = 5000
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, occurred_at, actor_type, coalesce(actor_id, ''), target_type, coalesce(target_id, ''), event_type, details
		FROM audit_events
		ORDER BY occurred_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	events := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		var id int64
		var details []byte
		if err := rows.Scan(&id, &event.Occurred, &event.ActorType, &event.ActorID, &event.Target, &event.TargetID, &event.Type, &details); err != nil {
			continue
		}
		event.ID = strconv.FormatInt(id, 10)
		_ = json.Unmarshal(details, &event.Details)
		if event.Details == nil {
			event.Details = map[string]any{}
		}
		events = append(events, event)
	}
	return events
}

func (p *Postgres) Create(ctx context.Context, name string, now time.Time) (node.Node, string, error) {
	if name == "" {
		return node.Node{}, "", fmt.Errorf("node name is required")
	}
	token, err := auth.NewOpaqueToken(32)
	if err != nil {
		return node.Node{}, "", err
	}
	id, err := randomID("node")
	if err != nil {
		return node.Node{}, "", err
	}
	n := node.Node{
		ID:               id,
		Name:             name,
		TokenHash:        auth.HashToken("node", token),
		TokenFingerprint: auth.TokenFingerprint(token),
		CreatedAt:        now.UTC(),
	}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO velocity_nodes (id, name, token_hash, token_fingerprint, disabled, created_at)
		VALUES ($1, $2, $3, $4, false, $5)
		RETURNING id, name, token_hash, token_fingerprint, disabled, created_at, last_heartbeat_at
	`, n.ID, n.Name, n.TokenHash, n.TokenFingerprint, n.CreatedAt).Scan(
		&n.ID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt,
	)
	if err != nil {
		return node.Node{}, "", err
	}
	return n, token, nil
}

func (p *Postgres) Authenticate(ctx context.Context, token string) (node.Node, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, token_hash, token_fingerprint, disabled, created_at, last_heartbeat_at
		FROM velocity_nodes
		WHERE disabled = false
	`)
	if err != nil {
		return node.Node{}, err
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err == nil && auth.ConstantTimeTokenEqual("node", token, n.TokenHash) {
			return n, nil
		}
	}
	return node.Node{}, fmt.Errorf("node token is invalid")
}

func (p *Postgres) Rotate(ctx context.Context, id string, now time.Time) (node.Node, string, error) {
	token, err := auth.NewOpaqueToken(32)
	if err != nil {
		return node.Node{}, "", err
	}
	var n node.Node
	err = p.pool.QueryRow(ctx, `
		UPDATE velocity_nodes
		SET token_hash = $2, token_fingerprint = $3, updated_at = now()
		WHERE id = $1
		RETURNING id, name, token_hash, token_fingerprint, disabled, created_at, last_heartbeat_at
	`, id, auth.HashToken("node", token), auth.TokenFingerprint(token)).Scan(
		&n.ID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return node.Node{}, "", fmt.Errorf("node not found")
	}
	if err != nil {
		return node.Node{}, "", err
	}
	return n, token, nil
}

func (p *Postgres) Heartbeat(ctx context.Context, token string, now time.Time) (node.Node, error) {
	n, err := p.Authenticate(ctx, token)
	if err != nil {
		return node.Node{}, err
	}
	now = now.UTC()
	err = p.pool.QueryRow(ctx, `
		UPDATE velocity_nodes
		SET last_heartbeat_at = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, name, token_hash, token_fingerprint, disabled, created_at, last_heartbeat_at
	`, n.ID, now).Scan(&n.ID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt)
	return n, err
}

func (p *Postgres) List(ctx context.Context) []node.Node {
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, token_hash, token_fingerprint, disabled, created_at, last_heartbeat_at
		FROM velocity_nodes
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	nodes := make([]node.Node, 0)
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (p *Postgres) scanPlayer(row pgx.Row) (identity.Player, error) {
	player, err := scanPlayerRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Player{}, fmt.Errorf("offline player not found: %w", ErrNotFound)
	}
	return player, err
}

type playerScanner interface {
	Scan(dest ...any) error
}

func scanPlayerRow(row playerScanner) (identity.Player, error) {
	var player identity.Player
	var uuidText string
	var kind string
	err := row.Scan(
		&player.ID,
		&kind,
		&uuidText,
		&player.RawOfflineName,
		&player.NormalizedName,
		&player.ProtocolName,
		&player.Locked,
	)
	if err != nil {
		return identity.Player{}, err
	}
	uuid, err := identity.ParseUUID(uuidText)
	if err != nil {
		return identity.Player{}, err
	}
	player.Kind = identity.PlayerKind(kind)
	player.UUID = uuid
	return player, nil
}

func randomID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(raw), nil
}

func scanNodeRow(row playerScanner) (node.Node, error) {
	var n node.Node
	err := row.Scan(&n.ID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt)
	return n, err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS players (
	id text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('premium', 'offline')),
	uuid text NOT NULL UNIQUE,
	premium_uuid text UNIQUE,
	raw_offline_name text,
	normalized_name text,
	protocol_name text NOT NULL,
	locked boolean NOT NULL DEFAULT false,
	registration_server_id text,
	last_seen_server_id text,
	last_seen_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT offline_has_raw_name CHECK (kind != 'offline' OR raw_offline_name IS NOT NULL),
	CONSTRAINT premium_has_no_raw_name CHECK (kind != 'premium' OR raw_offline_name IS NULL),
	CONSTRAINT offline_protocol_marked CHECK (kind != 'offline' OR left(protocol_name, 1) = '#'),
	CONSTRAINT premium_protocol_unmarked CHECK (kind != 'premium' OR left(protocol_name, 1) != '#')
);

CREATE UNIQUE INDEX IF NOT EXISTS players_offline_normalized_unique
	ON players (normalized_name)
	WHERE kind = 'offline';

CREATE TABLE IF NOT EXISTS offline_credentials (
	player_id text PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
	password_hash text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_events (
	id bigserial PRIMARY KEY,
	occurred_at timestamptz NOT NULL DEFAULT now(),
	actor_type text NOT NULL,
	actor_id text,
	target_type text NOT NULL,
	target_id text,
	event_type text NOT NULL,
	details jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS velocity_nodes (
	id text PRIMARY KEY,
	name text NOT NULL,
	token_hash text NOT NULL,
	token_fingerprint text NOT NULL,
	disabled boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	last_heartbeat_at timestamptz
);
`
