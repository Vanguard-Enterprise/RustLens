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
	});

	it("creates and stores a CSRF token when none exists", async () => {
		const set = vi.fn();
		cookiesMock.mockResolvedValue({
			get: vi.fn(() => undefined),
			set,
		});

		const { getCsrfToken } = await import("./csrf");
		const token = await getCsrfToken();

		expect(token).toBeTruthy();
		expect(set).toHaveBeenCalledOnce();
		expect(set).toHaveBeenCalledWith(
			"rustlens_csrf",
			token,
			expect.objectContaining({
				httpOnly: false,
				sameSite: "lax",
				path: "/",
			}),
		);
	});

	it("accepts matching token plus same-origin Origin header", async () => {
		cookiesMock.mockResolvedValue({
			get: vi.fn((name: string) =>
				name === "rustlens_csrf" ? { value: "known-token" } : undefined,
			),
		});

		const { validateCsrfRequest } = await import("./csrf");
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://rustlens.test",
			},
		});

		await expect(validateCsrfRequest(request, "known-token")).resolves.toBe(true);
	});

	it("rejects mismatched tokens", async () => {
		cookiesMock.mockResolvedValue({
			get: vi.fn(() => ({ value: "known-token" })),
		});

		const { validateCsrfRequest } = await import("./csrf");
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://rustlens.test",
			},
		});

		await expect(validateCsrfRequest(request, "different-token")).resolves.toBe(false);
	});

	it("rejects cross-site requests even with a matching token", async () => {
		cookiesMock.mockResolvedValue({
			get: vi.fn(() => ({ value: "known-token" })),
		});

		const { validateCsrfRequest } = await import("./csrf");
		const request = new Request("https://rustlens.test/auth/login", {
			method: "POST",
			headers: {
				origin: "https://attacker.test",
			},
		});

		await expect(validateCsrfRequest(request, "known-token")).resolves.toBe(false);
	});
});
