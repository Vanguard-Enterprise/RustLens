import { createHash, randomBytes, randomUUID } from "node:crypto";
import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { getAppOrigin } from "@/lib/app-origin";

function createPkceVerifier() {
	return randomBytes(32).toString("base64url");
}

function createPkceChallenge(verifier: string) {
	return createHash("sha256").update(verifier).digest("base64url");
}

export async function GET(request: Request) {
	const clientId = process.env.DISCORD_CLIENT_ID;
	if (!clientId) {
		return NextResponse.redirect(new URL("/login?error=discord-config", request.url));
	}

	const state = randomUUID();
	const codeVerifier = createPkceVerifier();
	const callbackUrl = `${getAppOrigin()}/auth/callback/discord`;
	const authorizeUrl = new URL("https://discord.com/oauth2/authorize");
	authorizeUrl.searchParams.set("client_id", clientId);
	authorizeUrl.searchParams.set("response_type", "code");
	authorizeUrl.searchParams.set("redirect_uri", callbackUrl);
	authorizeUrl.searchParams.set("scope", "identify email");
	authorizeUrl.searchParams.set("prompt", "consent");
	authorizeUrl.searchParams.set("state", state);
	authorizeUrl.searchParams.set("code_challenge", createPkceChallenge(codeVerifier));
	authorizeUrl.searchParams.set("code_challenge_method", "S256");

	const cookieStore = await cookies();
	cookieStore.set("rustlens_discord_oauth_state", state, {
		httpOnly: true,
		sameSite: "lax",
		secure: process.env.NODE_ENV === "production",
		path: "/",
		maxAge: 60 * 10,
	});
	cookieStore.set("rustlens_discord_oauth_verifier", codeVerifier, {
		httpOnly: true,
		sameSite: "lax",
		secure: process.env.NODE_ENV === "production",
		path: "/",
		maxAge: 60 * 10,
	});

	return NextResponse.redirect(authorizeUrl);
}
