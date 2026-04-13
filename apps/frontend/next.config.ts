import type { NextConfig } from "next";

const contentSecurityPolicy = [
	"default-src 'self'",
	"base-uri 'self'",
	"font-src 'self' https://fonts.gstatic.com data:",
	"form-action 'self'",
	"frame-ancestors 'none'",
	"img-src 'self' data: https:",
	"object-src 'none'",
	"script-src 'self'",
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
	"connect-src 'self'",
].join("; ");

const nextConfig: NextConfig = {
	poweredByHeader: false,
	async headers() {
		return [
			{
				source: "/:path*",
				headers: [
					{
						key: "Content-Security-Policy",
						value: contentSecurityPolicy,
					},
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
				],
			},
		];
	},
};

export default nextConfig;
