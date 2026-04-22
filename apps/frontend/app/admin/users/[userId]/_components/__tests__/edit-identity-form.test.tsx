import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { EditIdentityForm } from "../edit-identity-form";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ refresh: vi.fn() }),
}));

describe("EditIdentityForm", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("renders initial values in editable fields", () => {
		render(
			<EditIdentityForm
				userId="u1"
				initialDisplayName="Alice"
				initialAvatarUrl="https://example.com/a.png"
				csrfToken="t"
			/>,
		);
		expect(screen.getByLabelText(/display name/i)).toHaveValue("Alice");
		expect(screen.getByLabelText(/avatar url/i)).toHaveValue(
			"https://example.com/a.png",
		);
	});

	it("POSTs to the dispatcher with changed fields only", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		render(
			<EditIdentityForm
				userId="u1"
				initialDisplayName="Alice"
				initialAvatarUrl=""
				csrfToken="csrf-123"
			/>,
		);
		const nameInput = screen.getByLabelText(/display name/i);
		fireEvent.change(nameInput, { target: { value: "  Alice New  " } });
		fireEvent.submit(nameInput.closest("form") as HTMLFormElement);

		await waitFor(() => expect(fetchMock).toHaveBeenCalled());
		const [url, init] = fetchMock.mock.calls[0];
		expect(String(url)).toBe("/api/app/admin/actions/users.edit");
		expect((init as RequestInit).method).toBe("POST");
		const headers = new Headers((init as RequestInit).headers);
		expect(headers.get("X-CSRF-Token")).toBe("csrf-123");
		const body = JSON.parse(String((init as RequestInit).body));
		expect(body.params.userId).toBe("u1");
		expect(body.params.displayName).toBe("Alice New");
		expect(body.params.avatarUrl).toBeUndefined();
	});

	it("blocks submit when nothing changed", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true }), { status: 200 }),
		);
		render(
			<EditIdentityForm
				userId="u1"
				initialDisplayName="Alice"
				initialAvatarUrl=""
				csrfToken="t"
			/>,
		);
		const form = screen.getByLabelText(/display name/i).closest("form") as HTMLFormElement;
		fireEvent.submit(form);
		await waitFor(() =>
			expect(screen.getByText(/nothing to update/i)).toBeInTheDocument(),
		);
		expect(fetchMock).not.toHaveBeenCalled();
	});
});
