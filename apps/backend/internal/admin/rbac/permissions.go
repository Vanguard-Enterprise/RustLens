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
	PermUsersCreate            = "users.create"
	PermUsersConnectionsManage = "users.connections.manage"
	PermUsersDelete            = "users.delete"
	PermUsersAuditRead         = "users.audit.read"
	PermUsersForceUnlink       = "users.force_unlink_steam"

	PermAdminsGrant             = "admins.grant"
	PermCommandsRateLimitBypass = "commands.rate_limit.bypass"
	PermCommandsFraudBypass     = "commands.fraud.bypass"
	PermProxiesRead             = "proxies.read"
	PermProxiesEdit             = "proxies.edit"
	PermProxiesRotateCreds      = "proxies.rotate_credentials"
	PermBmServersRead           = "bm_servers.read"
	PermBmServersEdit           = "bm_servers.edit"
	PermLinksReview             = "links.review"
	PermLinksApprove            = "links.approve"
	PermGuildsUntrackServer     = "guilds.untrack_server"
	PermGuildsRead              = "guilds.read"
	PermRolesRead               = "roles.read"
	PermAuditRead               = "audit.read"
	PermAPIKeysManage           = "api_keys.manage"
	PermBotRoutesRead           = "bot_routes.read"
	PermBotRoutesWrite          = "bot_routes.write"
)

var permissions = []Permission{
	{Key: PermUsersRead, Description: "View user profiles and the users list"},
	{Key: PermUsersEdit, Description: "Edit a user's profile fields (display name, avatar)"},
	{Key: PermUsersRolesAssign, Description: "Assign or revoke admin roles on users"},
	{Key: PermUsersSuspend, Description: "Suspend or unsuspend a user account"},
	{Key: PermUsersSecurityReset, Description: "Invalidate a user's sessions"},
	{Key: PermUsersCreate, Description: "Create a new user account"},
	{Key: PermUsersConnectionsManage, Description: "Manage a user's connected external accounts"},
	{Key: PermUsersDelete, Description: "Soft-delete a user account"},
	{Key: PermUsersAuditRead, Description: "Read user-scoped audit trail"},
	{Key: PermUsersForceUnlink, Description: "Force-unlink a Steam account from a user"},
	{Key: PermProxiesRead, Description: "View the outbound proxy provider list"},
	{Key: PermProxiesEdit, Description: "Create, update, or delete outbound proxy providers"},
	{Key: PermProxiesRotateCreds, Description: "Rotate outbound proxy provider credentials"},
	{Key: PermBmServersRead, Description: "View the tracked BattleMetrics server list"},
	{Key: PermBmServersEdit, Description: "Create, update, or delete tracked BattleMetrics servers"},
	{Key: PermLinksReview, Description: "View the Steam-Discord link review queue"},
	{Key: PermLinksApprove, Description: "Approve or reject queued Steam-Discord links"},
	{Key: PermGuildsUntrackServer, Description: "Remove a server from a guild's tracked set"},
	{Key: PermGuildsRead, Description: "View the admin guilds list"},
	{Key: PermRolesRead, Description: "View the admin role catalog and members"},
	{Key: PermAdminsGrant, Description: "Grant or revoke admin roles; edit role-permission mappings"},
	{Key: PermAuditRead, Description: "Read the admin audit log"},
	{Key: PermCommandsRateLimitBypass, Description: "Bypass slash-command rate limits (no cooldown)"},
	{Key: PermCommandsFraudBypass, Description: "Exempt slash-command invocations from fraud detection counters and alerts"},
	{Key: PermAPIKeysManage, Description: "Create, rotate, and revoke API keys (cannot grant permissions the creator does not currently hold)"},
	{Key: PermBotRoutesRead, Description: "Allow an API key to call GET endpoints under /api/internal/bot/*"},
	{Key: PermBotRoutesWrite, Description: "Allow an API key to call POST/PUT/DELETE/PATCH endpoints under /api/internal/bot/*"},
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
