export type AdminActionKey =
  | "users.edit"
  | "users.ban.set"
  | "users.suspend"
  | "users.unsuspend"
  | "users.delete"
  | "users.password.forceReset"
  | "users.force_unlink_steam"
  | "users.roles.grant"
  | "users.roles.revoke"
  | "users.email.setPrimary"
  | "users.email.add"
  | "users.email.remove"
  | "users.email.resendVerification"
  | "users.invite"
  | "users.connections.unlink"
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
  "users.ban.set": {
    key: "users.ban.set",
    label: "Manage restrictions",
    permission: "users.suspend",
    requiresReason: true,
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
    dangerous: true,
    resourceType: "user",
  },
  "users.unsuspend": {
    key: "users.unsuspend",
    label: "Unsuspend user",
    permission: "users.suspend",
    requiresReason: false,
    dangerous: false,
    resourceType: "user",
  },
  "users.delete": {
    key: "users.delete",
    label: "Delete user",
    permission: "users.delete",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
  "users.password.forceReset": {
    key: "users.password.forceReset",
    label: "Force password reset",
    permission: "users.security.reset",
    requiresReason: true,
    dangerous: true,
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
  "users.email.setPrimary": {
    key: "users.email.setPrimary",
    label: "Set primary email",
    permission: "users.email.manage",
    requiresReason: false,
    dangerous: false,
    resourceType: "user",
  },
  "users.email.add": {
    key: "users.email.add",
    label: "Add email",
    permission: "users.email.manage",
    requiresReason: false,
    dangerous: false,
    resourceType: "user",
  },
  "users.email.remove": {
    key: "users.email.remove",
    label: "Remove email",
    permission: "users.email.manage",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
  "users.email.resendVerification": {
    key: "users.email.resendVerification",
    label: "Send verification",
    permission: "users.email.manage",
    requiresReason: false,
    dangerous: false,
    resourceType: "user",
  },
  "users.invite": {
    key: "users.invite",
    label: "Invite user",
    permission: "users.create",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
  "users.connections.unlink": {
    key: "users.connections.unlink",
    label: "Unlink connection",
    permission: "users.connections.manage",
    requiresReason: true,
    dangerous: true,
    resourceType: "user",
  },
};
