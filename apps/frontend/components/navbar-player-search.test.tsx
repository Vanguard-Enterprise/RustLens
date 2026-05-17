import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

vi.mock("@/app/_actions/player-search", () => ({
	searchPlayersAction: vi.fn(),
}));

const pushMock = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ push: pushMock }),
}));

import { searchPlayersAction } from "@/app/_actions/player-search";
import { NavbarPlayerSearch } from "./navbar-player-search";

describe("NavbarPlayerSearch", () => {
	it("debounces and calls the action once for rapid typing", async () => {
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockReset();
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [], mode: "name" });

		render(<NavbarPlayerSearch />);
		const input = screen.getByRole("searchbox") as HTMLInputElement;

		// Simulate rapid typing — each keystroke updates value and resets the debounce timer.
		fireEvent.change(input, { target: { value: "b" } });
		fireEvent.change(input, { target: { value: "be" } });
		fireEvent.change(input, { target: { value: "bea" } });
		fireEvent.change(input, { target: { value: "bean" } });

		await waitFor(
			() => {
				expect(searchPlayersAction).toHaveBeenCalledTimes(1);
			},
			{ timeout: 1000 },
		);
		expect(searchPlayersAction).toHaveBeenLastCalledWith("bean", false);
	});

	it("renders an admin-only hint on 403", async () => {
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockReset();
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockResolvedValue({ error: "forbidden" });

		render(<NavbarPlayerSearch />);
		const input = screen.getByRole("searchbox") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "freetext" } });

		expect(await screen.findByText(/identifier-only search available/i)).toBeInTheDocument();
	});

	it("does nothing for queries shorter than 2 characters", async () => {
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockReset();

		render(<NavbarPlayerSearch />);
		const input = screen.getByRole("searchbox") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "a" } });

		await new Promise((r) => setTimeout(r, 400));
		expect(searchPlayersAction).not.toHaveBeenCalled();
	});

	it("Enter redirects to login when not authed and never calls the action", async () => {
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockReset();
		const assign = vi.fn();
		Object.defineProperty(window, "location", {
			configurable: true,
			value: { assign },
		});

		render(<NavbarPlayerSearch isAuthed={false} />);
		const input = screen.getByRole("searchbox") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "abcd" } });
		fireEvent.keyDown(input, { key: "Enter" });

		expect(assign).toHaveBeenCalledWith("/api/auth/discord");
		await new Promise((r) => setTimeout(r, 400));
		expect(searchPlayersAction).not.toHaveBeenCalled();
	});

	it("Enter pushes /search when authed", async () => {
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockReset();
		(searchPlayersAction as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [], mode: "name" });
		pushMock.mockReset();

		render(<NavbarPlayerSearch isAuthed />);
		const input = screen.getByRole("searchbox") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "alice" } });
		fireEvent.keyDown(input, { key: "Enter" });

		expect(pushMock).toHaveBeenCalledWith("/search?q=alice");
	});
});
