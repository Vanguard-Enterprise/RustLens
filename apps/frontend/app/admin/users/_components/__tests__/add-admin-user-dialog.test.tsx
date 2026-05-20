import { describe, it, expect, vi } from "vitest";
import {
	render,
	screen,
	fireEvent,
	waitFor,
	act,
} from "@testing-library/react";
import type { AdminRoleItem, AdminUserItem } from "@/lib/rustlens-api";
import { AddAdminUserDialog } from "../add-admin-user-dialog";

const roles: AdminRoleItem[] = [
	{
		id: "r1",
		key: "moderator",
		name: "Moderator",
		description: "Mods",
		isSystem: false,
		permissions: ["users.read"],
		memberCount: 0,
	},
	{
		id: "r2",
		key: "superadmin",
		name: "Superadmin",
		description: "All",
		isSystem: true,
		permissions: ["admins.grant"],
		memberCount: 0,
	},
];

const userBob: AdminUserItem = {
	user: {
		id: "u-bob",
		discordId: "111",
		discordUsername: "bob#0001",
		displayName: "Bob",
		avatarUrl: "",
		roles: [],
		createdAt: "2026-01-01T00:00:00Z",
		updatedAt: "2026-01-01T00:00:00Z",
		lastLoginAt: "2026-01-01T00:00:00Z",
	},
	discordId: "111",
	discordUsername: "bob#0001",
	guildCount: 0,
	steamConnectionCount: 0,
	adminRoles: [],
	hasUserAppInstalled: false,
};

function renderDialog(overrides: Partial<React.ComponentProps<typeof AddAdminUserDialog>> = {}) {
	const props: React.ComponentProps<typeof AddAdminUserDialog> = {
		open: true,
		onOpenChange: vi.fn(),
		assignableRoles: roles,
		canGrantSuperadmin: false,
		onSearch: vi.fn().mockResolvedValue([userBob]),
		onConfirm: vi.fn().mockResolvedValue(undefined),
		...overrides,
	};
	render(<AddAdminUserDialog {...props} />);
	return props;
}

describe("AddAdminUserDialog", () => {
	it("opens in the search stage", () => {
		renderDialog();
		expect(
			screen.getByPlaceholderText(/search users/i),
		).toBeInTheDocument();
		// Role picker (stage 2) is not visible yet.
		expect(screen.queryByLabelText(/^role$/i)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /grant role/i }),
		).not.toBeInTheDocument();
	});

	it("advances to the role stage when a user is picked", async () => {
		renderDialog();
		const input = screen.getByPlaceholderText(/search users/i);
		await act(async () => {
			fireEvent.change(input, { target: { value: "bob" } });
		});
		const option = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(option);
		});
		expect(await screen.findByLabelText(/^role$/i)).toBeInTheDocument();
		expect(screen.getByText(/selected user/i)).toBeInTheDocument();
		expect(screen.getByText("Bob")).toBeInTheDocument();
	});

	it("returns to the search stage when Change is clicked", async () => {
		renderDialog();
		const input = screen.getByPlaceholderText(/search users/i);
		await act(async () => {
			fireEvent.change(input, { target: { value: "bob" } });
		});
		const bobOption = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(bobOption);
		});
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /change/i }));
		});
		expect(screen.getByPlaceholderText(/search users/i)).toBeInTheDocument();
	});

	it("disables superadmin when canGrantSuperadmin is false", async () => {
		renderDialog();
		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		const bobOption = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(bobOption);
		});
		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		const superadmin = await screen.findByRole("option", {
			name: /superadmin/i,
		});
		expect(superadmin).toHaveAttribute("aria-disabled", "true");
	});

	it("submits userId, roleKey, and trimmed reason", async () => {
		const onConfirm = vi.fn().mockResolvedValue(undefined);
		const onOpenChange = vi.fn();
		renderDialog({ onConfirm, onOpenChange });

		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		const bobOption = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(bobOption);
		});

		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		const moderatorOption = await screen.findByRole("option", { name: /moderator/i });
		await act(async () => {
			fireEvent.click(moderatorOption);
		});

		const reason = screen.getByPlaceholderText(/reason/i);
		fireEvent.change(reason, { target: { value: "  promotion  " } });

		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(onConfirm).toHaveBeenCalledWith("u-bob", "moderator", "promotion");
		});
		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("keeps the dialog open and shows the error when onConfirm rejects", async () => {
		const onConfirm = vi.fn().mockRejectedValue(new Error("already_has_role"));
		const onOpenChange = vi.fn();
		renderDialog({ onConfirm, onOpenChange });

		await act(async () => {
			fireEvent.change(screen.getByPlaceholderText(/search users/i), {
				target: { value: "bob" },
			});
		});
		const bobOption = await screen.findByRole("option", { name: /bob/i });
		await act(async () => {
			fireEvent.click(bobOption);
		});
		await act(async () => {
			fireEvent.click(screen.getByLabelText(/^role$/i));
		});
		const moderatorOption = await screen.findByRole("option", { name: /moderator/i });
		await act(async () => {
			fireEvent.click(moderatorOption);
		});
		fireEvent.change(screen.getByPlaceholderText(/reason/i), {
			target: { value: "x" },
		});
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
		});

		await waitFor(() => {
			expect(screen.getByText(/already_has_role/i)).toBeInTheDocument();
		});
		// Dialog stays open: onOpenChange(false) is NOT called on failure.
		expect(onOpenChange).not.toHaveBeenCalledWith(false);
	});
});
