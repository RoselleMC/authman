package yggdrasil

import (
	"context"
	"errors"
	"fmt"

	"github.com/RoselleMC/authman/internal/identity"
)

var ErrProfileNotFound = errors.New("profile not found")

type HasJoinedRequest struct {
	Username string
	ServerID string
	IP       string
}

type PremiumVerifier interface {
	HasJoined(ctx context.Context, request HasJoinedRequest) (Profile, error)
}

type OfflineResolver interface {
	ResolveOffline(ctx context.Context, rawName string) (identity.Player, error)
}

type JoinService struct {
	Premium PremiumVerifier
	Offline OfflineResolver
}

func (s JoinService) HasJoined(ctx context.Context, request HasJoinedRequest) (Profile, error) {
	if request.Username == "" {
		return Profile{}, fmt.Errorf("username is required")
	}
	if request.ServerID == "" {
		return Profile{}, fmt.Errorf("serverId is required")
	}
	if looksOffline(request.Username) {
		return s.offlineProfile(ctx, trimOfflineMarker(request.Username))
	}
	if s.Premium != nil {
		profile, err := s.Premium.HasJoined(ctx, request)
		if err == nil {
			return profile, nil
		}
		if !errors.Is(err, ErrProfileNotFound) {
			profile, offlineErr := s.offlineProfile(ctx, request.Username)
			if offlineErr == nil {
				return profile, nil
			}
			if !errors.Is(offlineErr, ErrProfileNotFound) {
				return Profile{}, offlineErr
			}
			return Profile{}, err
		}
	}
	return s.offlineProfile(ctx, request.Username)
}

func (s JoinService) offlineProfile(ctx context.Context, rawName string) (Profile, error) {
	if s.Offline == nil {
		return Profile{}, ErrProfileNotFound
	}
	player, err := s.Offline.ResolveOffline(ctx, rawName)
	if err != nil {
		return Profile{}, err
	}
	if err := player.ValidateIsolation(); err != nil {
		return Profile{}, err
	}
	return FromPlayer(player), nil
}

func looksOffline(username string) bool {
	return len(username) > 0 && username[0] == '#'
}

func trimOfflineMarker(username string) string {
	if looksOffline(username) {
		return username[1:]
	}
	return username
}
