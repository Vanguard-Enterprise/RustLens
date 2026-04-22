import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { PurgePanel } from "../purge-panel";

const pushMock = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ push: pushMock, refresh: vi.fn() }),
}));

describe("PurgePanel", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		pushMock.mockClear();
	});

	it("disables Purge until typed id matches AND reason is filled", () => {
		render(<PurgePanel userId="u1" csrfToken="t" selfTarget={false} />);
		const btn = screen.getByRole("button", { name: /purge/i });
		expect(btn).toBeDisabled();
		fireEvent.change(screen.getByLabelText(/confirm user id/i), {
			target: { value: "u1" },
		});
		expect(btn).toBeDisabled(); // reason still empty
		fireEvent.change(screen.getByLabelText(/reason/i), {
			target: { value: "gdpr" },
		});
		expect(btn).not.toBeDisabled();
	});

	it("sends confirmId and posts to users.purge", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true, result: {} }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		render(<PurgePanel userId="u1" csrfToken="t" selfTarget={false} />);
		fireEvent.change(screen.getByLabelText(/confirm user id/i), {
			target: { value: "u1" },
		});
		fireEvent.change(screen.getByLabelText(/reason/i), {
			target: { value: "gdpr" },
		});
		fireEvent.click(screen.getByRole("button", { name: /purge/i }));
		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalled();
			const [url, init] = fetchMock.mock.calls[0];
			expect(String(url)).toContain("/api/app/admin/actions/users.purge");
			const body = JSON.parse(String((init as RequestInit).body));
			expect(body.params.userId).toBe("u1");
			expect(body.params.confirmId).toBe("u1");
			expect(body.reason).toBe("gdpr");
		});
	});
});
