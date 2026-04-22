export type AdminActionKey =
  | "users.edit"
  | "users.suspend"
  | "users.force_unlink_steam"
  | "users.roles.grant"
  | "users.roles.revoke"
  | "proxies.edit"
  | "proxies.rotate_credentials"
  | "links.approve"
  | "guilds.untrack_server"
  | "admins.grant";

export interface AdminActionMeta {
  key: AdminActionKey;
  label: string;
  permission: string;
  requiresReason: boolean;
  dangerous: boolean;
  resourceType: "user" | "proxy" | "link" | "guild" | "admin";
}

export const ADMIN_ACTIONS: Record<AdminActionKey, AdminActionMeta> = {
  "users.edit": {
    key: "users.edit",
    label: "Edit user",
    permission: "users.edit",
    requiresReason: false,
    dangerous: false,
    resourceType: "user",
  },
  "users.roles.grant": {
    key: "users.roles.grant",
    label: "Grant role",
    permission: "users.roles.assign",
    requiresReason: true,
    dangerous: false,
    resourceType: "user",
  },
  "users.roles.revoke": {
    key: "users.roles.revoke",
    label: "Revoke role",
    permission: "users.roles.assign",
    requiresReason: true,
    dangerous: false,
    resourceType: "user",
  },
  "users.suspend": {
    key: "users.suspend",
    label: "Suspend user",
    permission: "users.suspend",
    requiresReason: true,
    dangerous: false,
    resourceType: "user",
  },
  "users.force_unlink_steam": {
    key: "users.force_unlink_steam",
    label: "Force-unlink Steam",
    permission: "users.force_unlink_steam",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
  "proxies.edit": {
    key: "proxies.edit",
    label: "Edit proxy",
    permission: "proxies.edit",
    requiresReason: true,
    dangerous: false,
    resourceType: "proxy",
  },
  "proxies.rotate_credentials": {
    key: "proxies.rotate_credentials",
    label: "Rotate credentials",
    permission: "proxies.rotate_credentials",
    requiresReason: true,
    dangerous: true,
    resourceType: "proxy",
  },
  "links.approve": {
    key: "links.approve",
    label: "Approve link",
    permission: "links.approve",
    requiresReason: true,
    dangerous: false,
    resourceType: "link",
  },
  "guilds.untrack_server": {
    key: "guilds.untrack_server",
    label: "Untrack server",
    permission: "guilds.untrack_server",
    requiresReason: true,
    dangerous: false,
    resourceType: "guild",
  },
  "admins.grant": {
    key: "admins.grant",
    label: "Grant admin role",
    permission: "admins.grant",
    requiresReason: true,
    dangerous: true,
    resourceType: "admin",
  },
};
