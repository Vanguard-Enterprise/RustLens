import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { UsersBulkToolbar } from "../users-bulk-toolbar";

const roleOptions = [
	{ value: "user_support", label: "User Support" },
	{ value: "user_admin", label: "User Admin" },
];

describe("UsersBulkToolbar", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
		vi.spyOn(window, "confirm").mockReturnValue(true);
	});

	it("returns null when nothing is selected", () => {
		const { container } = render(
			<UsersBulkToolbar
				selected={[]}
				roleOptions={roleOptions}
				csrfToken="t"
				currentQuery="status=active"
				onClear={() => {}}
			/>,
		);
		expect(container).toBeEmptyDOMElement();
	});

	it("posts users.bulk with suspend payload and shows summary", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(
				JSON.stringify({
					ok: true,
					result: {
						succeeded: 2,
						failed: 0,
						results: [
							{ userId: "u1", ok: true },
							{ userId: "u2", ok: true },
						],
					},
				}),
				{ status: 200, headers: { "Content-Type": "application/json" } },
			),
		);
		render(
			<UsersBulkToolbar
				selected={["u1", "u2"]}
				roleOptions={roleOptions}
				csrfToken="tok"
				currentQuery="status=active"
				onClear={() => {}}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));
		await waitFor(() => expect(fetchMock).toHaveBeenCalled());
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("/api/app/admin/actions/users.bulk");
		const body = JSON.parse((init as RequestInit).body as string);
		expect(body.params.action).toBe("suspend");
		expect(body.params.userIds).toEqual(["u1", "u2"]);
		await waitFor(() =>
			expect(screen.getByText(/2 succeeded, 0 failed/i)).toBeInTheDocument(),
		);
	});

	it("renders partial-failure summary", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(
				JSON.stringify({
					ok: true,
					result: {
						succeeded: 1,
						failed: 1,
						results: [
							{ userId: "u1", ok: true },
							{ userId: "u2", ok: false, error: "boom" },
						],
					},
				}),
				{ status: 200, headers: { "Content-Type": "application/json" } },
			),
		);
		render(
			<UsersBulkToolbar
				selected={["u1", "u2"]}
				roleOptions={roleOptions}
				csrfToken="tok"
				currentQuery="status=active"
				onClear={() => {}}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));
		await waitFor(() =>
			expect(screen.getByText(/1 succeeded, 1 failed/i)).toBeInTheDocument(),
		);
	});

	it("builds export CSV URL with selected ids and current filters", () => {
		render(
			<UsersBulkToolbar
				selected={["u1", "u2"]}
				roleOptions={roleOptions}
				csrfToken="tok"
				currentQuery="status=active&role=user_support"
				onClear={() => {}}
			/>,
		);
		const link = screen.getByRole("link", { name: /export csv/i });
		const href = link.getAttribute("href") ?? "";
		expect(href).toContain("/api/app/admin/users/export.csv?");
		expect(href).toContain("status=active");
		expect(href).toContain("role=user_support");
		expect(href).toContain("ids=u1%2Cu2");
	});
});
