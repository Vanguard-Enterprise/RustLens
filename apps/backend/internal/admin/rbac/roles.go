package rbac

const (
	RoleSuperadmin = "superadmin"
	RoleModerator  = "moderator"
	RoleInfraAdmin = "infra_admin"
	RoleGuildOps   = "guild_ops"
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
	}
}
