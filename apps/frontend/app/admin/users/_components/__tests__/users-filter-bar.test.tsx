import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersFilterBar } from "../users-filter-bar";

const replace = vi.fn();
let currentSearch = "page=5";

vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams(currentSearch),
}));

beforeEach(() => {
	replace.mockClear();
	currentSearch = "page=5";
});

const roleOptions = [
	{ value: "superadmin", label: "Superadmin" },
	{ value: "moderator", label: "Moderator" },
];

function openFilterPopover() {
	fireEvent.click(screen.getByRole("button", { name: /^Filter/i }));
}

describe("UsersFilterBar", () => {
	it("pressing Enter in the search field resets page and writes the URL", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		const input = screen.getByLabelText(/Search users/i);
		fireEvent.change(input, { target: { value: "alice" } });
		fireEvent.keyDown(input, { key: "Enter" });
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("q=alice");
		expect(arg).not.toContain("page=");
	});

	it("Clear inside the filter popover resets the URL to the base pathname", () => {
		currentSearch = "page=5&status=suspended&q=alice";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		fireEvent.click(screen.getByRole("button", { name: /^Clear/i }));
		expect(replace).toHaveBeenCalledWith("/admin/users");
	});

	it("selecting 'Suspended' on the Status toggle group writes status=suspended", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		fireEvent.click(screen.getByRole("radio", { name: /^Suspended$/i }));
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("status=suspended");
		expect(arg).not.toContain("page=");
	});

	it("selecting 'Any' on the Status toggle group removes the status param", () => {
		currentSearch = "page=5&status=suspended";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		// Four ToggleGroups each have an "Any" option. The first in render order is Status.
		const anyRadios = screen.getAllByRole("radio", { name: /^Any$/i });
		fireEvent.click(anyRadios[0]);
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).not.toContain("status=");
	});

	it("toggling a role inside the nested Role dropdown writes role=<key>", () => {
		render(<UsersFilterBar roleOptions={roleOptions} />);
		openFilterPopover();
		// Radix DropdownMenu opens on pointerdown, not click — fire both.
		const rolesBtn = screen.getByRole("button", { name: /Select roles/i });
		fireEvent.pointerDown(rolesBtn, { button: 0, ctrlKey: false });
		fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /Moderator/i }));
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("role=moderator");
	});

	it("Filter button shows a badge with the count of active filters (excluding search)", () => {
		currentSearch = "status=suspended&discord=linked&role=admin&q=alice";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		// search (q=alice) is NOT counted; status + discord + role = 3
		const trigger = screen.getByRole("button", { name: /^Filter/i });
		expect(trigger).toHaveTextContent("3");
	});

	it("Filter button shows no badge when no filters are active", () => {
		currentSearch = "";
		render(<UsersFilterBar roleOptions={roleOptions} />);
		const trigger = screen.getByRole("button", { name: /^Filter/i });
		expect(trigger).not.toHaveTextContent(/\d/);
	});
});
