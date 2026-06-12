package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Passport-only lifecycle: registration and premium verification no longer
// create an implicit same-named profile. Profiles are created explicitly by
// the player (limbo profile dialog / web portal) or by admins.

// CreateOfflinePassport stores a new offline passport with credentials and no
// profiles.
func (p *Postgres) CreateOfflinePassport(ctx context.Context, rawName string, passwordHash string, encryptedPassword string, keyFingerprint string) (identity.Passport, error) {
	passport, err := identity.NewOfflinePassport("", rawName)
	if err != nil {
		return identity.Passport{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return identity.Passport{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO passports (uuid, kind, username, username_normalized, raw_offline_name, status, skin_source, created_at, updated_at)
		VALUES ($1, 'offline', $2, $3, $4, $5, $6, $7, $8)
	`, passport.UUID.String(), passport.Username, passport.UsernameNormalized, passport.RawOfflineName, passport.Status, NormalizePassportSkinSource(passport.Kind, passport.SkinSource), passport.CreatedAt, passport.UpdatedAt); err != nil {
		return identity.Passport{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO offline_passport_credentials (passport_id, password_hash, encrypted_password, password_key_fingerprint)
		VALUES ($1, $2, $3, $4)
	`, passport.ID, passwordHash, encryptedPassword, keyFingerprint); err != nil {
		return identity.Passport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Passport{}, err
	}
	return passport, nil
}

// UpsertPremiumPassport stores or refreshes a premium passport without
// touching its profiles. Skins resolve at serve time through the passport
// skin pipeline, so Mojang properties are not persisted here.
func (p *Postgres) UpsertPremiumPassport(ctx context.Context, name string, uuid identity.UUID) (identity.Passport, error) {
	username := strings.TrimSpace(name)
	if username == "" {
		return identity.Passport{}, fmt.Errorf("premium name is required")
	}
	passport := identity.NewPremiumPassport("", username, uuid)
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		INSERT INTO passports (uuid, kind, username, username_normalized, status, skin_source, created_at, updated_at)
		VALUES ($1, 'premium', $2, $3, 'active', $4, $5, $6)
		ON CONFLICT (uuid) DO UPDATE
		SET username = EXCLUDED.username,
			username_normalized = EXCLUDED.username_normalized,
			updated_at = now()
		RETURNING `+passportSelectColumns+`
	`, passport.UUID.String(), passport.Username, strings.ToLower(passport.Username), NormalizePassportSkinSource(passport.Kind, passport.SkinSource), passport.CreatedAt, passport.UpdatedAt))
	if err != nil {
		return identity.Passport{}, err
	}
	return passport, nil
}

// SetPassportPremiumTextures persists the verified Mojang profile properties
// (skin/cape) for a premium passport so they survive across logins and feed
// the passport skin pipeline.
func (p *Postgres) SetPassportPremiumTextures(ctx context.Context, passportID string, properties []identity.ProfileProperty) error {
	if len(properties) == 0 {
		return nil
	}
	encoded, err := json.Marshal(properties)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO passport_premium_textures (passport_id, properties, updated_at)
		VALUES ($1, $2::jsonb, now())
		ON CONFLICT (passport_id) DO UPDATE
		SET properties = EXCLUDED.properties, updated_at = now()
	`, passportID, string(encoded))
	return err
}

// GetPassportPremiumTextures returns the stored verified Mojang properties for
// a premium passport, or nil when none are stored.
func (p *Postgres) GetPassportPremiumTextures(ctx context.Context, passportID string) []identity.ProfileProperty {
	var raw []byte
	if err := p.pool.QueryRow(ctx, `SELECT properties FROM passport_premium_textures WHERE passport_id = $1`, passportID).Scan(&raw); err != nil {
		return nil
	}
	var props []identity.ProfileProperty
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil
	}
	return props
}

// GetPremiumPassportByUsername finds a premium passport by its Mojang name.
func (p *Postgres) GetPremiumPassportByUsername(ctx context.Context, username string) (identity.Passport, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if normalized == "" {
		return identity.Passport{}, fmt.Errorf("username is required")
	}
	passport, err := scanPassportRow(p.pool.QueryRow(ctx, `
		SELECT `+passportSelectColumns+`
		FROM passports
		WHERE kind = 'premium' AND username_normalized = $1
	`, normalized))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, err
}

// CreateOfflinePassport stores a new offline passport with credentials and no
// profiles (in-memory variant).
func (m *Memory) CreateOfflinePassport(ctx context.Context, rawName string, passwordHash string, encryptedPassword string, keyFingerprint string) (identity.Passport, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.Passport{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.passportByUsername[name.Normalized]; ok {
		return identity.Passport{}, fmt.Errorf("offline passport already exists")
	}
	passport, err := identity.NewOfflinePassport("", rawName)
	if err != nil {
		return identity.Passport{}, err
	}
	passport.SkinSource = NormalizePassportSkinSource(passport.Kind, passport.SkinSource)
	m.passportsByID[passport.ID] = passport
	m.passportByUsername[passport.UsernameNormalized] = passport.ID
	m.credentialsByPlayer[passport.ID] = OfflineCredential{
		PlayerID:               passport.ID,
		PassportID:             passport.ID,
		PasswordHash:           passwordHash,
		EncryptedPassword:      encryptedPassword,
		PasswordKeyFingerprint: keyFingerprint,
	}
	return passport, nil
}

// UpsertPremiumPassport stores or refreshes a premium passport without
// touching its profiles (in-memory variant).
func (m *Memory) UpsertPremiumPassport(ctx context.Context, name string, uuid identity.UUID) (identity.Passport, error) {
	username := strings.TrimSpace(name)
	if username == "" {
		return identity.Passport{}, fmt.Errorf("premium name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, passport := range m.passportsByID {
		if passport.Kind == identity.PassportKindPremium && passport.UUID.String() == uuid.String() {
			passport.Username = username
			passport.UsernameNormalized = strings.ToLower(username)
			passport.UpdatedAt = time.Now().UTC()
			m.passportsByID[id] = passport
			return passport, nil
		}
	}
	passport := identity.NewPremiumPassport("", username, uuid)
	passport.SkinSource = NormalizePassportSkinSource(passport.Kind, passport.SkinSource)
	m.passportsByID[passport.ID] = passport
	return passport, nil
}

// SetPassportPremiumTextures persists verified Mojang properties (in-memory).
func (m *Memory) SetPassportPremiumTextures(ctx context.Context, passportID string, properties []identity.ProfileProperty) error {
	if len(properties) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.premiumTextures == nil {
		m.premiumTextures = map[string][]identity.ProfileProperty{}
	}
	m.premiumTextures[passportID] = append([]identity.ProfileProperty(nil), properties...)
	return nil
}

// GetPassportPremiumTextures returns stored verified Mojang properties (in-memory).
func (m *Memory) GetPassportPremiumTextures(ctx context.Context, passportID string) []identity.ProfileProperty {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]identity.ProfileProperty(nil), m.premiumTextures[passportID]...)
}

// GetPremiumPassportByUsername finds a premium passport by its Mojang name
// (in-memory variant).
func (m *Memory) GetPremiumPassportByUsername(ctx context.Context, username string) (identity.Passport, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if normalized == "" {
		return identity.Passport{}, fmt.Errorf("username is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, passport := range m.passportsByID {
		if passport.Kind == identity.PassportKindPremium && passport.UsernameNormalized == normalized {
			return passport, nil
		}
	}
	return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
}
