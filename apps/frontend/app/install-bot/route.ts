import { NextResponse } from "next/server";

// /install-bot sends users to Discord's bot authorize page. This is a route
// handler rather than a next.config redirect because config redirects bake
// their destination into the build manifest at `next build` time. That froze
// client_id to whatever DISCORD_CLIENT_ID was during the image build (unset),
// producing client_id=undefined in prod. Reading the env here runs at request
// time, so the Vault-injected runtime value is used.
export const dynamic = "force-dynamic";

export function GET(request: Request) {
	const clientId = process.env.DISCORD_CLIENT_ID;
	if (!clientId) {
		return NextResponse.redirect(
			new URL("/login?error=discord-config", request.url),
		);
	}

	return NextResponse.redirect(
		`https://discord.com/oauth2/authorize?client_id=${clientId}&permissions=8&scope=bot%20applications.commands`,
		307,
	);
}
