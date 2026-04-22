import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { SuspendPanel } from "../suspend-panel";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ refresh: vi.fn(), push: vi.fn() }),
}));

describe("SuspendPanel", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("disables suspend on self", () => {
		render(
			<SuspendPanel
				userId="u1"
				csrfToken="t"
				suspended={false}
				selfTarget
			/>,
		);
		const btn = screen.getByRole("button", { name: /suspend/i });
		expect(btn).toBeDisabled();
	});

	it("requires a reason before enabling suspend", () => {
		render(
			<SuspendPanel
				userId="u1"
				csrfToken="t"
				suspended={false}
				selfTarget={false}
			/>,
		);
		const btn = screen.getByRole("button", { name: /suspend/i });
		expect(btn).toBeDisabled();
		fireEvent.change(screen.getByPlaceholderText(/why is this account/i), {
			target: { value: "abuse" },
		});
		expect(btn).not.toBeDisabled();
	});

	it("posts correct payload to users.suspend", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		render(
			<SuspendPanel
				userId="u1"
				csrfToken="t"
				suspended={false}
				selfTarget={false}
			/>,
		);
		fireEvent.change(screen.getByPlaceholderText(/why is this account/i), {
			target: { value: "abuse" },
		});
		fireEvent.click(screen.getByRole("button", { name: /suspend/i }));
		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalled();
			const [url, init] = fetchMock.mock.calls[0];
			expect(String(url)).toContain(
				"/api/app/admin/actions/users.suspend",
			);
			const body = JSON.parse(String((init as RequestInit).body));
			expect(body.params.userId).toBe("u1");
			expect(body.reason).toBe("abuse");
			expect(body.typedConfirmation).toBe("u1");
		});
	});

	it("renders unsuspend when already suspended", () => {
		render(
			<SuspendPanel
				userId="u1"
				csrfToken="t"
				suspended
				suspendReason="abuse"
				selfTarget={false}
			/>,
		);
		expect(screen.getByText(/abuse/i)).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /unsuspend/i }),
		).toBeInTheDocument();
	});
});
