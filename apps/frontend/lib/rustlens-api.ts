import "server-only";

import { redirect } from "next/navigation";
import { clearSession, getSessionToken } from "@/lib/session";

const SESSION_INVALID_ERRORS = new Set([
	"session_required",
	"session_revoked",
	"invalid_session",
]);

export type AppUser = {
	id: string;
	displayName: string;
	avatarUrl: string;
	roles: string[];
	createdAt: string;
	updatedAt: string;
	lastLoginAt: string;
	sessionEpoch?: number;
};

export type ConnectedAccount = {
	provider: string;
	providerAccountId: string;
	username: string;
	displayName: string;
	avatarUrl: string;
	profileEmail: string;
	createdAt: string;
	updatedAt: string;
};

export type UserEmail = {
	email: string;
	isPrimary: boolean;
	isVerified: boolean;
	createdAt: string;
	updatedAt: string;
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
	discordAccount: ConnectedAccount | null;
	primaryEmail: UserEmail | null;
	emails: UserEmail[];
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

export type AdminUserItem = {
	user: AppUser;
	discordId: string;
	discordUsername: string;
	primaryEmail: string;
	emailVerified: boolean;
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
	emailVerified?: "any" | "yes" | "no";
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
	activeMembers: number;
	linkedMembers: number;
	trackedServers: number;
	adminRoleId: string;
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
};

export type AuditEventItem = {
	id: string;
	actorUserId: string;
	targetUserId: string;
	eventType: string;
	payload: Record<string, unknown>;
	createdAt: string;
};

export type AdminUserEmail = {
	email: string;
	isPrimary: boolean;
	isVerified: boolean;
};

export type AdminUserConnection = {
	provider: string;
	providerAccountId: string;
	username: string;
	connectedAt: string;
};

export type AdminUserGuild = {
	guildId: string;
	nick?: string;
};

export type AdminUserAuditItem = AuditEventItem;

export type AdminUserDetail = {
	user: AppUser;
	primaryEmail: string;
	emails: AdminUserEmail[];
	connections: AdminUserConnection[];
	guilds: AdminUserGuild[];
	adminRoles: string[];
	suspendedAt?: string;
	suspendReason?: string;
	deletedAt?: string;
	recentAudit: AdminUserAuditItem[];
};

export type TokenResponse = {
	status: string;
	linked?: boolean;
	debugToken?: string;
};

/**
 * Envelope returned by /api/app/admin/actions/{type} on success. `result` is
 * whatever the action's Execute() put in ActionResult.Payload.
 */
export type AdminActionResponse<T = unknown> = {
	ok: true;
	result: T;
};

/** Output of users.password.forceReset — contains the one-time reset URL. */
export type ForceResetOutput = {
	userId: string;
	tokenUrl: string;
};

/** Output of users.email.resendVerification — contains the one-time verification URL. */
export type ResendVerificationOutput = {
	userId: string;
	email: string;
	tokenUrl: string;
};

/** Output of users.invite — newly created user id and one-time sign-in URL. */
export type InviteUserOutput = {
	userId: string;
	tokenUrl: string;
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
			const payload = (await response.json()) as { error?: string };
			if (payload.error) {
				errorCode = payload.error;
				message = payload.error;
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
			redirect("/login?error=session");
		}
		throw new Error(message);
	}

	return (await response.json()) as T;
}

export function completeDiscordAuth(payload: {
	discordId: string;
	username: string;
	displayName: string;
	avatarUrl: string;
	email: string;
	emailVerified: boolean;
}) {
	return backendFetch<CurrentUser>("/api/app/auth/discord/complete", {
		method: "POST",
		body: JSON.stringify(payload),
	});
}

export function registerWithPassword(payload: {
	email: string;
	password: string;
	displayName: string;
}) {
	return backendFetch<CurrentUser>("/api/app/auth/register", {
		method: "POST",
		body: JSON.stringify(payload),
	});
}

export function loginWithPassword(payload: { email: string; password: string }) {
	return backendFetch<CurrentUser>("/api/app/auth/login", {
		method: "POST",
		body: JSON.stringify(payload),
	});
}

export function requestPasswordReset(email: string) {
	return backendFetch<TokenResponse>("/api/app/auth/password/reset/request", {
		method: "POST",
		body: JSON.stringify({ email }),
	});
}

export function consumePasswordReset(payload: {
	token: string;
	newPassword: string;
}) {
	return backendFetch<CurrentUser>("/api/app/auth/password/reset/consume", {
		method: "POST",
		body: JSON.stringify(payload),
	});
}

export function requestEmailVerification(sessionToken?: string) {
	return backendFetch<TokenResponse>(
		"/api/app/auth/email/verify/request",
		{ method: "POST" },
		{ requireSession: true, sessionToken },
	);
}

export function consumeEmailVerification(token: string) {
	return backendFetch<CurrentUser>("/api/app/auth/email/verify/consume", {
		method: "POST",
		body: JSON.stringify({ token }),
	});
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

export async function getAdminUsers(params: AdminUsersQuery = {}): Promise<AdminUsersPage> {
	const sp = new URLSearchParams();
	if (params.q) sp.set("q", params.q);
	for (const r of params.role ?? []) sp.append("role", r);
	if (params.status) sp.set("status", params.status);
	if (params.discord) sp.set("discord", params.discord);
	if (params.steam) sp.set("steam", params.steam);
	if (params.emailVerified) sp.set("emailVerified", params.emailVerified);
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

export async function getAdminGuilds() {
	const payload = await backendFetch<{ items: AdminGuildItem[] }>(
		"/api/app/admin/guilds",
		undefined,
		{ requireSession: true },
	);
	return payload.items;
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
