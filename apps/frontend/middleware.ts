import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

const isDev = process.env.NODE_ENV !== "production";

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
