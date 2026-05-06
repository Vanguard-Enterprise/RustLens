import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { GrantRoleDialog } from "../grant-role-dialog";

const roles = [
	{ id: "r1", key: "moderator", name: "Moderator", description: "Mods", isSystem: false, permissions: [], memberCount: 0 },
	{ id: "r2", key: "superadmin", name: "Superadmin", description: "All", isSystem: true, permissions: [], memberCount: 0 },
];

describe("GrantRoleDialog", () => {
	it("calls onConfirm with userId, roleKey, and reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue(undefined);
		render(
			<GrantRoleDialog
				open
				onOpenChange={() => {}}
				userId="u1"
				assignableRoles={roles.filter((r) => r.key !== "superadmin")}
				canGrantSuperadmin={false}
				onConfirm={onConfirm}
			/>,
		);

		// Pick the moderator role from the picker.
		fireEvent.click(screen.getByRole("combobox"));
		fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));

		// Type a reason.
		const reason = await screen.findByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "promotion" } });

		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith("u1", "moderator", "promotion");
		});
	});
});
