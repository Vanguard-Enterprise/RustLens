package rbac

const (
	RoleSuperadmin  = "superadmin"
	RoleModerator   = "moderator"
	RoleInfraAdmin  = "infra_admin"
	RoleGuildOps    = "guild_ops"
	RoleUserSupport = "user_support"
	RoleUserAdmin   = "user_admin"
	RoleUserAuditor = "user_auditor"
	RolePRO         = "PRO"
)

type RoleSeed struct {
	Key         string
	Name        string
	Description string
	IsSystem    bool
	Color       string
	Permissions []string // empty for superadmin (implicit all)
}

func SystemRoles() []RoleSeed {
	return []RoleSeed{
		{
			Key:         RoleSuperadmin,
			Name:        "Super Admin",
			Description: "Implicit all permissions; only a superadmin can grant this role.",
			IsSystem:    true,
			Permissions: nil,
		},
		{
			Key:         RoleModerator,
			Name:        "Moderator",
			Description: "User moderation and link review.",
			Permissions: []string{
				"users.suspend",
				"users.force_unlink_steam",
				"links.review",
				"links.approve",
				"audit.read",
			},
		},
		{
			Key:         RoleInfraAdmin,
			Name:        "Infrastructure Admin",
			Description: "Manage outbound proxies and infrastructure configuration.",
			Permissions: []string{
				"proxies.read",
				"proxies.edit",
				"proxies.rotate_credentials",
				"bm_servers.read",
				"bm_servers.edit",
				"audit.read",
			},
		},
		{
			Key:         RoleGuildOps,
			Name:        "Guild Operations",
			Description: "Intervene in guild configuration.",
			Permissions: []string{
				"guilds.untrack_server",
				"audit.read",
			},
		},
		{
			Key:         RoleUserSupport,
			Name:        "User Support",
			Description: "Read users + audit + invalidate sessions",
			IsSystem:    true,
			Permissions: []string{
				PermUsersRead, PermUsersAuditRead,
				PermUsersSecurityReset,
				PermUsersEdit,
			},
		},
		{
			Key:         RoleUserAdmin,
			Name:        "User Admin",
			Description: "Full user lifecycle management (non-destructive)",
			IsSystem:    true,
			Permissions: []string{
				PermUsersRead, PermUsersAuditRead, PermUsersEdit,
				PermUsersRolesAssign, PermUsersSuspend,
				PermUsersSecurityReset,
				PermUsersCreate, PermUsersConnectionsManage,
				PermUsersDelete,
			},
		},
		{
			Key:         RoleUserAuditor,
			Name:        "User Auditor",
			Description: "Read-only users view with audit trail",
			IsSystem:    true,
			Permissions: []string{PermUsersRead, PermUsersAuditRead},
		},
		{
			Key:         RolePRO,
			Name:        "PRO",
			Description: "Yellow system role for PRO users.",
			IsSystem:    true,
			Color:       "#facc15",
			Permissions: []string{},
		},
	}
}
