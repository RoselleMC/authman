package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoselleMC/authman/core/internal/audit"
	"github.com/RoselleMC/authman/core/internal/auth"
	"github.com/RoselleMC/authman/core/internal/identity"
	"github.com/RoselleMC/authman/core/internal/mojang"
	"github.com/RoselleMC/authman/core/internal/rbac"
	"github.com/go-webauthn/webauthn/webauthn"
)

type Memory struct {
	mu                   sync.RWMutex
	nextID               int
	nextAuditID          int
	passportsByID        map[string]identity.Passport
	profilesByID         map[string]identity.Profile
	passportByUsername   map[string]string
	profileLinks         map[string]identity.ProfilePassportLink
	profilesByPassport   map[string]map[string]struct{}
	playersByID          map[string]identity.Player
	offlineByNormalized  map[string]string
	protocolNameIndex    map[string]string
	credentialsByPlayer  map[string]OfflineCredential
	sessionsByID         map[string]auth.Session
	portalLinksByToken   map[string]auth.PortalLink
	auditEvents          []audit.Event
	mojangRoutes         map[string]mojang.Route
	systemSettings       map[string]map[string]any
	presencesByID        map[string]PlayerPresence
	bansByID             map[string]PlayerBan
	nodeActionsByID      map[string]NodeAction
	downstreamServers    map[string]DownstreamServer
	downstreamPrivileges map[string]map[string]DownstreamServerPrivilegedPassport
	limboBlueprints      map[string]LimboBlueprint
	profileSkins         map[string]ProfileSkin
	premiumTextures      map[string][]identity.ProfileProperty
	passportSkins        map[string]PassportSkin
	transferGrants       map[string]auth.TransferGrant
	extensionData        map[string]ExtensionPlayerData
	adminRoles           map[string]rbac.Role
	adminUsers           map[string]AdminUser
	adminProfiles        map[string]AdminProfile
	adminSecurity        map[string]AdminSecurity
	adminPasskeys        map[string]AdminPasskey
	pendingAdminMFAs     map[string]PendingAdminMFA
	adminTrustedDevices  map[string]AdminTrustedDevice
	adminPasswordResets  map[string]AdminPasswordReset
	externalAPITokens    map[string]ExternalAPIToken
}

func NewMemory() *Memory {
	m := &Memory{
		passportsByID:        make(map[string]identity.Passport),
		profilesByID:         make(map[string]identity.Profile),
		passportByUsername:   make(map[string]string),
		profileLinks:         make(map[string]identity.ProfilePassportLink),
		profilesByPassport:   make(map[string]map[string]struct{}),
		playersByID:          make(map[string]identity.Player),
		offlineByNormalized:  make(map[string]string),
		protocolNameIndex:    make(map[string]string),
		credentialsByPlayer:  make(map[string]OfflineCredential),
		sessionsByID:         make(map[string]auth.Session),
		portalLinksByToken:   make(map[string]auth.PortalLink),
		mojangRoutes:         make(map[string]mojang.Route),
		systemSettings:       make(map[string]map[string]any),
		presencesByID:        make(map[string]PlayerPresence),
		bansByID:             make(map[string]PlayerBan),
		nodeActionsByID:      make(map[string]NodeAction),
		downstreamServers:    make(map[string]DownstreamServer),
		downstreamPrivileges: make(map[string]map[string]DownstreamServerPrivilegedPassport),
		limboBlueprints:      make(map[string]LimboBlueprint),
		profileSkins:         make(map[string]ProfileSkin),
		premiumTextures:      make(map[string][]identity.ProfileProperty),
		passportSkins:        make(map[string]PassportSkin),
		transferGrants:       make(map[string]auth.TransferGrant),
		extensionData:        make(map[string]ExtensionPlayerData),
		adminRoles:           make(map[string]rbac.Role),
		adminUsers:           make(map[string]AdminUser),
		adminProfiles:        make(map[string]AdminProfile),
		adminSecurity:        make(map[string]AdminSecurity),
		adminPasskeys:        make(map[string]AdminPasskey),
		pendingAdminMFAs:     make(map[string]PendingAdminMFA),
		adminTrustedDevices:  make(map[string]AdminTrustedDevice),
		adminPasswordResets:  make(map[string]AdminPasswordReset),
		externalAPITokens:    make(map[string]ExternalAPIToken),
	}
	return m
}

func (m *Memory) CreateOfflinePassportProfile(ctx context.Context, rawName string, protocolName string, passwordHash string, encryptedPassword string, keyFingerprint string) (identity.PassportProfile, error) {
	name, err := identity.NormalizeOfflineName(rawName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	if strings.TrimSpace(protocolName) == "" {
		protocolName = rawName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.passportByUsername[name.Normalized]; ok {
		return identity.PassportProfile{}, fmt.Errorf("offline passport already exists")
	}
	passport, err := identity.NewOfflinePassport("", rawName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	uniqueName, err := m.uniqueProtocolNameLocked(protocolName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	profile, err := identity.NewOfflineProfile("", uniqueName.Protocol, passport.ID)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	pp := m.storePassportProfileLocked(passport, profile, true)
	m.credentialsByPlayer[passport.ID] = OfflineCredential{
		PlayerID:               passport.ID,
		PassportID:             passport.ID,
		PasswordHash:           passwordHash,
		EncryptedPassword:      encryptedPassword,
		PasswordKeyFingerprint: keyFingerprint,
	}
	return pp, nil
}

func (m *Memory) UpsertPremiumPassportProfile(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.PassportProfile, error) {
	protocolName := strings.TrimSpace(name)
	if protocolName == "" {
		return identity.PassportProfile{}, fmt.Errorf("premium name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, passport := range m.passportsByID {
		if passport.Kind == identity.PassportKindPremium && passport.UUID.String() == uuid.String() {
			passport.Username = protocolName
			passport.UsernameNormalized = strings.ToLower(protocolName)
			passport.UpdatedAt = time.Now().UTC()
			m.passportsByID[passport.ID] = passport
			profile, err := m.primaryProfileForPassportLocked(passport.ID)
			if err != nil {
				return identity.PassportProfile{}, err
			}
			profile.ProfileProperties = append([]identity.ProfileProperty(nil), properties...)
			profile.UpdatedAt = time.Now().UTC()
			m.profilesByID[profile.ID] = profile
			m.protocolNameIndex[strings.ToLower(profile.ProtocolName)] = profile.ID
			player := identity.PlayerFromPassportProfile(passport, profile)
			m.playersByID[player.ID] = player
			return identity.PassportProfile{Passport: passport, Profile: profile, Link: m.profileLinks[profile.ID]}, nil
		}
	}
	passport := identity.NewPremiumPassport("", protocolName, uuid)
	uniqueName, err := m.uniqueProtocolNameLocked(protocolName)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	profile, err := identity.NewPremiumProfile("", uniqueName.Protocol, uuid, properties, passport.ID)
	if err != nil {
		return identity.PassportProfile{}, err
	}
	return m.storePassportProfileLocked(passport, profile, true), nil
}

func (m *Memory) GetPassportByID(ctx context.Context, id string) (identity.Passport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	passport, ok := m.passportsByID[id]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, nil
}

func (m *Memory) GetPassportByUsername(ctx context.Context, username string) (identity.Passport, error) {
	name, err := identity.NormalizeOfflineName(username)
	if err != nil {
		return identity.Passport{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.passportByUsername[name.Normalized]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return m.passportsByID[id], nil
}

func (m *Memory) GetProfileByID(ctx context.Context, id string) (identity.Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	profile, ok := m.profilesByID[id]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return profile, nil
}

func (m *Memory) GetProfileByProtocolName(ctx context.Context, protocolName string) (identity.Profile, error) {
	key := strings.ToLower(strings.TrimSpace(protocolName))
	if key == "" {
		return identity.Profile{}, fmt.Errorf("protocol name is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.protocolNameIndex[key]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	return m.profilesByID[id], nil
}

func (m *Memory) GetPassportForProfile(ctx context.Context, profileID string) (identity.Passport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	link, ok := m.profileLinks[profileID]
	if !ok {
		return identity.Passport{}, fmt.Errorf("profile link not found: %w", ErrNotFound)
	}
	passport, ok := m.passportsByID[link.PassportID]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	return passport, nil
}

func (m *Memory) GetPrimaryProfileForPassport(ctx context.Context, passportID string) (identity.Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.primaryProfileForPassportLocked(passportID)
}

func (m *Memory) GetProfileSkin(ctx context.Context, profileID string) (ProfileSkin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	skin, ok := m.profileSkins[strings.TrimSpace(profileID)]
	if !ok {
		return ProfileSkin{}, fmt.Errorf("profile skin not found: %w", ErrNotFound)
	}
	return cloneProfileSkin(skin), nil
}

func (m *Memory) SetProfileSkin(ctx context.Context, profileID string, skin ProfileSkin, properties []identity.ProfileProperty) (identity.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profileID = strings.TrimSpace(profileID)
	profile, ok := m.profilesByID[profileID]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	now := time.Now().UTC()
	skin.ProfileID = profileID
	if skin.Model != "slim" {
		skin.Model = "wide"
	}
	if skin.SkinContentType == "" {
		skin.SkinContentType = "image/png"
	}
	if skin.CreatedAt.IsZero() {
		skin.CreatedAt = now
	}
	skin.UpdatedAt = now
	m.profileSkins[profileID] = cloneProfileSkin(skin)
	profile.SkinSource = "custom"
	profile.ProfileProperties = append([]identity.ProfileProperty(nil), properties...)
	profile.UpdatedAt = now
	m.profilesByID[profileID] = profile
	m.refreshProfilePlayerLocked(profileID)
	return profile, nil
}

func (m *Memory) DeleteProfileSkin(ctx context.Context, profileID string, properties []identity.ProfileProperty, skinSource string) (identity.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profileID = strings.TrimSpace(profileID)
	profile, ok := m.profilesByID[profileID]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	delete(m.profileSkins, profileID)
	if skinSource != "mojang" {
		skinSource = "none"
	}
	profile.SkinSource = skinSource
	profile.ProfileProperties = append([]identity.ProfileProperty(nil), properties...)
	profile.UpdatedAt = time.Now().UTC()
	m.profilesByID[profileID] = profile
	m.refreshProfilePlayerLocked(profileID)
	return profile, nil
}

func (m *Memory) SetProfileSkinSource(ctx context.Context, profileID string, skinSource string, properties []identity.ProfileProperty) (identity.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profileID = strings.TrimSpace(profileID)
	profile, ok := m.profilesByID[profileID]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	switch strings.TrimSpace(skinSource) {
	case "passport", "mojang", "custom", "none":
		profile.SkinSource = strings.TrimSpace(skinSource)
	default:
		profile.SkinSource = "passport"
	}
	profile.ProfileProperties = append([]identity.ProfileProperty(nil), properties...)
	profile.UpdatedAt = time.Now().UTC()
	m.profilesByID[profileID] = profile
	m.refreshProfilePlayerLocked(profileID)
	return profile, nil
}

func (m *Memory) GetPassportSkin(ctx context.Context, passportID string) (PassportSkin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	skin, ok := m.passportSkins[strings.TrimSpace(passportID)]
	if !ok {
		return PassportSkin{}, fmt.Errorf("passport skin not found: %w", ErrNotFound)
	}
	return clonePassportSkin(skin), nil
}

func (m *Memory) SetPassportSkin(ctx context.Context, passportID string, skin PassportSkin) (identity.Passport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID = strings.TrimSpace(passportID)
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	now := time.Now().UTC()
	skin.PassportID = passportID
	if skin.Model != "slim" {
		skin.Model = "wide"
	}
	if skin.SkinContentType == "" {
		skin.SkinContentType = "image/png"
	}
	if skin.CreatedAt.IsZero() {
		skin.CreatedAt = now
	}
	skin.UpdatedAt = now
	m.passportSkins[passportID] = clonePassportSkin(skin)
	passport.SkinSource = PassportSkinSourceCustom
	passport.UpdatedAt = now
	m.passportsByID[passportID] = passport
	return passport, nil
}

func (m *Memory) SetPassportSkinSource(ctx context.Context, passportID string, skinSource string) (identity.Passport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID = strings.TrimSpace(passportID)
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	passport.SkinSource = NormalizePassportSkinSource(passport.Kind, skinSource)
	passport.UpdatedAt = time.Now().UTC()
	m.passportsByID[passportID] = passport
	return passport, nil
}

func (m *Memory) DeletePassportSkin(ctx context.Context, passportID string) (identity.Passport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID = strings.TrimSpace(passportID)
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	delete(m.passportSkins, passportID)
	passport.SkinSource = PassportSkinSourceUpstream
	passport.UpdatedAt = time.Now().UTC()
	m.passportsByID[passportID] = passport
	return passport, nil
}

func (m *Memory) ListProfilesForPassport(ctx context.Context, passportID string) []identity.Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := m.profilesByPassport[passportID]
	out := make([]identity.Profile, 0, len(ids))
	for id := range ids {
		if profile, ok := m.profilesByID[id]; ok {
			out = append(out, profile)
		}
	}
	// Match Postgres ordering (primary first, then protocol name) so callers
	// that assume profiles[0] is primary behave identically across stores.
	sort.Slice(out, func(i, j int) bool {
		pi := m.profileLinks[out[i].ID].IsPrimary
		pj := m.profileLinks[out[j].ID].IsPrimary
		if pi != pj {
			return pi
		}
		return out[i].ProtocolName < out[j].ProtocolName
	})
	return out
}

func (m *Memory) ListPassports(ctx context.Context) []identity.Passport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]identity.Passport, 0, len(m.passportsByID))
	for _, passport := range m.passportsByID {
		out = append(out, passport)
	}
	return out
}

func (m *Memory) ListPassportsPage(ctx context.Context, query IdentityListQuery) ([]identity.Passport, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	search := strings.ToLower(strings.TrimSpace(query.Search))
	filtered := make([]identity.Passport, 0, len(m.passportsByID))
	for _, passport := range m.passportsByID {
		if query.Kind != "" && string(passport.Kind) != query.Kind {
			continue
		}
		if query.Status != "" && string(passport.Status) != query.Status {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(passport.ID), search) &&
			!strings.Contains(strings.ToLower(passport.Username), search) &&
			!strings.Contains(strings.ToLower(passport.UsernameNormalized), search) &&
			!strings.Contains(strings.ToLower(passport.UUID.String()), search) &&
			!strings.Contains(strings.ToLower(passport.UUID.Compact()), search) {
			continue
		}
		filtered = append(filtered, passport)
	}
	sortPassportsPage(filtered, query.Sort, query.Dir)
	start, end := listQueryBounds(len(filtered), query.Page, query.PageSize)
	return append([]identity.Passport(nil), filtered[start:end]...), len(filtered), nil
}

func (m *Memory) ListProfiles(ctx context.Context) []identity.Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]identity.Profile, 0, len(m.profilesByID))
	for _, profile := range m.profilesByID {
		out = append(out, profile)
	}
	return out
}

func (m *Memory) ListProfilesPage(ctx context.Context, query IdentityListQuery) ([]identity.Profile, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	search := strings.ToLower(strings.TrimSpace(query.Search))
	filtered := make([]identity.Profile, 0, len(m.profilesByID))
	for _, profile := range m.profilesByID {
		if query.Status != "" && string(profile.Status) != query.Status {
			continue
		}
		_, bound := m.profileLinks[profile.ID]
		if query.Binding == "bound" && !bound {
			continue
		}
		if query.Binding == "unbound" && bound {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(profile.ID), search) &&
			!strings.Contains(strings.ToLower(profile.ProtocolName), search) &&
			!strings.Contains(strings.ToLower(profile.NormalizedName), search) &&
			!strings.Contains(strings.ToLower(profile.UUID.String()), search) &&
			!strings.Contains(strings.ToLower(profile.UUID.Compact()), search) {
			continue
		}
		filtered = append(filtered, profile)
	}
	sortProfilesPage(filtered, query.Sort, query.Dir)
	start, end := listQueryBounds(len(filtered), query.Page, query.PageSize)
	return append([]identity.Profile(nil), filtered[start:end]...), len(filtered), nil
}

func listQueryBounds(total int, page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	} else if pageSize > 100 {
		pageSize = 100
	}
	start := (page - 1) * pageSize
	if start >= total {
		return total, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func sortPassportsPage(passports []identity.Passport, key string, dir string) {
	desc := dir == "desc"
	sort.SliceStable(passports, func(i, j int) bool {
		a := passports[i]
		b := passports[j]
		cmp := 0
		switch key {
		case "username":
			cmp = strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
		case "kind":
			cmp = strings.Compare(string(a.Kind), string(b.Kind))
		case "status":
			cmp = strings.Compare(string(a.Status), string(b.Status))
		case "uuid":
			cmp = strings.Compare(a.UUID.String(), b.UUID.String())
		case "lastSeen":
			cmp = compareOptionalTime(a.LastSeenAt, b.LastSeenAt)
		default:
			cmp = compareTimeDesc(a.CreatedAt, b.CreatedAt)
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func sortProfilesPage(profiles []identity.Profile, key string, dir string) {
	desc := dir == "desc"
	sort.SliceStable(profiles, func(i, j int) bool {
		a := profiles[i]
		b := profiles[j]
		cmp := 0
		switch key {
		case "protocol":
			cmp = strings.Compare(strings.ToLower(a.ProtocolName), strings.ToLower(b.ProtocolName))
		case "uuid":
			cmp = strings.Compare(a.UUID.String(), b.UUID.String())
		case "status":
			cmp = strings.Compare(string(a.Status), string(b.Status))
		case "lastSeen":
			cmp = compareOptionalTime(a.LastSeenAt, b.LastSeenAt)
		default:
			cmp = strings.Compare(strings.ToLower(a.ProtocolName), strings.ToLower(b.ProtocolName))
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareOptionalTime(a *time.Time, b *time.Time) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	return compareTimeAsc(*a, *b)
}

func compareTimeDesc(a time.Time, b time.Time) int {
	if a.Equal(b) {
		return 0
	}
	if a.After(b) {
		return -1
	}
	return 1
}

func compareTimeAsc(a time.Time, b time.Time) int {
	if a.Equal(b) {
		return 0
	}
	if a.Before(b) {
		return -1
	}
	return 1
}

func (m *Memory) CreateProfile(ctx context.Context, profile identity.Profile) (identity.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.protocolNameIndex[strings.ToLower(profile.ProtocolName)]; ok {
		return identity.Profile{}, fmt.Errorf("profile protocol name already exists")
	}
	if profile.UUID == (identity.UUID{}) {
		uuid, err := identity.RandomProfileUUID()
		if err != nil {
			return identity.Profile{}, err
		}
		profile.UUID = uuid
	}
	profile.ID = profile.UUID.String()
	if profile.Status == "" {
		profile.Status = identity.ProfileStatusActive
	}
	if profile.SkinSource == "" {
		profile.SkinSource = "passport"
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	m.profilesByID[profile.ID] = profile
	m.protocolNameIndex[strings.ToLower(profile.ProtocolName)] = profile.ID
	return profile, nil
}

func (m *Memory) BindProfileToPassport(ctx context.Context, profileID string, passportID string, primary bool) (identity.PassportProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return identity.PassportProfile{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	profile, ok := m.profilesByID[profileID]
	if !ok {
		return identity.PassportProfile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	if existing, ok := m.profileLinks[profileID]; ok && existing.PassportID != passportID {
		return identity.PassportProfile{}, fmt.Errorf("profile is already bound")
	}
	return m.linkProfileLocked(passport, profile, primary), nil
}

func (m *Memory) UnbindProfile(ctx context.Context, profileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, ok := m.profileLinks[profileID]
	if !ok {
		return fmt.Errorf("profile link not found: %w", ErrNotFound)
	}
	delete(m.profileLinks, profileID)
	delete(m.playersByID, profileID)
	if ids := m.profilesByPassport[link.PassportID]; ids != nil {
		delete(ids, profileID)
	}
	return nil
}

func (m *Memory) SetPassportStatus(ctx context.Context, id string, status identity.PassportStatus) (identity.Passport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passport, ok := m.passportsByID[id]
	if !ok {
		return identity.Passport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	passport.Status = status
	passport.UpdatedAt = time.Now().UTC()
	m.passportsByID[id] = passport
	if status == identity.PassportStatusDeleted {
		m.cleanupDeletedPassportReferencesLocked(id)
	}
	m.refreshPassportPlayersLocked(passport.ID)
	return passport, nil
}

func (m *Memory) cleanupDeletedPassportReferencesLocked(passportID string) {
	passportID = strings.TrimSpace(passportID)
	if passportID == "" {
		return
	}
	for serverID, rows := range m.downstreamPrivileges {
		delete(rows, passportID)
		if len(rows) == 0 {
			delete(m.downstreamPrivileges, serverID)
		}
	}
}

func (m *Memory) SetProfileStatus(ctx context.Context, id string, status identity.ProfileStatus) (identity.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile, ok := m.profilesByID[id]
	if !ok {
		return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
	}
	profile.Status = status
	profile.UpdatedAt = time.Now().UTC()
	m.profilesByID[id] = profile
	m.refreshProfilePlayerLocked(profile.ID)
	return profile, nil
}

func (m *Memory) GetPassportCredential(ctx context.Context, username string) (identity.Passport, PassportCredential, error) {
	passport, err := m.GetPassportByUsername(ctx, username)
	if err != nil {
		return identity.Passport{}, PassportCredential{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	credential, ok := m.credentialsByPlayer[passport.ID]
	if !ok {
		return identity.Passport{}, PassportCredential{}, fmt.Errorf("passport credential not found: %w", ErrNotFound)
	}
	return passport, passportCredentialFromOffline(credential), nil
}

func (m *Memory) RecordPassportLoginFailure(ctx context.Context, passportID string, now time.Time) (PassportCredential, error) {
	credential, err := m.RecordOfflineLoginFailure(ctx, passportID, now)
	return passportCredentialFromOffline(credential), err
}

func (m *Memory) RecordPassportLoginSuccess(ctx context.Context, passportID string) error {
	return m.RecordOfflineLoginSuccess(ctx, passportID)
}

func (m *Memory) RecordPlayerSeen(ctx context.Context, passportID string, profileID string, serverID string, ip string, geo *identity.IPGeo, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID = strings.TrimSpace(passportID)
	profileID = strings.TrimSpace(profileID)
	serverID = strings.TrimSpace(serverID)
	ip = strings.TrimSpace(ip)
	if passportID == "" && profileID != "" {
		if link, ok := m.profileLinks[profileID]; ok {
			passportID = link.PassportID
		}
	}
	seenAt := now.UTC()
	if passportID != "" {
		passport, ok := m.passportsByID[passportID]
		if ok {
			passport.LastSeenServer = serverID
			passport.LastSeenAt = &seenAt
			passport.LastSeenIP = ip
			passport.LastSeenGeo = cloneIPGeo(geo)
			passport.UpdatedAt = seenAt
			m.passportsByID[passportID] = passport
		}
	}
	if profileID != "" {
		profile, ok := m.profilesByID[profileID]
		if ok {
			profile.LastSeenServer = serverID
			profile.LastSeenAt = &seenAt
			profile.LastSeenIP = ip
			profile.LastSeenGeo = cloneIPGeo(geo)
			profile.UpdatedAt = seenAt
			m.profilesByID[profileID] = profile
			m.refreshProfilePlayerLocked(profileID)
		}
	} else if passportID != "" {
		m.refreshPassportPlayersLocked(passportID)
	}
	return nil
}

func (m *Memory) UpdatePassportPassword(ctx context.Context, passportID string, passwordHash string, encryptedPassword string, keyFingerprint string) error {
	return m.UpdateOfflinePassword(ctx, passportID, passwordHash, encryptedPassword, keyFingerprint)
}

func (m *Memory) SetPassportPasswordRecovery(ctx context.Context, passportID string, encryptedPassword string, keyFingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID = m.passportIDForCredentialLocked(passportID)
	credential, ok := m.credentialsByPlayer[passportID]
	if !ok {
		return fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	credential.PlayerID = passportID
	credential.PassportID = passportID
	credential.EncryptedPassword = encryptedPassword
	credential.PasswordKeyFingerprint = keyFingerprint
	credential.PasswordUpdatedAt = ptrTime(time.Now().UTC())
	m.credentialsByPlayer[passportID] = credential
	return nil
}

func (m *Memory) FactoryResetPlayerData(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passportsByID = make(map[string]identity.Passport)
	m.profilesByID = make(map[string]identity.Profile)
	m.passportByUsername = make(map[string]string)
	m.profileLinks = make(map[string]identity.ProfilePassportLink)
	m.profilesByPassport = make(map[string]map[string]struct{})
	m.playersByID = make(map[string]identity.Player)
	m.offlineByNormalized = make(map[string]string)
	m.protocolNameIndex = make(map[string]string)
	m.credentialsByPlayer = make(map[string]OfflineCredential)
	for id, session := range m.sessionsByID {
		if session.Kind == auth.SessionPlayer {
			delete(m.sessionsByID, id)
		}
	}
	m.portalLinksByToken = make(map[string]auth.PortalLink)
	m.auditEvents = nil
	m.presencesByID = make(map[string]PlayerPresence)
	m.bansByID = make(map[string]PlayerBan)
	for id, action := range m.nodeActionsByID {
		if action.PassportID != "" || action.ProfileID != "" || action.PresenceID != "" {
			delete(m.nodeActionsByID, id)
		}
	}
	m.downstreamPrivileges = make(map[string]map[string]DownstreamServerPrivilegedPassport)
	m.profileSkins = make(map[string]ProfileSkin)
	m.premiumTextures = make(map[string][]identity.ProfileProperty)
	m.passportSkins = make(map[string]PassportSkin)
	m.transferGrants = make(map[string]auth.TransferGrant)
	m.extensionData = make(map[string]ExtensionPlayerData)
	return nil
}

func cloneProfileSkin(skin ProfileSkin) ProfileSkin {
	skin.SkinPNG = append([]byte(nil), skin.SkinPNG...)
	skin.CapePNG = append([]byte(nil), skin.CapePNG...)
	skin.ElytraPNG = append([]byte(nil), skin.ElytraPNG...)
	return skin
}

func clonePassportSkin(skin PassportSkin) PassportSkin {
	skin.SkinPNG = append([]byte(nil), skin.SkinPNG...)
	skin.CapePNG = append([]byte(nil), skin.CapePNG...)
	skin.ElytraPNG = append([]byte(nil), skin.ElytraPNG...)
	return skin
}

func cloneIPGeo(geo *identity.IPGeo) *identity.IPGeo {
	if geo == nil {
		return nil
	}
	out := *geo
	out.Locales = map[string]identity.IPGeoLocale{}
	for key, value := range geo.Locales {
		out.Locales[key] = value
	}
	return &out
}

func (m *Memory) CreateOfflinePlayer(ctx context.Context, rawName string, passwordHash string) (identity.Player, error) {
	pp, err := m.CreateOfflinePassportProfile(ctx, rawName, rawName, passwordHash, "", "")
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfileLink(pp), nil
}

func (m *Memory) UpsertPremiumPlayer(ctx context.Context, name string, uuid identity.UUID, properties []identity.ProfileProperty) (identity.Player, error) {
	pp, err := m.UpsertPremiumPassportProfile(ctx, name, uuid, properties)
	if err != nil {
		return identity.Player{}, err
	}
	return identity.PlayerFromPassportProfileLink(pp), nil
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

func (m *Memory) GetPlayerByProtocolName(ctx context.Context, protocolName string) (identity.Player, error) {
	key := strings.ToLower(strings.TrimSpace(protocolName))
	if key == "" {
		return identity.Player{}, fmt.Errorf("protocol name is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.protocolNameIndex[key]
	if !ok {
		for candidateID, player := range m.playersByID {
			if strings.EqualFold(player.ProtocolName, protocolName) {
				id = candidateID
				ok = true
				break
			}
		}
	}
	if !ok {
		return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
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
	for _, passport := range m.passportsByID {
		if passport.Kind == identity.PassportKindPremium && strings.EqualFold(passport.UsernameNormalized, name.Normalized) {
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
	passport, err := m.GetPassportForProfile(ctx, player.ID)
	if err != nil {
		return identity.Player{}, OfflineCredential{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	credential, ok := m.credentialsByPlayer[passport.ID]
	if !ok {
		return identity.Player{}, OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	return player, credential, nil
}

func (m *Memory) GetPlayerByID(ctx context.Context, id string) (identity.Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	player, ok := m.playersByID[id]
	if ok {
		return player, nil
	}
	passport, ok := m.passportsByID[id]
	if ok {
		profile, err := m.primaryProfileForPassportLocked(id)
		if err != nil {
			return identity.Player{}, err
		}
		return identity.PlayerFromPassportProfile(passport, profile), nil
	}
	return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
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
	if passport, ok := m.passportsByID[id]; ok {
		if locked {
			passport.Status = identity.PassportStatusLocked
		} else {
			passport.Status = identity.PassportStatusActive
		}
		passport.UpdatedAt = time.Now().UTC()
		m.passportsByID[id] = passport
		m.refreshPassportPlayersLocked(id)
		profile, err := m.primaryProfileForPassportLocked(id)
		if err != nil {
			return identity.Player{}, err
		}
		return identity.PlayerFromPassportProfile(passport, profile), nil
	}
	profile, ok := m.profilesByID[id]
	if !ok {
		return identity.Player{}, fmt.Errorf("player not found: %w", ErrNotFound)
	}
	if locked {
		profile.Status = identity.ProfileStatusLocked
	} else {
		profile.Status = identity.ProfileStatusActive
	}
	profile.UpdatedAt = time.Now().UTC()
	m.profilesByID[id] = profile
	return m.refreshProfilePlayerLocked(id), nil
}

func (m *Memory) UpdateOfflinePassword(ctx context.Context, id string, passwordHash string, encryptedPassword string, keyFingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID := id
	if _, ok := m.passportsByID[passportID]; !ok {
		link, ok := m.profileLinks[id]
		if !ok {
			return fmt.Errorf("player not found: %w", ErrNotFound)
		}
		passportID = link.PassportID
	}
	passport := m.passportsByID[passportID]
	if passport.Kind != identity.PassportKindOffline {
		return fmt.Errorf("player is not offline")
	}
	credential := m.credentialsByPlayer[passportID]
	credential.PlayerID = passportID
	credential.PassportID = passportID
	credential.PasswordHash = passwordHash
	credential.EncryptedPassword = encryptedPassword
	credential.PasswordKeyFingerprint = keyFingerprint
	credential.PasswordUpdatedAt = ptrTime(time.Now().UTC())
	credential.FailedAttempts = 0
	credential.LockedUntil = nil
	m.credentialsByPlayer[passportID] = credential
	return nil
}

func (m *Memory) RecordOfflineLoginFailure(ctx context.Context, playerID string, now time.Time) (OfflineCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID := m.passportIDForCredentialLocked(playerID)
	credential, ok := m.credentialsByPlayer[passportID]
	if !ok {
		return OfflineCredential{}, fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	credential.FailedAttempts++
	if credential.FailedAttempts >= 5 {
		lockedUntil := now.UTC().Add(15 * time.Minute)
		credential.LockedUntil = &lockedUntil
	}
	credential.PlayerID = passportID
	credential.PassportID = passportID
	m.credentialsByPlayer[passportID] = credential
	return credential, nil
}

func (m *Memory) RecordOfflineLoginSuccess(ctx context.Context, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passportID := m.passportIDForCredentialLocked(playerID)
	credential, ok := m.credentialsByPlayer[passportID]
	if !ok {
		return fmt.Errorf("offline credential not found: %w", ErrNotFound)
	}
	credential.FailedAttempts = 0
	credential.LockedUntil = nil
	credential.PlayerID = passportID
	credential.PassportID = passportID
	m.credentialsByPlayer[passportID] = credential
	return nil
}

func (m *Memory) nextMemoryID(prefix string) string {
	m.nextID++
	return fmt.Sprintf("%s-%d", prefix, m.nextID)
}

func (m *Memory) uniqueProtocolNameLocked(raw string) (identity.OfflineName, error) {
	for attempt := 1; attempt <= 9999; attempt++ {
		name, err := uniqueProtocolCandidate(raw, attempt)
		if err != nil {
			return identity.OfflineName{}, err
		}
		if _, ok := m.protocolNameIndex[name.Normalized]; !ok {
			return name, nil
		}
	}
	return identity.OfflineName{}, fmt.Errorf("profile protocol name has no available unique variant")
}

func (m *Memory) storePassportProfileLocked(passport identity.Passport, profile identity.Profile, primary bool) identity.PassportProfile {
	passport.SkinSource = NormalizePassportSkinSource(passport.Kind, passport.SkinSource)
	m.passportsByID[passport.ID] = passport
	if passport.Kind == identity.PassportKindOffline {
		m.passportByUsername[passport.UsernameNormalized] = passport.ID
	}
	m.profilesByID[profile.ID] = profile
	m.protocolNameIndex[strings.ToLower(profile.ProtocolName)] = profile.ID
	return m.linkProfileLocked(passport, profile, primary)
}

func (m *Memory) linkProfileLocked(passport identity.Passport, profile identity.Profile, primary bool) identity.PassportProfile {
	if primary {
		for profileID, link := range m.profileLinks {
			if link.PassportID == passport.ID && link.IsPrimary {
				link.IsPrimary = false
				m.profileLinks[profileID] = link
			}
		}
	}
	link := identity.ProfilePassportLink{
		ProfileID:  profile.ID,
		PassportID: passport.ID,
		IsPrimary:  primary,
		LinkedAt:   time.Now().UTC(),
	}
	m.profileLinks[profile.ID] = link
	if m.profilesByPassport[passport.ID] == nil {
		m.profilesByPassport[passport.ID] = make(map[string]struct{})
	}
	m.profilesByPassport[passport.ID][profile.ID] = struct{}{}
	player := identity.PlayerFromPassportProfile(passport, profile)
	m.playersByID[player.ID] = player
	if passport.Kind == identity.PassportKindOffline {
		m.offlineByNormalized[passport.UsernameNormalized] = profile.ID
	}
	return identity.PassportProfile{Passport: passport, Profile: profile, Link: link}
}

func (m *Memory) primaryProfileForPassportLocked(passportID string) (identity.Profile, error) {
	ids := m.profilesByPassport[passportID]
	for profileID := range ids {
		if link, ok := m.profileLinks[profileID]; ok && link.IsPrimary {
			if profile, ok := m.profilesByID[profileID]; ok {
				return profile, nil
			}
		}
	}
	for profileID := range ids {
		if profile, ok := m.profilesByID[profileID]; ok {
			return profile, nil
		}
	}
	return identity.Profile{}, fmt.Errorf("profile not found: %w", ErrNotFound)
}

func (m *Memory) refreshPassportPlayersLocked(passportID string) {
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return
	}
	for profileID := range m.profilesByPassport[passportID] {
		if profile, ok := m.profilesByID[profileID]; ok {
			m.playersByID[profileID] = identity.PlayerFromPassportProfile(passport, profile)
		}
	}
}

func (m *Memory) refreshProfilePlayerLocked(profileID string) identity.Player {
	profile := m.profilesByID[profileID]
	link := m.profileLinks[profileID]
	passport := m.passportsByID[link.PassportID]
	player := identity.PlayerFromPassportProfile(passport, profile)
	m.playersByID[player.ID] = player
	return player
}

func (m *Memory) passportIDForCredentialLocked(id string) string {
	if _, ok := m.credentialsByPlayer[id]; ok {
		return id
	}
	if link, ok := m.profileLinks[id]; ok {
		return link.PassportID
	}
	return id
}

func passportCredentialFromOffline(credential OfflineCredential) PassportCredential {
	passportID := credential.PassportID
	if passportID == "" {
		passportID = credential.PlayerID
	}
	return PassportCredential{
		PassportID:             passportID,
		PasswordHash:           credential.PasswordHash,
		EncryptedPassword:      credential.EncryptedPassword,
		PasswordKeyFingerprint: credential.PasswordKeyFingerprint,
		PasswordUpdatedAt:      credential.PasswordUpdatedAt,
		FailedAttempts:         credential.FailedAttempts,
		LockedUntil:            credential.LockedUntil,
	}
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

func (m *Memory) GetAuditEvent(ctx context.Context, id string) (audit.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, event := range m.auditEvents {
		if event.ID == id {
			return event, nil
		}
	}
	return audit.Event{}, fmt.Errorf("audit event not found: %w", ErrNotFound)
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

func (m *Memory) ListAuditEventsPage(ctx context.Context, query AuditEventQuery) ([]audit.Event, int, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 25
	} else if query.PageSize > 100 {
		query.PageSize = 100
	}
	related := map[string]struct{}{}
	for _, id := range query.RelatedIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			related[id] = struct{}{}
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	filtered := make([]audit.Event, 0, len(m.auditEvents))
	for i := len(m.auditEvents) - 1; i >= 0; i-- {
		event := m.auditEvents[i]
		if query.ActorType != "" && string(event.ActorType) != query.ActorType {
			continue
		}
		if query.TargetType != "" && string(event.Target) != query.TargetType {
			continue
		}
		if query.EventType != "" && !strings.Contains(strings.ToLower(event.Type), strings.ToLower(query.EventType)) {
			continue
		}
		if query.Since != nil && event.Occurred.Before(*query.Since) {
			continue
		}
		if query.Until != nil && event.Occurred.After(*query.Until) {
			continue
		}
		if len(related) > 0 && !memoryAuditEventMatchesIDs(event, related) {
			continue
		}
		filtered = append(filtered, event)
	}
	total := len(filtered)
	start := (query.Page - 1) * query.PageSize
	if start >= total {
		return []audit.Event{}, total, nil
	}
	end := start + query.PageSize
	if end > total {
		end = total
	}
	return append([]audit.Event(nil), filtered[start:end]...), total, nil
}

func memoryAuditEventMatchesIDs(event audit.Event, ids map[string]struct{}) bool {
	if _, ok := ids[event.ActorID]; ok {
		return true
	}
	if _, ok := ids[event.TargetID]; ok {
		return true
	}
	for _, value := range event.Details {
		switch typed := value.(type) {
		case string:
			if _, ok := ids[typed]; ok {
				return true
			}
		case []string:
			for _, item := range typed {
				if _, ok := ids[item]; ok {
					return true
				}
			}
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					if _, exists := ids[text]; exists {
						return true
					}
				}
			}
		}
	}
	return false
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

func (m *Memory) GetMojangRoute(ctx context.Context, id string) (mojang.Route, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	route, ok := m.mojangRoutes[strings.TrimSpace(id)]
	if !ok {
		return mojang.Route{}, fmt.Errorf("mojang route not found: %w", ErrNotFound)
	}
	return route, nil
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

func (m *Memory) ListProfilePresences(ctx context.Context, profileID string) []PlayerPresence {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []PlayerPresence{}
	for _, presence := range m.presencesByID {
		if presence.ProfileID == profileID && presence.EndedAt == nil {
			out = append(out, presence)
		}
	}
	return out
}

func (m *Memory) ListPassportPresences(ctx context.Context, passportID string) []PlayerPresence {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []PlayerPresence{}
	for _, presence := range m.presencesByID {
		if presence.PassportID == passportID && presence.EndedAt == nil {
			out = append(out, presence)
		}
	}
	return out
}

func (m *Memory) UpsertPresence(ctx context.Context, presence PlayerPresence) (PlayerPresence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if presence.ID == "" {
		m.nextID++
		presence.ID = "presence-" + strconv.Itoa(m.nextID)
	}
	if presence.ConnectedAt.IsZero() {
		presence.ConnectedAt = now
	}
	if presence.LastSeenAt.IsZero() {
		presence.LastSeenAt = now
	}
	for _, existing := range m.presencesByID {
		if existing.EndedAt == nil && existing.ID != presence.ID && existing.ProfileID == presence.ProfileID && existing.ServerID == presence.ServerID {
			return PlayerPresence{}, fmt.Errorf("profile already online on server")
		}
	}
	m.presencesByID[presence.ID] = presence
	return presence, nil
}

func (m *Memory) EndPresence(ctx context.Context, id string, reason string, endedAt time.Time) (PlayerPresence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	presence, ok := m.presencesByID[id]
	if !ok {
		return PlayerPresence{}, fmt.Errorf("presence not found: %w", ErrNotFound)
	}
	when := endedAt.UTC()
	presence.EndedAt = &when
	presence.EndReason = strings.TrimSpace(reason)
	m.presencesByID[id] = presence
	return presence, nil
}

func (m *Memory) EndProfilePresences(ctx context.Context, profileID string, reason string, endedAt time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endPresencesLocked(func(p PlayerPresence) bool { return p.ProfileID == profileID }, reason, endedAt)
}

func (m *Memory) EndPassportPresences(ctx context.Context, passportID string, reason string, endedAt time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endPresencesLocked(func(p PlayerPresence) bool { return p.PassportID == passportID }, reason, endedAt)
}

func (m *Memory) endPresencesLocked(match func(PlayerPresence) bool, reason string, endedAt time.Time) int {
	count := 0
	when := endedAt.UTC()
	for id, presence := range m.presencesByID {
		if presence.EndedAt == nil && match(presence) {
			presence.EndedAt = &when
			presence.EndReason = strings.TrimSpace(reason)
			m.presencesByID[id] = presence
			count++
		}
	}
	return count
}

func (m *Memory) EnqueueNodeAction(ctx context.Context, action NodeAction) (NodeAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(action.NodeID) == "" {
		return NodeAction{}, fmt.Errorf("node action node id is required")
	}
	if action.Type == "" {
		action.Type = NodeActionDisconnect
	}
	if strings.TrimSpace(action.ID) == "" {
		m.nextID++
		action.ID = "node-action-" + strconv.Itoa(m.nextID)
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now().UTC()
	}
	m.nodeActionsByID[action.ID] = action
	return action, nil
}

func (m *Memory) ListPendingNodeActions(ctx context.Context, nodeID string, now time.Time, limit int) []NodeAction {
	nodeID = strings.TrimSpace(nodeID)
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []NodeAction{}
	for _, action := range m.nodeActionsByID {
		if action.NodeID != nodeID || action.AckedAt != nil {
			continue
		}
		if action.ExpiresAt != nil && !action.ExpiresAt.After(now.UTC()) {
			continue
		}
		out = append(out, action)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (m *Memory) AckNodeActions(ctx context.Context, nodeID string, ids []string, now time.Time) int {
	nodeID = strings.TrimSpace(nodeID)
	if len(ids) == 0 {
		return 0
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			idSet[trimmed] = struct{}{}
		}
	}
	when := now.UTC()
	count := 0
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range idSet {
		action, ok := m.nodeActionsByID[id]
		if !ok || action.NodeID != nodeID || action.AckedAt != nil {
			continue
		}
		action.AckedAt = &when
		m.nodeActionsByID[id] = action
		count++
	}
	return count
}

func (m *Memory) ListBans(ctx context.Context, scope BanScope, targetID string, includeInactive bool, now time.Time) []PlayerBan {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []PlayerBan{}
	for _, ban := range m.bansByID {
		if ban.Scope != scope || ban.TargetID != targetID {
			continue
		}
		if !includeInactive && !banActive(ban, now) {
			continue
		}
		out = append(out, ban)
	}
	return out
}

func (m *Memory) CreateBan(ctx context.Context, ban PlayerBan) (PlayerBan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ban.ID == "" {
		m.nextID++
		ban.ID = "ban-" + strconv.Itoa(m.nextID)
	}
	if ban.CreatedAt.IsZero() {
		ban.CreatedAt = time.Now().UTC()
	}
	m.bansByID[ban.ID] = ban
	return ban, nil
}

func (m *Memory) GetBan(ctx context.Context, id string) (PlayerBan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ban, ok := m.bansByID[strings.TrimSpace(id)]
	if !ok {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	return ban, nil
}

func (m *Memory) ExtendBan(ctx context.Context, id string, expiresAt time.Time) (PlayerBan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ban, ok := m.bansByID[strings.TrimSpace(id)]
	if !ok {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	when := expiresAt.UTC()
	ban.ExpiresAt = &when
	m.bansByID[id] = ban
	return ban, nil
}

func (m *Memory) RevokeBan(ctx context.Context, id string, revokedBy string, reason string, now time.Time) (PlayerBan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ban, ok := m.bansByID[id]
	if !ok {
		return PlayerBan{}, fmt.Errorf("ban not found: %w", ErrNotFound)
	}
	when := now.UTC()
	ban.RevokedAt = &when
	ban.RevokedBy = strings.TrimSpace(revokedBy)
	ban.RevokeReason = strings.TrimSpace(reason)
	m.bansByID[id] = ban
	return ban, nil
}

func (m *Memory) ActiveBanFor(ctx context.Context, passportID string, profileID string, now time.Time) (PlayerBan, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ban := range m.bansByID {
		if ban.Scope == BanScopePassport && ban.TargetID == passportID && banActive(ban, now) {
			return ban, true
		}
	}
	for _, ban := range m.bansByID {
		if ban.Scope == BanScopeProfile && ban.TargetID == profileID && banActive(ban, now) {
			return ban, true
		}
	}
	return PlayerBan{}, false
}

func banActive(ban PlayerBan, now time.Time) bool {
	if ban.RevokedAt != nil {
		return false
	}
	return ban.ExpiresAt == nil || ban.ExpiresAt.After(now.UTC())
}

func (m *Memory) GetSystemSetting(ctx context.Context, key string) (map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.systemSettings[strings.TrimSpace(key)]
	if !ok {
		return nil, fmt.Errorf("system setting not found: %w", ErrNotFound)
	}
	return cloneMap(value), nil
}

func (m *Memory) SetSystemSetting(ctx context.Context, key string, value map[string]any) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("system setting key is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemSettings[key] = cloneMap(value)
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
		uuid, err := identity.RandomProfileUUID()
		if err != nil {
			return DownstreamServer{}, err
		}
		server.ID = uuid.String()
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
	delete(m.downstreamPrivileges, id)
	return nil
}

func (m *Memory) ListDownstreamServerPrivilegedPassports(ctx context.Context, serverID string, query IdentityListQuery) ([]DownstreamServerPrivilegedPassport, int, error) {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.downstreamServers[serverID]; !ok {
		return nil, 0, fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	rows := make([]DownstreamServerPrivilegedPassport, 0, len(m.downstreamPrivileges[serverID]))
	for passportID, allow := range m.downstreamPrivileges[serverID] {
		passport, ok := m.passportsByID[passportID]
		if !ok {
			continue
		}
		if query.Kind != "" && string(passport.Kind) != query.Kind {
			continue
		}
		if query.Status != "" && string(passport.Status) != query.Status {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(passport.ID), search) &&
			!strings.Contains(strings.ToLower(passport.Username), search) &&
			!strings.Contains(strings.ToLower(passport.UsernameNormalized), search) &&
			!strings.Contains(strings.ToLower(passport.UUID.String()), search) &&
			!strings.Contains(strings.ToLower(passport.UUID.Compact()), search) {
			continue
		}
		allow.Passport = passport
		rows = append(rows, allow)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Passport.Username) < strings.ToLower(rows[j].Passport.Username)
	})
	total := len(rows)
	start, end := listQueryBounds(total, query.Page, query.PageSize)
	return append([]DownstreamServerPrivilegedPassport(nil), rows[start:end]...), total, nil
}

func (m *Memory) AddDownstreamServerPrivilegedPassport(ctx context.Context, serverID string, passportID string, createdBy string) (DownstreamServerPrivilegedPassport, error) {
	serverID = strings.TrimSpace(serverID)
	passportID = strings.TrimSpace(passportID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.downstreamServers[serverID]; !ok {
		return DownstreamServerPrivilegedPassport{}, fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	passport, ok := m.passportsByID[passportID]
	if !ok {
		return DownstreamServerPrivilegedPassport{}, fmt.Errorf("passport not found: %w", ErrNotFound)
	}
	if passport.Status == identity.PassportStatusDeleted {
		return DownstreamServerPrivilegedPassport{}, fmt.Errorf("passport is deleted")
	}
	if m.downstreamPrivileges[serverID] == nil {
		m.downstreamPrivileges[serverID] = make(map[string]DownstreamServerPrivilegedPassport)
	}
	if existing, ok := m.downstreamPrivileges[serverID][passportID]; ok {
		existing.Passport = passport
		return existing, nil
	}
	allow := DownstreamServerPrivilegedPassport{
		ServerID:   serverID,
		PassportID: passportID,
		Privileges: []string{"maintenance_join"},
		CreatedBy:  strings.TrimSpace(createdBy),
		CreatedAt:  time.Now().UTC(),
		Passport:   passport,
	}
	m.downstreamPrivileges[serverID][passportID] = allow
	return allow, nil
}

func (m *Memory) RemoveDownstreamServerPrivilegedPassport(ctx context.Context, serverID string, passportID string) error {
	serverID = strings.TrimSpace(serverID)
	passportID = strings.TrimSpace(passportID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.downstreamServers[serverID]; !ok {
		return fmt.Errorf("downstream server not found: %w", ErrNotFound)
	}
	if _, ok := m.downstreamPrivileges[serverID][passportID]; !ok {
		return fmt.Errorf("downstream server passport allow not found: %w", ErrNotFound)
	}
	delete(m.downstreamPrivileges[serverID], passportID)
	return nil
}

func (m *Memory) DownstreamServerHasPrivilegedPassport(ctx context.Context, serverID string, passportID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.downstreamPrivileges[strings.TrimSpace(serverID)][strings.TrimSpace(passportID)]
	return ok
}

func (m *Memory) ListLimboBlueprints(ctx context.Context) []LimboBlueprint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	blueprints := make([]LimboBlueprint, 0, len(m.limboBlueprints))
	for _, blueprint := range m.limboBlueprints {
		blueprints = append(blueprints, cloneLimboBlueprint(blueprint))
	}
	return blueprints
}

func (m *Memory) GetLimboBlueprint(ctx context.Context, id string) (LimboBlueprint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	blueprint, ok := m.limboBlueprints[id]
	if !ok {
		return LimboBlueprint{}, fmt.Errorf("limbo blueprint not found: %w", ErrNotFound)
	}
	return cloneLimboBlueprint(blueprint), nil
}

func (m *Memory) UpsertLimboBlueprint(ctx context.Context, blueprint LimboBlueprint) (LimboBlueprint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if strings.TrimSpace(blueprint.ID) == "" {
		m.nextID++
		blueprint.ID = "limbo-blueprint-" + strconv.Itoa(m.nextID)
		blueprint.CreatedAt = now
	} else if existing, ok := m.limboBlueprints[blueprint.ID]; ok && !existing.CreatedAt.IsZero() {
		blueprint.CreatedAt = existing.CreatedAt
	}
	if blueprint.CreatedAt.IsZero() {
		blueprint.CreatedAt = now
	}
	blueprint.UpdatedAt = now
	if blueprint.Preview == nil {
		blueprint.Preview = map[string]any{}
	}
	if blueprint.Config == nil {
		blueprint.Config = map[string]any{}
	}
	m.limboBlueprints[blueprint.ID] = cloneLimboBlueprint(blueprint)
	return cloneLimboBlueprint(blueprint), nil
}

func (m *Memory) DeleteLimboBlueprint(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.limboBlueprints[id]; !ok {
		return fmt.Errorf("limbo blueprint not found: %w", ErrNotFound)
	}
	delete(m.limboBlueprints, id)
	return nil
}

func (m *Memory) SaveTransferGrant(ctx context.Context, grant auth.TransferGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transferGrants[grant.TokenHash] = grant
	return nil
}

func (m *Memory) ConsumeTransferGrant(ctx context.Context, tokenHash string, serverID string, uuid string, protocolName string, gateNodeID string, allowedPortalSources []string, now time.Time) (auth.TransferGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	grant, ok := m.transferGrants[tokenHash]
	if !ok {
		return auth.TransferGrant{}, fmt.Errorf("transfer grant not found: %w", ErrNotFound)
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
	m.transferGrants[tokenHash] = grant
	return grant, nil
}

func portalGrantSourceAllowed(grant auth.TransferGrant, allowed []string) bool {
	if strings.HasPrefix(strings.TrimSpace(grant.PortalSource), "downstream-command:") {
		return true
	}
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		source := strings.TrimSpace(item)
		if source == "" {
			continue
		}
		if strings.EqualFold(source, grant.PortalSource) || strings.EqualFold(source, grant.PortalNodeID) {
			return true
		}
	}
	return false
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

func (m *Memory) ListAdminRoles(ctx context.Context) []rbac.Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roles := make([]rbac.Role, 0, len(m.adminRoles))
	for _, role := range m.adminRoles {
		roles = append(roles, cloneAdminRole(role))
	}
	return rbac.MergeDefaultRoles(roles)
}

func (m *Memory) ListAdminUsers(ctx context.Context) []AdminUser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]AdminUser, 0, len(m.adminUsers))
	for _, user := range m.adminUsers {
		user.PasswordHash = ""
		users = append(users, user)
	}
	return users
}

func (m *Memory) GetAdminUser(ctx context.Context, id string) (AdminUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.adminUsers[id]
	if !ok {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	return user, nil
}

func (m *Memory) FindAdminUserByIdentifier(ctx context.Context, identifier string) (AdminUser, error) {
	identifier = strings.TrimSpace(identifier)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, user := range m.adminUsers {
		if strings.EqualFold(user.Username, identifier) || (user.Email != "" && strings.EqualFold(user.Email, identifier)) {
			return user, nil
		}
	}
	return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
}

func (m *Memory) CreateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.adminUsers {
		if strings.EqualFold(existing.Username, user.Username) || (user.Email != "" && strings.EqualFold(existing.Email, user.Email)) {
			return AdminUser{}, fmt.Errorf("admin user already exists")
		}
	}
	m.nextID++
	user.ID = "admin-" + strconv.Itoa(m.nextID)
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.Status == "" {
		user.Status = "active"
	}
	m.adminUsers[user.ID] = user
	user.PasswordHash = ""
	return user, nil
}

func (m *Memory) UpdateAdminUserProfile(ctx context.Context, id string, username string, email string) (AdminUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.adminUsers[id]
	if !ok {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	for _, existing := range m.adminUsers {
		if existing.ID == id {
			continue
		}
		if strings.EqualFold(existing.Username, username) || (email != "" && strings.EqualFold(existing.Email, email)) {
			return AdminUser{}, fmt.Errorf("admin user already exists")
		}
	}
	user.Username = username
	user.Email = email
	user.UpdatedAt = time.Now().UTC()
	m.adminUsers[id] = user
	user.PasswordHash = ""
	return user, nil
}

func (m *Memory) UpdateAdminUser(ctx context.Context, user AdminUser) (AdminUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.adminUsers[user.ID]
	if !ok {
		return AdminUser{}, fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	for _, other := range m.adminUsers {
		if other.ID == user.ID {
			continue
		}
		if strings.EqualFold(other.Username, user.Username) || (user.Email != "" && strings.EqualFold(other.Email, user.Email)) {
			return AdminUser{}, fmt.Errorf("admin user already exists")
		}
	}
	existing.Username = user.Username
	existing.Email = user.Email
	existing.Role = user.Role
	existing.Status = user.Status
	existing.UpdatedAt = time.Now().UTC()
	m.adminUsers[user.ID] = existing
	existing.PasswordHash = ""
	return existing, nil
}

func (m *Memory) UpdateAdminUserPassword(ctx context.Context, id string, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.adminUsers[id]
	if !ok {
		return fmt.Errorf("admin user not found: %w", ErrNotFound)
	}
	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now().UTC()
	m.adminUsers[id] = user
	return nil
}

func (m *Memory) GetAdminProfile(ctx context.Context, adminID string) (AdminProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	profile, ok := m.adminProfiles[adminID]
	if !ok {
		return AdminProfile{}, fmt.Errorf("admin profile not found: %w", ErrNotFound)
	}
	return profile, nil
}

func (m *Memory) UpsertAdminProfile(ctx context.Context, profile AdminProfile) (AdminProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if profile.AdminID == "" {
		return AdminProfile{}, fmt.Errorf("admin id is required")
	}
	if existing, ok := m.adminProfiles[profile.AdminID]; ok && !existing.CreatedAt.IsZero() {
		profile.CreatedAt = existing.CreatedAt
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	m.adminProfiles[profile.AdminID] = profile
	return profile, nil
}

func (m *Memory) UpsertAdminRole(ctx context.Context, role rbac.Role) (rbac.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
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
	if existing, ok := m.adminRoles[role.ID]; ok && !existing.CreatedAt.IsZero() {
		role.CreatedAt = existing.CreatedAt
	}
	if role.CreatedAt.IsZero() {
		role.CreatedAt = now
	}
	role.UpdatedAt = now
	role.System = false
	role.Permissions = rbac.NormalizePermissions(role.Permissions)
	m.adminRoles[role.ID] = cloneAdminRole(role)
	return cloneAdminRole(role), nil
}

func (m *Memory) DeleteAdminRole(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id = rbac.RoleID(id)
	if base, ok := rbac.DefaultRole(id); ok && base.System {
		return fmt.Errorf("system role cannot be deleted")
	}
	if _, ok := m.adminRoles[id]; !ok {
		return fmt.Errorf("role not found: %w", ErrNotFound)
	}
	for _, user := range m.adminUsers {
		if user.Role == id {
			return fmt.Errorf("role is still assigned")
		}
	}
	delete(m.adminRoles, id)
	return nil
}

func (m *Memory) GetAdminSecurity(ctx context.Context, adminID string) (AdminSecurity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	security, ok := m.adminSecurity[adminID]
	if !ok {
		return defaultAdminSecurity(adminID), nil
	}
	return security, nil
}

func (m *Memory) UpsertAdminSecurity(ctx context.Context, security AdminSecurity) (AdminSecurity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if security.AdminID == "" {
		return AdminSecurity{}, fmt.Errorf("admin id is required")
	}
	if existing, ok := m.adminSecurity[security.AdminID]; ok && !existing.CreatedAt.IsZero() {
		security.CreatedAt = existing.CreatedAt
	}
	if security.CreatedAt.IsZero() {
		security.CreatedAt = now
	}
	if security.MFARequirement == "" {
		security.MFARequirement = "new_device"
	}
	security.UpdatedAt = now
	m.adminSecurity[security.AdminID] = security
	return security, nil
}

func (m *Memory) ListAdminPasskeys(ctx context.Context, adminID string) []AdminPasskey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	passkeys := make([]AdminPasskey, 0)
	for _, passkey := range m.adminPasskeys {
		if passkey.AdminID == adminID {
			passkeys = append(passkeys, passkey)
		}
	}
	return passkeys
}

func (m *Memory) CreateAdminPasskey(ctx context.Context, passkey AdminPasskey) (AdminPasskey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	passkey.ID = "passkey-" + strconv.Itoa(m.nextID)
	now := time.Now().UTC()
	passkey.CreatedAt = now
	m.adminPasskeys[passkey.ID] = passkey
	return passkey, nil
}

func (m *Memory) UpdateAdminPasskeyCredential(ctx context.Context, id string, credential webauthn.Credential, lastUsedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passkey, ok := m.adminPasskeys[id]
	if !ok {
		return fmt.Errorf("passkey not found: %w", ErrNotFound)
	}
	passkey.Credential = credential
	passkey.LastUsedAt = ptrTime(lastUsedAt.UTC())
	m.adminPasskeys[id] = passkey
	return nil
}

func (m *Memory) DeleteAdminPasskey(ctx context.Context, adminID string, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	passkey, ok := m.adminPasskeys[id]
	if !ok || passkey.AdminID != adminID {
		return fmt.Errorf("passkey not found: %w", ErrNotFound)
	}
	delete(m.adminPasskeys, id)
	return nil
}

func (m *Memory) SavePendingAdminMFA(ctx context.Context, pending PendingAdminMFA) (PendingAdminMFA, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	pending.ID = "mfa-" + strconv.Itoa(m.nextID)
	m.pendingAdminMFAs[pending.ID] = pending
	return pending, nil
}

func (m *Memory) GetPendingAdminMFA(ctx context.Context, id string) (PendingAdminMFA, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pending, ok := m.pendingAdminMFAs[id]
	if !ok {
		return PendingAdminMFA{}, fmt.Errorf("pending mfa not found: %w", ErrNotFound)
	}
	return pending, nil
}

func (m *Memory) DeletePendingAdminMFA(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pendingAdminMFAs, id)
	return nil
}

func (m *Memory) CreateAdminTrustedDevice(ctx context.Context, device AdminTrustedDevice) (AdminTrustedDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	device.ID = "trusted-" + strconv.Itoa(m.nextID)
	device.CreatedAt = time.Now().UTC()
	m.adminTrustedDevices[device.ID] = device
	return device, nil
}

func (m *Memory) GetAdminTrustedDevice(ctx context.Context, tokenHash string, now time.Time) (AdminTrustedDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, device := range m.adminTrustedDevices {
		if device.TokenHash == tokenHash && now.UTC().Before(device.ExpiresAt) {
			return device, nil
		}
	}
	return AdminTrustedDevice{}, fmt.Errorf("trusted device not found: %w", ErrNotFound)
}

func (m *Memory) SaveAdminPasswordReset(ctx context.Context, reset AdminPasswordReset) (AdminPasswordReset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	reset.ID = "admin-reset-" + strconv.Itoa(m.nextID)
	if reset.CreatedAt.IsZero() {
		reset.CreatedAt = time.Now().UTC()
	}
	m.adminPasswordResets[reset.ID] = reset
	return reset, nil
}

func (m *Memory) GetAdminPasswordReset(ctx context.Context, tokenHash string, now time.Time) (AdminPasswordReset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, reset := range m.adminPasswordResets {
		if reset.TokenHash == tokenHash && reset.UsedAt == nil && now.UTC().Before(reset.ExpiresAt) {
			return reset, nil
		}
	}
	return AdminPasswordReset{}, fmt.Errorf("password reset not found: %w", ErrNotFound)
}

func (m *Memory) MarkAdminPasswordResetUsed(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	reset, ok := m.adminPasswordResets[id]
	if !ok {
		return fmt.Errorf("password reset not found: %w", ErrNotFound)
	}
	usedAt := now.UTC()
	reset.UsedAt = &usedAt
	m.adminPasswordResets[id] = reset
	return nil
}

func (m *Memory) ListExternalAPITokens(ctx context.Context) []ExternalAPIToken {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tokens := make([]ExternalAPIToken, 0, len(m.externalAPITokens))
	for _, token := range m.externalAPITokens {
		tokens = append(tokens, cloneExternalAPIToken(token))
	}
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i].CreatedAt.After(tokens[j].CreatedAt)
	})
	return tokens
}

func (m *Memory) GetExternalAPIToken(ctx context.Context, id string) (ExternalAPIToken, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	token, ok := m.externalAPITokens[id]
	if !ok {
		return ExternalAPIToken{}, fmt.Errorf("external api token not found: %w", ErrNotFound)
	}
	return cloneExternalAPIToken(token), nil
}

func (m *Memory) CreateExternalAPIToken(ctx context.Context, token ExternalAPIToken) (ExternalAPIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	token.ID = "extapi-" + strconv.Itoa(m.nextID)
	now := time.Now().UTC()
	token.CreatedAt = now
	token.UpdatedAt = now
	if token.Status == "" {
		token.Status = ExternalAPITokenActive
	}
	m.externalAPITokens[token.ID] = token
	return cloneExternalAPIToken(token), nil
}

func (m *Memory) UpdateExternalAPIToken(ctx context.Context, token ExternalAPIToken) (ExternalAPIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.externalAPITokens[token.ID]
	if !ok {
		return ExternalAPIToken{}, fmt.Errorf("external api token not found: %w", ErrNotFound)
	}
	existing.Name = token.Name
	existing.Status = token.Status
	existing.UpdatedAt = time.Now().UTC()
	m.externalAPITokens[token.ID] = existing
	return cloneExternalAPIToken(existing), nil
}

func (m *Memory) DeleteExternalAPIToken(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.externalAPITokens[id]; !ok {
		return fmt.Errorf("external api token not found: %w", ErrNotFound)
	}
	delete(m.externalAPITokens, id)
	return nil
}

func (m *Memory) AuthenticateExternalAPIToken(ctx context.Context, rawToken string, now time.Time, clientIP string, path string) (ExternalAPIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, token := range m.externalAPITokens {
		if auth.ConstantTimeTokenEqual("external-api", rawToken, token.TokenHash) {
			if token.Status != ExternalAPITokenActive {
				return ExternalAPIToken{}, fmt.Errorf("external api token disabled: %w", ErrNotFound)
			}
			token.CallCount++
			usedAt := now.UTC()
			token.LastUsedAt = &usedAt
			token.LastUsedIP = strings.TrimSpace(clientIP)
			token.LastUsedPath = strings.TrimSpace(path)
			token.UpdatedAt = usedAt
			m.externalAPITokens[id] = token
			return cloneExternalAPIToken(token), nil
		}
	}
	return ExternalAPIToken{}, fmt.Errorf("external api token not found: %w", ErrNotFound)
}

func cloneAdminRole(role rbac.Role) rbac.Role {
	role.Permissions = append([]string(nil), role.Permissions...)
	return role
}

func cloneExternalAPIToken(token ExternalAPIToken) ExternalAPIToken {
	if token.LastUsedAt != nil {
		usedAt := token.LastUsedAt.UTC()
		token.LastUsedAt = &usedAt
	}
	return token
}

func defaultAdminSecurity(adminID string) AdminSecurity {
	return AdminSecurity{
		AdminID:         adminID,
		MFARequirement:  "new_device",
		PreferredLocale: "system",
		PreferredTheme:  "system",
	}
}
