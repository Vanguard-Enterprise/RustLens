import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { routeHitCounter, ssrDurationHist } from "@/lib/observability/metrics";

const isDev = process.env.NODE_ENV !== "production";

// Bounded-cardinality route labels for OTel attributes. Dynamic path segments
// are collapsed to their Next.js route-pattern equivalent so each app route
// produces exactly one metric series instead of one per ID.
const DYNAMIC_ROUTE_PATTERNS: ReadonlyArray<readonly [RegExp, string]> = [
	[/^\/dashboard\/guilds\/[^/]+\/?$/, "/dashboard/guilds/:guildId"],
	[/^\/player\/[^/]+\/?$/, "/player/:steamId"],
	[/^\/admin\/users\/[^/]+\/?$/, "/admin/users/:userId"],
	[/^\/admin\/api-keys\/[^/]+\/revoke\/?$/, "/admin/api-keys/:id/revoke"],
	[/^\/admin\/api-keys\/[^/]+\/rotate\/?$/, "/admin/api-keys/:id/rotate"],
];

function normalizeRoute(pathname: string): string {
	for (const [re, route] of DYNAMIC_ROUTE_PATTERNS) {
		if (re.test(pathname)) return route;
	}
	return pathname;
}

/**
 * Per-request CSP middleware.
 *
 * The previous static CSP in next.config.ts shipped script-src 'self'
 * 'unsafe-inline' — necessary because Next emits inline hydration <script>
 * blocks. With 'unsafe-inline' present, the CSP was almost cosmetic against
 * a real XSS.
 *
 * Approach here is the canonical Next.js nonce pattern:
 *  1. Mint a fresh nonce per request.
 *  2. Set Content-Security-Policy with 'nonce-{nonce}' and 'strict-dynamic'
 *     so the only inline scripts that execute are ones carrying that nonce.
 *     'unsafe-inline' is left as a hint for non-CSP3 browsers but is ignored
 *     in CSP3-supporting browsers when 'strict-dynamic' is present.
 *  3. Forward the nonce as `x-nonce` request header so the root layout (and
 *     Next's own framework code) can attach it to inline <script> tags.
 *
 * Trade-off: every matched route is now dynamic. Acceptable here because the
 * dashboard/admin tree was already dynamic (cookies + headers reads), and the
 * marketing surface is small.
 */
export function middleware(request: NextRequest) {
	const start = performance.now();
	const route = normalizeRoute(request.nextUrl.pathname);
	routeHitCounter.add(1, { route });

	const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
	const connectSrc = isDev ? "connect-src 'self' ws: wss:" : "connect-src 'self'";
	const scriptSrc = isDev
		? `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' 'unsafe-eval' 'unsafe-inline'`
		: `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' 'unsafe-inline'`;

	const csp = [
		"default-src 'self'",
		"base-uri 'self'",
		"font-src 'self' https://fonts.gstatic.com data:",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"frame-src 'none'",
		"img-src 'self' data: https:",
		"manifest-src 'self'",
		"media-src 'self'",
		"object-src 'none'",
		"worker-src 'self' blob:",
		scriptSrc,
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		connectSrc,
		"upgrade-insecure-requests",
	].join("; ");

	const requestHeaders = new Headers(request.headers);
	requestHeaders.set("x-nonce", nonce);

	const response = NextResponse.next({ request: { headers: requestHeaders } });
	response.headers.set("Content-Security-Policy", csp);

	// Records middleware processing time per route. This is the only per-route
	// timing signal we can capture from a Next.js middleware — true SSR
	// duration (server-component rendering, streaming) happens downstream of
	// `NextResponse.next()` and is covered by the auto-instrumented
	// `http_server_request_duration` metric from @vercel/otel.
	ssrDurationHist.record((performance.now() - start) / 1000, { route });

	return response;
}

export const config = {
	matcher: [
		// Skip Next internals, static assets, and the rewritten backend API.
		// Apply the CSP to every other route so the rendered HTML carries it.
		{
			source: "/((?!api|_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
		},
	],
};
