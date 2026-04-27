package rbac

type Permission struct {
	Key         string
	Description string
}

const (
	PermUsersRead              = "users.read"
	PermUsersEdit              = "users.edit"
	PermUsersRolesAssign       = "users.roles.assign"
	PermUsersSuspend           = "users.suspend"
	PermUsersSecurityReset     = "users.security.reset"
	PermUsersEmailManage       = "users.email.manage"
	PermUsersCreate            = "users.create"
	PermUsersConnectionsManage = "users.connections.manage"
	PermUsersDelete            = "users.delete"
	PermUsersExport            = "users.export"
	PermUsersAuditRead         = "users.audit.read"
	PermUsersForceUnlink       = "users.force_unlink_steam"

	PermAdminsGrant            = "admins.grant"
	PermProxiesEdit            = "proxies.edit"
	PermProxiesRotateCreds     = "proxies.rotate_credentials"
	PermLinksReview            = "links.review"
	PermLinksApprove           = "links.approve"
	PermGuildsUntrackServer    = "guilds.untrack_server"
	PermAuditRead              = "audit.read"
)

var permissions = []Permission{
	{Key: PermUsersRead, Description: "View user profiles and the users list"},
	{Key: PermUsersEdit, Description: "Edit a user's profile fields (display name, avatar)"},
	{Key: PermUsersRolesAssign, Description: "Assign or revoke admin roles on users"},
	{Key: PermUsersSuspend, Description: "Suspend or unsuspend a user account"},
	{Key: PermUsersSecurityReset, Description: "Trigger password resets and invalidate sessions"},
	{Key: PermUsersEmailManage, Description: "Manage user emails and verification state"},
	{Key: PermUsersCreate, Description: "Create a new user account"},
	{Key: PermUsersConnectionsManage, Description: "Manage a user's connected external accounts"},
	{Key: PermUsersDelete, Description: "Soft-delete a user account"},
	{Key: PermUsersExport, Description: "Export user data (CSV / JSON)"},
	{Key: PermUsersAuditRead, Description: "Read user-scoped audit trail"},
	{Key: PermUsersForceUnlink, Description: "Force-unlink a Steam account from a user"},
	{Key: PermProxiesEdit, Description: "Create, update, or delete outbound proxy providers"},
	{Key: PermProxiesRotateCreds, Description: "Rotate outbound proxy provider credentials"},
	{Key: PermLinksReview, Description: "View the Steam-Discord link review queue"},
	{Key: PermLinksApprove, Description: "Approve or reject queued Steam-Discord links"},
	{Key: PermGuildsUntrackServer, Description: "Remove a server from a guild's tracked set"},
	{Key: PermAdminsGrant, Description: "Grant or revoke admin roles; edit role-permission mappings"},
	{Key: PermAuditRead, Description: "Read the admin audit log"},
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
