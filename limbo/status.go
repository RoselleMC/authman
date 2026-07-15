package limbgo

import (
	"context"
	"encoding/json"
	"net"

	"go.minekube.com/common/minecraft/component"
)

// Status describes the server-list status response shown by Minecraft clients.
type Status struct {
	VersionName string
	Protocol    int32

	Description component.Component

	MaxPlayers    int
	OnlinePlayers int
	SamplePlayers []StatusSamplePlayer
	HidePlayers   bool

	Favicon string

	EnforcesSecureChat  *bool
	PreviewsChat        *bool
	PreventsChatReports *bool

	Raw map[string]any
}

// StatusSamplePlayer is one optional player sample entry in the server list.
type StatusSamplePlayer struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// StatusOptions is the compatibility input shape used by protocol routers.
type StatusOptions struct {
	VersionName string
	Protocol    int32

	Description component.Component

	MaxPlayers    int
	OnlinePlayers int
	SamplePlayers []StatusSamplePlayer
	HidePlayers   bool

	Favicon string

	EnforcesSecureChat  *bool
	PreviewsChat        *bool
	PreventsChatReports *bool

	Raw map[string]any
}

// StatusRequest describes one server-list status request.
type StatusRequest struct {
	Protocol   int32
	Address    string
	Port       uint16
	RemoteAddr net.Addr
}

// StatusProvider dynamically produces server-list status responses.
type StatusProvider interface {
	Status(ctx context.Context, request StatusRequest) (Status, error)
}

// StatusProviderFunc adapts a function to StatusProvider.
type StatusProviderFunc func(context.Context, StatusRequest) (Status, error)

// Status implements StatusProvider.
func (fn StatusProviderFunc) Status(ctx context.Context, request StatusRequest) (Status, error) {
	return fn(ctx, request)
}

// StaticStatus returns a provider that always returns the same status.
func StaticStatus(status Status) StatusProvider {
	return StatusProviderFunc(func(context.Context, StatusRequest) (Status, error) {
		return status, nil
	})
}

// Status converts options into a Status value.
func (o StatusOptions) Status() Status {
	return Status{
		VersionName:         o.VersionName,
		Protocol:            o.Protocol,
		Description:         o.Description,
		MaxPlayers:          o.MaxPlayers,
		OnlinePlayers:       o.OnlinePlayers,
		SamplePlayers:       o.SamplePlayers,
		HidePlayers:         o.HidePlayers,
		Favicon:             o.Favicon,
		EnforcesSecureChat:  o.EnforcesSecureChat,
		PreviewsChat:        o.PreviewsChat,
		PreventsChatReports: o.PreventsChatReports,
		Raw:                 o.Raw,
	}
}

// StatusWithDefaults returns a copy of status with legacy description/max
// defaults applied.
func StatusWithDefaults(status Status, protocol int32, description string, maxPlayers int) Status {
	if status.VersionName == "" {
		status.VersionName = "limbgo"
	}
	if status.Protocol == 0 {
		status.Protocol = protocol
	}
	if status.Description == nil {
		if description == "" {
			description = "limbgo"
		}
		status.Description = &component.Text{Content: description}
	}
	if status.MaxPlayers <= 0 {
		if maxPlayers <= 0 {
			maxPlayers = 1
		}
		status.MaxPlayers = maxPlayers
	}
	return status
}

// MarshalStatusJSON serializes a status response for the given client protocol.
func MarshalStatusJSON(status Status, protocol int32) ([]byte, error) {
	status = StatusWithDefaults(status, protocol, "", 1)
	description, err := MarshalComponentJSON(protocol, status.Description)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"version": map[string]any{
			"name":     status.VersionName,
			"protocol": status.Protocol,
		},
		"description": json.RawMessage(description),
	}
	if !status.HidePlayers {
		players := map[string]any{
			"max":    status.MaxPlayers,
			"online": status.OnlinePlayers,
		}
		if len(status.SamplePlayers) > 0 {
			players["sample"] = status.SamplePlayers
		}
		payload["players"] = players
	}
	if status.Favicon != "" {
		payload["favicon"] = status.Favicon
	}
	if status.EnforcesSecureChat != nil {
		payload["enforcesSecureChat"] = *status.EnforcesSecureChat
	}
	if status.PreviewsChat != nil {
		payload["previewsChat"] = *status.PreviewsChat
	}
	if status.PreventsChatReports != nil {
		payload["preventsChatReports"] = *status.PreventsChatReports
	}
	for key, value := range status.Raw {
		payload[key] = value
	}
	return json.Marshal(payload)
}

// MarshalStatusResponse serializes a status response from compatibility
// options.
func MarshalStatusResponse(protocol int32, options StatusOptions) ([]byte, error) {
	return MarshalStatusJSON(options.Status(), protocol)
}
