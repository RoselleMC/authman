package yggdrasil

import "github.com/RoselleMC/authman/internal/identity"

const (
	PropertyTextures      = "textures"
	PropertyAuthmanKind   = "authman_kind"
	PropertyAuthmanPlayer = "authman_player_id"
)

type Property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type Profile struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Properties []Property `json:"properties,omitempty"`
}

func FromPlayer(player identity.Player) Profile {
	properties := make([]Property, 0, len(player.ProfileProperties)+2)
	for _, property := range player.ProfileProperties {
		properties = append(properties, Property{
			Name:      property.Name,
			Value:     property.Value,
			Signature: property.Signature,
		})
	}
	if player.Kind == identity.PlayerKindOffline {
		properties = append(properties,
			Property{Name: PropertyAuthmanKind, Value: string(identity.PlayerKindOffline)},
			Property{Name: PropertyAuthmanPlayer, Value: player.ID},
		)
	}
	return Profile{
		ID:         player.UUID.Compact(),
		Name:       player.ProtocolName,
		Properties: properties,
	}
}
