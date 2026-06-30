package identity

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	OfflineNameMinLength  = 3
	OfflineNameMaxLength  = 16
	ProtocolNameMinLength = 1
	ProtocolNameMaxLength = 16
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
	return normalizeName(raw, "offline username", OfflineNameMinLength, OfflineNameMaxLength, isOfflineNameRune)
}

func NormalizeProtocolName(raw string) (OfflineName, error) {
	return normalizeName(raw, "profile protocol name", ProtocolNameMinLength, ProtocolNameMaxLength, isProtocolNameRune)
}

func normalizeName(raw string, label string, minLength int, maxLength int, validRune func(rune) bool) (OfflineName, error) {
	name := strings.TrimSpace(raw)
	if name != raw {
		return OfflineName{}, fmt.Errorf("%s must not contain leading or trailing whitespace", label)
	}
	length := utf8.RuneCountInString(name)
	if length < minLength || length > maxLength {
		return OfflineName{}, fmt.Errorf("%s length must be between %d and %d characters", label, minLength, maxLength)
	}
	for _, r := range name {
		if !validRune(r) {
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

func isProtocolNameRune(r rune) bool {
	if isOfflineNameRune(r) {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}
