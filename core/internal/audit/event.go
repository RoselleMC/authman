package audit

import (
	"strings"
	"time"
)

type ActorType string
type TargetType string

const (
	ActorAdmin  ActorType = "admin"
	ActorNode   ActorType = "node"
	ActorPlayer ActorType = "player"
	ActorSystem ActorType = "system"
)

const (
	TargetPlayer           TargetType = "player"
	TargetPassport         TargetType = "passport"
	TargetProfile          TargetType = "profile"
	TargetNode             TargetType = "node"
	TargetDownstreamServer TargetType = "downstream_server"
	TargetMojangProxy      TargetType = "mojang_proxy"
	TargetPortalSession    TargetType = "portal_session"
	TargetExtensionData    TargetType = "extension_data"
	TargetSystem           TargetType = "system"
)

type Event struct {
	ID            string
	Occurred      time.Time
	SchemaVersion int
	Category      string
	Outcome       string
	Source        string
	SessionID     string
	CorrelationID string
	ActorType     ActorType
	ActorID       string
	Target        TargetType
	TargetID      string
	Type          string
	Details       map[string]any
}

func NewEvent(now time.Time, actorType ActorType, actorID string, target TargetType, targetID string, eventType string, details map[string]any) Event {
	return Event{
		Occurred:      now.UTC(),
		SchemaVersion: 1,
		Category:      CategoryFromType(eventType),
		ActorType:     actorType,
		ActorID:       actorID,
		Target:        target,
		TargetID:      targetID,
		Type:          eventType,
		Details:       details,
	}
}

func CategoryFromType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return "unknown"
	}
	if idx := strings.IndexByte(eventType, '.'); idx > 0 {
		return eventType[:idx]
	}
	return eventType
}
