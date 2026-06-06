package identity

import (
	"fmt"
	"strings"
)

const (
	OfflineNameMinLength = 3
	OfflineNameMaxLength = 15
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
	name := strings.TrimSpace(raw)
	if name != raw {
		return OfflineName{}, fmt.Errorf("offline username must not contain leading or trailing whitespace")
	}
	if len(name) < OfflineNameMinLength || len(name) > OfflineNameMaxLength {
		return OfflineName{}, fmt.Errorf("offline username length must be between %d and %d", OfflineNameMinLength, OfflineNameMaxLength)
	}
	for _, r := range name {
		if !isOfflineNameRune(r) {
			return OfflineName{}, fmt.Errorf("offline username contains invalid character %q", r)
		}
	}
	normalized := strings.ToLower(name)
	if _, ok := reservedOfflineNames[normalized]; ok {
		return OfflineName{}, fmt.Errorf("offline username is reserved")
	}
	return OfflineName{
		Raw:        name,
		Normalized: normalized,
		Protocol:   "#" + name,
	}, nil
}

func isOfflineNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '_'
}
