//! Static RBAC permission catalog, mirroring the Go
//! `internal/admin/rbac/permissions.go` exactly.
//!
//! The catalog is the canonical list returned by `GET /api/app/admin/permissions`
//! and used to fill a `superadmin` role's `permissions[]` on the roles endpoint.
//! Order matches the Go `permissions` slice (NOT sorted) — the permissions
//! endpoint returns it verbatim, so the order is part of the contract.
//!
//! NOTE: this is distinct from the `admin_permissions` DB table. The live `/me`
//! superadmin permission set is read from that table (see
//! [`crate::identity::load_permissions`]); this static catalog backs the admin
//! UI's permission grid and the superadmin role display.

use serde::Serialize;

/// A single permission entry (key + human description).
#[derive(Debug, Clone, Serialize)]
pub struct Permission {
    pub key: &'static str,
    pub description: &'static str,
}

// Permission key constants (mirror the Go `Perm*` consts).
pub const USERS_READ: &str = "users.read";
pub const USERS_EDIT: &str = "users.edit";
pub const USERS_ROLES_ASSIGN: &str = "users.roles.assign";
pub const USERS_SUSPEND: &str = "users.suspend";
pub const USERS_SECURITY_RESET: &str = "users.security.reset";
pub const USERS_CREATE: &str = "users.create";
pub const USERS_CONNECTIONS_MANAGE: &str = "users.connections.manage";
pub const USERS_DELETE: &str = "users.delete";
pub const USERS_AUDIT_READ: &str = "users.audit.read";
pub const USERS_FORCE_UNLINK: &str = "users.force_unlink_steam";
pub const ADMINS_GRANT: &str = "admins.grant";
pub const COMMANDS_RATE_LIMIT_BYPASS: &str = "commands.rate_limit.bypass";
pub const COMMANDS_FRAUD_BYPASS: &str = "commands.fraud.bypass";
pub const PROXIES_READ: &str = "proxies.read";
pub const PROXIES_EDIT: &str = "proxies.edit";
pub const PROXIES_ROTATE_CREDS: &str = "proxies.rotate_credentials";
pub const BM_SERVERS_READ: &str = "bm_servers.read";
pub const BM_SERVERS_EDIT: &str = "bm_servers.edit";
pub const LINKS_REVIEW: &str = "links.review";
pub const LINKS_APPROVE: &str = "links.approve";
pub const GUILDS_UNTRACK_SERVER: &str = "guilds.untrack_server";
pub const GUILDS_READ: &str = "guilds.read";
pub const ROLES_READ: &str = "roles.read";
pub const AUDIT_READ: &str = "audit.read";
pub const API_KEYS_MANAGE: &str = "api_keys.manage";
pub const BOT_ROUTES_READ: &str = "bot_routes.read";
pub const BOT_ROUTES_WRITE: &str = "bot_routes.write";

/// The superadmin role key — implies every permission.
pub const ROLE_SUPERADMIN: &str = "superadmin";

/// The full permission catalog, in Go `permissions` slice order.
pub const ALL_PERMISSIONS: &[Permission] = &[
    Permission { key: USERS_READ, description: "View user profiles and the users list" },
    Permission { key: USERS_EDIT, description: "Edit a user's profile fields (display name, avatar)" },
    Permission { key: USERS_ROLES_ASSIGN, description: "Assign or revoke admin roles on users" },
    Permission { key: USERS_SUSPEND, description: "Suspend or unsuspend a user account" },
    Permission { key: USERS_SECURITY_RESET, description: "Invalidate a user's sessions" },
    Permission { key: USERS_CREATE, description: "Create a new user account" },
    Permission { key: USERS_CONNECTIONS_MANAGE, description: "Manage a user's connected external accounts" },
    Permission { key: USERS_DELETE, description: "Soft-delete a user account" },
    Permission { key: USERS_AUDIT_READ, description: "Read user-scoped audit trail" },
    Permission { key: USERS_FORCE_UNLINK, description: "Force-unlink a Steam account from a user" },
    Permission { key: PROXIES_READ, description: "View the outbound proxy provider list" },
    Permission { key: PROXIES_EDIT, description: "Create, update, or delete outbound proxy providers" },
    Permission { key: PROXIES_ROTATE_CREDS, description: "Rotate outbound proxy provider credentials" },
    Permission { key: BM_SERVERS_READ, description: "View the tracked BattleMetrics server list" },
    Permission { key: BM_SERVERS_EDIT, description: "Create, update, or delete tracked BattleMetrics servers" },
    Permission { key: LINKS_REVIEW, description: "View the Steam-Discord link review queue" },
    Permission { key: LINKS_APPROVE, description: "Approve or reject queued Steam-Discord links" },
    Permission { key: GUILDS_UNTRACK_SERVER, description: "Remove a server from a guild's tracked set" },
    Permission { key: GUILDS_READ, description: "View the admin guilds list" },
    Permission { key: ROLES_READ, description: "View the admin role catalog and members" },
    Permission { key: ADMINS_GRANT, description: "Grant or revoke admin roles; edit role-permission mappings" },
    Permission { key: AUDIT_READ, description: "Read the admin audit log" },
    Permission { key: COMMANDS_RATE_LIMIT_BYPASS, description: "Bypass slash-command rate limits (no cooldown)" },
    Permission { key: COMMANDS_FRAUD_BYPASS, description: "Exempt slash-command invocations from fraud detection counters and alerts" },
    Permission { key: API_KEYS_MANAGE, description: "Create, rotate, and revoke API keys (cannot grant permissions the creator does not currently hold)" },
    Permission { key: BOT_ROUTES_READ, description: "Allow an API key to call GET endpoints under /api/internal/bot/*" },
    Permission { key: BOT_ROUTES_WRITE, description: "Allow an API key to call POST/PUT/DELETE/PATCH endpoints under /api/internal/bot/*" },
];

/// Whether `key` is a known permission (mirrors `IsValidPermission`).
pub fn is_valid_permission(key: &str) -> bool {
    ALL_PERMISSIONS.iter().any(|p| p.key == key)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_has_27_entries_in_go_order() {
        assert_eq!(ALL_PERMISSIONS.len(), 27);
        // First and last anchor the slice order (must match Go, not sorted).
        assert_eq!(ALL_PERMISSIONS[0].key, "users.read");
        assert_eq!(ALL_PERMISSIONS[26].key, "bot_routes.write");
        // admins.grant sits after roles.read in the Go slice, not alphabetized.
        let grant = ALL_PERMISSIONS.iter().position(|p| p.key == "admins.grant").unwrap();
        let roles = ALL_PERMISSIONS.iter().position(|p| p.key == "roles.read").unwrap();
        assert!(grant > roles, "catalog order must match the Go slice");
    }

    #[test]
    fn validity_check() {
        assert!(is_valid_permission("users.read"));
        assert!(!is_valid_permission("nope.invalid"));
    }
}
