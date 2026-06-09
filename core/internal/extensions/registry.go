package extensions

import (
	"context"
	"time"

	"github.com/RoselleMC/authman/core/internal/identity"
)

type Visibility string

const (
	VisibilityPrivate       Visibility = "private"
	VisibilityPlayerVisible Visibility = "player_visible"
	VisibilityPublic        Visibility = "public"
)

type FieldType string

const (
	FieldText    FieldType = "text"
	FieldBoolean FieldType = "boolean"
	FieldBadge   FieldType = "badge"
)

type Field struct {
	Key        string     `json:"key"`
	Label      string     `json:"label"`
	Type       FieldType  `json:"type"`
	Visibility Visibility `json:"visibility,omitempty"`
	Tone       string     `json:"tone,omitempty"`
	Safe       bool       `json:"safe,omitempty"`
}

type Schema struct {
	Version int     `json:"version"`
	Title   string  `json:"title"`
	Fields  []Field `json:"fields"`
}

type RegistryEntry struct {
	Provider      string         `json:"provider"`
	Title         string         `json:"title"`
	Visibility    Visibility     `json:"visibility"`
	Schema        Schema         `json:"schema"`
	LastUpdate    *time.Time     `json:"last_update"`
	PreviewValues map[string]any `json:"preview_values"`
}

type PlayerData struct {
	ServerSlug        string         `json:"server_slug,omitempty"`
	ServerDisplayName string         `json:"server_display_name,omitempty"`
	Provider          string         `json:"provider"`
	Schema            Schema         `json:"schema"`
	Values            map[string]any `json:"values"`
	UpdatedAt         string         `json:"updated_at,omitempty"`
}

type Provider interface {
	Entry(ctx context.Context) RegistryEntry
	PlayerData(ctx context.Context, player identity.Player, serverSlug string) (PlayerData, bool)
}

type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: append([]Provider(nil), providers...)}
}

func DefaultRegistry() *Registry {
	return NewRegistry(IdentityProvider{})
}

func (r *Registry) Entries(ctx context.Context) []RegistryEntry {
	entries := make([]RegistryEntry, 0, len(r.providers))
	for _, provider := range r.providers {
		entries = append(entries, provider.Entry(ctx))
	}
	return entries
}

func (r *Registry) PlayerData(ctx context.Context, player identity.Player, serverSlug string) []PlayerData {
	rows := make([]PlayerData, 0, len(r.providers))
	for _, provider := range r.providers {
		data, ok := provider.PlayerData(ctx, player, serverSlug)
		if ok {
			rows = append(rows, data)
		}
	}
	return rows
}

type IdentityProvider struct{}

func (IdentityProvider) Entry(ctx context.Context) RegistryEntry {
	schema := identitySchema()
	return RegistryEntry{
		Provider:      "authman.identity",
		Title:         schema.Title,
		Visibility:    VisibilityPlayerVisible,
		Schema:        schema,
		LastUpdate:    nil,
		PreviewValues: map[string]any{"kind": string(identity.PlayerKindOffline), "protocol_name": "#Steve"},
	}
}

func (IdentityProvider) PlayerData(ctx context.Context, player identity.Player, serverSlug string) (PlayerData, bool) {
	slug := serverSlug
	if slug == "" {
		slug = "default"
	}
	return PlayerData{
		ServerSlug:        slug,
		ServerDisplayName: "Default Server",
		Provider:          "authman.identity",
		Schema:            identitySchema(),
		Values: map[string]any{
			"kind":          string(player.Kind),
			"protocol_name": player.ProtocolName,
			"uuid":          player.UUID.String(),
			"locked":        player.Locked,
		},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, true
}

func identitySchema() Schema {
	return Schema{
		Version: 1,
		Title:   "Authman Identity",
		Fields: []Field{
			{Key: "kind", Label: "Account type", Type: FieldBadge, Visibility: VisibilityPlayerVisible, Tone: "info"},
			{Key: "protocol_name", Label: "Protocol name", Type: FieldText, Visibility: VisibilityPlayerVisible},
			{Key: "uuid", Label: "UUID", Type: FieldText, Visibility: VisibilityPlayerVisible},
			{Key: "locked", Label: "Locked", Type: FieldBoolean, Visibility: VisibilityPlayerVisible},
		},
	}
}
