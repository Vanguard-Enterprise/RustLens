import type { NextConfig } from "next";

// Note: Content-Security-Policy is now set per-request in middleware.ts with
// a fresh nonce + 'strict-dynamic', not as a static header here. The static
// headers below are response-scope and don't need per-request data.

const nextConfig: NextConfig = {
	poweredByHeader: false,
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
