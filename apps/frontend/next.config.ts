import type { NextConfig } from "next";

// Note: Content-Security-Policy is now set per-request in middleware.ts with
// a fresh nonce + 'strict-dynamic', not as a static header here. The static
// headers below are response-scope and don't need per-request data.

const nextConfig: NextConfig = {
	poweredByHeader: false,
	// Stable per-build identifier emitted on assets as ?dpl= and on client
	// requests as x-deployment-id. On mismatch, Next triggers a hard reload
	// instead of failing with "Failed to find Server Action" against an older
	// HTML/JS cached by the browser.
	deploymentId: process.env.DEPLOYMENT_VERSION,
	async rewrites() {
		return [
			{
				source: "/api/:path*",
				destination: "http://localhost:8080/api/:path*",
			},
		];
	},
	async redirects() {
		return [
			{ source: "/admin/members", destination: "/admin/users", permanent: true },
			{ source: "/admin/members/:path*", destination: "/admin/users/:path*", permanent: true },
			{ source: "/discord", destination: "https://discord.gg/HrREy36F7m", permanent: false },
			// /install-bot is handled by app/install-bot/route.ts so DISCORD_CLIENT_ID
			// is read at request time. A config redirect would bake the value into the
			// build manifest, freezing it to whatever was set during `next build`.
		];
	},
	async headers() {
		return [
			{
				source: "/:path*",
				headers: [
					{
						key: "Referrer-Policy",
						value: "strict-origin-when-cross-origin",
					},
					{
						key: "X-Content-Type-Options",
						value: "nosniff",
					},
					{
						key: "X-Frame-Options",
						value: "DENY",
					},
					{
						key: "Permissions-Policy",
						value: "camera=(), microphone=(), geolocation=()",
					},
					{
						key: "Strict-Transport-Security",
						value: "max-age=63072000; includeSubDomains; preload",
					},
				],
			},
		];
	},
};

export default nextConfig;
