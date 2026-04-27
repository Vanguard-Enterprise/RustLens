import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

const envSnapshot = { ...process.env };

describe("rate-limit", () => {
	beforeEach(() => {
		vi.resetModules();
		process.env = { ...envSnapshot, NODE_ENV: "test" };
	});

	afterEach(() => {
		process.env = { ...envSnapshot };
	});

	it("ignores generic forwarded headers unless an explicit trusted IP header is configured", async () => {
		process.env.TRUST_PROXY_HEADERS = "true";

		const { getClientAddress } = await import("./rate-limit");
		const request = new Request("https://rustlens.test/login", {
			headers: {
				"x-forwarded-for": "198.51.100.24",
				"x-real-ip": "198.51.100.24",
			},
		});

		expect(getClientAddress(request)).toBe("unknown");
	});

	it("uses the configured trusted proxy IP header when enabled", async () => {
		process.env.TRUST_PROXY_HEADERS = "true";
		process.env.TRUST_PROXY_IP_HEADER = "x-rustlens-client-ip";

		const { getClientAddress } = await import("./rate-limit");
		const request = new Request("https://rustlens.test/login", {
			headers: {
				"x-rustlens-client-ip": "198.51.100.24",
			},
		});

		expect(getClientAddress(request)).toBe("198.51.100.24");
	});

	it("falls back to in-memory limiting outside production when redis is not configured", async () => {
		const { consumeRateLimit } = await import("./rate-limit");

		await expect(
			consumeRateLimit("auth:test", { max: 2, windowMs: 60_000 }),
		).resolves.toEqual({ allowed: true, retryAfterSeconds: 0 });
		await expect(
			consumeRateLimit("auth:test", { max: 2, windowMs: 60_000 }),
		).resolves.toEqual({ allowed: true, retryAfterSeconds: 0 });

		const blocked = await consumeRateLimit("auth:test", {
			max: 2,
			windowMs: 60_000,
		});
		expect(blocked.allowed).toBe(false);
		expect(blocked.retryAfterSeconds).toBeGreaterThan(0);
	});

	it("requires redis-backed rate limiting in production", async () => {
		vi.stubEnv("NODE_ENV", "production");

		const { consumeRateLimit } = await import("./rate-limit");

		let error: Error | undefined;
		try {
			await consumeRateLimit("auth:test", { max: 1, windowMs: 60_000 });
		} catch (err) {
			error = err as Error;
			"RATE_LIMIT_REDIS_URL or REDIS_URL must be configured in production for shared auth rate limiting",
		);
	});
});
