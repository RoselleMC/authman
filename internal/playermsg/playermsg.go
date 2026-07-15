// Package playermsg is the single source of truth for player-visible message
// keys, defaults, placeholders, and the auth dialog document model shared by
// Authman Core and the Authman Limbo portal. All texts are MiniMessage source
// strings; consumers parse them locally and fall back to plain text on error.
package playermsg

import (
	"fmt"
	"strings"

	"github.com/RoselleMC/authman/limbo"
	"go.minekube.com/common/minecraft/component"
)

// Scopes group message keys by the consumer that renders them.
const (
	ScopeLimbo = "limbo"
	ScopeGate  = "gate"
)

// Def describes one configurable flat message.
type Def struct {
	Key          string   `json:"key"`
	Default      string   `json:"default"`
	Placeholders []string `json:"placeholders,omitempty"`
	Scope        string   `json:"scope"`
}

var defs = []Def{
	{Key: "limbo.error.password_required", Default: "Password is required.", Scope: ScopeLimbo},
	{Key: "limbo.error.invalid_password", Default: "Invalid Authman password.", Scope: ScopeLimbo},
	{Key: "limbo.error.passwords_mismatch", Default: "Passwords do not match.", Scope: ScopeLimbo},
	{Key: "limbo.error.register_failed", Default: "Authman could not register this offline passport.", Scope: ScopeLimbo},
	{Key: "limbo.error.resolve_failed", Default: "Authman could not resolve this passport.", Scope: ScopeLimbo},
	{Key: "limbo.error.target_failed", Default: "Authman could not resolve a downstream target.", Scope: ScopeLimbo},
	{Key: "limbo.error.grant_failed", Default: "Authman could not create a transfer grant.", Scope: ScopeLimbo},
	{Key: "limbo.error.dialog_payload", Default: "Authman could not read the dialog response. Please try again.", Scope: ScopeLimbo},
	{Key: "limbo.error.portal_locked", Default: "Authman portal is locked.", Placeholders: []string{"player"}, Scope: ScopeLimbo},
	{Key: "limbo.kick.client_too_old", Default: "Authman Limbo requires Minecraft 1.21.6 or newer.", Placeholders: []string{"player"}, Scope: ScopeLimbo},
	{Key: "limbo.kick.transfer_unsupported", Default: "Authman transfer requires Minecraft 1.20.5+ with vanilla transfer-cookie support.", Placeholders: []string{"player"}, Scope: ScopeLimbo},
	{Key: "limbo.success.actionbar", Default: "Authman login accepted", Placeholders: []string{"player", "server"}, Scope: ScopeLimbo},
	{Key: "limbo.success.title", Default: "Welcome", Placeholders: []string{"player", "server"}, Scope: ScopeLimbo},
	{Key: "limbo.success.subtitle", Default: "Preparing transfer", Placeholders: []string{"player", "server"}, Scope: ScopeLimbo},
	{Key: "limbo.error.profile_name_invalid", Default: "That profile name is not allowed. Use 1-16 language characters, numbers, or underscores.", Scope: ScopeLimbo},
	{Key: "limbo.error.profile_name_taken", Default: "That profile name is already taken.", Scope: ScopeLimbo},
	{Key: "limbo.error.profile_limit_reached", Default: "This passport already has the maximum of {max} profiles.", Placeholders: []string{"max", "count"}, Scope: ScopeLimbo},
	{Key: "limbo.error.profile_create_failed", Default: "Authman could not create this profile.", Scope: ScopeLimbo},
	{Key: "limbo.error.profile_selection_invalid", Default: "Pick one of your profiles to continue.", Scope: ScopeLimbo},
	{Key: "gate.kick.unavailable", Default: "<red>Authman 暂时不可用，请稍后重试。<newline>Authman is temporarily unavailable.</red>", Scope: ScopeGate},
	{Key: "gate.kick.missing_transfer_grant", Default: "<red>Please join through the Authman login portal.<newline>请从 Authman 登录门户进入。</red><newline><gray>This downstream server only accepts Authman transfer sessions.</gray>", Placeholders: []string{"player"}, Scope: ScopeGate},
	{Key: "gate.kick.transfer_unsupported", Default: "<red>Your client does not support Authman transfer cookies.<newline>当前客户端不支持 Authman 转送票据。</red><newline><gray>Please use Minecraft 1.20.5 or newer.</gray>", Placeholders: []string{"player"}, Scope: ScopeGate},
	{Key: "gate.kick.validation_timeout", Default: "<red>Authman did not receive the transfer ticket in time.<newline>Authman 未能及时收到转送票据。</red><newline><gray>Please return to the login portal and try again.</gray>", Placeholders: []string{"player"}, Scope: ScopeGate},
	{Key: "gate.kick.already_online", Default: "<red>This profile is already online on this server.<newline>该档案已在此下游服务器在线。</red><newline><gray>If this is stale, Authman is refreshing the status now. Please try again shortly.</gray>", Placeholders: []string{"player"}, Scope: ScopeGate},
	{Key: "gate.kick.locked", Default: "<red>This Authman account is locked.<newline>该 Authman 账号已锁定。</red>", Placeholders: []string{"player"}, Scope: ScopeGate},
	{Key: "gate.kick.banned", Default: "<red>You are banned from this server.</red><newline><gray>{reason}</gray>", Placeholders: []string{"player", "reason"}, Scope: ScopeGate},
	{Key: "gate.kick.default_disconnect", Default: "Authman disconnected this session.", Placeholders: []string{"player"}, Scope: ScopeGate},
}

// Dialog screens.
const (
	ScreenLogin         = "login"
	ScreenRegister      = "register"
	ScreenProfileCreate = "profile_create"
	ScreenProfileSelect = "profile_select"
)

// Screens returns every configurable dialog screen in display order.
func Screens() []string {
	return []string{ScreenLogin, ScreenRegister, ScreenProfileCreate, ScreenProfileSelect}
}

// Body block visibility conditions evaluated by the limbo portal at show time.
const (
	WhenAlways             = "always"
	WhenAuthRequired       = "auth_required"
	WhenPremiumPassthrough = "premium_passthrough"
	WhenPremiumUnverified  = "premium_unverified"
)

const (
	maxTextLength  = 1024
	maxTitleLength = 256
	maxElemWidth   = 1024
)

// Defaults returns the built-in message map for one scope, or all scopes when
// scope is empty.
func Defaults(scope string) map[string]string {
	out := make(map[string]string, len(defs))
	for _, def := range defs {
		if scope != "" && def.Scope != scope {
			continue
		}
		out[def.Key] = def.Default
	}
	return out
}

// Placeholders returns the allowed placeholder names per message key.
func Placeholders() map[string][]string {
	out := make(map[string][]string, len(defs))
	for _, def := range defs {
		out[def.Key] = append([]string(nil), def.Placeholders...)
	}
	return out
}

// KnownKey reports whether key is a registered flat message key.
func KnownKey(key string) bool {
	for _, def := range defs {
		if def.Key == key {
			return true
		}
	}
	return false
}

// Effective merges overrides over the built-in defaults for one scope.
func Effective(scope string, overrides map[string]string) map[string]string {
	out := Defaults(scope)
	for key, value := range overrides {
		if _, ok := out[key]; ok && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

// ValidateMessages checks flat message overrides and returns per-key errors.
func ValidateMessages(overrides map[string]string) map[string]string {
	errs := map[string]string{}
	for key, value := range overrides {
		if !KnownKey(key) {
			errs[key] = "unknown message key"
			continue
		}
		if msg := validateText(value, maxTextLength); msg != "" {
			errs[key] = msg
		}
	}
	return errs
}

func validateText(value string, max int) string {
	if len(value) > max {
		return fmt.Sprintf("text exceeds %d characters", max)
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if _, err := limbgo.ParseMiniMessage(value); err != nil {
		return "invalid MiniMessage: " + err.Error()
	}
	return ""
}

// Substitute replaces {name} placeholders with sanitized values. Values are
// stripped of MiniMessage tag delimiters so player-controlled input cannot
// inject formatting.
func Substitute(text string, vars map[string]string) string {
	if len(vars) == 0 {
		return text
	}
	for name, value := range vars {
		text = strings.ReplaceAll(text, "{"+name+"}", sanitizeValue(value))
	}
	return text
}

// SubstituteRaw replaces {name} placeholders with trusted MiniMessage values
// (for example an admin-configured error text injected into an error block).
func SubstituteRaw(text string, vars map[string]string) string {
	for name, value := range vars {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func sanitizeValue(value string) string {
	value = strings.ReplaceAll(value, "<", "")
	return strings.ReplaceAll(value, ">", "")
}

// RenderComponent substitutes sanitized vars into a MiniMessage source string
// and parses it. On parse failure it degrades to a plain-text component, never
// failing the calling flow.
func RenderComponent(text string, vars map[string]string) component.Component {
	resolved := Substitute(text, vars)
	parsed, err := limbgo.ParseMiniMessage(resolved)
	if err == nil && parsed != nil {
		return parsed
	}
	return &component.Text{Content: stripTags(resolved)}
}

var tagStripper = strings.NewReplacer("<newline>", "\n", "<br>", "\n", "<br/>", "\n")

func stripTags(value string) string {
	return tagStripper.Replace(value)
}
