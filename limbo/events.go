package limbgo

import (
	"context"

	"github.com/RoselleMC/authman/limbo/dialog"
	"go.minekube.com/common/minecraft/component"
)

// PlayerSession is the API exposed to event handlers for the connected player.
type PlayerSession interface {
	Player() Player
	Capabilities() SessionCapabilities
	SendMessage(ctx context.Context, message component.Component) error
	SendActionBar(ctx context.Context, message component.Component) error
	ShowTitle(ctx context.Context, title Title) error
	ClearTitle(ctx context.Context, reset bool) error
	ShowDialog(ctx context.Context, dialog dialog.Dialog) error
	ClearDialog(ctx context.Context) error
	AddResourcePack(ctx context.Context, pack ResourcePack) error
	RemoveResourcePack(ctx context.Context, id string) error
	StoreCookie(ctx context.Context, key string, value []byte) error
	Transfer(ctx context.Context, host string, port int) error
	Disconnect(ctx context.Context, reason component.Component) error
}

// ResourcePack describes a client resource pack offer. ID is an application
// stable identifier. Protocol adapters derive the UUID required by modern
// clients from this ID when it is not already a dashed UUID.
type ResourcePack struct {
	ID       string
	URL      string
	Hash     string
	Required bool
	Prompt   component.Component
}

// ResourcePackStatus is the client-reported state for a resource pack offer.
type ResourcePackStatus string

const (
	ResourcePackAccepted           ResourcePackStatus = "accepted"
	ResourcePackDeclined           ResourcePackStatus = "declined"
	ResourcePackDownloaded         ResourcePackStatus = "downloaded"
	ResourcePackInvalidURL         ResourcePackStatus = "invalid_url"
	ResourcePackFailedDownload     ResourcePackStatus = "failed_download"
	ResourcePackFailedReload       ResourcePackStatus = "failed_reload"
	ResourcePackSuccessfullyLoaded ResourcePackStatus = "successfully_loaded"
	ResourcePackDiscarded          ResourcePackStatus = "discarded"
)

// Title is a client title overlay. Title and Subtitle may be nil when only
// updating timings.
type Title struct {
	Title    component.Component
	Subtitle component.Component
	Times    *TitleTimes
}

// TitleTimes contains title animation timings in client ticks.
type TitleTimes struct {
	FadeInTicks  int32
	StayTicks    int32
	FadeOutTicks int32
}

// TitleTimesTicks returns a reusable TitleTimes pointer for API call sites.
func TitleTimesTicks(fadeIn, stay, fadeOut int32) *TitleTimes {
	return &TitleTimes{FadeInTicks: fadeIn, StayTicks: stay, FadeOutTicks: fadeOut}
}

// SessionCapabilities describes optional vanilla features available for the
// connected player's protocol.
type SessionCapabilities struct {
	SystemMessage      bool
	ActionBar          bool
	Title              bool
	Dialog             bool
	ResourcePack       bool
	RemoveResourcePack bool
	StoreCookie        bool
	Transfer           bool
	Disconnect         bool
}

// PlayerEventHandler receives optional player actions after the limbo join
// sequence has completed.
type PlayerEventHandler interface {
	HandleJoin(ctx context.Context, session PlayerSession, event *JoinEvent) error
	HandleChat(ctx context.Context, session PlayerSession, event *ChatEvent) error
	HandleCommand(ctx context.Context, session PlayerSession, event *CommandEvent) error
	HandleDialogClick(ctx context.Context, session PlayerSession, event *DialogClickEvent) error
}

// PlayerEventHandlerFuncs adapts functions to PlayerEventHandler.
type PlayerEventHandlerFuncs struct {
	Join                 func(context.Context, PlayerSession, *JoinEvent) error
	Chat                 func(context.Context, PlayerSession, *ChatEvent) error
	Command              func(context.Context, PlayerSession, *CommandEvent) error
	DialogClick          func(context.Context, PlayerSession, *DialogClickEvent) error
	ResourcePackResponse func(context.Context, PlayerSession, *ResourcePackResponseEvent) error
}

// HandleJoin implements PlayerEventHandler.
func (h PlayerEventHandlerFuncs) HandleJoin(ctx context.Context, session PlayerSession, event *JoinEvent) error {
	if h.Join == nil {
		return nil
	}
	return h.Join(ctx, session, event)
}

// HandleChat implements PlayerEventHandler.
func (h PlayerEventHandlerFuncs) HandleChat(ctx context.Context, session PlayerSession, event *ChatEvent) error {
	if h.Chat == nil {
		return nil
	}
	return h.Chat(ctx, session, event)
}

// HandleCommand implements PlayerEventHandler.
func (h PlayerEventHandlerFuncs) HandleCommand(ctx context.Context, session PlayerSession, event *CommandEvent) error {
	if h.Command == nil {
		return nil
	}
	return h.Command(ctx, session, event)
}

// HandleDialogClick implements PlayerEventHandler.
func (h PlayerEventHandlerFuncs) HandleDialogClick(ctx context.Context, session PlayerSession, event *DialogClickEvent) error {
	if h.DialogClick == nil {
		return nil
	}
	return h.DialogClick(ctx, session, event)
}

// ResourcePackResponseHandler can be implemented by an event handler that wants
// client resource-pack status updates.
type ResourcePackResponseHandler interface {
	HandleResourcePackResponse(ctx context.Context, session PlayerSession, event *ResourcePackResponseEvent) error
}

// HandleResourcePackResponse implements ResourcePackResponseHandler.
func (h PlayerEventHandlerFuncs) HandleResourcePackResponse(ctx context.Context, session PlayerSession, event *ResourcePackResponseEvent) error {
	if h.ResourcePackResponse == nil {
		return nil
	}
	return h.ResourcePackResponse(ctx, session, event)
}

// JoinEvent is emitted after the initial limbo join and spawn chunk have been
// sent. Session methods are safe to call from this callback.
type JoinEvent struct {
	Player   Player
	Protocol int
}

// ChatEvent is emitted when a player sends a chat message.
type ChatEvent struct {
	Player   Player
	Message  string
	Protocol int
	Canceled bool
}

// CommandEvent is emitted when a player sends a command.
type CommandEvent struct {
	Player   Player
	Command  string
	Protocol int
	Canceled bool
}

// DialogClickEvent is emitted for minecraft:custom dialog and text click
// actions. Payload is the raw anonymous NBT body sent by the client when one is
// present.
type DialogClickEvent struct {
	Player   Player
	ID       string
	Payload  []byte
	Protocol int
	Canceled bool
}

// ResourcePackResponseEvent is emitted when the client reports a status change
// for a resource pack offered through PlayerSession.AddResourcePack.
type ResourcePackResponseEvent struct {
	Player     Player
	ID         string
	Pack       ResourcePack
	Status     ResourcePackStatus
	StatusCode int32
	Protocol   int
}
