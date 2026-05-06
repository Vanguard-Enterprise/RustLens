import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { UserCombobox } from "../user-combobox";

const search = vi.fn();

vi.mock("@/lib/rustlens-api", async () => ({
	searchAdminUsers: (q: string) => search(q),
}));

beforeEach(() => {
	search.mockReset();
});

describe("UserCombobox", () => {
	it("debounces search calls", async () => {
		search.mockResolvedValue([]);
		render(<UserCombobox label="Actor" onSelect={() => {}} onClear={() => {}} value={null} />);
		fireEvent.click(screen.getByRole("button", { name: /search admin/i }));
		const input = await screen.findByPlaceholderText(/search/i);
		fireEvent.change(input, { target: { value: "ali" } });
		fireEvent.change(input, { target: { value: "alic" } });
		fireEvent.change(input, { target: { value: "alice" } });
		await waitFor(() => expect(search).toHaveBeenCalledTimes(1));
		expect(search).toHaveBeenLastCalledWith("alice");
	});

	it("shows chip with clear when value is set", () => {
		const onClear = vi.fn();
		render(
			<UserCombobox
				label="Actor"
				onSelect={() => {}}
				onClear={onClear}
				value={{ id: "u1", displayName: "Alice", avatarUrl: null }}
			/>,
		);
		expect(screen.getByText("Alice")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /clear/i }));
		expect(onClear).toHaveBeenCalled();
	});
});
