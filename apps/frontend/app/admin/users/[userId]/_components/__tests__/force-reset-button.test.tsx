import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { ForceResetButton } from "../force-reset-button";

describe("ForceResetButton", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		Object.assign(navigator, {
			clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
		});
	});

	it("shows modal with returned URL after success", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(
				JSON.stringify({
					ok: true,
					result: { userId: "u1", tokenUrl: "https://x/reset?token=abc" },
				}),
				{ status: 200, headers: { "Content-Type": "application/json" } },
			),
		);
		render(
			<ForceResetButton userId="u1" csrfToken="t" selfTarget={false} />,
		);
		fireEvent.change(
			screen.getByPlaceholderText(/why are you forcing/i),
			{ target: { value: "lost mfa" } },
		);
		fireEvent.click(
			screen.getByRole("button", { name: /force password reset/i }),
		);
		await waitFor(() => {
			expect(screen.getByLabelText(/one-time url/i)).toHaveValue(
				"https://x/reset?token=abc",
			);
		});
	});

	it("is disabled on self-target", () => {
		render(<ForceResetButton userId="u1" csrfToken="t" selfTarget />);
		expect(
			screen.getByRole("button", { name: /force password reset/i }),
		).toBeDisabled();
	});
});
