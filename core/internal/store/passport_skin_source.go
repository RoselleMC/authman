package store

import (
	"strings"

	"github.com/RoselleMC/authman/core/internal/identity"
)

const (
	PassportSkinSourceUpstream = "upstream"
	PassportSkinSourceCustom   = "custom"
)

func NormalizePassportSkinSource(_ identity.PassportKind, value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PassportSkinSourceCustom:
		return PassportSkinSourceCustom
	case "mojang", "default", PassportSkinSourceUpstream:
		return PassportSkinSourceUpstream
	default:
		return PassportSkinSourceUpstream
	}
}
