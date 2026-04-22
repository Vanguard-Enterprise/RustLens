import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { InviteUserDialog } from "../invite-user-dialog";

describe("InviteUserDialog", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		Object.assign(navigator, {
			clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
		});
	});

	async function openDialog() {
		render(<InviteUserDialog csrfToken="t" />);
		fireEvent.click(screen.getByRole("button", { name: /invite user/i }));
		await screen.findByLabelText(/^email$/i);
	}

	it("requires a valid email before enabling submit", async () => {
		await openDialog();
		const submit = screen.getByRole("button", { name: /send invite/i });
		expect(submit).toBeDisabled();
		fireEvent.change(screen.getByLabelText(/^email$/i), {
			target: { value: "not-an-email" },
		});
		fireEvent.change(screen.getByLabelText(/reason/i), {
			target: { value: "because" },
		});
		expect(submit).toBeDisabled();
	});

	it("shows token URL modal on success", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(
				JSON.stringify({
					ok: true,
					result: {
						userId: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
						tokenUrl: "https://x/sign-in?token=abc",
					},
				}),
				{ status: 200, headers: { "Content-Type": "application/json" } },
			),
		);
		await openDialog();
		fireEvent.change(screen.getByLabelText(/^email$/i), {
			target: { value: "a@b.co" },
		});
		fireEvent.change(screen.getByLabelText(/reason/i), {
			target: { value: "welcome" },
		});
		fireEvent.click(screen.getByRole("button", { name: /send invite/i }));
		await waitFor(() => {
			expect(screen.getByLabelText(/one-time url/i)).toHaveValue(
				"https://x/sign-in?token=abc",
			);
		});
	});

	it("shows inline error on failure", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(
				JSON.stringify({ error: "conflict", message: "email already in use" }),
				{ status: 409, headers: { "Content-Type": "application/json" } },
			),
		);
		await openDialog();
		fireEvent.change(screen.getByLabelText(/^email$/i), {
			target: { value: "a@b.co" },
		});
		fireEvent.change(screen.getByLabelText(/reason/i), {
			target: { value: "welcome" },
		});
		fireEvent.click(screen.getByRole("button", { name: /send invite/i }));
		await waitFor(() => {
			expect(
				screen.getByText(/email already in use/i),
			).toBeInTheDocument();
		});
	});
});
