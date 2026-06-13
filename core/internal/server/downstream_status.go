package server

import (
	"context"
	"strings"
	"time"
)

type effectiveDownstreamStatus struct {
	OnlinePlayers int
	MaxPlayers    int
	Source        string
	ReportedAt    *time.Time
	Stale         bool
}

func (s *Server) effectiveDownstreamStatus(ctx context.Context, serverID string, now time.Time) effectiveDownstreamStatus {
	serverID = stringsTrim(serverID)
	freshness := s.downstreamStatusFreshness(ctx)
	if status, ok := s.store.GetDownstreamServerStatus(ctx, serverID); ok {
		reportedAt := status.ReportedAt.UTC()
		stale := reportedAt.IsZero() || now.UTC().Sub(reportedAt) > freshness
		if !stale {
			return effectiveDownstreamStatus{
				OnlinePlayers: clampNonNegative(status.OnlinePlayers),
				MaxPlayers:    clampNonNegative(status.MaxPlayers),
				Source:        emptyFallback(status.Source, "heartbeat"),
				ReportedAt:    &reportedAt,
				Stale:         false,
			}
		}
	}
	return effectiveDownstreamStatus{
		OnlinePlayers: clampNonNegative(s.store.CountActivePresencesForServer(ctx, serverID)),
		MaxPlayers:    0,
		Source:        "presence",
		Stale:         true,
	}
}

func (s *Server) downstreamStatusFreshness(ctx context.Context) time.Duration {
	settings := s.nodeCommunicationSettings(ctx)
	seconds := settings.HeartbeatIntervalSeconds*2 + 5
	if seconds < 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func downstreamRuntimeStatusData(status effectiveDownstreamStatus) map[string]any {
	var reportedAt any
	if status.ReportedAt != nil {
		reportedAt = *status.ReportedAt
	}
	return map[string]any{
		"online_players": status.OnlinePlayers,
		"max_players":    status.MaxPlayers,
		"source":         status.Source,
		"reported_at":    reportedAt,
		"stale":          status.Stale,
	}
}

func addDownstreamRuntimeStatus(data map[string]any, status effectiveDownstreamStatus) map[string]any {
	data["online_players"] = status.OnlinePlayers
	data["max_players"] = status.MaxPlayers
	data["runtime_status"] = downstreamRuntimeStatusData(status)
	return data
}

func clampNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func emptyFallback(value string, fallback string) string {
	if stringsTrim(value) == "" {
		return fallback
	}
	return value
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
