package identity

import "fmt"

type PlayerKind string

const (
	PlayerKindPremium PlayerKind = "premium"
	PlayerKindOffline PlayerKind = "offline"
)

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
}

type ProfileProperty struct {
	Name      string
	Value     string
	Signature string
}

func NewOfflinePlayer(id string, rawName string) (Player, error) {
	name, err := NormalizeOfflineName(rawName)
	if err != nil {
		return Player{}, err
	}
	return Player{
		ID:             id,
		Kind:           PlayerKindOffline,
		UUID:           OfflineUUID(name.Normalized),
		RawOfflineName: name.Raw,
		NormalizedName: name.Normalized,
		ProtocolName:   name.Protocol,
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
