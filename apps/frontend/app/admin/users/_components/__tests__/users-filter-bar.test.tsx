import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersFilterBar } from "../users-filter-bar";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams("page=5&status=active"),
}));

describe("UsersFilterBar", () => {
	it("pressing Enter in the search field resets page and writes the URL", () => {
		replace.mockClear();
		render(
			<UsersFilterBar
				roleOptions={[{ value: "superadmin", label: "Superadmin" }]}
			/>,
		);
		const input = screen.getByLabelText(/Search users/i);
		fireEvent.change(input, { target: { value: "alice" } });
		fireEvent.keyDown(input, { key: "Enter" });
		expect(replace).toHaveBeenCalled();
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("q=alice");
		expect(arg).not.toContain("page=");
	});

	it("Clear resets the URL to the base pathname", () => {
		replace.mockClear();
		render(
			<UsersFilterBar
				roleOptions={[{ value: "superadmin", label: "Superadmin" }]}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));
		expect(replace).toHaveBeenCalledWith("/admin/users");
	});
});
