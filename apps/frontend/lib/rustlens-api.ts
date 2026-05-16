import "server-only";

import { redirect } from "next/navigation";
import { clearSession, getSessionToken } from "@/lib/session";
import { noticeFromBackendError } from "@/lib/session-errors";

const SESSION_INVALID_ERRORS = new Set([
	"session_required",
	"session_revoked",
	"invalid_session",
]);

export type AppUser = {
	id: string;
	discordId: string;
	discordUsername: string;
	discordGlobalName?: string;
	/** Discord CDN avatar HASH, NOT a URL. */
	discordAvatar?: string;
	displayName: string;
	avatarUrl: string;
	roles: string[];
	createdAt: string;
	updatedAt: string;
	lastLoginAt: string;
	sessionEpoch?: number;
};

export type LinkedSteamAccount = {
	steamId: string;
	bmId: string;
	bmName: string;
	avatarImage: string;
	type: string;
	source: string;
};

export type UserGuildMembership = {
	guildId: string;
	isActive: boolean;
	firstSeenAt: string;
	lastSeenAt: string;
	leftAt: string | null;
};

export type CurrentUser = {
	user: AppUser;
	steamAccounts: LinkedSteamAccount[];
	guilds: UserGuildMembership[];
	/**
	 * Admin role keys granted via admin_user_roles (e.g. "superadmin",
	 * "moderator"). Distinct from `user.roles` which is the legacy
	 * app_users.roles JSONB column.
	 */
	adminRoles: string[];
	/**
	 * Flattened permission keys resolved from the user's admin roles.
	 * Superadmin implies every registered permission.
	 */
	permissions: string[];
};

export type DashboardGuildOwner = {
	id: string;
	username: string;
	globalName: string | null;
	avatarHash: string | null;
};

export type DashboardGuild = {
	id: string;
	name: string;
	iconHash: string | null;
	bannerHash: string | null;
	approximateMemberCount: number | null;
	trackedServerCount: number;
	owner: DashboardGuildOwner | null;
};

export type GuildLink = {
	steamId: string;
	linkedAt: string;
};

export type AdminUserItem = {
	user: AppUser;
	discordId: string;
	discordUsername: string;
	discordGlobalName?: string;
	discordAvatar?: string;
	guildCount: number;
	steamConnectionCount: number;
	adminRoles: string[];
	suspendedAt?: string;
	deletedAt?: string;
};

export type AdminUsersQuery = {
	q?: string;
	role?: string[];
	status?: "active" | "suspended" | "deleted" | "any";
	discord?: "any" | "linked" | "unlinked";
	steam?: "any" | "linked" | "unlinked";
	sort?: string;
	page?: number;
	pageSize?: number;
};

export type AdminUsersPage = {
	items: AdminUserItem[];
	total: number;
	page: number;
	pageSize: number;
};

export type AdminGuildItem = {
	guildId: string;
	name: string;
	iconHash: string | null;
	ownerDiscordId: string | null;
	ownerUsername: string | null;
	ownerGlobalName: string | null;
	ownerAvatarHash: string | null;
	approximateMemberCount: number | null;
	isInstalled: boolean;
	leftAt: string | null;
	isConfigured: boolean;
	trackedServerCount: number;
};

export type AdminGuildsQuery = {
	q?: string;
	page?: number;
	pageSize?: number;
};

export type AdminGuildsPage = {
	items: AdminGuildItem[];
	total: number;
	page: number;
	pageSize: number;
};

export type AdminUserInstallItem = {
	discordId: string;
	username: string;
	globalName: string | null;
	avatarHash: string | null;
	scopes: string[];
	isInstalled: boolean;
	installedAt: string;
	leftAt: string | null;
	lastSeenAt: string | null;
};

export type AdminUserInstallsQuery = {
	q?: string;
	page?: number;
	pageSize?: number;
};

export type AdminUserInstallsPage = {
	items: AdminUserInstallItem[];
	total: number;
	page: number;
	pageSize: number;
};

export type AdminServerItem = {
	serverBmId: string;
	name: string;
	lastPolledAt: string | null;
	lastPollError: string;
	guildCount: number;
};

export type AdminRoleItem = {
	id: string;
	key: string;
	name: string;
	description: string | null;
	isSystem: boolean;
	permissions: string[];
	memberCount: number;
};

export type AdminPermissionItem = {
	key: string;
	description: string;
};

export type AuditEventItem = {
	id: string;
	actorUserId: string;
	targetUserId: string;
	eventType: string;
	payload: Record<string, unknown>;
	createdAt: string;
	actorDisplayName?: string | null;
	actorAvatarUrl?: string | null;
	targetDisplayName?: string | null;
	targetAvatarUrl?: string | null;
	result?: "success" | "error" | null;
	reason?: string | null;
};

export type AdminUserGuild = {
	guildId: string;
	nick?: string;
};

export type AdminUserAuditItem = AuditEventItem;

export type AdminUserDetail = {
	user: AppUser;
	discordId: string;
	discordUsername: string;
	discordGlobalName?: string;
	discordAvatar?: string;
	guilds: AdminUserGuild[];
	adminRoles: string[];
	suspendedAt?: string;
	suspendReason?: string;
	deletedAt?: string;
	recentAudit: AdminUserAuditItem[];
};

export type NotificationPreferences = {
	notifyInApp: boolean;
	notifyDiscordAnnouncements: boolean;
};

export type NotificationPreferencesPatch = Partial<NotificationPreferences>;

/**
 * Envelope returned by /api/app/admin/actions/{type} on success. `result` is
 * whatever the action's Execute() put in ActionResult.Payload.
 */
export type AdminActionResponse<T = unknown> = {
	ok: true;
	result: T;
};

const API_BASE_URL =
	process.env.RUSTLENS_API_URL?.replace(/\/+$/, "") ?? "http://127.0.0.1:8080";

function getAppSecret() {
	const secret = process.env.APP_API_INTERNAL_SECRET;
	if (!secret) {
		throw new Error("APP_API_INTERNAL_SECRET is required");
	}
	return secret;
}

async function backendFetch<T>(
	path: string,
	init: RequestInit = {},
	options: { requireSession?: boolean; sessionToken?: string } = {},
): Promise<T> {
	const headers = new Headers(init.headers);
	headers.set("Content-Type", "application/json");
	headers.set("X-RustLens-App-Secret", getAppSecret());
	if (options.requireSession) {
		const sessionToken = options.sessionToken ?? (await getSessionToken());
		if (!sessionToken) {
			throw new Error("RustLens session is required");
		}
		headers.set("X-RustLens-Session", sessionToken);
	}

	const response = await fetch(`${API_BASE_URL}${path}`, {
		...init,
		headers,
		cache: "no-store",
	});

	if (!response.ok) {
		let message = `RustLens API request failed: ${response.status}`;
		let errorCode: string | undefined;
		try {
			const payload = (await response.json()) as { error?: string; message?: string };
			if (payload.error) {
				errorCode = payload.error;
				message = payload.message || payload.error;
			}
		} catch {
			// Ignore JSON parsing failures and fall back to the status message.
		}
		if (
			response.status === 401 &&
			options.requireSession &&
			errorCode &&
			SESSION_INVALID_ERRORS.has(errorCode)
		) {
			await clearSession();
			const notice = noticeFromBackendError(errorCode);
			redirect(`/login?notice=${notice}`);
		}
		throw new Error(message);
	}

	return (await response.json()) as T;
}

type AdminActionEnvelope = {
	params: Record<string, unknown>;
	reason: string;
};

/**
 * Posts to the admin action dispatcher. Returns the action's result payload
 * (the inner `result` field from the standard `{ok, result}` envelope).
 *
 * Server-only helper; consumed by Server Actions in app/admin/.../actions.ts.
 */
export async function postAdminAction<T = unknown>(
	type: string,
	params: Record<string, unknown>,
	reason: string,
): Promise<T> {
	const envelope: AdminActionEnvelope = { params, reason };
	const payload = await backendFetch<AdminActionResponse<T>>(
		`/api/app/admin/actions/${encodeURIComponent(type)}`,
		{ method: "POST", body: JSON.stringify(envelope) },
		{ requireSession: true },
	);
	return payload.result;
}

export function getCurrentUser() {
	return backendFetch<CurrentUser>("/api/app/me", undefined, { requireSession: true });
}

export async function getCurrentUserSafe() {
	try {
		return await getCurrentUser();
	} catch {
		return null;
	}
}

export function getNotificationPreferences() {
	return backendFetch<NotificationPreferences>(
		"/api/app/me/preferences",
		undefined,
		{ requireSession: true },
	);
}

export function updateNotificationPreferences(patch: NotificationPreferencesPatch) {
	return backendFetch<NotificationPreferences>(
		"/api/app/me/preferences",
		{ method: "PATCH", body: JSON.stringify(patch) },
		{ requireSession: true },
	);
}

/**
 * Returns the guilds the current user is a member of and that have RustLens
 * installed, with metadata (name, owner, icon, banner, member count) suitable
 * for rendering on the user dashboard.
 */
export async function getMyInstalledGuilds(): Promise<DashboardGuild[]> {
	const payload = await backendFetch<{ items: DashboardGuild[] }>(
		"/api/app/me/guilds/installed",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

/**
 * Returns the current user's Discord<->Steam links scoped to a single guild.
 * Throws if the caller is not a member of the guild (backend returns 403).
 */
export async function getMyGuildLinks(guildId: string): Promise<GuildLink[]> {
	const payload = await backendFetch<{ items: GuildLink[] }>(
		`/api/app/me/guilds/${encodeURIComponent(guildId)}/links`,
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

export async function getAdminUsers(params: AdminUsersQuery = {}): Promise<AdminUsersPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	for (const r of params.role ?? []) sp.append("role", r);
	if (params.status) sp.set("status", params.status);
	if (params.discord) sp.set("discord", params.discord);
	if (params.steam) sp.set("steam", params.steam);
	if (params.sort) sp.set("sort", params.sort);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminUsersPage>(
		`/api/app/admin/users${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminGuilds(params: AdminGuildsQuery = {}): Promise<AdminGuildsPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminGuildsPage>(
		`/api/app/admin/guilds${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminUserInstalls(
	params: AdminUserInstallsQuery = {},
): Promise<AdminUserInstallsPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminUserInstallsPage>(
		`/api/app/admin/user-installs${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminServers() {
	const payload = await backendFetch<{ items: AdminServerItem[] }>(
		"/api/app/admin/servers",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

export async function getAdminRoles() {
	const payload = await backendFetch<{ items: AdminRoleItem[] }>(
		"/api/app/admin/roles",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

export async function getAdminPermissions() {
	const payload = await backendFetch<{ items: AdminPermissionItem[] }>(
		"/api/app/admin/permissions",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

export async function getAdminUser(userId: string): Promise<AdminUserDetail> {
	return backendFetch<AdminUserDetail>(
		`/api/app/admin/users/${encodeURIComponent(userId)}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminUserAudit(
	userId: string,
	opts: { limit?: number; before?: string } = {},
): Promise<{ items: AdminUserAuditItem[] }> {
	const sp = new URLSearchParams();
	if (opts.limit) sp.set("limit", String(opts.limit));
	if (opts.before) sp.set("before", opts.before);
	const qs = sp.toString();
	return backendFetch<{ items: AdminUserAuditItem[] }>(
		`/api/app/admin/users/${encodeURIComponent(userId)}/audit${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAuditEvents(limit = 30) {
	const payload = await backendFetch<{ items: AuditEventItem[] }>(
		`/api/app/admin/audit?limit=${limit}`,
		undefined,
		{ requireSession: true },
	);
	return payload.items;
}

export type AdminAuditQuery = {
	targetUserId?: string;
	actorUserId?: string;
	eventType?: string[];
	result?: "success" | "error";
	since?: string; // RFC3339; resolved server-side from a preset key
	before?: string; // RFC3339 cursor
	pageSize?: number;
};

export type AdminAuditPage = {
	items: AuditEventItem[];
	nextCursor: string | null;
};

export async function getAdminAudit(params: AdminAuditQuery = {}): Promise<AdminAuditPage> {
	const sp = new URLSearchParams();
	if (params.targetUserId) sp.set("targetUserId", params.targetUserId);
	if (params.actorUserId) sp.set("actorUserId", params.actorUserId);
	for (const t of params.eventType ?? []) sp.append("eventType", t);
	if (params.result) sp.set("result", params.result);
	if (params.since) sp.set("since", params.since);
	if (params.before) sp.set("before", params.before);
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminAuditPage>(
		`/api/app/admin/audit${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function getAdminAuditEventTypes(): Promise<string[]> {
	const payload = await backendFetch<{ types: string[] | null }>(
		"/api/app/admin/audit/event-types",
		undefined,
		{ requireSession: true },
	);
	return payload.types ?? [];
}

export async function getAdminAuditEvent(id: string): Promise<AuditEventItem> {
	return backendFetch<AuditEventItem>(
		`/api/app/admin/audit/${encodeURIComponent(id)}`,
		undefined,
		{ requireSession: true },
	);
}

/**
 * Thin wrapper used by the Audit page's actor/target combobox.
 * Pinned to status="any" so suspended/deleted users still surface.
 */
export async function searchAdminUsers(q: string): Promise<AdminUserItem[]> {
	const trimmed = q.trim();
	if (trimmed.length < 2) return [];
	const page = await getAdminUsers({ q: trimmed, pageSize: 10, status: "any" });
	return page.items;
}

// --- Outbound proxies ---

export type AdminProxyStatus = "active" | "paused" | "broken";

export type AdminProxyItem = {
	id: number;
	endpoint: string;
	enabled: boolean;
	status: AdminProxyStatus;
	autoDisabledAt?: string;
	createdAt?: string;
	updatedAt?: string;
};

export type AdminProxiesQuery = {
	q?: string;
	status?: AdminProxyStatus;
	page?: number;
	pageSize?: number;
};

export type AdminProxiesPage = {
	items: AdminProxyItem[];
	total: number;
	page: number;
	pageSize: number;
};

export async function getAdminProxies(
	params: AdminProxiesQuery = {},
): Promise<AdminProxiesPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.status) sp.set("status", params.status);
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	const qs = sp.toString();
	return backendFetch<AdminProxiesPage>(
		`/api/app/admin/proxies${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function createAdminProxy(
	endpoint: string,
	reason: string,
): Promise<AdminProxyItem> {
	return backendFetch<AdminProxyItem>(
		"/api/app/admin/proxies",
		{ method: "POST", body: JSON.stringify({ endpoint, reason }) },
		{ requireSession: true },
	);
}

export async function updateAdminProxy(
	id: number,
	enabled: boolean,
	reason: string,
): Promise<AdminProxyItem> {
	return backendFetch<AdminProxyItem>(
		`/api/app/admin/proxies/${id}`,
		{ method: "PATCH", body: JSON.stringify({ enabled, reason }) },
		{ requireSession: true },
	);
}

export async function deleteAdminProxy(
	id: number,
	reason: string,
): Promise<{ status: string }> {
	const qs = reason ? `?reason=${encodeURIComponent(reason)}` : "";
	return backendFetch<{ status: string }>(
		`/api/app/admin/proxies/${id}${qs}`,
		{ method: "DELETE" },
		{ requireSession: true },
	);
}

export type BulkProxyAction = "enable" | "disable" | "delete" | "clear_broken";

export type BulkProxyResult = {
	action: BulkProxyAction;
	succeeded: number;
	failed: number;
	results: Array<{ id: number; ok: boolean; error?: string }>;
};

export async function bulkAdminProxies(
	action: BulkProxyAction,
	ids: number[],
	reason: string,
): Promise<BulkProxyResult> {
	return backendFetch<BulkProxyResult>(
		"/api/app/admin/proxies/bulk",
		{ method: "POST", body: JSON.stringify({ action, ids, reason }) },
		{ requireSession: true },
	);
}

// --- BattleMetrics tracked servers ---

export type AdminBmServerItem = {
	bmId: string;
	name: string;
	isVitality: boolean;
	isAtlas: boolean;
	isVital: boolean;
	isModded: boolean;
	isOfficial: boolean;
	isBuild: boolean;
	highFrequency: boolean;
	lastPolledAt?: string;
	lastPollError: string;
};

export type AdminBmServerSortKey =
	| "name"
	| "bmId"
	| "highFrequency"
	| "lastPolledAt"
	| "pollStatus";

export type AdminBmServerSortOrder = "asc" | "desc";

export type AdminBmServersQuery = {
	q?: string;
	highFrequency?: boolean;
	hasError?: boolean;
	isVitality?: boolean;
	isAtlas?: boolean;
	isVital?: boolean;
	isModded?: boolean;
	isOfficial?: boolean;
	isBuild?: boolean;
	page?: number;
	pageSize?: number;
	sortBy?: AdminBmServerSortKey;
	sortOrder?: AdminBmServerSortOrder;
};

export type AdminBmServersPage = {
	items: AdminBmServerItem[];
	total: number;
	page: number;
	pageSize: number;
};

export type AdminBmServerInput = {
	bmId: string;
	name: string;
	isVitality: boolean;
	isAtlas: boolean;
	isVital: boolean;
	isModded: boolean;
	isOfficial: boolean;
	isBuild: boolean;
	highFrequency: boolean;
};

export async function getAdminBmServers(
	params: AdminBmServersQuery = {},
): Promise<AdminBmServersPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	if (params.highFrequency !== undefined)
		sp.set("highFrequency", String(params.highFrequency));
	if (params.hasError !== undefined) sp.set("hasError", String(params.hasError));
	if (params.isVitality !== undefined)
		sp.set("isVitality", String(params.isVitality));
	if (params.isAtlas !== undefined) sp.set("isAtlas", String(params.isAtlas));
	if (params.isVital !== undefined) sp.set("isVital", String(params.isVital));
	if (params.isModded !== undefined) sp.set("isModded", String(params.isModded));
	if (params.isOfficial !== undefined)
		sp.set("isOfficial", String(params.isOfficial));
	if (params.isBuild !== undefined) sp.set("isBuild", String(params.isBuild));
	if (params.page) sp.set("page", String(params.page));
	if (params.pageSize) sp.set("pageSize", String(params.pageSize));
	if (params.sortBy) sp.set("sortBy", params.sortBy);
	if (params.sortOrder) sp.set("sortOrder", params.sortOrder);
	const qs = sp.toString();
	return backendFetch<AdminBmServersPage>(
		`/api/app/admin/bm-servers${qs ? `?${qs}` : ""}`,
		undefined,
		{ requireSession: true },
	);
}

export async function createAdminBmServer(
	input: AdminBmServerInput,
	reason: string,
): Promise<AdminBmServerItem> {
	return backendFetch<AdminBmServerItem>(
		"/api/app/admin/bm-servers",
		{ method: "POST", body: JSON.stringify({ ...input, reason }) },
		{ requireSession: true },
	);
}

export async function updateAdminBmServer(
	currentBmId: string,
	input: AdminBmServerInput,
	reason: string,
): Promise<AdminBmServerItem> {
	return backendFetch<AdminBmServerItem>(
		`/api/app/admin/bm-servers/${encodeURIComponent(currentBmId)}`,
		{ method: "PUT", body: JSON.stringify({ ...input, reason }) },
		{ requireSession: true },
	);
}

export async function deleteAdminBmServer(
	bmId: string,
	reason: string,
): Promise<{ status: string }> {
	const qs = reason ? `?reason=${encodeURIComponent(reason)}` : "";
	return backendFetch<{ status: string }>(
		`/api/app/admin/bm-servers/${encodeURIComponent(bmId)}${qs}`,
		{ method: "DELETE" },
		{ requireSession: true },
	);
}

export type BulkBmServerAction = "set_high_freq" | "set_standard" | "delete";

export type BulkBmServerResult = {
	action: BulkBmServerAction;
	succeeded: number;
	failed: number;
	results: Array<{ bmId: string; ok: boolean; error?: string }>;
};

export async function bulkAdminBmServers(
	action: BulkBmServerAction,
	bmIds: string[],
	reason: string,
): Promise<BulkBmServerResult> {
	return backendFetch<BulkBmServerResult>(
		"/api/app/admin/bm-servers/bulk",
		{ method: "POST", body: JSON.stringify({ action, bmIds, reason }) },
		{ requireSession: true },
	);
}

export type CreateAdminRoleInput = {
	key: string;
	name: string;
	description: string;
	permissions: string[];
};

export async function createAdminRole(input: CreateAdminRoleInput, reason: string) {
	return postAdminAction<{ id: string; key: string }>("roles.create", input, reason);
}

export async function updateAdminRole(
	id: string,
	patch: { name: string; description: string },
	reason: string,
) {
	return postAdminAction<{ id: string }>("roles.update", { id, ...patch }, reason);
}

export async function setAdminRolePermissions(id: string, permissions: string[], reason: string) {
	return postAdminAction<{ id: string; count: number }>(
		"roles.permissions.set",
		{ id, permissions },
		reason,
	);
}

export async function deleteAdminRole(id: string, reason: string) {
	return postAdminAction<{ id: string }>("roles.delete", { id }, reason);
}

// --- User actions (single) ---

export type AssignRoleResult = { userId: string; roleKey: string };

export async function grantAdminUserRole(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<AssignRoleResult> {
	return postAdminAction<AssignRoleResult>("users.roles.grant", { userId, roleKey }, reason);
}

export async function revokeAdminUserRole(
	userId: string,
	roleKey: string,
	reason: string,
): Promise<AssignRoleResult> {
	return postAdminAction<AssignRoleResult>("users.roles.revoke", { userId, roleKey }, reason);
}

export type EditUserPatch = {
	displayName?: string;
	avatarUrl?: string;
};

export async function editAdminUser(
	userId: string,
	patch: EditUserPatch,
	reason: string,
) {
	return postAdminAction<{ userId: string }>("users.edit", { userId, ...patch }, reason);
}

export async function suspendAdminUser(userId: string, reason: string) {
	return postAdminAction<{ userId: string }>("users.suspend", { userId }, reason);
}

export async function unsuspendAdminUser(userId: string, reason: string) {
	return postAdminAction<{ userId: string }>("users.unsuspend", { userId }, reason);
}

export async function deleteAdminUser(
	userId: string,
	reason: string,
	typedConfirmation: string,
) {
	return postAdminAction<{ userId: string }>(
		"users.delete",
		{ userId, typedConfirmation },
		reason,
	);
}

// --- Bulk action ---

export type BulkUserSubAction = "suspend" | "unsuspend" | "roles.grant" | "roles.revoke";

export type BulkUserResult = {
	action: BulkUserSubAction;
	succeeded: number;
	failed: number;
	results: Array<{ userId: string; ok: boolean; error?: string }>;
};

export async function bulkAdminUserAction(
	action: BulkUserSubAction,
	userIds: string[],
	params: Record<string, unknown>,
	reason: string,
): Promise<BulkUserResult> {
	return postAdminAction<BulkUserResult>(
		"users.bulk",
		{ action, userIds, params },
		reason,
	);
}

