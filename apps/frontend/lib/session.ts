import "server-only";

import { createHmac, timingSafeEqual } from "node:crypto";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import type { CurrentUser } from "@/lib/rustlens-api";

export type SessionUser = {
	userId: string;
	displayName: string;
	avatarUrl: string;
	roles: string[];
	/**
	 * Admin role keys from admin_user_roles. Non-admin users have an empty
	 * array. Used by requireAdminSession to gate /admin access.
	 */
	adminRoles?: string[];
	permissions?: string[];
	discordId?: string;
	epoch: number;
	expiresAt: number;
};

export const SESSION_COOKIE = "rustlens_session";
const SESSION_TTL_SECONDS = 60 * 60 * 24 * 14;

function getSessionSecret() {
	const secret = process.env.APP_SESSION_SECRET;
	if (!secret) {
		throw new Error("APP_SESSION_SECRET is required");
	}
	return secret;
}

function base64UrlEncode(input: string) {
	return Buffer.from(input, "utf8").toString("base64url");
}

function base64UrlDecode(input: string) {
	return Buffer.from(input, "base64url").toString("utf8");
}

function signPayload(encodedPayload: string) {
	return createHmac("sha256", getSessionSecret())
		.update(encodedPayload)
		.digest("base64url");
}

function serializeSession(session: SessionUser) {
	const encodedPayload = base64UrlEncode(JSON.stringify(session));
	return `${encodedPayload}.${signPayload(encodedPayload)}`;
}

export function serializeSessionToken(session: SessionUser) {
	return serializeSession(session);
}

function parseSession(token: string): SessionUser | null {
	const [encodedPayload, signature] = token.split(".");
	if (!encodedPayload || !signature) {
		return null;
	}

	const expectedSignature = signPayload(encodedPayload);
	const actualBuffer = Buffer.from(signature);
	const expectedBuffer = Buffer.from(expectedSignature);
	if (
		actualBuffer.length !== expectedBuffer.length ||
		!timingSafeEqual(actualBuffer, expectedBuffer)
	) {
		return null;
	}

	try {
		const payload = JSON.parse(base64UrlDecode(encodedPayload)) as SessionUser;
		if (!payload.userId || payload.expiresAt < Date.now()) {
			return null;
		}
		return payload;
	} catch {
		return null;
	}
}

export function toSessionUser(currentUser: CurrentUser): SessionUser {
	return {
		userId: currentUser.user.id,
		displayName: currentUser.user.displayName,
		avatarUrl: currentUser.user.avatarUrl,
		roles: currentUser.user.roles,
		adminRoles: currentUser.adminRoles ?? [],
		permissions: currentUser.permissions ?? [],
		discordId: currentUser.user.discordId,
		epoch: currentUser.user.sessionEpoch ?? 0,
		expiresAt: Date.now() + SESSION_TTL_SECONDS * 1000,
	};
}

export async function getSession() {
	const cookieStore = await cookies();
	const token = cookieStore.get(SESSION_COOKIE)?.value;
	if (!token) {
		return null;
	}
	return parseSession(token);
}

export async function getSessionToken() {
	const cookieStore = await cookies();
	return cookieStore.get(SESSION_COOKIE)?.value ?? null;
}

export async function requireSession() {
	const session = await getSession();
	if (!session) {
		redirect("/login");
	}
	return session;
}

/**
 * @deprecated Kept as an alias during migration to RBAC. New code should use
 * `requireAdminSession` (any admin role) plus route-level `requirePermission`.
 * This now delegates to `requireAdminSession` so users granted admin roles via
 * `admin_user_roles` are recognised — previously this only matched the legacy
 * "internal_admin" string in `app_users.roles`, which the new RBAC system no
 * longer populates.
 */
export async function requireInternalAdminSession() {
	return requireAdminSession();
}

export async function requireAdminSession() {
	const session = await getSession();
	if (!session) redirect("/login");

	// Always resolve admin access from live backend state so role grants/revokes
	// take effect without waiting for the next login. Dynamic import avoids a
	// static circular dependency with rustlens-api (which imports from here).
	const { getCurrentUser } = await import("@/lib/rustlens-api");
	let liveAdminRoles: string[] = [];
	let livePermissions: string[] = [];
	let liveRoles: string[] = session.roles ?? [];
	try {
		const current = await getCurrentUser();
		liveAdminRoles = current.adminRoles ?? [];
		livePermissions = current.permissions ?? [];
		liveRoles = current.user.roles ?? [];
	} catch {
		redirect("/login");
	}

	const legacyInternalAdmin = liveRoles.includes("internal_admin");
	if (liveAdminRoles.length === 0 && livePermissions.length === 0 && !legacyInternalAdmin) {
		redirect("/dashboard");
	}

	return {
		...session,
		roles: liveRoles,
		adminRoles: liveAdminRoles,
		permissions: livePermissions,
	};
}

export async function requirePermission(key: string) {
	const session = await requireAdminSession();
	const perms = session.permissions ?? [];
	if (!perms.includes(key)) redirect("/admin");
	return session;
}

export async function setSession(session: SessionUser) {
	const cookieStore = await cookies();
	cookieStore.set(SESSION_COOKIE, serializeSession(session), {
		httpOnly: true,
		sameSite: "lax",
		// Always Secure on the server side. Next dev over http://localhost
		// still accepts Secure cookies (Chrome treats localhost as secure),
		// so gating this on NODE_ENV opened a window where a staging build
		// served behind HTTPS but running with NODE_ENV !== "production"
		// would ship the session cookie without the Secure flag.
		secure: true,
		path: "/",
		maxAge: SESSION_TTL_SECONDS,
	});
}

export async function clearSession() {
	const cookieStore = await cookies();
	cookieStore.delete(SESSION_COOKIE);
}
