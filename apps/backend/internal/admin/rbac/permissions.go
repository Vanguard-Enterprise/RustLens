package rbac

type Permission struct {
	Key         string
	Description string
}

var permissions = []Permission{
	{Key: "users.suspend", Description: "Suspend or unsuspend a user account"},
	{Key: "users.force_unlink_steam", Description: "Force-unlink a Steam account from a user"},
	{Key: "proxies.edit", Description: "Create, update, or delete outbound proxy providers"},
	{Key: "proxies.rotate_credentials", Description: "Rotate outbound proxy provider credentials"},
	{Key: "links.review", Description: "View the Steam-Discord link review queue"},
	{Key: "links.approve", Description: "Approve or reject queued Steam-Discord links"},
	{Key: "guilds.untrack_server", Description: "Remove a server from a guild's tracked set"},
	{Key: "admins.grant", Description: "Grant or revoke admin roles; edit role-permission mappings"},
	{Key: "audit.read", Description: "Read the admin audit log"},
}

func AllPermissions() []Permission {
	out := make([]Permission, len(permissions))
	copy(out, permissions)
	return out
}

func IsValidPermission(key string) bool {
	for _, p := range permissions {
		if p.Key == key {
			return true
		}
	}
	return false
}
