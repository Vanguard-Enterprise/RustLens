import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { UnlinkConnectionButton } from "../unlink-connection-button";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ refresh: vi.fn() }),
}));

describe("UnlinkConnectionButton", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("shows confirm dialog then sends correct payload", async () => {
		const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true, result: {} }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		render(
			<UnlinkConnectionButton
				userId="u1"
				provider="discord"
				providerAccountId="dc_1"
				csrfToken="t"
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /^unlink$/i }));
		const confirm = await screen.findByRole("button", { name: /^unlink/i });
		// There are two "Unlink" buttons (trigger + confirm) — confirm uses "Unlink…" loading
		// state, so query by its initial text "Unlink" inside the dialog footer.
		expect(confirm).toBeInTheDocument();
		fireEvent.change(screen.getByLabelText(/type user id to confirm/i), {
			target: { value: "u1" },
		});
		fireEvent.change(screen.getByLabelText(/^reason$/i), {
			target: { value: "cleanup" },
		});
		const confirmButtons = screen.getAllByRole("button", { name: /^unlink$/i });
		const confirmBtn = confirmButtons[confirmButtons.length - 1];
		fireEvent.click(confirmBtn);
		await waitFor(() => {
			expect(spy).toHaveBeenCalledWith(
				"/api/app/admin/actions/users.connections.unlink",
				expect.objectContaining({
					body: JSON.stringify({
						params: {
							userId: "u1",
							provider: "discord",
							providerAccountId: "dc_1",
						},
						reason: "cleanup",
						typedConfirmation: "u1",
					}),
				}),
			);
		});
	});

	it("blocks confirm until typed id matches", async () => {
		render(
			<UnlinkConnectionButton
				userId="u1"
				provider="steam"
				providerAccountId="st_1"
				csrfToken="t"
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /^unlink$/i }));
		await screen.findByLabelText(/type user id to confirm/i);
		const confirmButtons = screen.getAllByRole("button", { name: /^unlink$/i });
		const confirmBtn = confirmButtons[confirmButtons.length - 1];
		expect(confirmBtn).toBeDisabled();
	});
});
