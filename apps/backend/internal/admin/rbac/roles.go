package rbac

const (
	RoleSuperadmin  = "superadmin"
	RoleModerator   = "moderator"
	RoleInfraAdmin  = "infra_admin"
	RoleGuildOps    = "guild_ops"
	RoleUserSupport = "user_support"
	RoleUserAdmin   = "user_admin"
	RoleUserAuditor = "user_auditor"
)

type RoleSeed struct {
	Key         string
	Name        string
	Description string
	IsSystem    bool
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
				"proxies.edit",
				"proxies.rotate_credentials",
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
			Description: "Read users + audit + trigger password resets and verification resends",
			IsSystem:    true,
			Permissions: []string{
				PermUsersRead, PermUsersAuditRead,
				PermUsersSecurityReset, PermUsersEmailManage,
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
				PermUsersSecurityReset, PermUsersEmailManage,
				PermUsersCreate, PermUsersConnectionsManage,
				PermUsersDelete, PermUsersExport,
			},
		},
		{
			Key:         RoleUserAuditor,
			Name:        "User Auditor",
			Description: "Read-only users view with export",
			IsSystem:    true,
			Permissions: []string{PermUsersRead, PermUsersAuditRead, PermUsersExport},
		},
	}
}
