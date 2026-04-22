import { beforeEach, describe, expect, it, vi } from "vitest";

const cookiesMock = vi.fn();

vi.mock("server-only", () => ({}));

vi.mock("next/headers", () => ({
	cookies: cookiesMock,
}));

vi.mock("@/lib/app-origin", () => ({
	getAppOrigin: vi.fn(() => "https://rustlens.test"),
}));

describe("csrf", () => {
	beforeEach(() => {
		vi.resetModules();
		cookiesMock.mockReset();
		vi.unstubAllEnvs();
		vi.stubEnv("APP_SESSION_SECRET", "test-session-secret");
	});

	it("creates a signed CSRF token without mutating cookies", async () => {
		const { getCsrfToken } = await import("./csrf");
		const token = await getCsrfToken();

		expect(token).toBeTruthy();
		expect(token.split(".")).toHaveLength(3);
		expect(cookiesMock).not.toHaveBeenCalled();
	});

	it("accepts a valid token plus same-origin Origin header", async () => {
		const { getCsrfToken, validateCsrfRequest } = await import("./csrf");
		const token = await getCsrfToken();
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://rustlens.test",
			},
		});

		await expect(validateCsrfRequest(request, token)).resolves.toBe(true);
	});

	it("rejects tampered tokens", async () => {
		const { getCsrfToken, validateCsrfRequest } = await import("./csrf");
		const token = await getCsrfToken();
		const [issuedAt, nonce] = token.split(".");
		const tamperedToken = `${issuedAt}.${nonce}.different-signature`;
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://rustlens.test",
			},
		});

		await expect(validateCsrfRequest(request, tamperedToken)).resolves.toBe(false);
	});

	it("rejects cross-site requests even with a valid token", async () => {
		const { getCsrfToken, validateCsrfRequest } = await import("./csrf");
		const token = await getCsrfToken();
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://attacker.test",
			},
		});

		await expect(validateCsrfRequest(request, token)).resolves.toBe(false);
	});

	it("rejects expired tokens", async () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-04-18T12:00:00Z"));

		const { getCsrfToken, validateCsrfRequest } = await import("./csrf");
		const token = await getCsrfToken();

		vi.setSystemTime(new Date("2026-04-18T21:00:01Z"));

		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://rustlens.test",
			},
		});

		await expect(validateCsrfRequest(request, token)).resolves.toBe(false);
		vi.useRealTimers();
	});
});
