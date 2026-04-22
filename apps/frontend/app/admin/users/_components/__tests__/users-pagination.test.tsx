import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UsersPagination } from "../users-pagination";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
	useRouter: () => ({ replace }),
	usePathname: () => "/admin/users",
	useSearchParams: () => new URLSearchParams(""),
}));

describe("UsersPagination", () => {
	it("Next advances the page", () => {
		render(<UsersPagination total={120} page={1} pageSize={50} />);
		fireEvent.click(screen.getByRole("button", { name: /Next/i }));
		const arg = (replace.mock.calls.at(-1) ?? [""])[0] as string;
		expect(arg).toContain("page=2");
	});
	it("Previous disabled on page 1", () => {
		render(<UsersPagination total={120} page={1} pageSize={50} />);
		expect(
			screen.getByRole("button", { name: /Previous/i }),
		).toBeDisabled();
	});
});
