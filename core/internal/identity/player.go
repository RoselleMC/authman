package identity

import (
	"fmt"
	"strings"
	"time"
)

type PlayerKind string

const (
	PlayerKindPremium PlayerKind = "premium"
	PlayerKindOffline PlayerKind = "offline"
)

type PassportKind = PlayerKind

const (
	PassportKindPremium PassportKind = PlayerKindPremium
	PassportKindOffline PassportKind = PlayerKindOffline
)

type PassportStatus string

const (
	PassportStatusActive              PassportStatus = "active"
	PassportStatusLocked              PassportStatus = "locked"
	PassportStatusPendingVerification PassportStatus = "pending_verification"
	PassportStatusDeleted             PassportStatus = "deleted"
)

type ProfileStatus string

const (
	ProfileStatusActive   ProfileStatus = "active"
	ProfileStatusLocked   ProfileStatus = "locked"
	ProfileStatusArchived ProfileStatus = "archived"
)

type Passport struct {
	ID                 string
	Kind               PassportKind
	UUID               UUID
	PremiumUUID        *UUID
	Username           string
	UsernameNormalized string
	RawOfflineName     string
	Status             PassportStatus
	SkinSource         string
	RegistrationServer string
	LastSeenServer     string
	LastSeenAt         *time.Time
	LastSeenIP         string
	LastSeenGeo        *IPGeo
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Profile struct {
	ID                  string
	UUID                UUID
	ProtocolName        string
	NormalizedName      string
	DisplayName         string
	Status              ProfileStatus
	SkinSource          string
	ProfileProperties   []ProfileProperty
	CreatedFromPassport string
	LastSeenServer      string
	LastSeenAt          *time.Time
	LastSeenIP          string
	LastSeenGeo         *IPGeo
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ProfilePassportLink struct {
	ProfileID  string
	PassportID string
	IsPrimary  bool
	LinkedAt   time.Time
}

type PassportProfile struct {
	Passport Passport
	Profile  Profile
	Link     ProfilePassportLink
}

type Player struct {
	ID                 string
	Kind               PlayerKind
	UUID               UUID
	PremiumUUID        *UUID
	RawOfflineName     string
	NormalizedName     string
	ProtocolName       string
	Locked             bool
	ProfileProperties  []ProfileProperty
	RegistrationServer string
	LastSeenServer     string
	LastSeenAt         *time.Time
	LastSeenIP         string
	LastSeenGeo        *IPGeo
}

type ProfileProperty struct {
	Name      string
	Value     string
	Signature string
}

type IPGeo struct {
	IP          string                 `json:"ip"`
	CountryCode string                 `json:"country_code"`
	ISP         string                 `json:"isp,omitempty"`
	ASN         string                 `json:"asn,omitempty"`
	Locales     map[string]IPGeoLocale `json:"locales"`
}

type IPGeoLocale struct {
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city,omitempty"`
}

func NewOfflinePlayer(id string, rawName string) (Player, error) {
	name, err := NormalizeOfflineName(rawName)
	if err != nil {
		return Player{}, err
	}
	uuid, err := RandomProfileUUID()
	if err != nil {
		return Player{}, err
	}
	return Player{
		ID:             id,
		Kind:           PlayerKindOffline,
		UUID:           uuid,
		RawOfflineName: name.Raw,
		NormalizedName: name.Normalized,
		ProtocolName:   name.Protocol,
	}, nil
}

func NewOfflinePassport(_ string, rawName string) (Passport, error) {
	name, err := NormalizeOfflineName(rawName)
	if err != nil {
		return Passport{}, err
	}
	uuid := OfflinePassportUUID(name.Normalized)
	now := time.Now().UTC()
	return Passport{
		ID:                 uuid.String(),
		Kind:               PassportKindOffline,
		UUID:               uuid,
		Username:           name.Raw,
		UsernameNormalized: name.Normalized,
		RawOfflineName:     name.Raw,
		Status:             PassportStatusActive,
		SkinSource:         "upstream",
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func NewOfflineProfile(_ string, protocolName string, createdFromPassport string) (Profile, error) {
	name, err := NormalizeProtocolName(protocolName)
	if err != nil {
		return Profile{}, err
	}
	uuid, err := RandomProfileUUID()
	if err != nil {
		return Profile{}, err
	}
	now := time.Now().UTC()
	return Profile{
		ID:                  uuid.String(),
		UUID:                uuid,
		ProtocolName:        name.Protocol,
		NormalizedName:      name.Normalized,
		DisplayName:         name.Protocol,
		Status:              ProfileStatusActive,
		SkinSource:          "passport",
		ProfileProperties:   []ProfileProperty{},
		CreatedFromPassport: createdFromPassport,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func NewPremiumPlayer(id string, name string, uuid UUID, properties []ProfileProperty) Player {
	premiumUUID := uuid
	return Player{
		ID:                id,
		Kind:              PlayerKindPremium,
		UUID:              uuid,
		PremiumUUID:       &premiumUUID,
		NormalizedName:    name,
		ProtocolName:      name,
		ProfileProperties: append([]ProfileProperty(nil), properties...),
	}
}

func NewPremiumPassport(_ string, name string, uuid UUID) Passport {
	now := time.Now().UTC()
	premiumUUID := uuid
	normalized := strings.ToLower(strings.TrimSpace(name))
	return Passport{
		ID:                 uuid.String(),
		Kind:               PassportKindPremium,
		UUID:               uuid,
		PremiumUUID:        &premiumUUID,
		Username:           name,
		UsernameNormalized: normalized,
		Status:             PassportStatusActive,
		SkinSource:         "upstream",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func NewPremiumProfile(_ string, name string, _ UUID, properties []ProfileProperty, createdFromPassport string) (Profile, error) {
	protocol, err := NormalizeProtocolName(name)
	if err != nil {
		return Profile{}, err
	}
	uuid, err := RandomProfileUUID()
	if err != nil {
		return Profile{}, err
	}
	now := time.Now().UTC()
	return Profile{
		ID:                  uuid.String(),
		UUID:                uuid,
		ProtocolName:        protocol.Protocol,
		NormalizedName:      protocol.Normalized,
		DisplayName:         protocol.Protocol,
		Status:              ProfileStatusActive,
		SkinSource:          "passport",
		ProfileProperties:   append([]ProfileProperty(nil), properties...),
		CreatedFromPassport: createdFromPassport,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func PlayerFromPassportProfile(passport Passport, profile Profile) Player {
	locked := passport.Status == PassportStatusLocked || profile.Status == ProfileStatusLocked
	return Player{
		ID:                 profile.ID,
		Kind:               PlayerKind(passport.Kind),
		UUID:               profile.UUID,
		PremiumUUID:        passport.PremiumUUID,
		RawOfflineName:     passport.RawOfflineName,
		NormalizedName:     profile.NormalizedName,
		ProtocolName:       profile.ProtocolName,
		Locked:             locked,
		ProfileProperties:  append([]ProfileProperty(nil), profile.ProfileProperties...),
		RegistrationServer: passport.RegistrationServer,
		LastSeenServer:     profile.LastSeenServer,
		LastSeenAt:         profile.LastSeenAt,
		LastSeenIP:         profile.LastSeenIP,
		LastSeenGeo:        profile.LastSeenGeo,
	}
}

func PlayerFromPassportProfileLink(pp PassportProfile) Player {
	return PlayerFromPassportProfile(pp.Passport, pp.Profile)
}

func (p Player) ValidateIsolation() error {
	switch p.Kind {
	case PlayerKindOffline:
		if p.PremiumUUID != nil {
			return fmt.Errorf("offline player must not have premium uuid")
		}
		if p.RawOfflineName == "" || p.ProtocolName == "" {
			return fmt.Errorf("offline player must have offline names")
		}
	case PlayerKindPremium:
		if p.PremiumUUID == nil {
			return fmt.Errorf("premium player must have premium uuid")
		}
		if p.RawOfflineName != "" {
			return fmt.Errorf("premium player must not have raw offline name")
		}
		if p.ProtocolName == "" {
			return fmt.Errorf("premium protocol name is required")
		}
	default:
		return fmt.Errorf("unknown player kind %q", p.Kind)
	}
	return nil
}
