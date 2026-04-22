import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { RoleGrantControls } from "../role-grant-controls";
import type { AdminRoleItem } from "@/lib/rustlens-api";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ refresh: vi.fn() }),
}));

const allRoles: AdminRoleItem[] = [
	{
		id: "r1",
		key: "moderator",
		name: "Moderator",
		description: null,
		isSystem: true,
		permissions: [],
	},
	{
		id: "r2",
		key: "superadmin",
		name: "Superadmin",
		description: null,
		isSystem: true,
		permissions: [],
	},
];

describe("RoleGrantControls", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it("hides grant/revoke superadmin buttons when session lacks admins.grant", () => {
		render(
			<RoleGrantControls
				userId="u1"
				currentRoles={["superadmin"]}
				allRoles={allRoles}
				canAssign={true}
				canGrantSuperadmin={false}
				csrfToken="t"
			/>,
		);
		// No button for superadmin row should be rendered.
		expect(screen.queryByRole("button", { name: /revoke/i })).toBeNull();
		// But grant button for moderator (not currently assigned) should appear.
		expect(screen.getByRole("button", { name: /grant/i })).toBeInTheDocument();
	});

	it("POSTs users.roles.grant when clicking grant on an available role", async () => {
		const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ ok: true }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		render(
			<RoleGrantControls
				userId="u1"
				currentRoles={[]}
				allRoles={allRoles.filter((r) => r.key === "moderator")}
				canAssign={true}
				canGrantSuperadmin={false}
				csrfToken="csrf-xyz"
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /grant/i }));
		const [reasonInput] = await screen.findAllByPlaceholderText(/reason/i);
		fireEvent.change(reasonInput, { target: { value: "promote" } });
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /confirm/i, hidden: true }));
		});
		await waitFor(() => expect(fetchMock).toHaveBeenCalled());
		const [url, init] = fetchMock.mock.calls[0];
		expect(String(url)).toContain("/api/app/admin/actions/users.roles.grant");
		const body = JSON.parse(String((init as RequestInit).body));
		expect(body.reason).toBe("promote");
		expect(body.params.userId).toBe("u1");
		expect(body.params.roleKey).toBe("moderator");
	});
});
