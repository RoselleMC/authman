package rbac

import (
	"slices"
	"strings"
	"time"
)

type Permission struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	System      bool      `json:"system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var Catalog = []Permission{
	{Key: "admin.overview.read", Group: "overview", Label: "View overview", Description: "Read dashboard metrics and recent activity."},
	{Key: "players.read", Group: "players", Label: "View players", Description: "Read player lists, identity details, sessions, and extension data."},
	{Key: "players.lock", Group: "players", Label: "Lock players", Description: "Lock and unlock player accounts."},
	{Key: "players.password.reset", Group: "players", Label: "Reset offline passwords", Description: "Generate or set replacement credentials for offline players."},
	{Key: "players.portal_link.create", Group: "players", Label: "Create portal links", Description: "Generate one-time player portal login links."},
	{Key: "nodes.read", Group: "nodes", Label: "View Authman nodes", Description: "Read registered login portal and downstream node heartbeats."},
	{Key: "nodes.write", Group: "nodes", Label: "Manage Authman nodes", Description: "Create, disable, rotate, and revoke node credentials."},
	{Key: "mojang.read", Group: "mojang", Label: "View Mojang upstream", Description: "Read Mojang route health, cache state, and proxy events."},
	{Key: "mojang.write", Group: "mojang", Label: "Manage Mojang proxies", Description: "Create and delete HTTP/SOCKS5 Mojang upstream routes."},
	{Key: "servers.read", Group: "servers", Label: "View downstream servers", Description: "Read downstream portal contexts and theme settings."},
	{Key: "servers.write", Group: "servers", Label: "Manage downstream servers", Description: "Create, update, and delete downstream server portal contexts."},
	{Key: "extensions.read", Group: "extensions", Label: "View extensions", Description: "Read extension schemas and player-visible data providers."},
	{Key: "audit.read", Group: "audit", Label: "View audit log", Description: "Read central audit events across Authman."},
	{Key: "settings.read", Group: "settings", Label: "View settings", Description: "Read administrator, role, system, and security settings."},
	{Key: "admin.users.read", Group: "settings", Label: "View administrators", Description: "Read administrator identities and role assignments."},
	{Key: "admin.users.write", Group: "settings", Label: "Manage administrators", Description: "Create and modify administrator identities and roles."},
	{Key: "admin.users.security.write", Group: "security", Label: "Manage administrator MFA", Description: "Disable administrator TOTP and remove administrator passkeys."},
	{Key: "admin.roles.read", Group: "settings", Label: "View roles", Description: "Read role definitions and permission grants."},
	{Key: "admin.roles.write", Group: "settings", Label: "Manage roles", Description: "Modify editable role permission grants."},
	{Key: "system.read", Group: "settings", Label: "View system summary", Description: "Read runtime version, database, and feature-flag status."},
	{Key: "security.read", Group: "security", Label: "View security settings", Description: "Read email, SMTP, and multi-factor authentication settings."},
	{Key: "security.write", Group: "security", Label: "Manage security settings", Description: "Modify email, SMTP, and multi-factor authentication settings."},
	{Key: "external_api.read", Group: "settings", Label: "View external API tokens", Description: "Read player-panel API token metadata and usage statistics."},
	{Key: "external_api.write", Group: "settings", Label: "Manage external API tokens", Description: "Create, disable, and revoke player-panel API tokens."},
	{Key: "external_api.delete", Group: "settings", Label: "Delete external API tokens", Description: "Permanently delete revoked player-panel API token records."},
}

var catalogKeys = func() map[string]struct{} {
	keys := make(map[string]struct{}, len(Catalog))
	for _, permission := range Catalog {
		keys[permission.Key] = struct{}{}
	}
	return keys
}()

func DefaultRoles() []Role {
	now := time.Now().UTC()
	return []Role{
		{
			ID:          "owner",
			Name:        "Owner",
			Description: "Full access to Authman. The bootstrap owner role is immutable.",
			Permissions: []string{"*"},
			System:      true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "admin",
			Name:        "Admin",
			Description: "Operational access for day-to-day Authman administration.",
			Permissions: []string{
				"admin.overview.read",
				"players.read",
				"players.lock",
				"players.password.reset",
				"players.portal_link.create",
				"nodes.read",
				"nodes.write",
				"mojang.read",
				"mojang.write",
				"servers.read",
				"servers.write",
				"extensions.read",
				"audit.read",
				"settings.read",
				"admin.users.read",
				"admin.roles.read",
				"system.read",
				"security.read",
				"external_api.read",
				"external_api.write",
				"external_api.delete",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "auditor",
			Name:        "Auditor",
			Description: "Read-only access for operational review and audit.",
			Permissions: []string{
				"admin.overview.read",
				"players.read",
				"nodes.read",
				"mojang.read",
				"servers.read",
				"extensions.read",
				"audit.read",
				"settings.read",
				"admin.users.read",
				"admin.roles.read",
				"system.read",
				"security.read",
				"external_api.read",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func DefaultRole(id string) (Role, bool) {
	for _, role := range DefaultRoles() {
		if role.ID == id {
			return role, true
		}
	}
	return Role{}, false
}

func RoleID(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	var b strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func MergeDefaultRoles(stored []Role) []Role {
	byID := make(map[string]Role, len(stored)+3)
	for _, role := range DefaultRoles() {
		byID[role.ID] = role
	}
	for _, role := range stored {
		if base, ok := byID[role.ID]; ok {
			if role.Name == "" {
				role.Name = base.Name
			}
			if role.Description == "" {
				role.Description = base.Description
			}
			role.System = base.System
		}
		role.Permissions = NormalizePermissions(role.Permissions)
		byID[role.ID] = role
	}
	roles := make([]Role, 0, len(byID))
	for _, role := range byID {
		roles = append(roles, role)
	}
	slices.SortFunc(roles, func(a, b Role) int {
		return strings.Compare(a.ID, b.ID)
	})
	return roles
}

func NormalizePermissions(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, permission := range input {
		permission = strings.TrimSpace(strings.ToLower(permission))
		if permission == "" {
			continue
		}
		if permission != "*" && !strings.HasSuffix(permission, ".*") {
			if _, ok := catalogKeys[permission]; !ok {
				continue
			}
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		out = append(out, permission)
	}
	slices.Sort(out)
	return out
}

func HasPermission(grants []string, permission string) bool {
	permission = strings.TrimSpace(strings.ToLower(permission))
	for _, grant := range grants {
		grant = strings.TrimSpace(strings.ToLower(grant))
		if grant == "*" || grant == permission {
			return true
		}
		if strings.HasSuffix(grant, ".*") && strings.HasPrefix(permission, strings.TrimSuffix(grant, ".*")+".") {
			return true
		}
	}
	return false
}
