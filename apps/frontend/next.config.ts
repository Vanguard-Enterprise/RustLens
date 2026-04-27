import type { NextConfig } from "next";

const isDev = process.env.NODE_ENV !== "production";

// 'unsafe-inline' is required: Next.js emits inline hydration <script> blocks.
// The strict alternative is per-request nonces, which would force every page
// (incl. the static landing) into dynamic rendering.
const scriptSrc = isDev
	? "script-src 'self' 'unsafe-eval' 'unsafe-inline'"
	: "script-src 'self' 'unsafe-inline'";

const connectSrc = isDev
	? "connect-src 'self' ws: wss:"
	: "connect-src 'self'";

const contentSecurityPolicy = [
	"default-src 'self'",
	"base-uri 'self'",
	"font-src 'self' https://fonts.gstatic.com data:",
	"form-action 'self'",
	"frame-ancestors 'none'",
	"img-src 'self' data: https:",
	"object-src 'none'",
	scriptSrc,
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
	connectSrc,
].join("; ");

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
