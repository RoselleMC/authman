package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RoselleMC/authman/internal/audit"
	"github.com/RoselleMC/authman/internal/auth"
	"github.com/RoselleMC/authman/internal/extensions"
	"github.com/RoselleMC/authman/internal/identity"
	"github.com/RoselleMC/authman/internal/mojang"
	"github.com/RoselleMC/authman/internal/node"
	"github.com/RoselleMC/authman/internal/rbac"
	"github.com/go-webauthn/webauthn/webauthn"
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

func (p *Postgres) PremiumNameExists(ctx context.Context, rawName string) bool {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return false
	}
	var exists bool
	err = p.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM players
			WHERE kind = 'premium' AND lower(protocol_name) = $1
		)
	`, name.Normalized).Scan(&exists)
	return err == nil && exists
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
	err = scanOfflineCredentialRow(p.pool.QueryRow(ctx, `
		SELECT player_id, password_hash, updated_at, failed_attempts, locked_until
		FROM offline_credentials
		WHERE player_id = $1
	`, player.ID), &credential)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.Player{}, OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
		}
		return identity.Player{}, OfflineCredential{}, err
	}
	return player, credential, nil
}

func (p *Postgres) RecordOfflineLoginFailure(ctx context.Context, playerID string, now time.Time) (OfflineCredential, error) {
	var credential OfflineCredential
	err := scanOfflineCredentialRow(p.pool.QueryRow(ctx, `
		UPDATE offline_credentials
		SET failed_attempts = failed_attempts + 1,
			locked_until = CASE
				WHEN failed_attempts + 1 >= 5 THEN $2::timestamptz + interval '15 minutes'
				ELSE locked_until
			END,
			updated_at = now()
		WHERE player_id = $1
		RETURNING player_id, password_hash, updated_at, failed_attempts, locked_until
	`, playerID, now.UTC()), &credential)
	if errors.Is(err, pgx.ErrNoRows) {
		return OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	return credential, err
}

func (p *Postgres) RecordOfflineLoginSuccess(ctx context.Context, playerID string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE offline_credentials
		SET failed_attempts = 0,
			locked_until = NULL,
			updated_at = now()
		WHERE player_id = $1
	`, playerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	return nil
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
		SET password_hash = $2,
			failed_attempts = 0,
			locked_until = NULL,
			updated_at = now()
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

func (p *Postgres) SaveSession(ctx context.Context, session auth.Session) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO web_sessions (id, kind, subject_id, csrf_token_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
		SET kind = EXCLUDED.kind,
			subject_id = EXCLUDED.subject_id,
			csrf_token_hash = EXCLUDED.csrf_token_hash,
			expires_at = EXCLUDED.expires_at
	`, session.ID, session.Kind, session.SubjectID, session.CSRFToken, session.CreatedAt, session.ExpiresAt)
	return err
}

func (p *Postgres) GetSession(ctx context.Context, id string) (auth.Session, error) {
	session, err := scanSessionRow(p.pool.QueryRow(ctx, `
		SELECT id, kind, subject_id, csrf_token_hash, created_at, expires_at
		FROM web_sessions
		WHERE id = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, fmt.Errorf("session not found: %w", ErrNotFound)
	}
	return session, err
}

func (p *Postgres) UpdateSession(ctx context.Context, session auth.Session) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE web_sessions
		SET csrf_token_hash = $2, expires_at = $3
		WHERE id = $1
	`, session.ID, session.CSRFToken, session.ExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) DeleteSession(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, id)
	return err
}

func (p *Postgres) SavePortalLink(ctx context.Context, link auth.PortalLink) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO portal_login_links (id, kind, player_id, server_id, token_hash, status, created_at, expires_at, used_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE
		SET status = EXCLUDED.status,
			used_at = EXCLUDED.used_at
	`, link.ID, link.Kind, link.PlayerID, link.ServerID, link.TokenHash, link.Status, link.CreatedAt, link.ExpiresAt, link.UsedAt)
	return err
}

func (p *Postgres) GetPortalLink(ctx context.Context, tokenHash string) (auth.PortalLink, error) {
	link, err := scanPortalLinkRow(p.pool.QueryRow(ctx, `
		SELECT id, kind, player_id, server_id, token_hash, status, created_at, expires_at, used_at
		FROM portal_login_links
		WHERE token_hash = $1
	`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.PortalLink{}, fmt.Errorf("portal link not found: %w", ErrNotFound)
	}
	return link, err
}

func (p *Postgres) MarkPortalLinkUsed(ctx context.Context, tokenHash string, now time.Time) (auth.PortalLink, error) {
	link, err := scanPortalLinkRow(p.pool.QueryRow(ctx, `
		UPDATE portal_login_links
		SET status = $2, used_at = $3
		WHERE token_hash = $1
		RETURNING id, kind, player_id, server_id, token_hash, status, created_at, expires_at, used_at
	`, tokenHash, auth.PortalLinkUsed, now.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.PortalLink{}, fmt.Errorf("portal link not found: %w", ErrNotFound)
	}
	return link, err
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
		ServerID:         "default",
		Name:             name,
		TokenHash:        auth.HashToken("node", token),
		TokenFingerprint: auth.TokenFingerprint(token),
		CreatedAt:        now.UTC(),
	}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO velocity_nodes (id, server_id, name, token_hash, token_fingerprint, disabled, created_at)
		VALUES ($1, $2, $3, $4, $5, false, $6)
		RETURNING id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
	`, n.ID, n.ServerID, n.Name, n.TokenHash, n.TokenFingerprint, n.CreatedAt).Scan(
		&n.ID, &n.ServerID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt,
	)
	if err != nil {
		return node.Node{}, "", err
	}
	return n, token, nil
}

func (p *Postgres) Authenticate(ctx context.Context, token string) (node.Node, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
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
		RETURNING id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
	`, id, auth.HashToken("node", token), auth.TokenFingerprint(token)).Scan(
		&n.ID, &n.ServerID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt,
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
		RETURNING id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
	`, n.ID, now).Scan(&n.ID, &n.ServerID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt)
	return n, err
}

func (p *Postgres) Register(ctx context.Context, registration node.Registration, now time.Time) (node.Node, error) {
	if registration.InstanceFingerprint == "" {
		return node.Node{}, fmt.Errorf("instance fingerprint is required")
	}
	if registration.Name == "" {
		registration.Name = "velocity-" + registration.InstanceFingerprint[:minInt(8, len(registration.InstanceFingerprint))]
	}
	if registration.ServerID == "" {
		registration.ServerID = "default"
	}
	id, err := randomID("node")
	if err != nil {
		return node.Node{}, err
	}
	now = now.UTC()
	var n node.Node
	err = p.pool.QueryRow(ctx, `
		INSERT INTO velocity_nodes (
			id, server_id, name, token_hash, token_fingerprint, instance_fingerprint,
			plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
		)
		VALUES ($1, $2, $3, '', $4, $5, $6, $7, false, $8, $8)
		ON CONFLICT (instance_fingerprint) WHERE instance_fingerprint <> '' DO UPDATE
		SET server_id = EXCLUDED.server_id,
			name = EXCLUDED.name,
			token_fingerprint = EXCLUDED.token_fingerprint,
			plugin_version = EXCLUDED.plugin_version,
			velocity_version = EXCLUDED.velocity_version,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			updated_at = now()
		RETURNING id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
	`, id, registration.ServerID, registration.Name, registration.AccessFingerprint, registration.InstanceFingerprint, registration.PluginVersion, registration.VelocityVersion, now).Scan(
		&n.ID, &n.ServerID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt,
	)
	return n, err
}

func (p *Postgres) Delete(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM velocity_nodes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) List(ctx context.Context) []node.Node {
	rows, err := p.pool.Query(ctx, `
		SELECT id, server_id, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
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

func (p *Postgres) ListMojangRoutes(ctx context.Context) []mojang.Route {
	rows, err := p.pool.Query(ctx, `
		SELECT id, kind, url, weight, disabled
		FROM mojang_routes
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	routes := make([]mojang.Route, 0)
	for rows.Next() {
		route, err := scanMojangRouteRow(rows)
		if err == nil {
			routes = append(routes, route)
		}
	}
	return routes
}

func (p *Postgres) UpsertMojangRoute(ctx context.Context, route mojang.Route) (mojang.Route, error) {
	err := p.pool.QueryRow(ctx, `
		INSERT INTO mojang_routes (id, kind, url, weight, disabled)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET kind = EXCLUDED.kind,
			url = EXCLUDED.url,
			weight = EXCLUDED.weight,
			disabled = EXCLUDED.disabled,
			updated_at = now()
		RETURNING id, kind, url, weight, disabled
	`, route.ID, route.Kind, route.URL, route.Weight, route.Disabled).Scan(
		&route.ID, &route.Kind, &route.URL, &route.Weight, &route.Disabled,
	)
	return route, err
}

func (p *Postgres) DeleteMojangRoute(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM mojang_routes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mojang route not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) ListDownstreamServers(ctx context.Context) []DownstreamServer {
	rows, err := p.pool.Query(ctx, `
		SELECT id, slug, display_name, status, registration_open, portal_theme, portal_config, extension_providers, created_at, updated_at
		FROM downstream_servers
		ORDER BY display_name ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	servers := make([]DownstreamServer, 0)
	for rows.Next() {
		server, err := scanDownstreamServerRow(rows)
		if err == nil {
			servers = append(servers, server)
		}
	}
	return servers
}

func (p *Postgres) GetDownstreamServer(ctx context.Context, idOrSlug string) (DownstreamServer, error) {
	server, err := scanDownstreamServerRow(p.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, status, registration_open, portal_theme, portal_config, extension_providers, created_at, updated_at
		FROM downstream_servers
		WHERE id = $1 OR slug = $1
	`, idOrSlug))
	if errors.Is(err, pgx.ErrNoRows) {
		return DownstreamServer{}, fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	return server, err
}

func (p *Postgres) UpsertDownstreamServer(ctx context.Context, server DownstreamServer) (DownstreamServer, error) {
	server = normalizeDownstreamServer(server)
	if server.ID == "" {
		id, err := randomID("server")
		if err != nil {
			return DownstreamServer{}, err
		}
		server.ID = id
	}
	theme, err := json.Marshal(server.PortalTheme)
	if err != nil {
		return DownstreamServer{}, err
	}
	config, err := json.Marshal(server.PortalConfig)
	if err != nil {
		return DownstreamServer{}, err
	}
	out, err := scanDownstreamServerRow(p.pool.QueryRow(ctx, `
		INSERT INTO downstream_servers (id, slug, display_name, status, registration_open, portal_theme, portal_config, extension_providers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET slug = EXCLUDED.slug,
			display_name = EXCLUDED.display_name,
			status = EXCLUDED.status,
			registration_open = EXCLUDED.registration_open,
			portal_theme = EXCLUDED.portal_theme,
			portal_config = EXCLUDED.portal_config,
			extension_providers = EXCLUDED.extension_providers,
			updated_at = now()
		RETURNING id, slug, display_name, status, registration_open, portal_theme, portal_config, extension_providers, created_at, updated_at
	`, server.ID, server.Slug, server.DisplayName, server.Status, server.RegistrationOpen, theme, config, server.ExtensionProviders))
	if err != nil {
		return DownstreamServer{}, err
	}
	return out, nil
}

func (p *Postgres) DeleteDownstreamServer(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM downstream_servers WHERE id = $1 AND id <> 'default'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) ListExtensionPlayerData(ctx context.Context, playerID string, serverSlug string, includePrivate bool) []ExtensionPlayerData {
	query := `
		SELECT id, server_id, player_id, provider, schema_json, data_json, visibility, source, created_at, updated_at
		FROM extension_player_data
		WHERE player_id = $1
		  AND ($2 = '' OR server_id = $2)
		  AND ($3 OR visibility <> 'private')
		ORDER BY server_id ASC, provider ASC
	`
	rows, err := p.pool.Query(ctx, query, playerID, serverSlug, includePrivate)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]ExtensionPlayerData, 0)
	for rows.Next() {
		data, err := scanExtensionPlayerDataRow(rows)
		if err == nil {
			out = append(out, data)
		}
	}
	return out
}

func (p *Postgres) UpsertExtensionPlayerData(ctx context.Context, data ExtensionPlayerData) (ExtensionPlayerData, error) {
	if data.ID == "" {
		id, err := randomID("extdata")
		if err != nil {
			return ExtensionPlayerData{}, err
		}
		data.ID = id
	}
	if data.Visibility == "" {
		data.Visibility = extensions.VisibilityPlayerVisible
	}
	if data.Source == "" {
		data.Source = "node_api"
	}
	schema, err := json.Marshal(data.Schema)
	if err != nil {
		return ExtensionPlayerData{}, err
	}
	values, err := json.Marshal(data.Values)
	if err != nil {
		return ExtensionPlayerData{}, err
	}
	out, err := scanExtensionPlayerDataRow(p.pool.QueryRow(ctx, `
		INSERT INTO extension_player_data (id, server_id, player_id, provider, schema_json, data_json, visibility, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (server_id, player_id, provider) DO UPDATE
		SET schema_json = EXCLUDED.schema_json,
			data_json = EXCLUDED.data_json,
			visibility = EXCLUDED.visibility,
			source = EXCLUDED.source,
			updated_at = now()
		RETURNING id, server_id, player_id, provider, schema_json, data_json, visibility, source, created_at, updated_at
	`, data.ID, data.ServerID, data.PlayerID, data.Provider, schema, values, data.Visibility, data.Source))
	if err != nil {
		return ExtensionPlayerData{}, err
	}
	return out, nil
}

func (p *Postgres) ListAdminRoles(ctx context.Context) []rbac.Role {
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, description, permissions, system, created_at, updated_at
		FROM admin_roles
		ORDER BY id ASC
	`)
	if err != nil {
		return rbac.DefaultRoles()
	}
	defer rows.Close()
	roles := make([]rbac.Role, 0)
	for rows.Next() {
		var role rbac.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Permissions, &role.System, &role.CreatedAt, &role.UpdatedAt); err == nil {
			role.Permissions = rbac.NormalizePermissions(role.Permissions)
			roles = append(roles, role)
		}
	}
	return rbac.MergeDefaultRoles(roles)
}

func (p *Postgres) ListAdminUsers(ctx context.Context) []AdminUser {
	rows, err := p.pool.Query(ctx, `
		SELECT id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
		FROM admin_users
		ORDER BY username ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		user, err := scanAdminUserRow(rows)
		if err == nil {
			user.PasswordHash = ""
			users = append(users, user)
		}
	}
	return users
}

func (p *Postgres) GetAdminUser(ctx context.Context, id string) (AdminUser, error) {
	user, err := scanAdminUserRow(p.pool.QueryRow(ctx, `
		SELECT id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
		FROM admin_users
		WHERE id = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	return user, err
}

func (p *Postgres) FindAdminUserByIdentifier(ctx context.Context, identifier string) (AdminUser, error) {
	user, err := scanAdminUserRow(p.pool.QueryRow(ctx, `
		SELECT id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
		FROM admin_users
		WHERE lower(username) = lower($1) OR lower(coalesce(email, '')) = lower($1)
	`, strings.TrimSpace(identifier)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	return user, err
}

func (p *Postgres) CreateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error) {
	id, err := randomID("admin")
	if err != nil {
		return AdminUser{}, err
	}
	user.ID = id
	if user.Status == "" {
		user.Status = "active"
	}
	email := any(nil)
	if user.Email != "" {
		email = user.Email
	}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO admin_users (id, username, email, password_hash, role_id, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
	`, user.ID, user.Username, email, user.PasswordHash, user.Role, user.Status).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return AdminUser{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

func (p *Postgres) UpdateAdminUserProfile(ctx context.Context, id string, username string, email string) (AdminUser, error) {
	emailValue := any(nil)
	if email != "" {
		emailValue = email
	}
	user, err := scanAdminUserRow(p.pool.QueryRow(ctx, `
		UPDATE admin_users
		SET username = $2, email = $3, updated_at = now()
		WHERE id = $1
		RETURNING id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
	`, id, username, emailValue))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	if err != nil {
		return AdminUser{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

func (p *Postgres) UpdateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error) {
	emailValue := any(nil)
	if user.Email != "" {
		emailValue = user.Email
	}
	updated, err := scanAdminUserRow(p.pool.QueryRow(ctx, `
		UPDATE admin_users
		SET username = $2, email = $3, role_id = $4, status = $5, updated_at = now()
		WHERE id = $1
		RETURNING id, username, coalesce(email, ''), password_hash, role_id, status, created_at, updated_at
	`, user.ID, user.Username, emailValue, user.Role, user.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	if err != nil {
		return AdminUser{}, err
	}
	updated.PasswordHash = ""
	return updated, nil
}

func (p *Postgres) GetAdminProfile(ctx context.Context, adminID string) (AdminProfile, error) {
	var profile AdminProfile
	err := p.pool.QueryRow(ctx, `
		SELECT admin_id, username, coalesce(email, ''), coalesce(avatar_url, ''), created_at, updated_at
		FROM admin_profiles
		WHERE admin_id = $1
	`, adminID).Scan(
		&profile.AdminID,
		&profile.Username,
		&profile.Email,
		&profile.AvatarURL,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminProfile{}, fmt.Errorf("admin profile not found: %w", ErrNotFound)
	}
	return profile, err
}

func (p *Postgres) UpsertAdminProfile(ctx context.Context, profile AdminProfile) (AdminProfile, error) {
	emailValue := any(nil)
	if profile.Email != "" {
		emailValue = profile.Email
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO admin_profiles (admin_id, username, email, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (admin_id) DO UPDATE SET
			username = EXCLUDED.username,
			email = EXCLUDED.email,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = now()
		RETURNING admin_id, username, coalesce(email, ''), coalesce(avatar_url, ''), created_at, updated_at
	`, profile.AdminID, profile.Username, emailValue, profile.AvatarURL).Scan(
		&profile.AdminID,
		&profile.Username,
		&profile.Email,
		&profile.AvatarURL,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	return profile, err
}

func (p *Postgres) UpsertAdminRole(ctx context.Context, role rbac.Role) (rbac.Role, error) {
	if base, ok := rbac.DefaultRole(role.ID); ok {
		if base.System {
			return rbac.Role{}, fmt.Errorf("system role cannot be modified")
		}
		if role.Name == "" {
			role.Name = base.Name
		}
		if role.Description == "" {
			role.Description = base.Description
		}
	}
	role.Permissions = rbac.NormalizePermissions(role.Permissions)
	if role.ID == "" {
		return rbac.Role{}, fmt.Errorf("role id is required")
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO admin_roles (id, name, description, permissions, system)
		VALUES ($1, $2, $3, $4, false)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			permissions = EXCLUDED.permissions,
			updated_at = now()
		RETURNING id, name, description, permissions, system, created_at, updated_at
	`, role.ID, role.Name, role.Description, role.Permissions).Scan(
		&role.ID,
		&role.Name,
		&role.Description,
		&role.Permissions,
		&role.System,
		&role.CreatedAt,
		&role.UpdatedAt,
	)
	if err != nil {
		return rbac.Role{}, err
	}
	role.Permissions = rbac.NormalizePermissions(role.Permissions)
	return role, nil
}

func (p *Postgres) DeleteAdminRole(ctx context.Context, id string) error {
	id = rbac.RoleID(id)
	if base, ok := rbac.DefaultRole(id); ok && base.System {
		return fmt.Errorf("system role cannot be deleted")
	}
	var assigned bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admin_users WHERE role_id = $1)`, id).Scan(&assigned); err != nil {
		return err
	}
	if assigned {
		return fmt.Errorf("role is still assigned")
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM admin_roles WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) GetAdminSecurity(ctx context.Context, adminID string) (AdminSecurity, error) {
	var security AdminSecurity
	err := p.pool.QueryRow(ctx, `
		SELECT admin_id, totp_enabled, coalesce(totp_secret, ''), mfa_requirement, preferred_locale, preferred_theme, created_at, updated_at
		FROM admin_security
		WHERE admin_id = $1
	`, adminID).Scan(
		&security.AdminID,
		&security.TOTPEnabled,
		&security.TOTPSecret,
		&security.MFARequirement,
		&security.PreferredLocale,
		&security.PreferredTheme,
		&security.CreatedAt,
		&security.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultAdminSecurity(adminID), nil
	}
	return security, err
}

func (p *Postgres) UpsertAdminSecurity(ctx context.Context, security AdminSecurity) (AdminSecurity, error) {
	if security.MFARequirement == "" {
		security.MFARequirement = "new_device"
	}
	if security.PreferredLocale == "" {
		security.PreferredLocale = "system"
	}
	if security.PreferredTheme == "" {
		security.PreferredTheme = "system"
	}
	err := p.pool.QueryRow(ctx, `
		INSERT INTO admin_security (admin_id, totp_enabled, totp_secret, mfa_requirement, preferred_locale, preferred_theme)
		VALUES ($1, $2, nullif($3, ''), $4, $5, $6)
		ON CONFLICT (admin_id) DO UPDATE SET
			totp_enabled = EXCLUDED.totp_enabled,
			totp_secret = EXCLUDED.totp_secret,
			mfa_requirement = EXCLUDED.mfa_requirement,
			preferred_locale = EXCLUDED.preferred_locale,
			preferred_theme = EXCLUDED.preferred_theme,
			updated_at = now()
		RETURNING admin_id, totp_enabled, coalesce(totp_secret, ''), mfa_requirement, preferred_locale, preferred_theme, created_at, updated_at
	`, security.AdminID, security.TOTPEnabled, security.TOTPSecret, security.MFARequirement, security.PreferredLocale, security.PreferredTheme).Scan(
		&security.AdminID,
		&security.TOTPEnabled,
		&security.TOTPSecret,
		&security.MFARequirement,
		&security.PreferredLocale,
		&security.PreferredTheme,
		&security.CreatedAt,
		&security.UpdatedAt,
	)
	return security, err
}

func (p *Postgres) ListAdminPasskeys(ctx context.Context, adminID string) []AdminPasskey {
	rows, err := p.pool.Query(ctx, `
		SELECT id, admin_id, name, credential_json, created_at, last_used_at
		FROM admin_passkeys
		WHERE admin_id = $1
		ORDER BY created_at ASC
	`, adminID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	passkeys := make([]AdminPasskey, 0)
	for rows.Next() {
		passkey, err := scanAdminPasskeyRow(rows)
		if err == nil {
			passkeys = append(passkeys, passkey)
		}
	}
	return passkeys
}

func (p *Postgres) CreateAdminPasskey(ctx context.Context, passkey AdminPasskey) (AdminPasskey, error) {
	id, err := randomID("passkey")
	if err != nil {
		return AdminPasskey{}, err
	}
	passkey.ID = id
	credentialJSON, err := json.Marshal(passkey.Credential)
	if err != nil {
		return AdminPasskey{}, err
	}
	return scanAdminPasskeyRow(p.pool.QueryRow(ctx, `
		INSERT INTO admin_passkeys (id, admin_id, name, credential_id, credential_json)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, admin_id, name, credential_json, created_at, last_used_at
	`, passkey.ID, passkey.AdminID, passkey.Name, passkey.Credential.ID, credentialJSON))
}

func (p *Postgres) UpdateAdminPasskeyCredential(ctx context.Context, id string, credential webauthn.Credential, lastUsedAt time.Time) error {
	credentialJSON, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE admin_passkeys
		SET credential_json = $2, last_used_at = $3
		WHERE id = $1
	`, id, credentialJSON, lastUsedAt.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passkey not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) DeleteAdminPasskey(ctx context.Context, adminID string, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM admin_passkeys WHERE admin_id = $1 AND id = $2`, adminID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passkey not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) SavePendingAdminMFA(ctx context.Context, pending PendingAdminMFA) (PendingAdminMFA, error) {
	id, err := randomID("mfa")
	if err != nil {
		return PendingAdminMFA{}, err
	}
	pending.ID = id
	err = p.pool.QueryRow(ctx, `
		INSERT INTO pending_admin_mfa (id, admin_id, webauthn_session_json, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, admin_id, coalesce(webauthn_session_json, '{}'::jsonb), expires_at
	`, pending.ID, pending.AdminID, optionalJSON(pending.WebAuthnSessionJSON), pending.ExpiresAt).Scan(
		&pending.ID,
		&pending.AdminID,
		&pending.WebAuthnSessionJSON,
		&pending.ExpiresAt,
	)
	return pending, err
}

func (p *Postgres) GetPendingAdminMFA(ctx context.Context, id string) (PendingAdminMFA, error) {
	var pending PendingAdminMFA
	err := p.pool.QueryRow(ctx, `
		SELECT id, admin_id, coalesce(webauthn_session_json, '{}'::jsonb), expires_at
		FROM pending_admin_mfa
		WHERE id = $1
	`, id).Scan(&pending.ID, &pending.AdminID, &pending.WebAuthnSessionJSON, &pending.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingAdminMFA{}, fmt.Errorf("pending mfa not found: %w", ErrNotFound)
	}
	return pending, err
}

func (p *Postgres) DeletePendingAdminMFA(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM pending_admin_mfa WHERE id = $1`, id)
	return err
}

func (p *Postgres) CreateAdminTrustedDevice(ctx context.Context, device AdminTrustedDevice) (AdminTrustedDevice, error) {
	id, err := randomID("trusted")
	if err != nil {
		return AdminTrustedDevice{}, err
	}
	device.ID = id
	err = p.pool.QueryRow(ctx, `
		INSERT INTO admin_trusted_devices (id, admin_id, token_hash, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, admin_id, token_hash, user_agent, created_at, expires_at
	`, device.ID, device.AdminID, device.TokenHash, device.UserAgent, device.ExpiresAt).Scan(
		&device.ID,
		&device.AdminID,
		&device.TokenHash,
		&device.UserAgent,
		&device.CreatedAt,
		&device.ExpiresAt,
	)
	return device, err
}

func (p *Postgres) GetAdminTrustedDevice(ctx context.Context, tokenHash string, now time.Time) (AdminTrustedDevice, error) {
	var device AdminTrustedDevice
	err := p.pool.QueryRow(ctx, `
		SELECT id, admin_id, token_hash, user_agent, created_at, expires_at
		FROM admin_trusted_devices
		WHERE token_hash = $1 AND expires_at > $2
	`, tokenHash, now.UTC()).Scan(
		&device.ID,
		&device.AdminID,
		&device.TokenHash,
		&device.UserAgent,
		&device.CreatedAt,
		&device.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminTrustedDevice{}, fmt.Errorf("trusted device not found: %w", ErrNotFound)
	}
	return device, err
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

func scanOfflineCredentialRow(row playerScanner, credential *OfflineCredential) error {
	var updatedAt time.Time
	var lockedUntil *time.Time
	if err := row.Scan(&credential.PlayerID, &credential.PasswordHash, &updatedAt, &credential.FailedAttempts, &lockedUntil); err != nil {
		return err
	}
	credential.PasswordUpdatedAt = &updatedAt
	credential.LockedUntil = lockedUntil
	return nil
}

func scanAdminUserRow(row playerScanner) (AdminUser, error) {
	var user AdminUser
	err := row.Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}

func scanAdminPasskeyRow(row playerScanner) (AdminPasskey, error) {
	var passkey AdminPasskey
	var credentialJSON []byte
	err := row.Scan(
		&passkey.ID,
		&passkey.AdminID,
		&passkey.Name,
		&credentialJSON,
		&passkey.CreatedAt,
		&passkey.LastUsedAt,
	)
	if err != nil {
		return AdminPasskey{}, err
	}
	if err := json.Unmarshal(credentialJSON, &passkey.Credential); err != nil {
		return AdminPasskey{}, err
	}
	return passkey, nil
}

func optionalJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(raw)
}

func randomID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(raw), nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func scanNodeRow(row playerScanner) (node.Node, error) {
	var n node.Node
	err := row.Scan(&n.ID, &n.ServerID, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &n.CreatedAt, &n.LastHeartbeatAt)
	return n, err
}

func scanMojangRouteRow(row playerScanner) (mojang.Route, error) {
	var route mojang.Route
	err := row.Scan(&route.ID, &route.Kind, &route.URL, &route.Weight, &route.Disabled)
	return route, err
}

func scanDownstreamServerRow(row playerScanner) (DownstreamServer, error) {
	var server DownstreamServer
	var themeRaw []byte
	var configRaw []byte
	err := row.Scan(
		&server.ID,
		&server.Slug,
		&server.DisplayName,
		&server.Status,
		&server.RegistrationOpen,
		&themeRaw,
		&configRaw,
		&server.ExtensionProviders,
		&server.CreatedAt,
		&server.UpdatedAt,
	)
	if err != nil {
		return DownstreamServer{}, err
	}
	if len(themeRaw) > 0 {
		if err := json.Unmarshal(themeRaw, &server.PortalTheme); err != nil {
			return DownstreamServer{}, err
		}
	}
	if len(configRaw) > 0 {
		if err := json.Unmarshal(configRaw, &server.PortalConfig); err != nil {
			return DownstreamServer{}, err
		}
	}
	return normalizeDownstreamServer(server), nil
}

func scanExtensionPlayerDataRow(row playerScanner) (ExtensionPlayerData, error) {
	var data ExtensionPlayerData
	var schemaRaw []byte
	var valuesRaw []byte
	var visibility string
	err := row.Scan(
		&data.ID,
		&data.ServerID,
		&data.PlayerID,
		&data.Provider,
		&schemaRaw,
		&valuesRaw,
		&visibility,
		&data.Source,
		&data.CreatedAt,
		&data.UpdatedAt,
	)
	if err != nil {
		return ExtensionPlayerData{}, err
	}
	data.Visibility = extensions.Visibility(visibility)
	if err := json.Unmarshal(schemaRaw, &data.Schema); err != nil {
		return ExtensionPlayerData{}, err
	}
	if err := json.Unmarshal(valuesRaw, &data.Values); err != nil {
		return ExtensionPlayerData{}, err
	}
	return data, nil
}

func scanSessionRow(row playerScanner) (auth.Session, error) {
	var session auth.Session
	var kind string
	err := row.Scan(
		&session.ID,
		&kind,
		&session.SubjectID,
		&session.CSRFToken,
		&session.CreatedAt,
		&session.ExpiresAt,
	)
	session.Kind = auth.SessionKind(kind)
	return session, err
}

func scanPortalLinkRow(row playerScanner) (auth.PortalLink, error) {
	var link auth.PortalLink
	var kind string
	var status string
	err := row.Scan(
		&link.ID,
		&kind,
		&link.PlayerID,
		&link.ServerID,
		&link.TokenHash,
		&status,
		&link.CreatedAt,
		&link.ExpiresAt,
		&link.UsedAt,
	)
	link.Kind = auth.PortalLinkKind(kind)
	link.Status = auth.PortalLinkStatus(status)
	return link, err
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
	failed_attempts integer NOT NULL DEFAULT 0,
	locked_until timestamptz,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE offline_credentials ADD COLUMN IF NOT EXISTS failed_attempts integer NOT NULL DEFAULT 0;
ALTER TABLE offline_credentials ADD COLUMN IF NOT EXISTS locked_until timestamptz;

CREATE TABLE IF NOT EXISTS web_sessions (
	id text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('admin', 'player')),
	subject_id text NOT NULL,
	csrf_token_hash text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS web_sessions_expires_at_idx ON web_sessions (expires_at);

CREATE TABLE IF NOT EXISTS portal_login_links (
	id text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('premium', 'offline')),
	player_id text NOT NULL,
	server_id text,
	token_hash text NOT NULL UNIQUE,
	status text NOT NULL CHECK (status IN ('active', 'used', 'revoked')),
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL,
	used_at timestamptz
);

CREATE INDEX IF NOT EXISTS portal_login_links_token_hash_idx ON portal_login_links (token_hash);
CREATE INDEX IF NOT EXISTS portal_login_links_expires_at_idx ON portal_login_links (expires_at);

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

CREATE TABLE IF NOT EXISTS admin_roles (
	id text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	permissions text[] NOT NULL DEFAULT ARRAY[]::text[],
	system boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_users (
	id text PRIMARY KEY,
	username text NOT NULL UNIQUE,
	email text UNIQUE,
	password_hash text NOT NULL,
	role_id text NOT NULL,
	status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_profiles (
	admin_id text PRIMARY KEY,
	username text NOT NULL,
	email text,
	avatar_url text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_security (
	admin_id text PRIMARY KEY,
	totp_enabled boolean NOT NULL DEFAULT false,
	totp_secret text,
	mfa_requirement text NOT NULL DEFAULT 'new_device' CHECK (mfa_requirement IN ('new_device', 'always')),
	preferred_locale text NOT NULL DEFAULT 'system',
	preferred_theme text NOT NULL DEFAULT 'system',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_passkeys (
	id text PRIMARY KEY,
	admin_id text NOT NULL,
	name text NOT NULL,
	credential_id bytea NOT NULL UNIQUE,
	credential_json jsonb NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	last_used_at timestamptz
);

CREATE INDEX IF NOT EXISTS admin_passkeys_admin_id_idx ON admin_passkeys (admin_id);

CREATE TABLE IF NOT EXISTS pending_admin_mfa (
	id text PRIMARY KEY,
	admin_id text NOT NULL,
	webauthn_session_json jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS pending_admin_mfa_expires_at_idx ON pending_admin_mfa (expires_at);

CREATE TABLE IF NOT EXISTS admin_trusted_devices (
	id text PRIMARY KEY,
	admin_id text NOT NULL,
	token_hash text NOT NULL UNIQUE,
	user_agent text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS admin_trusted_devices_admin_id_idx ON admin_trusted_devices (admin_id);
CREATE INDEX IF NOT EXISTS admin_trusted_devices_expires_at_idx ON admin_trusted_devices (expires_at);

CREATE TABLE IF NOT EXISTS velocity_nodes (
	id text PRIMARY KEY,
	server_id text NOT NULL DEFAULT 'default',
	name text NOT NULL,
	token_hash text NOT NULL,
	token_fingerprint text NOT NULL,
	disabled boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	last_heartbeat_at timestamptz
);

ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS server_id text NOT NULL DEFAULT 'default';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS instance_fingerprint text NOT NULL DEFAULT '';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS plugin_version text NOT NULL DEFAULT '';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS velocity_version text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS velocity_nodes_instance_fingerprint_unique
	ON velocity_nodes (instance_fingerprint)
	WHERE instance_fingerprint <> '';

CREATE TABLE IF NOT EXISTS mojang_routes (
	id text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('http', 'socks5')),
	url text NOT NULL,
	weight integer NOT NULL DEFAULT 1,
	disabled boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS downstream_servers (
	id text PRIMARY KEY,
	slug text NOT NULL UNIQUE,
	display_name text NOT NULL,
	status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'hidden', 'disabled')),
	registration_open boolean NOT NULL DEFAULT true,
	portal_theme jsonb NOT NULL DEFAULT '{}'::jsonb,
	portal_config jsonb NOT NULL DEFAULT '{}'::jsonb,
	extension_providers text[] NOT NULL DEFAULT ARRAY[]::text[],
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO downstream_servers (
	id,
	slug,
	display_name,
	status,
	registration_open,
	portal_theme,
	portal_config,
	extension_providers
)
VALUES (
	'default',
	'default',
	'Default Server',
	'active',
	true,
	'{"primary_color":"#16a34a","accent_color":"#2563eb","portal_message":"Welcome to Authman","display_name":"Default Server","description":"Default Authman downstream context"}'::jsonb,
	'{"registration_strategy":"open","show_in_global":true}'::jsonb,
	ARRAY['authman.identity']::text[]
)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS extension_player_data (
	id text PRIMARY KEY,
	server_id text NOT NULL,
	player_id text NOT NULL REFERENCES players(id) ON DELETE CASCADE,
	provider text NOT NULL,
	schema_json jsonb NOT NULL,
	data_json jsonb NOT NULL,
	visibility text NOT NULL DEFAULT 'player_visible' CHECK (visibility IN ('private', 'player_visible', 'public')),
	source text NOT NULL DEFAULT 'node_api',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (server_id, player_id, provider)
);

CREATE INDEX IF NOT EXISTS extension_player_data_player_idx ON extension_player_data (player_id);
`
