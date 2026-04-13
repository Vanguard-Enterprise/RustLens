import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { getAppOrigin } from "@/lib/app-origin";
import { completeDiscordAuth } from "@/lib/rustlens-api";
import { setSession, toSessionUser } from "@/lib/session";

function buildDiscordAvatarUrl(
	discordId: string,
	avatarHash: string | null,
) {
	if (!avatarHash) {
		return "";
	}
	return `https://cdn.discordapp.com/avatars/${discordId}/${avatarHash}.png?size=256`;
}

export async function GET(request: Request) {
	const url = new URL(request.url);
	const code = url.searchParams.get("code");
	const state = url.searchParams.get("state");
	const cookieStore = await cookies();
	const storedState = cookieStore.get("rustlens_discord_oauth_state")?.value;
	const codeVerifier = cookieStore.get("rustlens_discord_oauth_verifier")?.value;
	cookieStore.delete("rustlens_discord_oauth_state");
	cookieStore.delete("rustlens_discord_oauth_verifier");

	if (!code || !state || !storedState || !codeVerifier || state !== storedState) {
		return NextResponse.redirect(new URL("/login?error=discord-state", request.url));
	}

	const clientId = process.env.DISCORD_CLIENT_ID;
	const clientSecret = process.env.DISCORD_CLIENT_SECRET;
	if (!clientId || !clientSecret) {
		return NextResponse.redirect(new URL("/login?error=discord-config", request.url));
	}

	const callbackUrl = `${getAppOrigin()}/auth/callback/discord`;
	const tokenResponse = await fetch("https://discord.com/api/oauth2/token", {
		method: "POST",
		headers: {
			"Content-Type": "application/x-www-form-urlencoded",
		},
		body: new URLSearchParams({
			client_id: clientId,
			client_secret: clientSecret,
			grant_type: "authorization_code",
			code,
			code_verifier: codeVerifier,
			redirect_uri: callbackUrl,
		}),
		cache: "no-store",
		signal: AbortSignal.timeout(10_000),
	});

	if (!tokenResponse.ok) {
		return NextResponse.redirect(new URL("/login?error=discord-token", request.url));
	}

	const tokenPayload = (await tokenResponse.json()) as {
		access_token: string;
	};
	const profileResponse = await fetch("https://discord.com/api/users/@me", {
		headers: {
			Authorization: `Bearer ${tokenPayload.access_token}`,
		},
		cache: "no-store",
		signal: AbortSignal.timeout(10_000),
	});

	if (!profileResponse.ok) {
		return NextResponse.redirect(new URL("/login?error=discord-profile", request.url));
	}

	const profile = (await profileResponse.json()) as {
		id: string;
		username: string;
		global_name: string | null;
		avatar: string | null;
		email?: string;
		verified?: boolean;
	};

	try {
		const currentUser = await completeDiscordAuth({
			discordId: profile.id,
			username: profile.username,
			displayName: profile.global_name ?? profile.username,
			avatarUrl: buildDiscordAvatarUrl(profile.id, profile.avatar),
			email: profile.email ?? "",
			emailVerified: profile.verified ?? false,
		});

		await setSession(toSessionUser(currentUser));
		return NextResponse.redirect(new URL("/dashboard", request.url));
	} catch {
		return NextResponse.redirect(new URL("/login?error=discord-auth", request.url));
	}
}
