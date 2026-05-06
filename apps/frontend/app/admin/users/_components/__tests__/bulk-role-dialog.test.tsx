import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { BulkRoleDialog } from "../bulk-role-dialog";

const roles = [
	{ id: "r1", key: "moderator", name: "Moderator", description: "Mods", isSystem: false, permissions: [], memberCount: 0 },
	{ id: "r2", key: "superadmin", name: "Superadmin", description: "All", isSystem: true, permissions: [], memberCount: 0 },
];

describe("BulkRoleDialog", () => {
	it("excludes superadmin from picker", () => {
		render(
			<BulkRoleDialog
				open
				onOpenChange={() => {}}
				mode="grant"
				userIds={["u1", "u2"]}
				roles={roles}
				onConfirm={vi.fn()}
			/>,
		);
		fireEvent.click(screen.getByRole("combobox"));
		expect(screen.queryByRole("option", { name: /superadmin/i })).toBeNull();
		expect(screen.getByRole("option", { name: /moderator/i })).toBeInTheDocument();
	});

	it("calls onConfirm with mode, userIds, params, reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue({
			action: "roles.grant",
			succeeded: 2,
			failed: 0,
			results: [],
		});
		render(
			<BulkRoleDialog
				open
				onOpenChange={() => {}}
				mode="grant"
				userIds={["u1", "u2"]}
				roles={roles}
				onConfirm={onConfirm}
			/>,
		);
		fireEvent.click(screen.getByRole("combobox"));
		fireEvent.click(await screen.findByRole("option", { name: /moderator/i }));
		const reason = await screen.findByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "ops cleanup" } });
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role to/i }));
		});
		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith(
				"roles.grant",
				["u1", "u2"],
				{ roleKey: "moderator" },
				"ops cleanup",
			);
		});
	});
});
