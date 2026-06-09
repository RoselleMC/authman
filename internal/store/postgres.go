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

const passportSelectColumns = "uuid, kind, uuid, username, username_normalized, raw_offline_name, status, registration_server_id, last_seen_server_id, last_seen_at, last_seen_ip, last_seen_geo, created_at, updated_at"
const profileSelectColumns = "uuid, uuid, protocol_name, normalized_name, display_name, status, skin_source, profile_properties, created_from_passport_id, last_seen_server_id, last_seen_at, last_seen_ip, last_seen_geo, created_at, updated_at"
const nodeSelectColumns = "id, server_id, mode, name, token_hash, token_fingerprint, instance_fingerprint, plugin_version, velocity_version, disabled, runtime_config, created_at, last_heartbeat_at"
const limboBlueprintSelectColumns = "id, name, description, filename, content_type, size_bytes, sha256, schematic, preview, config, created_at, updated_at"
const profileSkinSelectColumns = "profile_id, model, skin_png, skin_content_type, skin_sha256, COALESCE(cape_png, ''::bytea), COALESCE(cape_content_type, ''), COALESCE(cape_sha256, ''), COALESCE(elytra_png, ''::bytea), COALESCE(elytra_content_type, ''), COALESCE(elytra_sha256, ''), created_at, updated_at"

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

func uniqueProtocolCandidate(raw string, attempt int) (identity.OfflineName, error) {
	name, err := identity.NormalizeProtocolName(raw)
	if err != nil {
		return identity.OfflineName{}, err
	}
	if attempt <= 1 {
		return name, nil
	}
	suffix := "_" + strconv.Itoa(attempt)
	limit := identity.OfflineNameMaxLength - len(suffix)
	if limit < identity.OfflineNameMinLength {
		return identity.OfflineName{}, fmt.Errorf("profile protocol name cannot be made unique")
	}
	base := name.Protocol
	if len(base) > limit {
		base = base[:limit]
	}
	return identity.NormalizeProtocolName(base + suffix)
}

func uniqueProtocolNameTx(ctx context.Context, tx pgx.Tx, raw string) (identity.OfflineName, error) {
	for attempt := 1; attempt <= 9999; attempt++ {
		name, err := uniqueProtocolCandidate(raw, attempt)
		if err != nil {
			return identity.OfflineName{}, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM profiles WHERE normalized_name = $1)
		`, name.Normalized).Scan(&exists); err != nil {
			return identity.OfflineName{}, err
		}
		if !exists {
			return name, nil
		}
	}
	return identity.OfflineName{}, fmt.Errorf("profile protocol name has no available unique variant")
}

func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, postgresSchema)
	return err
}

func (p *Postgres) CreateOfflinePassportProfile(ctx context.Context, rawName string, protocolName string, passwordHash string) (identity.PassportProfile, error) {
	if strings.TrimSpace(protocolName) == "" {
		protocolName = rawName
	}
	passport, err := identity.NewOfflinePassport("", rawName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO passports (uuid, kind, username, username_normalized, raw_offline_name, status, created_at, updated_at)
		VALUES ($1, 'offline', $2, $3, $4, $5, $6, $7)
	`, passport.UUID.String(), passport.Username, passport.UsernameNormalized, passport.RawOfflineName, passport.Status, passport.CreatedAt, passport.UpdatedAt); err != nil {
		return identity.PassportProfile{}, err
	}
	uniqueName, err := uniqueProtocolNameTx(ctx, tx, protocolName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	profile, err := identity.NewOfflineProfile("", uniqueName.Protocol, passport.ID)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	propsJSON, err := json.Marshal(profile.ProfileProperties)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profiles (uuid, protocol_name, normalized_name, display_name, status, skin_source, profile_properties, created_from_passport_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
	`, profile.UUID.String(), profile.ProtocolName, profile.NormalizedName, profile.DisplayName, profile.Status, profile.SkinSource, string(propsJSON), passport.ID, profile.CreatedAt, profile.UpdatedAt); err != nil {
		return identity.PassportProfile{}, err
	}
	link := identity.ProfilePassportLink{ProfileID: profile.ID, PassportID: passport.ID, IsPrimary: true, LinkedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profile_passport_links (profile_id, passport_id, is_primary, linked_at)
		VALUES ($1, $2, true, $3)
	`, link.ProfileID, link.PassportID, link.LinkedAt); err != nil {
		return identity.PassportProfile{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO offline_passport_credentials (passport_id, password_hash)
		VALUES ($1, $2)
	`, passport.ID, passwordHash); err != nil {
		return identity.PassportProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.PassportProfile{}, err
	}
	return identity.PassportProfile{Passport: passport, Profile: profile, Link: link}, nil
}

func (p *Postgres) UpsertPremiumPassportProfile(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.PassportProfile, error) {
	protocolName := strings.TrimSpace(name)
	if protocolName == "" {
		return identity.PassportProfile{}, fmt.Errorf("premium name is required")
	}
	propsJSON, err := json.Marshal(properties)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	passport := identity.NewPremiumPassport("", protocolName, uuid)
	var profile identity.Profile
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	defer tx.Rollback(ctx)
	passport, err = scanPassportRow(tx.QueryRow(ctx, `
		INSERT INTO passports (uuid, kind, username, username_normalized, status, created_at, updated_at)
		VALUES ($1, 'premium', $2, $3, 'active', $4, $5)
		ON CONFLICT (uuid) DO UPDATE
		SET username = EXCLUDED.username,
			username_normalized = EXCLUDED.username_normalized,
			updated_at = now()
		RETURNING `+passportSelectColumns+`
	`, passport.UUID.String(), passport.Username, strings.ToLower(passport.Username), passport.CreatedAt, passport.UpdatedAt))
	if err != nil {
		return identity.PassportProfile{}, err
	}
	existingProfile, profileErr := scanProfileRow(tx.QueryRow(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles p
		JOIN profile_passport_links l ON l.profile_id = p.uuid
		WHERE l.passport_id = $1 AND l.is_primary = true
		LIMIT 1
	`, passport.ID))
	if profileErr == nil {
		profile = existingProfile
		profile, err = scanProfileRow(tx.QueryRow(ctx, `
			UPDATE profiles
			SET skin_source = 'mojang',
				profile_properties = $2::jsonb,
				updated_at = now()
			WHERE uuid = $1
			RETURNING `+profileSelectColumns+`
		`, profile.ID, string(propsJSON)))
		if err != nil {
			return identity.PassportProfile{}, err
		}
	} else {
		uniqueName, err := uniqueProtocolNameTx(ctx, tx, protocolName)
		if err != nil {
			return identity.PassportProfile{}, err
		}
		profile, err = identity.NewPremiumProfile("", uniqueName.Protocol, uuid, properties, passport.ID)
		if err != nil {
			return identity.PassportProfile{}, err
		}
		profile, err = scanProfileRow(tx.QueryRow(ctx, `
			INSERT INTO profiles (uuid, protocol_name, normalized_name, display_name, status, skin_source, profile_properties, created_from_passport_id)
			VALUES ($1, $2, $3, $4, 'active', 'mojang', $5::jsonb, $6)
			RETURNING `+profileSelectColumns+`
		`, profile.UUID.String(), profile.ProtocolName, profile.NormalizedName, profile.DisplayName, string(propsJSON), passport.ID))
		if err != nil {
			return identity.PassportProfile{}, err
		}
	}
	link := identity.ProfilePassportLink{ProfileID: profile.ID, PassportID: passport.ID, IsPrimary: true, LinkedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `
		UPDATE profile_passport_links SET is_primary = false WHERE passport_id = $1
	`, passport.ID); err != nil {
		return identity.PassportProfile{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profile_passport_links (profile_id, passport_id, is_primary, linked_at)
		VALUES ($1, $2, true, $3)
		ON CONFLICT (profile_id) DO UPDATE
		SET passport_id = EXCLUDED.passport_id,
			is_primary = true
	`, link.ProfileID, link.PassportID, link.LinkedAt); err != nil {
		return identity.PassportProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.PassportProfile{}, err
	}
	return identity.PassportProfile{Passport: passport, Profile: profile, Link: link}, nil
}

func (p *Postgres) CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error) {
	pp, err := p.CreateOfflinePassportProfile(ctx, rawName, rawName, passwordHash)
	if err == nil {
		return identity.PlayerFromPassportProfileLink(pp), nil
	}
	return identity.Player{}, err
}

func (p *Postgres) UpsertPremiumPlayer(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.Player, error) {
	pp, err := p.UpsertPremiumPassportProfile(ctx, name, uuid, properties)
	if err == nil {
		return identity.PlayerFromPassportProfileLink(pp), nil
	}
	return identity.Player{}, err
}

func (p *Postgres) GetPassportByID(ctx context.Context, id string) (identity.Passport, error) {
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		SELECT `+passportSelectColumns+`
		FROM passports
		WHERE uuid = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, err
}

func (p *Postgres) GetPassportByUsername(ctx context.Context, username string) (identity.Passport, error) {
	name, err := identity.NormalizeOfflineName(username)
	if err != nil {
		return identity.Passport{}, err
	}
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		SELECT `+passportSelectColumns+`
		FROM passports
		WHERE kind = 'offline' AND username_normalized = $1
	`, name.Normalized))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, err
}

func (p *Postgres) GetProfileByID(ctx context.Context, id string) (identity.Profile, error) {
	profile, err := scanProfileRow(p.pool.QueryRow(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles
		WHERE uuid = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return profile, err
}

func (p *Postgres) GetProfileByProtocolName(ctx context.Context, protocolName string) (identity.Profile, error) {
	name := strings.ToLower(strings.TrimSpace(protocolName))
	if name == "" {
		return identity.Profile{}, fmt.Errorf("protocol name is required")
	}
	profile, err := scanProfileRow(p.pool.QueryRow(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles
		WHERE lower(protocol_name) = $1
	`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return profile, err
}

func (p *Postgres) GetPassportForProfile(ctx context.Context, profileID string) (identity.Passport, error) {
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		SELECT `+passportSelectColumns+`
		FROM passports p
		JOIN profile_passport_links l ON l.passport_id = p.uuid
		WHERE l.profile_id = $1
	`, profileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, err
}

func (p *Postgres) GetPrimaryProfileForPassport(ctx context.Context, passportID string) (identity.Profile, error) {
	profile, err := scanProfileRow(p.pool.QueryRow(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles p
		JOIN profile_passport_links l ON l.profile_id = p.uuid
		WHERE l.passport_id = $1
		ORDER BY l.is_primary DESC, l.linked_at ASC
		LIMIT 1
	`, passportID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return profile, err
}

func (p *Postgres) GetProfileSkin(ctx context.Context, profileID string) (ProfileSkin, error) {
	skin, err := scanProfileSkinRow(p.pool.QueryRow(ctx, `
		SELECT `+profileSkinSelectColumns+`
		FROM profile_skins
		WHERE profile_id = $1
	`, strings.TrimSpace(profileID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfileSkin{}, fmt.Errorf("profile skin not found: %w", ErrNotFound)
	}
	return skin, err
}

func (p *Postgres) SetProfileSkin(ctx context.Context, profileID string, skin ProfileSkin, properties []identity.ProfileProperty) (identity.Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return identity.Profile{}, fmt.Errorf("profile id is required")
	}
	propsJSON, err := json.Marshal(properties)
	if err != nil {
		return identity.Profile{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.Profile{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO profile_skins (
			profile_id, model, skin_png, skin_content_type, skin_sha256,
			cape_png, cape_content_type, cape_sha256,
			elytra_png, elytra_content_type, elytra_sha256
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (profile_id) DO UPDATE
		SET model = EXCLUDED.model,
			skin_png = EXCLUDED.skin_png,
			skin_content_type = EXCLUDED.skin_content_type,
			skin_sha256 = EXCLUDED.skin_sha256,
			cape_png = EXCLUDED.cape_png,
			cape_content_type = EXCLUDED.cape_content_type,
			cape_sha256 = EXCLUDED.cape_sha256,
			elytra_png = EXCLUDED.elytra_png,
			elytra_content_type = EXCLUDED.elytra_content_type,
			elytra_sha256 = EXCLUDED.elytra_sha256,
			updated_at = now()
	`, profileID, normalizeSkinModel(skin.Model), skin.SkinPNG, defaultContentType(skin.SkinContentType), skin.SkinSHA256, nullableBytes(skin.CapePNG), nullString(skin.CapeContentType), nullString(skin.CapeSHA256), nullableBytes(skin.ElytraPNG), nullString(skin.ElytraContentType), nullString(skin.ElytraSHA256)); err != nil {
		return identity.Profile{}, err
	}
	profile, err := scanProfileRow(tx.QueryRow(ctx, `
		UPDATE profiles
		SET skin_source = 'custom',
			profile_properties = $2::jsonb,
			updated_at = now()
		WHERE uuid = $1
		RETURNING `+profileSelectColumns+`
	`, profileID, string(propsJSON)))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	if err != nil {
		return identity.Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Profile{}, err
	}
	return profile, nil
}

func (p *Postgres) DeleteProfileSkin(ctx context.Context, profileID string, properties []identity.ProfileProperty, skinSource string) (identity.Profile, error) {
	profileID = strings.TrimSpace(profileID)
	propsJSON, err := json.Marshal(properties)
	if err != nil {
		return identity.Profile{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.Profile{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM profile_skins WHERE profile_id = $1`, profileID); err != nil {
		return identity.Profile{}, err
	}
	profile, err := scanProfileRow(tx.QueryRow(ctx, `
		UPDATE profiles
		SET skin_source = $2,
			profile_properties = $3::jsonb,
			updated_at = now()
		WHERE uuid = $1
		RETURNING `+profileSelectColumns+`
	`, profileID, normalizeSkinSource(skinSource), string(propsJSON)))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	if err != nil {
		return identity.Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Profile{}, err
	}
	return profile, nil
}

func (p *Postgres) ListProfilesForPassport(ctx context.Context, passportID string) []identity.Profile {
	rows, err := p.pool.Query(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles p
		JOIN profile_passport_links l ON l.profile_id = p.uuid
		WHERE l.passport_id = $1
		ORDER BY l.is_primary DESC, p.protocol_name ASC
	`, passportID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []identity.Profile{}
	for rows.Next() {
		if profile, err := scanProfileRow(rows); err == nil {
			out = append(out, profile)
		}
	}
	return out
}

func (p *Postgres) ListPassports(ctx context.Context) []identity.Passport {
	rows, err := p.pool.Query(ctx, `
		SELECT `+passportSelectColumns+`
		FROM passports
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []identity.Passport{}
	for rows.Next() {
		if passport, err := scanPassportRow(rows); err == nil {
			out = append(out, passport)
		}
	}
	return out
}

func (p *Postgres) ListProfiles(ctx context.Context) []identity.Profile {
	rows, err := p.pool.Query(ctx, `
		SELECT `+profileSelectColumns+`
		FROM profiles
		ORDER BY protocol_name ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []identity.Profile{}
	for rows.Next() {
		if profile, err := scanProfileRow(rows); err == nil {
			out = append(out, profile)
		}
	}
	return out
}

func (p *Postgres) CreateProfile(ctx context.Context, profile identity.Profile) (identity.Profile, error) {
	if profile.UUID == (identity.UUID{}) {
		name, err := identity.NormalizeProtocolName(profile.ProtocolName)
		if err != nil {
			return identity.Profile{}, err
		}
		uuid, err := identity.RandomProfileUUID()
		if err != nil {
			return identity.Profile{}, err
		}
		profile.UUID = uuid
		profile.ProtocolName = name.Protocol
		profile.NormalizedName = name.Normalized
		if profile.DisplayName == "" {
			profile.DisplayName = name.Protocol
		}
	}
	profile.ID = profile.UUID.String()
	if profile.Status == "" {
		profile.Status = identity.ProfileStatusActive
	}
	if profile.SkinSource == "" {
		profile.SkinSource = "none"
	}
	propsJSON, err := json.Marshal(profile.ProfileProperties)
	if err != nil {
		return identity.Profile{}, err
	}
	createdFrom := nullString(profile.CreatedFromPassport)
	return scanProfileRow(p.pool.QueryRow(ctx, `
		INSERT INTO profiles (uuid, protocol_name, normalized_name, display_name, status, skin_source, profile_properties, created_from_passport_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
		RETURNING `+profileSelectColumns+`
	`, profile.UUID.String(), profile.ProtocolName, profile.NormalizedName, profile.DisplayName, profile.Status, profile.SkinSource, string(propsJSON), createdFrom))
}

func (p *Postgres) BindProfileToPassport(ctx context.Context, profileID string, passportID string, primary bool) (identity.PassportProfile, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	defer tx.Rollback(ctx)
	passport, err := scanPassportRow(tx.QueryRow(ctx, `SELECT `+passportSelectColumns+` FROM passports WHERE uuid = $1`, passportID))
	if err != nil {
		return identity.PassportProfile{}, err
	}
	profile, err := scanProfileRow(tx.QueryRow(ctx, `SELECT `+profileSelectColumns+` FROM profiles WHERE uuid = $1`, profileID))
	if err != nil {
		return identity.PassportProfile{}, err
	}
	if primary {
		if _, err := tx.Exec(ctx, `UPDATE profile_passport_links SET is_primary = false WHERE passport_id = $1`, passportID); err != nil {
			return identity.PassportProfile{}, err
		}
	}
	link := identity.ProfilePassportLink{ProfileID: profileID, PassportID: passportID, IsPrimary: primary, LinkedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profile_passport_links (profile_id, passport_id, is_primary, linked_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (profile_id) DO UPDATE
		SET passport_id = EXCLUDED.passport_id,
			is_primary = EXCLUDED.is_primary,
			linked_at = EXCLUDED.linked_at
	`, link.ProfileID, link.PassportID, link.IsPrimary, link.LinkedAt); err != nil {
		return identity.PassportProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.PassportProfile{}, err
	}
	return identity.PassportProfile{Passport: passport, Profile: profile, Link: link}, nil
}

func (p *Postgres) UnbindProfile(ctx context.Context, profileID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM profile_passport_links WHERE profile_id = $1`, profileID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("profile link not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) SetPassportStatus(ctx context.Context, id string, status identity.PassportStatus) (identity.Passport, error) {
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		UPDATE passports
		SET status = $2, updated_at = now()
		WHERE uuid = $1
		RETURNING `+passportSelectColumns+`
	`, id, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, err
}

func (p *Postgres) SetProfileStatus(ctx context.Context, id string, status identity.ProfileStatus) (identity.Profile, error) {
	profile, err := scanProfileRow(p.pool.QueryRow(ctx, `
		UPDATE profiles
		SET status = $2, updated_at = now()
		WHERE uuid = $1
		RETURNING `+profileSelectColumns+`
	`, id, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return profile, err
}

func (p *Postgres) GetPassportCredential(ctx context.Context, username string) (identity.Passport, PassportCredential, error) {
	passport, err := p.GetPassportByUsername(ctx, username)
	if err != nil {
		return identity.Passport{}, PassportCredential{}, err
	}
	credential, err := scanPassportCredentialRow(p.pool.QueryRow(ctx, `
		SELECT passport_id, password_hash, updated_at, failed_attempts, locked_until
		FROM offline_passport_credentials
		WHERE passport_id = $1
	`, passport.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, PassportCredential{}, fmt.Errorf("passport credential not found: %w", ErrNotFound)
	}
	return passport, credential, err
}

func (p *Postgres) RecordPassportLoginFailure(ctx context.Context, passportID string, now time.Time) (PassportCredential, error) {
	passportID = p.resolvePassportIDForCredential(ctx, passportID)
	credential, err := scanPassportCredentialRow(p.pool.QueryRow(ctx, `
		UPDATE offline_passport_credentials
		SET failed_attempts = failed_attempts + 1,
			locked_until = CASE
				WHEN failed_attempts + 1 >= 5 THEN $2::timestamptz + interval '15 minutes'
				ELSE locked_until
			END,
			updated_at = now()
		WHERE passport_id = $1
		RETURNING passport_id, password_hash, updated_at, failed_attempts, locked_until
	`, passportID, now.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return PassportCredential{}, fmt.Errorf("passport credential not found: %w", ErrNotFound)
	}
	return credential, err
}

func (p *Postgres) RecordPassportLoginSuccess(ctx context.Context, passportID string) error {
	passportID = p.resolvePassportIDForCredential(ctx, passportID)
	tag, err := p.pool.Exec(ctx, `
		UPDATE offline_passport_credentials
		SET failed_attempts = 0,
			locked_until = NULL,
			updated_at = now()
		WHERE passport_id = $1
	`, passportID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passport credential not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) RecordPlayerSeen(ctx context.Context, passportID string, profileID string, serverID string, ip string, geo *identity.IPGeo, now time.Time) error {
	passportID = strings.TrimSpace(passportID)
	profileID = strings.TrimSpace(profileID)
	serverID = strings.TrimSpace(serverID)
	ip = strings.TrimSpace(ip)
	geoJSON, err := marshalNullableGeo(geo)
	if err != nil {
		return err
	}
	if passportID == "" && profileID != "" {
		if passport, err := p.GetPassportForProfile(ctx, profileID); err == nil {
			passportID = passport.ID
		}
	}
	now = now.UTC()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if passportID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE passports
			SET last_seen_server_id = $2, last_seen_at = $3, last_seen_ip = $4, last_seen_geo = $5::jsonb, updated_at = now()
			WHERE uuid = $1
		`, passportID, nullString(serverID), now, nullString(ip), geoJSON); err != nil {
			return err
		}
	}
	if profileID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE profiles
			SET last_seen_server_id = $2, last_seen_at = $3, last_seen_ip = $4, last_seen_geo = $5::jsonb, updated_at = now()
			WHERE uuid = $1
		`, profileID, nullString(serverID), now, nullString(ip), geoJSON); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) UpdatePassportPassword(ctx context.Context, passportID string, passwordHash string) error {
	passportID = p.resolvePassportIDForCredential(ctx, passportID)
	tag, err := p.pool.Exec(ctx, `
		UPDATE offline_passport_credentials
		SET password_hash = $2,
			failed_attempts = 0,
			locked_until = NULL,
			updated_at = now()
		WHERE passport_id = $1
	`, passportID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passport credential not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) resolvePassportIDForCredential(ctx context.Context, id string) string {
	if _, err := p.GetPassportByID(ctx, id); err == nil {
		return id
	}
	if passport, err := p.GetPassportForProfile(ctx, id); err == nil {
		return passport.ID
	}
	return id
}

func (p *Postgres) GetOfflinePlayer(ctx context.Context, rawName string) (identity.Player, error) {
	passport, err := p.GetPassportByUsername(ctx, rawName)
	if err != nil {
		return identity.Player{}, err
	}
	profile, err := p.GetPrimaryProfileForPassport(ctx, passport.ID)
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfile(passport, profile), nil
}

func (p *Postgres) GetPlayerByProtocolName(ctx context.Context, protocolName string) (identity.Player, error) {
	profile, err := p.GetProfileByProtocolName(ctx, protocolName)
	if err != nil {
		return identity.Player{}, err
	}
	passport, err := p.GetPassportForProfile(ctx, profile.ID)
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfile(passport, profile), nil
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
			FROM passports
			WHERE kind = 'premium' AND lower(username) = $1
		)
	`, name.Normalized).Scan(&exists)
	return err == nil && exists
}

func (p *Postgres) GetPlayerByID(ctx context.Context, id string) (identity.Player, error) {
	if profile, err := p.GetProfileByID(ctx, id); err == nil {
		passport, err := p.GetPassportForProfile(ctx, profile.ID)
		if err != nil {
			return identity.Player{}, err
		}
		return identity.PlayerFromPassportProfile(passport, profile), nil
	}
	passport, err := p.GetPassportByID(ctx, id)
	if err != nil {
		return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
	}
	profile, err := p.GetPrimaryProfileForPassport(ctx, passport.ID)
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfile(passport, profile), nil
}

func (p *Postgres) GetOfflineCredential(ctx context.Context, rawName string) (identity.Player, OfflineCredential, error) {
	passport, credential, err := p.GetPassportCredential(ctx, rawName)
	if err != nil {
		return identity.Player{}, OfflineCredential{}, err
	}
	profile, err := p.GetPrimaryProfileForPassport(ctx, passport.ID)
	if err != nil {
		return identity.Player{}, OfflineCredential{}, err
	}
	return identity.PlayerFromPassportProfile(passport, profile), OfflineCredential{
		PlayerID:          credential.PassportID,
		PassportID:        credential.PassportID,
		PasswordHash:      credential.PasswordHash,
		PasswordUpdatedAt: credential.PasswordUpdatedAt,
		FailedAttempts:    credential.FailedAttempts,
		LockedUntil:       credential.LockedUntil,
	}, nil
}

func (p *Postgres) RecordOfflineLoginFailure(ctx context.Context, playerID string, now time.Time) (OfflineCredential, error) {
	credential, err := p.RecordPassportLoginFailure(ctx, playerID, now)
	return OfflineCredential{
		PlayerID:          credential.PassportID,
		PassportID:        credential.PassportID,
		PasswordHash:      credential.PasswordHash,
		PasswordUpdatedAt: credential.PasswordUpdatedAt,
		FailedAttempts:    credential.FailedAttempts,
		LockedUntil:       credential.LockedUntil,
	}, err
}

func (p *Postgres) RecordOfflineLoginSuccess(ctx context.Context, playerID string) error {
	return p.RecordPassportLoginSuccess(ctx, playerID)
}

func (p *Postgres) ListPlayers(ctx context.Context) []identity.Player {
	profiles := p.ListProfiles(ctx)
	players := make([]identity.Player, 0, len(profiles))
	for _, profile := range profiles {
		passport, err := p.GetPassportForProfile(ctx, profile.ID)
		if err == nil {
			players = append(players, identity.PlayerFromPassportProfile(passport, profile))
		}
	}
	return players
}

func (p *Postgres) SetPlayerLocked(ctx context.Context, id string, locked bool) (identity.Player, error) {
	if passport, err := p.GetPassportByID(ctx, id); err == nil {
		status := identity.PassportStatusActive
		if locked {
			status = identity.PassportStatusLocked
		}
		passport, err = p.SetPassportStatus(ctx, passport.ID, status)
		if err != nil {
			return identity.Player{}, err
		}
		profile, err := p.GetPrimaryProfileForPassport(ctx, passport.ID)
		if err != nil {
			return identity.Player{}, err
		}
		return identity.PlayerFromPassportProfile(passport, profile), nil
	}
	status := identity.ProfileStatusActive
	if locked {
		status = identity.ProfileStatusLocked
	}
	profile, err := p.SetProfileStatus(ctx, id, status)
	if err != nil {
		return identity.Player{}, err
	}
	passport, err := p.GetPassportForProfile(ctx, profile.ID)
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfile(passport, profile), nil
}

func (p *Postgres) UpdateOfflinePassword(ctx context.Context, id string, passwordHash string) error {
	if _, err := p.GetPassportByID(ctx, id); err != nil {
		passport, profileErr := p.GetPassportForProfile(ctx, id)
		if profileErr != nil {
			return fmt.Errorf("offline credential not found")
		}
		id = passport.ID
	}
	return p.UpdatePassportPassword(ctx, id, passwordHash)
}

func (p *Postgres) SaveSession(ctx context.Context, session auth.Session) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO web_sessions (id, kind, subject_id, selected_profile_id, csrf_token_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET kind = EXCLUDED.kind,
			subject_id = EXCLUDED.subject_id,
			selected_profile_id = EXCLUDED.selected_profile_id,
			csrf_token_hash = EXCLUDED.csrf_token_hash,
			expires_at = EXCLUDED.expires_at
	`, session.ID, session.Kind, session.SubjectID, nullString(session.SelectedProfileID), session.CSRFToken, session.CreatedAt, session.ExpiresAt)
	return err
}

func (p *Postgres) GetSession(ctx context.Context, id string) (auth.Session, error) {
	session, err := scanSessionRow(p.pool.QueryRow(ctx, `
		SELECT id, kind, subject_id, selected_profile_id, csrf_token_hash, created_at, expires_at
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
		SET selected_profile_id = $2, csrf_token_hash = $3, expires_at = $4
		WHERE id = $1
	`, session.ID, nullString(session.SelectedProfileID), session.CSRFToken, session.ExpiresAt)
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
	event = normalizeAuditEvent(event)
	details, err := json.Marshal(event.Details)
	if err != nil {
		return audit.Event{}, err
	}
	var id int64
	err = p.pool.QueryRow(ctx, `
		INSERT INTO audit_events (occurred_at, schema_version, category, outcome, source, session_id, correlation_id, actor_type, actor_id, target_type, target_id, event_type, client_ip, client_geo, request_id, path, method, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16, $17, $18)
		RETURNING id
	`,
		event.Occurred,
		event.SchemaVersion,
		nullString(event.Category),
		nullString(event.Outcome),
		nullString(event.Source),
		nullString(event.SessionID),
		nullString(event.CorrelationID),
		event.ActorType,
		nullString(event.ActorID),
		event.Target,
		nullString(event.TargetID),
		event.Type,
		nullString(detailString(event.Details, "client_ip")),
		detailJSON(event.Details, "client_geo"),
		nullString(detailString(event.Details, "request_id")),
		nullString(detailString(event.Details, "path")),
		nullString(detailString(event.Details, "method")),
		details,
	).Scan(&id)
	if err != nil {
		return audit.Event{}, err
	}
	event.ID = strconv.FormatInt(id, 10)
	return event, nil
}

func (p *Postgres) GetAuditEvent(ctx context.Context, id string) (audit.Event, error) {
	eventID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || eventID <= 0 {
		return audit.Event{}, fmt.Errorf("audit event not found: %w", ErrNotFound)
	}
	var event audit.Event
	var details []byte
	err = p.pool.QueryRow(ctx, `
		SELECT id, occurred_at, schema_version, coalesce(category, ''), coalesce(outcome, ''), coalesce(source, ''), coalesce(session_id, ''), coalesce(correlation_id, ''), actor_type, coalesce(actor_id, ''), target_type, coalesce(target_id, ''), event_type, details
		FROM audit_events
		WHERE id = $1
	`, eventID).Scan(&eventID, &event.Occurred, &event.SchemaVersion, &event.Category, &event.Outcome, &event.Source, &event.SessionID, &event.CorrelationID, &event.ActorType, &event.ActorID, &event.Target, &event.TargetID, &event.Type, &details)
	if errors.Is(err, pgx.ErrNoRows) {
		return audit.Event{}, fmt.Errorf("audit event not found: %w", ErrNotFound)
	}
	if err != nil {
		return audit.Event{}, err
	}
	event.ID = strconv.FormatInt(eventID, 10)
	_ = json.Unmarshal(details, &event.Details)
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	event = normalizeAuditEvent(event)
	return event, nil
}

func (p *Postgres) ListAuditEvents(ctx context.Context, limit int) []audit.Event {
	if limit <= 0 {
		limit = 100
	} else if limit > 5000 {
		limit = 5000
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, occurred_at, schema_version, coalesce(category, ''), coalesce(outcome, ''), coalesce(source, ''), coalesce(session_id, ''), coalesce(correlation_id, ''), actor_type, coalesce(actor_id, ''), target_type, coalesce(target_id, ''), event_type, details
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
		if err := rows.Scan(&id, &event.Occurred, &event.SchemaVersion, &event.Category, &event.Outcome, &event.Source, &event.SessionID, &event.CorrelationID, &event.ActorType, &event.ActorID, &event.Target, &event.TargetID, &event.Type, &details); err != nil {
			continue
		}
		event.ID = strconv.FormatInt(id, 10)
		_ = json.Unmarshal(details, &event.Details)
		if event.Details == nil {
			event.Details = map[string]any{}
		}
		events = append(events, normalizeAuditEvent(event))
	}
	return events
}

func (p *Postgres) ListAuditEventsPage(ctx context.Context, query AuditEventQuery) ([]audit.Event, int, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 25
	} else if query.PageSize > 100 {
		query.PageSize = 100
	}
	where, args := auditEventWhere(query)
	var total int
	countSQL := "SELECT count(*) FROM audit_events" + where
	if err := p.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := p.pool.Query(ctx, `
		SELECT id, occurred_at, schema_version, coalesce(category, ''), coalesce(outcome, ''), coalesce(source, ''), coalesce(session_id, ''), coalesce(correlation_id, ''), actor_type, coalesce(actor_id, ''), target_type, coalesce(target_id, ''), event_type, details
		FROM audit_events`+where+`
		ORDER BY occurred_at DESC, id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	return scanAuditEventRows(rows), total, nil
}

func auditEventWhere(query AuditEventQuery) (string, []any) {
	where := []string{}
	args := []any{}
	next := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if query.ActorType != "" {
		where = append(where, "actor_type = "+next(query.ActorType))
	}
	if query.TargetType != "" {
		where = append(where, "target_type = "+next(query.TargetType))
	}
	if query.EventType != "" {
		where = append(where, "event_type ILIKE "+next("%"+query.EventType+"%"))
	}
	if query.Since != nil {
		where = append(where, "occurred_at >= "+next(query.Since.UTC()))
	}
	if query.Until != nil {
		where = append(where, "occurred_at <= "+next(query.Until.UTC()))
	}
	related := make([]string, 0, len(query.RelatedIDs))
	for _, id := range query.RelatedIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholder := next(id)
		likePlaceholder := next("%" + id + "%")
		related = append(related, "(actor_id = "+placeholder+" OR target_id = "+placeholder+" OR details::text ILIKE "+likePlaceholder+")")
	}
	if len(related) > 0 {
		where = append(where, "("+strings.Join(related, " OR ")+")")
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func scanAuditEventRows(rows pgx.Rows) []audit.Event {
	events := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		var id int64
		var details []byte
		if err := rows.Scan(&id, &event.Occurred, &event.SchemaVersion, &event.Category, &event.Outcome, &event.Source, &event.SessionID, &event.CorrelationID, &event.ActorType, &event.ActorID, &event.Target, &event.TargetID, &event.Type, &details); err != nil {
			continue
		}
		event.ID = strconv.FormatInt(id, 10)
		_ = json.Unmarshal(details, &event.Details)
		if event.Details == nil {
			event.Details = map[string]any{}
		}
		events = append(events, normalizeAuditEvent(event))
	}
	return events
}

func normalizeAuditEvent(event audit.Event) audit.Event {
	if event.Occurred.IsZero() {
		event.Occurred = time.Now().UTC()
	} else {
		event.Occurred = event.Occurred.UTC()
	}
	if event.SchemaVersion <= 0 {
		event.SchemaVersion = 1
	}
	if strings.TrimSpace(event.Category) == "" {
		event.Category = audit.CategoryFromType(event.Type)
	}
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	if event.Outcome == "" {
		event.Outcome = detailString(event.Details, "outcome")
	}
	if event.Outcome == "" {
		event.Outcome = inferAuditOutcome(event.Type)
	}
	if event.Source == "" {
		event.Source = detailString(event.Details, "source")
	}
	if event.SessionID == "" {
		event.SessionID = detailString(event.Details, "session_id")
	}
	if event.CorrelationID == "" {
		event.CorrelationID = detailString(event.Details, "correlation_id")
	}
	return event
}

func inferAuditOutcome(eventType string) string {
	eventType = strings.ToLower(eventType)
	switch {
	case strings.Contains(eventType, "failure"), strings.Contains(eventType, "failed"):
		return "failure"
	case strings.Contains(eventType, "reject"), strings.Contains(eventType, "denied"):
		return "rejected"
	default:
		return "success"
	}
}

func (p *Postgres) Create(ctx context.Context, name string, now time.Time) (node.Node, string, error) {
	return p.CreateKind(ctx, name, "downstream_velocity", now)
}

func (p *Postgres) CreateKind(ctx context.Context, name string, kind string, now time.Time) (node.Node, string, error) {
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
		Mode:             node.NormalizeKind(kind),
		Name:             name,
		TokenHash:        auth.HashToken("node", token),
		TokenFingerprint: auth.TokenFingerprint(token),
		RuntimeConfig:    map[string]any{},
		CreatedAt:        now.UTC(),
	}
	n, err = scanNodeRow(p.pool.QueryRow(ctx, `
		INSERT INTO velocity_nodes (id, server_id, mode, name, token_hash, token_fingerprint, disabled, runtime_config, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, false, '{}'::jsonb, $7)
		RETURNING `+nodeSelectColumns+`
	`, n.ID, n.ServerID, n.Mode, n.Name, n.TokenHash, n.TokenFingerprint, n.CreatedAt))
	if err != nil {
		return node.Node{}, "", err
	}
	return n, token, nil
}

func (p *Postgres) Authenticate(ctx context.Context, token string) (node.Node, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+nodeSelectColumns+` FROM velocity_nodes WHERE disabled = false`)
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
	n, err := scanNodeRow(p.pool.QueryRow(ctx, `
		UPDATE velocity_nodes
		SET token_hash = $2, token_fingerprint = $3, disabled = false, updated_at = now()
		WHERE id = $1 AND disabled = false
		RETURNING `+nodeSelectColumns+`
	`, id, auth.HashToken("node", token), auth.TokenFingerprint(token)))
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
	n, err = scanNodeRow(p.pool.QueryRow(ctx, `
		UPDATE velocity_nodes
		SET last_heartbeat_at = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, n.ID, now))
	return n, err
}

func (p *Postgres) Register(ctx context.Context, registration node.Registration, now time.Time) (node.Node, error) {
	if registration.InstanceFingerprint == "" {
		return node.Node{}, fmt.Errorf("instance fingerprint is required")
	}
	if registration.Name == "" {
		registration.Name = "node-" + registration.InstanceFingerprint[:minInt(8, len(registration.InstanceFingerprint))]
	}
	if registration.Kind != "" {
		registration.Mode = node.NormalizeKind(registration.Kind)
	} else {
		registration.Mode = node.NormalizeKind(registration.Mode)
	}
	if registration.ServerID == "" {
		registration.ServerID = "default"
	}
	var disabled bool
	err := p.pool.QueryRow(ctx, `
		SELECT disabled FROM velocity_nodes WHERE instance_fingerprint = $1
	`, registration.InstanceFingerprint).Scan(&disabled)
	if err == nil && disabled {
		return node.Node{}, fmt.Errorf("node is revoked")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return node.Node{}, err
	}
	id, err := randomID("node")
	if err != nil {
		return node.Node{}, err
	}
	now = now.UTC()
	n, err := scanNodeRow(p.pool.QueryRow(ctx, `
		INSERT INTO velocity_nodes (
			id, server_id, mode, name, token_hash, token_fingerprint, instance_fingerprint,
			plugin_version, velocity_version, disabled, created_at, last_heartbeat_at
		)
		VALUES ($1, $2, $3, $4, '', $5, $6, $7, $8, false, $9, $9)
		ON CONFLICT (instance_fingerprint) WHERE instance_fingerprint <> '' DO UPDATE
		SET server_id = EXCLUDED.server_id,
			mode = EXCLUDED.mode,
			name = EXCLUDED.name,
			token_fingerprint = EXCLUDED.token_fingerprint,
			plugin_version = EXCLUDED.plugin_version,
			velocity_version = EXCLUDED.velocity_version,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			updated_at = now()
		RETURNING `+nodeSelectColumns+`
	`, id, registration.ServerID, registration.Mode, registration.Name, registration.AccessFingerprint, registration.InstanceFingerprint, registration.PluginVersion, registration.VelocityVersion, now))
	return n, err
}

func (p *Postgres) Get(ctx context.Context, id string) (node.Node, error) {
	n, err := scanNodeRow(p.pool.QueryRow(ctx, `SELECT `+nodeSelectColumns+` FROM velocity_nodes WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return node.Node{}, fmt.Errorf("node not found")
	}
	return n, err
}

func (p *Postgres) Update(ctx context.Context, id string, name string, runtime map[string]any) (node.Node, error) {
	runtimeJSON, err := json.Marshal(node.CloneRuntimeConfig(runtime))
	if err != nil {
		return node.Node{}, err
	}
	n, err := scanNodeRow(p.pool.QueryRow(ctx, `
		UPDATE velocity_nodes
		SET name = COALESCE(NULLIF(trim($2), ''), name),
			runtime_config = $3::jsonb,
			updated_at = now()
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, id, name, string(runtimeJSON)))
	if errors.Is(err, pgx.ErrNoRows) {
		return node.Node{}, fmt.Errorf("node not found")
	}
	return n, err
}

func (p *Postgres) Delete(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE velocity_nodes
		SET disabled = true, token_hash = '', token_fingerprint = '', updated_at = now()
		WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) List(ctx context.Context) []node.Node {
	rows, err := p.pool.Query(ctx, `SELECT `+nodeSelectColumns+` FROM velocity_nodes ORDER BY created_at ASC`)
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

func (p *Postgres) GetMojangRoute(ctx context.Context, id string) (mojang.Route, error) {
	route, err := scanMojangRouteRow(p.pool.QueryRow(ctx, `
		SELECT id, kind, url, weight, disabled
		FROM mojang_routes
		WHERE id = $1
	`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return mojang.Route{}, fmt.Errorf("mojang route not found: %w", ErrNotFound)
	}
	return route, err
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

func (p *Postgres) GetSystemSetting(ctx context.Context, key string) (map[string]any, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("system setting not found: %w", ErrNotFound)
	}
	var raw []byte
	err := p.pool.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("system setting not found: %w", ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

func (p *Postgres) SetSystemSetting(ctx context.Context, key string, value map[string]any) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("system setting key is required")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = now()
	`, key, raw)
	return err
}

func (p *Postgres) ListProfilePresences(ctx context.Context, profileID string) []PlayerPresence {
	return p.listPresences(ctx, "profile_id = $1", strings.TrimSpace(profileID))
}

func (p *Postgres) ListPassportPresences(ctx context.Context, passportID string) []PlayerPresence {
	return p.listPresences(ctx, "passport_id = $1", strings.TrimSpace(passportID))
}

func (p *Postgres) listPresences(ctx context.Context, where string, arg string) []PlayerPresence {
	rows, err := p.pool.Query(ctx, `
		SELECT id, passport_id, profile_id, server_id, node_id, protocol_name, uuid, remote_addr, connected_at, last_seen_at, ended_at, coalesce(end_reason, '')
		FROM player_presences
		WHERE ended_at IS NULL AND `+where+`
		ORDER BY connected_at DESC
	`, arg)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []PlayerPresence{}
	for rows.Next() {
		presence, err := scanPresenceRow(rows)
		if err == nil {
			out = append(out, presence)
		}
	}
	return out
}

func (p *Postgres) UpsertPresence(ctx context.Context, presence PlayerPresence) (PlayerPresence, error) {
	if strings.TrimSpace(presence.ID) == "" {
		id, err := randomID("presence")
		if err != nil {
			return PlayerPresence{}, err
		}
		presence.ID = id
	}
	now := time.Now().UTC()
	if presence.ConnectedAt.IsZero() {
		presence.ConnectedAt = now
	}
	if presence.LastSeenAt.IsZero() {
		presence.LastSeenAt = now
	}
	return scanPresenceRow(p.pool.QueryRow(ctx, `
		INSERT INTO player_presences (id, passport_id, profile_id, server_id, node_id, protocol_name, uuid, remote_addr, connected_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, passport_id, profile_id, server_id, node_id, protocol_name, uuid, remote_addr, connected_at, last_seen_at, ended_at, coalesce(end_reason, '')
	`, presence.ID, presence.PassportID, presence.ProfileID, presence.ServerID, presence.NodeID, presence.ProtocolName, presence.UUID, presence.RemoteAddr, presence.ConnectedAt, presence.LastSeenAt))
}

func (p *Postgres) EndPresence(ctx context.Context, id string, reason string, endedAt time.Time) (PlayerPresence, error) {
	presence, err := scanPresenceRow(p.pool.QueryRow(ctx, `
		UPDATE player_presences
		SET ended_at = $2, end_reason = $3
		WHERE id = $1 AND ended_at IS NULL
		RETURNING id, passport_id, profile_id, server_id, node_id, protocol_name, uuid, remote_addr, connected_at, last_seen_at, ended_at, coalesce(end_reason, '')
	`, strings.TrimSpace(id), endedAt.UTC(), strings.TrimSpace(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerPresence{}, fmt.Errorf("presence not found: %w", ErrNotFound)
	}
	return presence, err
}

func (p *Postgres) EndProfilePresences(ctx context.Context, profileID string, reason string, endedAt time.Time) int {
	return p.endPresences(ctx, "profile_id = $1", strings.TrimSpace(profileID), reason, endedAt)
}

func (p *Postgres) EndPassportPresences(ctx context.Context, passportID string, reason string, endedAt time.Time) int {
	return p.endPresences(ctx, "passport_id = $1", strings.TrimSpace(passportID), reason, endedAt)
}

func (p *Postgres) endPresences(ctx context.Context, where string, arg string, reason string, endedAt time.Time) int {
	tag, err := p.pool.Exec(ctx, `UPDATE player_presences SET ended_at = $2, end_reason = $3 WHERE ended_at IS NULL AND `+where, arg, endedAt.UTC(), strings.TrimSpace(reason))
	if err != nil {
		return 0
	}
	return int(tag.RowsAffected())
}

func (p *Postgres) EnqueueNodeAction(ctx context.Context, action NodeAction) (NodeAction, error) {
	if strings.TrimSpace(action.NodeID) == "" {
		return NodeAction{}, fmt.Errorf("node action node id is required")
	}
	if strings.TrimSpace(action.ID) == "" {
		id, err := randomID("node-action")
		if err != nil {
			return NodeAction{}, err
		}
		action.ID = id
	}
	if action.Type == "" {
		action.Type = NodeActionDisconnect
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now().UTC()
	}
	return scanNodeActionRow(p.pool.QueryRow(ctx, `
		INSERT INTO node_actions (id, node_id, action_type, presence_id, passport_id, profile_id, uuid, protocol_name, reason, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, node_id, action_type, presence_id, passport_id, profile_id, uuid, protocol_name, reason, created_at, expires_at, acked_at
	`, action.ID, action.NodeID, action.Type, action.PresenceID, action.PassportID, action.ProfileID, action.UUID, action.ProtocolName, action.Reason, action.CreatedAt.UTC(), action.ExpiresAt))
}

func (p *Postgres) ListPendingNodeActions(ctx context.Context, nodeID string, now time.Time, limit int) []NodeAction {
	nodeID = strings.TrimSpace(nodeID)
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, node_id, action_type, presence_id, passport_id, profile_id, uuid, protocol_name, reason, created_at, expires_at, acked_at
		FROM node_actions
		WHERE node_id = $1
		  AND acked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $2)
		ORDER BY created_at ASC
		LIMIT $3
	`, nodeID, now.UTC(), limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []NodeAction{}
	for rows.Next() {
		action, err := scanNodeActionRow(rows)
		if err == nil {
			out = append(out, action)
		}
	}
	return out
}

func (p *Postgres) AckNodeActions(ctx context.Context, nodeID string, ids []string, now time.Time) int {
	nodeID = strings.TrimSpace(nodeID)
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if nodeID == "" || len(clean) == 0 {
		return 0
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE node_actions
		SET acked_at = $3
		WHERE node_id = $1 AND id = ANY($2) AND acked_at IS NULL
	`, nodeID, clean, now.UTC())
	if err != nil {
		return 0
	}
	return int(tag.RowsAffected())
}

func (p *Postgres) ListBans(ctx context.Context, scope BanScope, targetID string, includeInactive bool, now time.Time) []PlayerBan {
	where := "scope = $1 AND target_id = $2"
	args := []any{scope, strings.TrimSpace(targetID)}
	if !includeInactive {
		where += " AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $3)"
		args = append(args, now.UTC())
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
		FROM player_bans
		WHERE `+where+`
		ORDER BY created_at DESC
	`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []PlayerBan{}
	for rows.Next() {
		ban, err := scanBanRow(rows)
		if err == nil {
			out = append(out, ban)
		}
	}
	return out
}

func (p *Postgres) CreateBan(ctx context.Context, ban PlayerBan) (PlayerBan, error) {
	if strings.TrimSpace(ban.ID) == "" {
		id, err := randomID("ban")
		if err != nil {
			return PlayerBan{}, err
		}
		ban.ID = id
	}
	if ban.CreatedAt.IsZero() {
		ban.CreatedAt = time.Now().UTC()
	}
	return scanBanRow(p.pool.QueryRow(ctx, `
		INSERT INTO player_bans (id, scope, target_id, reason, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
	`, ban.ID, ban.Scope, ban.TargetID, ban.Reason, ban.CreatedBy, ban.CreatedAt, ban.ExpiresAt))
}

func (p *Postgres) GetBan(ctx context.Context, id string) (PlayerBan, error) {
	ban, err := scanBanRow(p.pool.QueryRow(ctx, `
		SELECT id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
		FROM player_bans
		WHERE id = $1
	`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	return ban, err
}

func (p *Postgres) ExtendBan(ctx context.Context, id string, expiresAt time.Time) (PlayerBan, error) {
	ban, err := scanBanRow(p.pool.QueryRow(ctx, `
		UPDATE player_bans
		SET expires_at = $2
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
	`, strings.TrimSpace(id), expiresAt.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	return ban, err
}

func (p *Postgres) RevokeBan(ctx context.Context, id string, revokedBy string, reason string, now time.Time) (PlayerBan, error) {
	ban, err := scanBanRow(p.pool.QueryRow(ctx, `
		UPDATE player_bans
		SET revoked_by = $2, revoked_at = $3, revoke_reason = $4
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
	`, strings.TrimSpace(id), strings.TrimSpace(revokedBy), now.UTC(), strings.TrimSpace(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	return ban, err
}

func (p *Postgres) ActiveBanFor(ctx context.Context, passportID string, profileID string, now time.Time) (PlayerBan, bool) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, scope, target_id, reason, created_by, created_at, expires_at, coalesce(revoked_by, ''), revoked_at, coalesce(revoke_reason, '')
		FROM player_bans
		WHERE revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $3)
		  AND ((scope = 'passport' AND target_id = $1) OR (scope = 'profile' AND target_id = $2))
		ORDER BY CASE WHEN scope = 'passport' THEN 0 ELSE 1 END, created_at DESC
		LIMIT 1
	`, strings.TrimSpace(passportID), strings.TrimSpace(profileID), now.UTC())
	ban, err := scanBanRow(row)
	if err != nil {
		return PlayerBan{}, false
	}
	return ban, true
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

func (p *Postgres) ListLimboBlueprints(ctx context.Context) []LimboBlueprint {
	rows, err := p.pool.Query(ctx, fmt.Sprintf("SELECT %s FROM limbo_blueprints ORDER BY updated_at DESC, name ASC", limboBlueprintSelectColumns))
	if err != nil {
		return nil
	}
	defer rows.Close()
	blueprints := make([]LimboBlueprint, 0)
	for rows.Next() {
		blueprint, err := scanLimboBlueprintRow(rows)
		if err == nil {
			blueprints = append(blueprints, blueprint)
		}
	}
	return blueprints
}

func (p *Postgres) GetLimboBlueprint(ctx context.Context, id string) (LimboBlueprint, error) {
	blueprint, err := scanLimboBlueprintRow(p.pool.QueryRow(ctx, fmt.Sprintf("SELECT %s FROM limbo_blueprints WHERE id = $1", limboBlueprintSelectColumns), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return LimboBlueprint{}, fmt.Errorf("limbo blueprint not found: %w", ErrNotFound)
	}
	return blueprint, err
}

func (p *Postgres) UpsertLimboBlueprint(ctx context.Context, blueprint LimboBlueprint) (LimboBlueprint, error) {
	if strings.TrimSpace(blueprint.ID) == "" {
		id, err := randomID("limbo-blueprint")
		if err != nil {
			return LimboBlueprint{}, err
		}
		blueprint.ID = id
	}
	if blueprint.Preview == nil {
		blueprint.Preview = map[string]any{}
	}
	if blueprint.Config == nil {
		blueprint.Config = map[string]any{}
	}
	preview, err := json.Marshal(blueprint.Preview)
	if err != nil {
		return LimboBlueprint{}, err
	}
	config, err := json.Marshal(blueprint.Config)
	if err != nil {
		return LimboBlueprint{}, err
	}
	out, err := scanLimboBlueprintRow(p.pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO limbo_blueprints (id, name, description, filename, content_type, size_bytes, sha256, schematic, preview, config, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, now())
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
			description = EXCLUDED.description,
			filename = EXCLUDED.filename,
			content_type = EXCLUDED.content_type,
			size_bytes = EXCLUDED.size_bytes,
			sha256 = EXCLUDED.sha256,
			schematic = EXCLUDED.schematic,
			preview = EXCLUDED.preview,
			config = EXCLUDED.config,
			updated_at = now()
		RETURNING %s
	`, limboBlueprintSelectColumns),
		blueprint.ID,
		strings.TrimSpace(blueprint.Name),
		strings.TrimSpace(blueprint.Description),
		strings.TrimSpace(blueprint.Filename),
		strings.TrimSpace(blueprint.ContentType),
		blueprint.SizeBytes,
		strings.TrimSpace(blueprint.SHA256),
		blueprint.Schematic,
		preview,
		config,
	))
	if err != nil {
		return LimboBlueprint{}, err
	}
	return out, nil
}

func (p *Postgres) DeleteLimboBlueprint(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, "DELETE FROM limbo_blueprints WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("limbo blueprint not found: %w", ErrNotFound)
	}
	return nil
}

func (p *Postgres) SaveTransferGrant(ctx context.Context, grant auth.TransferGrant) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO transfer_grants (
			id,
			player_id,
			server_id,
			portal_node_id,
			portal_source,
			gate_node_id,
			token_hash,
			uuid,
			protocol_name,
			target_host,
			target_port,
			created_at,
			expires_at,
			consumed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, grant.ID, grant.PlayerID, grant.ServerID, grant.PortalNodeID, grant.PortalSource, grant.GateNodeID, grant.TokenHash, grant.UUID, grant.ProtocolName, grant.TargetHost, grant.TargetPort, grant.CreatedAt, grant.ExpiresAt, grant.ConsumedAt)
	return err
}

func (p *Postgres) ConsumeTransferGrant(ctx context.Context, tokenHash string, serverID string, uuid string, protocolName string, gateNodeID string, allowedPortalSources []string, now time.Time) (auth.TransferGrant, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return auth.TransferGrant{}, err
	}
	defer tx.Rollback(ctx)

	grant, err := scanTransferGrantRow(tx.QueryRow(ctx, `
		SELECT id, player_id, server_id, portal_node_id, portal_source, gate_node_id, token_hash, uuid, protocol_name, target_host, target_port, created_at, expires_at, consumed_at
		FROM transfer_grants
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant not found: %w", ErrNotFound)
	}
	if err != nil {
		return auth.TransferGrant{}, err
	}
	if grant.ConsumedAt != nil {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant already consumed")
	}
	if !now.UTC().Before(grant.ExpiresAt) {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant expired")
	}
	if grant.ServerID != serverID {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant target mismatch")
	}
	if grant.UUID != uuid {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant uuid mismatch")
	}
	if !strings.EqualFold(grant.ProtocolName, protocolName) {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant protocol name mismatch")
	}
	if !portalGrantSourceAllowed(grant, allowedPortalSources) {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant portal source denied")
	}
	consumedAt := now.UTC()
	grant.GateNodeID = gateNodeID
	grant.ConsumedAt = &consumedAt
	err = scanTransferGrantRowInto(tx.QueryRow(ctx, `
		UPDATE transfer_grants
		SET consumed_at = $2,
			gate_node_id = $3
		WHERE token_hash = $1
		RETURNING id, player_id, server_id, portal_node_id, portal_source, gate_node_id, token_hash, uuid, protocol_name, target_host, target_port, created_at, expires_at, consumed_at
	`, tokenHash, consumedAt, gateNodeID), &grant)
	if err != nil {
		return auth.TransferGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.TransferGrant{}, err
	}
	return grant, nil
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

type playerScanner interface {
	Scan(dest ...any) error
}

func scanPassportRow(row playerScanner) (identity.Passport, error) {
	var passport identity.Passport
	var uuidText string
	var rawOfflineName *string
	var kind string
	var status string
	var registrationServer *string
	var lastSeenServer *string
	var lastSeenAt *time.Time
	var lastSeenIP *string
	var lastSeenGeoRaw []byte
	err := row.Scan(
		&passport.ID,
		&kind,
		&uuidText,
		&passport.Username,
		&passport.UsernameNormalized,
		&rawOfflineName,
		&status,
		&registrationServer,
		&lastSeenServer,
		&lastSeenAt,
		&lastSeenIP,
		&lastSeenGeoRaw,
		&passport.CreatedAt,
		&passport.UpdatedAt,
	)
	if err != nil {
		return identity.Passport{}, err
	}
	uuid, err := identity.ParseUUID(uuidText)
	if err != nil {
		return identity.Passport{}, err
	}
	passport.UUID = uuid
	passport.ID = uuid.String()
	passport.Kind = identity.PassportKind(kind)
	passport.Status = identity.PassportStatus(status)
	if passport.Kind == identity.PassportKindPremium {
		premiumUUID := uuid
		passport.PremiumUUID = &premiumUUID
	}
	if rawOfflineName != nil {
		passport.RawOfflineName = *rawOfflineName
	}
	if registrationServer != nil {
		passport.RegistrationServer = *registrationServer
	}
	if lastSeenServer != nil {
		passport.LastSeenServer = *lastSeenServer
	}
	if lastSeenIP != nil {
		passport.LastSeenIP = *lastSeenIP
	}
	passport.LastSeenGeo = unmarshalIPGeo(lastSeenGeoRaw)
	passport.LastSeenAt = lastSeenAt
	return passport, nil
}

func scanProfileRow(row playerScanner) (identity.Profile, error) {
	var profile identity.Profile
	var uuidText string
	var status string
	var propertiesJSON []byte
	var createdFrom *string
	var lastSeenServer *string
	var lastSeenAt *time.Time
	var lastSeenIP *string
	var lastSeenGeoRaw []byte
	err := row.Scan(
		&profile.ID,
		&uuidText,
		&profile.ProtocolName,
		&profile.NormalizedName,
		&profile.DisplayName,
		&status,
		&profile.SkinSource,
		&propertiesJSON,
		&createdFrom,
		&lastSeenServer,
		&lastSeenAt,
		&lastSeenIP,
		&lastSeenGeoRaw,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		return identity.Profile{}, err
	}
	uuid, err := identity.ParseUUID(uuidText)
	if err != nil {
		return identity.Profile{}, err
	}
	profile.UUID = uuid
	profile.Status = identity.ProfileStatus(status)
	if len(propertiesJSON) > 0 {
		if err := json.Unmarshal(propertiesJSON, &profile.ProfileProperties); err != nil {
			return identity.Profile{}, err
		}
	}
	if createdFrom != nil {
		profile.CreatedFromPassport = *createdFrom
	}
	if lastSeenServer != nil {
		profile.LastSeenServer = *lastSeenServer
	}
	if lastSeenIP != nil {
		profile.LastSeenIP = *lastSeenIP
	}
	profile.LastSeenGeo = unmarshalIPGeo(lastSeenGeoRaw)
	profile.LastSeenAt = lastSeenAt
	return profile, nil
}

func scanProfileSkinRow(row playerScanner) (ProfileSkin, error) {
	var skin ProfileSkin
	err := row.Scan(
		&skin.ProfileID,
		&skin.Model,
		&skin.SkinPNG,
		&skin.SkinContentType,
		&skin.SkinSHA256,
		&skin.CapePNG,
		&skin.CapeContentType,
		&skin.CapeSHA256,
		&skin.ElytraPNG,
		&skin.ElytraContentType,
		&skin.ElytraSHA256,
		&skin.CreatedAt,
		&skin.UpdatedAt,
	)
	return skin, err
}

func unmarshalIPGeo(raw []byte) *identity.IPGeo {
	if len(raw) == 0 {
		return nil
	}
	var geo identity.IPGeo
	if err := json.Unmarshal(raw, &geo); err != nil {
		return nil
	}
	if geo.IP == "" {
		return nil
	}
	if geo.Locales == nil {
		geo.Locales = map[string]identity.IPGeoLocale{}
	}
	return &geo
}

func scanPassportCredentialRow(row playerScanner) (PassportCredential, error) {
	var credential PassportCredential
	var updatedAt *time.Time
	var lockedUntil *time.Time
	if err := row.Scan(&credential.PassportID, &credential.PasswordHash, &updatedAt, &credential.FailedAttempts, &lockedUntil); err != nil {
		return PassportCredential{}, err
	}
	credential.PasswordUpdatedAt = updatedAt
	credential.LockedUntil = lockedUntil
	return credential, nil
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

func scanTransferGrantRow(row playerScanner) (auth.TransferGrant, error) {
	var grant auth.TransferGrant
	err := scanTransferGrantRowInto(row, &grant)
	return grant, err
}

func scanTransferGrantRowInto(row playerScanner, grant *auth.TransferGrant) error {
	return row.Scan(
		&grant.ID,
		&grant.PlayerID,
		&grant.ServerID,
		&grant.PortalNodeID,
		&grant.PortalSource,
		&grant.GateNodeID,
		&grant.TokenHash,
		&grant.UUID,
		&grant.ProtocolName,
		&grant.TargetHost,
		&grant.TargetPort,
		&grant.CreatedAt,
		&grant.ExpiresAt,
		&grant.ConsumedAt,
	)
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
	var runtimeRaw []byte
	err := row.Scan(&n.ID, &n.ServerID, &n.Mode, &n.Name, &n.TokenHash, &n.TokenFingerprint, &n.InstanceFingerprint, &n.PluginVersion, &n.VelocityVersion, &n.Disabled, &runtimeRaw, &n.CreatedAt, &n.LastHeartbeatAt)
	if err != nil {
		return node.Node{}, err
	}
	n.Mode = node.NormalizeKind(n.Mode)
	if len(runtimeRaw) > 0 {
		_ = json.Unmarshal(runtimeRaw, &n.RuntimeConfig)
	}
	if n.RuntimeConfig == nil {
		n.RuntimeConfig = map[string]any{}
	}
	return n, nil
}

func scanMojangRouteRow(row playerScanner) (mojang.Route, error) {
	var route mojang.Route
	err := row.Scan(&route.ID, &route.Kind, &route.URL, &route.Weight, &route.Disabled)
	return route, err
}

func scanPresenceRow(row playerScanner) (PlayerPresence, error) {
	var presence PlayerPresence
	err := row.Scan(
		&presence.ID,
		&presence.PassportID,
		&presence.ProfileID,
		&presence.ServerID,
		&presence.NodeID,
		&presence.ProtocolName,
		&presence.UUID,
		&presence.RemoteAddr,
		&presence.ConnectedAt,
		&presence.LastSeenAt,
		&presence.EndedAt,
		&presence.EndReason,
	)
	return presence, err
}

func scanNodeActionRow(row playerScanner) (NodeAction, error) {
	var action NodeAction
	var actionType string
	err := row.Scan(
		&action.ID,
		&action.NodeID,
		&actionType,
		&action.PresenceID,
		&action.PassportID,
		&action.ProfileID,
		&action.UUID,
		&action.ProtocolName,
		&action.Reason,
		&action.CreatedAt,
		&action.ExpiresAt,
		&action.AckedAt,
	)
	action.Type = NodeActionType(actionType)
	return action, err
}

func scanBanRow(row playerScanner) (PlayerBan, error) {
	var ban PlayerBan
	err := row.Scan(
		&ban.ID,
		&ban.Scope,
		&ban.TargetID,
		&ban.Reason,
		&ban.CreatedBy,
		&ban.CreatedAt,
		&ban.ExpiresAt,
		&ban.RevokedBy,
		&ban.RevokedAt,
		&ban.RevokeReason,
	)
	return ban, err
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

func scanLimboBlueprintRow(row playerScanner) (LimboBlueprint, error) {
	var blueprint LimboBlueprint
	var previewRaw []byte
	var configRaw []byte
	err := row.Scan(
		&blueprint.ID,
		&blueprint.Name,
		&blueprint.Description,
		&blueprint.Filename,
		&blueprint.ContentType,
		&blueprint.SizeBytes,
		&blueprint.SHA256,
		&blueprint.Schematic,
		&previewRaw,
		&configRaw,
		&blueprint.CreatedAt,
		&blueprint.UpdatedAt,
	)
	if err != nil {
		return LimboBlueprint{}, err
	}
	if len(previewRaw) > 0 {
		if err := json.Unmarshal(previewRaw, &blueprint.Preview); err != nil {
			return LimboBlueprint{}, err
		}
	}
	if blueprint.Preview == nil {
		blueprint.Preview = map[string]any{}
	}
	if len(configRaw) > 0 {
		if err := json.Unmarshal(configRaw, &blueprint.Config); err != nil {
			return LimboBlueprint{}, err
		}
	}
	if blueprint.Config == nil {
		blueprint.Config = map[string]any{}
	}
	return cloneLimboBlueprint(blueprint), nil
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
	var selectedProfileID *string
	err := row.Scan(
		&session.ID,
		&kind,
		&session.SubjectID,
		&selectedProfileID,
		&session.CSRFToken,
		&session.CreatedAt,
		&session.ExpiresAt,
	)
	session.Kind = auth.SessionKind(kind)
	if selectedProfileID != nil {
		session.SelectedProfileID = *selectedProfileID
	}
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

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func defaultContentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "image/png"
	}
	return value
}

func normalizeSkinModel(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "slim") {
		return "slim"
	}
	return "wide"
}

func normalizeSkinSource(value string) string {
	switch strings.TrimSpace(value) {
	case "mojang", "custom", "none":
		return strings.TrimSpace(value)
	default:
		return "none"
	}
}

func marshalNullableGeo(geo *identity.IPGeo) (any, error) {
	if geo == nil {
		return nil, nil
	}
	raw, err := json.Marshal(geo)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

func detailString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	switch value := details[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func detailJSON(details map[string]any, key string) any {
	if details == nil || details[key] == nil {
		return nil
	}
	raw, err := json.Marshal(details[key])
	if err != nil {
		return nil
	}
	return string(raw)
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS passports (
	uuid text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('premium', 'offline')),
	username text NOT NULL,
	username_normalized text NOT NULL,
	raw_offline_name text,
	status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'locked', 'pending_verification', 'deleted')),
	registration_server_id text,
	last_seen_server_id text,
	last_seen_at timestamptz,
	last_seen_ip text,
	last_seen_geo jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT offline_passport_has_raw_name CHECK (kind != 'offline' OR raw_offline_name IS NOT NULL),
	CONSTRAINT premium_passport_has_no_raw_name CHECK (kind != 'premium' OR raw_offline_name IS NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS passports_offline_username_unique
	ON passports (username_normalized)
	WHERE kind = 'offline';

ALTER TABLE passports ADD COLUMN IF NOT EXISTS last_seen_ip text;
ALTER TABLE passports ADD COLUMN IF NOT EXISTS last_seen_geo jsonb;

CREATE TABLE IF NOT EXISTS profiles (
	uuid text PRIMARY KEY,
	protocol_name text NOT NULL,
	normalized_name text NOT NULL UNIQUE,
	display_name text NOT NULL,
	status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'locked', 'archived')),
	skin_source text NOT NULL DEFAULT 'none',
	profile_properties jsonb NOT NULL DEFAULT '[]'::jsonb,
	created_from_passport_id text REFERENCES passports(uuid) ON DELETE SET NULL,
	last_seen_server_id text,
	last_seen_at timestamptz,
	last_seen_ip text,
	last_seen_geo jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profile_passport_links (
	profile_id text PRIMARY KEY REFERENCES profiles(uuid) ON DELETE CASCADE,
	passport_id text NOT NULL REFERENCES passports(uuid) ON DELETE CASCADE,
	is_primary boolean NOT NULL DEFAULT false,
	linked_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS profile_passport_links_passport_idx ON profile_passport_links (passport_id);
CREATE UNIQUE INDEX IF NOT EXISTS profile_passport_links_primary_unique
	ON profile_passport_links (passport_id)
	WHERE is_primary = true;

ALTER TABLE profiles ADD COLUMN IF NOT EXISTS last_seen_ip text;
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS last_seen_geo jsonb;

CREATE TABLE IF NOT EXISTS profile_skins (
	profile_id text PRIMARY KEY REFERENCES profiles(uuid) ON DELETE CASCADE,
	model text NOT NULL DEFAULT 'wide' CHECK (model IN ('slim', 'wide')),
	skin_png bytea NOT NULL,
	skin_content_type text NOT NULL DEFAULT 'image/png',
	skin_sha256 text NOT NULL,
	cape_png bytea,
	cape_content_type text,
	cape_sha256 text,
	elytra_png bytea,
	elytra_content_type text,
	elytra_sha256 text,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE profile_skins ADD COLUMN IF NOT EXISTS elytra_png bytea;
ALTER TABLE profile_skins ADD COLUMN IF NOT EXISTS elytra_content_type text;
ALTER TABLE profile_skins ADD COLUMN IF NOT EXISTS elytra_sha256 text;

CREATE TABLE IF NOT EXISTS offline_passport_credentials (
	passport_id text PRIMARY KEY REFERENCES passports(uuid) ON DELETE CASCADE,
	password_hash text NOT NULL,
	failed_attempts integer NOT NULL DEFAULT 0,
	locked_until timestamptz,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS web_sessions (
	id text PRIMARY KEY,
	kind text NOT NULL CHECK (kind IN ('admin', 'player')),
	subject_id text NOT NULL,
	selected_profile_id text,
	csrf_token_hash text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL
);

ALTER TABLE web_sessions ADD COLUMN IF NOT EXISTS selected_profile_id text;
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
	schema_version integer NOT NULL DEFAULT 1,
	category text,
	outcome text,
	source text,
	session_id text,
	correlation_id text,
	actor_type text NOT NULL,
	actor_id text,
	target_type text NOT NULL,
	target_id text,
	event_type text NOT NULL,
	client_ip text,
	client_geo jsonb,
	request_id text,
	path text,
	method text,
	details jsonb NOT NULL DEFAULT '{}'::jsonb
);

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS schema_version integer NOT NULL DEFAULT 1;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS category text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS outcome text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS source text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS session_id text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS correlation_id text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS client_ip text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS client_geo jsonb;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS request_id text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS path text;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS method text;
CREATE INDEX IF NOT EXISTS audit_events_event_type_idx ON audit_events (event_type);
CREATE INDEX IF NOT EXISTS audit_events_category_idx ON audit_events (category);
CREATE INDEX IF NOT EXISTS audit_events_outcome_idx ON audit_events (outcome);
CREATE INDEX IF NOT EXISTS audit_events_client_ip_idx ON audit_events (client_ip);
CREATE INDEX IF NOT EXISTS audit_events_request_id_idx ON audit_events (request_id);
CREATE INDEX IF NOT EXISTS audit_events_session_id_idx ON audit_events (session_id);
CREATE INDEX IF NOT EXISTS audit_events_correlation_id_idx ON audit_events (correlation_id);
CREATE INDEX IF NOT EXISTS audit_events_details_gin_idx ON audit_events USING gin (details);

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

CREATE TABLE IF NOT EXISTS limbo_blueprints (
	id text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	filename text NOT NULL DEFAULT '',
	content_type text NOT NULL DEFAULT 'application/octet-stream',
	size_bytes bigint NOT NULL DEFAULT 0,
	sha256 text NOT NULL DEFAULT '',
	schematic bytea NOT NULL,
	preview jsonb NOT NULL DEFAULT '{}'::jsonb,
	config jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS limbo_blueprints_updated_at_idx ON limbo_blueprints (updated_at DESC);

CREATE TABLE IF NOT EXISTS velocity_nodes (
	id text PRIMARY KEY,
	server_id text NOT NULL DEFAULT 'default',
	mode text NOT NULL DEFAULT 'downstream_velocity',
	name text NOT NULL,
	token_hash text NOT NULL,
	token_fingerprint text NOT NULL,
	disabled boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	last_heartbeat_at timestamptz
);

ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS server_id text NOT NULL DEFAULT 'default';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS mode text NOT NULL DEFAULT 'downstream_velocity';
UPDATE velocity_nodes SET mode = 'limbo_portal' WHERE mode = 'portal';
UPDATE velocity_nodes SET mode = 'downstream_velocity' WHERE mode = 'gate';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS instance_fingerprint text NOT NULL DEFAULT '';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS plugin_version text NOT NULL DEFAULT '';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS velocity_version text NOT NULL DEFAULT '';
ALTER TABLE velocity_nodes ADD COLUMN IF NOT EXISTS runtime_config jsonb NOT NULL DEFAULT '{}'::jsonb;
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

CREATE TABLE IF NOT EXISTS system_settings (
	key text PRIMARY KEY,
	value jsonb NOT NULL DEFAULT '{}'::jsonb,
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS player_presences (
	id text PRIMARY KEY,
	passport_id text NOT NULL REFERENCES passports(uuid) ON DELETE CASCADE,
	profile_id text NOT NULL REFERENCES profiles(uuid) ON DELETE CASCADE,
	server_id text NOT NULL,
	node_id text NOT NULL DEFAULT '',
	protocol_name text NOT NULL,
	uuid text NOT NULL,
	remote_addr text NOT NULL DEFAULT '',
	connected_at timestamptz NOT NULL DEFAULT now(),
	last_seen_at timestamptz NOT NULL DEFAULT now(),
	ended_at timestamptz,
	end_reason text
);

CREATE INDEX IF NOT EXISTS player_presences_passport_active_idx ON player_presences (passport_id) WHERE ended_at IS NULL;
CREATE INDEX IF NOT EXISTS player_presences_profile_active_idx ON player_presences (profile_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS player_presences_profile_server_active_unique
	ON player_presences (profile_id, server_id)
	WHERE ended_at IS NULL;

CREATE TABLE IF NOT EXISTS node_actions (
	id text PRIMARY KEY,
	node_id text NOT NULL REFERENCES velocity_nodes(id) ON DELETE CASCADE,
	action_type text NOT NULL CHECK (action_type IN ('disconnect')),
	presence_id text NOT NULL DEFAULT '',
	passport_id text NOT NULL DEFAULT '',
	profile_id text NOT NULL DEFAULT '',
	uuid text NOT NULL DEFAULT '',
	protocol_name text NOT NULL DEFAULT '',
	reason text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz,
	acked_at timestamptz
);

CREATE INDEX IF NOT EXISTS node_actions_pending_idx
	ON node_actions (node_id, created_at)
	WHERE acked_at IS NULL;
CREATE INDEX IF NOT EXISTS node_actions_presence_idx ON node_actions (presence_id);
CREATE INDEX IF NOT EXISTS node_actions_profile_idx ON node_actions (profile_id);

CREATE TABLE IF NOT EXISTS player_bans (
	id text PRIMARY KEY,
	scope text NOT NULL CHECK (scope IN ('passport', 'profile')),
	target_id text NOT NULL,
	reason text NOT NULL,
	created_by text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz,
	revoked_by text,
	revoked_at timestamptz,
	revoke_reason text
);

CREATE INDEX IF NOT EXISTS player_bans_target_active_idx
	ON player_bans (scope, target_id)
	WHERE revoked_at IS NULL;

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
	'{"registration_strategy":"open","show_in_global":true,"host":"127.0.0.1","port":25565,"transfer_host":"127.0.0.1","transfer_port":25565,"motd":"Welcome to Authman","grant_required":true,"gate_enabled":true,"grant_ttl_seconds":45,"allowed_portal_sources":[],"portal_hosts":[]}'::jsonb,
	ARRAY['authman.identity']::text[]
)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS transfer_grants (
	id text PRIMARY KEY,
	player_id text NOT NULL REFERENCES profiles(uuid) ON DELETE CASCADE,
	server_id text NOT NULL REFERENCES downstream_servers(id) ON DELETE CASCADE,
	portal_node_id text NOT NULL DEFAULT '',
	portal_source text NOT NULL DEFAULT '',
	gate_node_id text NOT NULL DEFAULT '',
	token_hash text NOT NULL UNIQUE,
	uuid text NOT NULL,
	protocol_name text NOT NULL,
	target_host text NOT NULL,
	target_port integer NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL,
	consumed_at timestamptz
);

CREATE INDEX IF NOT EXISTS transfer_grants_token_hash_idx ON transfer_grants (token_hash);
CREATE INDEX IF NOT EXISTS transfer_grants_expires_at_idx ON transfer_grants (expires_at);
CREATE INDEX IF NOT EXISTS transfer_grants_server_player_idx ON transfer_grants (server_id, player_id);
ALTER TABLE transfer_grants ADD COLUMN IF NOT EXISTS portal_source text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS extension_player_data (
	id text PRIMARY KEY,
	server_id text NOT NULL,
	player_id text NOT NULL REFERENCES profiles(uuid) ON DELETE CASCADE,
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
