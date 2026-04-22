import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import {
	AddEmailForm,
	RemoveEmailButton,
	ResendVerificationButton,
	SetPrimaryButton,
} from "../email-actions";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ refresh: vi.fn() }),
}));

function okFetch(result: unknown) {
	return vi.spyOn(globalThis, "fetch").mockResolvedValue(
		new Response(JSON.stringify({ ok: true, result }), {
			status: 200,
			headers: { "Content-Type": "application/json" },
		}),
	);
}

describe("email-actions", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		Object.assign(navigator, {
			clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
		});
	});

	it("SetPrimaryButton dispatches correct payload", async () => {
		const spy = okFetch({ userId: "u1", email: "a@b.co" });
		render(
			<SetPrimaryButton userId="u1" email="a@b.co" csrfToken="t" />,
		);
		fireEvent.click(screen.getByRole("button", { name: /set primary/i }));
		await waitFor(() => {
			expect(spy).toHaveBeenCalledWith(
				"/api/app/admin/actions/users.email.setPrimary",
				expect.objectContaining({
					method: "POST",
					body: JSON.stringify({ params: { userId: "u1", email: "a@b.co" } }),
				}),
			);
		});
	});

	it("ResendVerificationButton shows token modal on success", async () => {
		okFetch({
			userId: "u1",
			email: "a@b.co",
			tokenUrl: "https://x/verify-email?token=abc",
		});
		render(
			<ResendVerificationButton userId="u1" email="a@b.co" csrfToken="t" />,
		);
		fireEvent.click(screen.getByRole("button", { name: /send verification/i }));
		await waitFor(() => {
			expect(screen.getByLabelText(/one-time url/i)).toHaveValue(
				"https://x/verify-email?token=abc",
			);
		});
	});

	it("AddEmailForm blocks submit with invalid email", () => {
		render(<AddEmailForm userId="u1" csrfToken="t" />);
		const submit = screen.getByRole("button", { name: /add email/i });
		expect(submit).toBeDisabled();
		fireEvent.change(screen.getByLabelText(/new email/i), {
			target: { value: "not-an-email" },
		});
		expect(submit).toBeDisabled();
	});

	it("AddEmailForm dispatches lowercased normalised email on submit", async () => {
		const spy = okFetch({ userId: "u1", email: "new@b.co" });
		render(<AddEmailForm userId="u1" csrfToken="t" />);
		fireEvent.change(screen.getByLabelText(/new email/i), {
			target: { value: "  NEW@B.Co " },
		});
		fireEvent.click(screen.getByRole("button", { name: /add email/i }));
		await waitFor(() => {
			expect(spy).toHaveBeenCalledWith(
				"/api/app/admin/actions/users.email.add",
				expect.objectContaining({
					body: JSON.stringify({
						params: { userId: "u1", email: "new@b.co" },
					}),
				}),
			);
		});
	});

	it("RemoveEmailButton requires typed confirmation + reason", async () => {
		const spy = okFetch({ userId: "u1", email: "a@b.co" });
		render(
			<RemoveEmailButton userId="u1" email="a@b.co" csrfToken="t" />,
		);
		fireEvent.click(screen.getByRole("button", { name: /^remove$/i }));
		const confirmBtn = await screen.findByRole("button", {
			name: /remove email/i,
		});
		expect(confirmBtn).toBeDisabled();
		fireEvent.change(screen.getByLabelText(/type email to confirm/i), {
			target: { value: "a@b.co" },
		});
		fireEvent.change(screen.getByLabelText(/^reason$/i), {
			target: { value: "obsolete" },
		});
		expect(confirmBtn).not.toBeDisabled();
		fireEvent.click(confirmBtn);
		await waitFor(() => {
			expect(spy).toHaveBeenCalledWith(
				"/api/app/admin/actions/users.email.remove",
				expect.objectContaining({
					body: JSON.stringify({
						params: { userId: "u1", email: "a@b.co" },
						reason: "obsolete",
						typedConfirmation: "a@b.co",
					}),
				}),
			);
		});
	});
});
