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
	permissions?: string[];
	discordId?: string;
	email?: string;
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
		discordId: currentUser.discordAccount?.providerAccountId,
		email: currentUser.primaryEmail?.email,
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
 * @deprecated use requireAdminSession + requirePermission; this will be removed once all admin pages migrate
 */
export async function requireInternalAdminSession() {
	const session = await requireSession();
	if (!session.roles.includes("internal_admin")) {
		redirect("/dashboard");
	}
	return session;
}

export async function requireAdminSession() {
	const session = await getSession();
	if (!session) redirect("/login");
	const isAdmin =
		(session.roles ?? []).length > 0 ||
		(session.permissions ?? []).length > 0;
	if (!isAdmin) redirect("/dashboard");
	return session;
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
		secure: process.env.NODE_ENV === "production",
		path: "/",
		maxAge: SESSION_TTL_SECONDS,
	});
}

export async function clearSession() {
	const cookieStore = await cookies();
	cookieStore.delete(SESSION_COOKIE);
}
