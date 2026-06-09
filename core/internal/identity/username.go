package identity

import (
	"fmt"
	"strings"
)

const (
	OfflineNameMinLength = 3
	OfflineNameMaxLength = 16
)

var reservedOfflineNames = map[string]struct{}{
	"admin":     {},
	"root":      {},
	"system":    {},
	"mojang":    {},
	"minecraft": {},
	"authman":   {},
	"console":   {},
}

type OfflineName struct {
	Raw        string
	Normalized string
	Protocol   string
}

func NormalizeOfflineName(raw string) (OfflineName, error) {
	return normalizeMinecraftName(raw, "offline username")
}

func NormalizeProtocolName(raw string) (OfflineName, error) {
	return normalizeMinecraftName(raw, "profile protocol name")
}

func normalizeMinecraftName(raw string, label string) (OfflineName, error) {
	name := strings.TrimSpace(raw)
	if name != raw {
		return OfflineName{}, fmt.Errorf("%s must not contain leading or trailing whitespace", label)
	}
	if len(name) < OfflineNameMinLength || len(name) > OfflineNameMaxLength {
		return OfflineName{}, fmt.Errorf("%s length must be between %d and %d", label, OfflineNameMinLength, OfflineNameMaxLength)
	}
	for _, r := range name {
		if !isOfflineNameRune(r) {
			return OfflineName{}, fmt.Errorf("%s contains invalid character %q", label, r)
		}
	}
	normalized := strings.ToLower(name)
	if _, ok := reservedOfflineNames[normalized]; ok {
		return OfflineName{}, fmt.Errorf("%s is reserved", label)
	}
	return OfflineName{
		Raw:        name,
		Normalized: normalized,
		Protocol:   name,
	}, nil
}

func isOfflineNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '_'
}
