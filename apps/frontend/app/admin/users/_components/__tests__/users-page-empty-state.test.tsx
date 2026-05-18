import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { UsersPageEmptyState } from "../users-page-empty-state";

describe("UsersPageEmptyState", () => {
	it("renders the headline, body, and CTA link to /admin/roles", () => {
		render(<UsersPageEmptyState />);
		expect(
			screen.getByRole("heading", { name: /no admin roles defined yet/i }),
		).toBeInTheDocument();
		const cta = screen.getByRole("link", { name: /go to roles/i });
		expect(cta).toHaveAttribute("href", "/admin/roles");
	});
});
