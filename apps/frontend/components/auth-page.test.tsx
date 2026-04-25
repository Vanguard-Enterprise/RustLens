import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { AuthPage } from "./auth-page";

describe("AuthPage", () => {
	it("renders a Discord sign-in button", () => {
		render(<AuthPage csrfToken="t" />);
		expect(screen.getByRole("link", { name: /sign in with discord/i })).toBeInTheDocument();
	});

	it("does not render email or password inputs", () => {
		render(<AuthPage csrfToken="t" />);
		expect(screen.queryByLabelText(/email/i)).toBeNull();
		expect(screen.queryByLabelText(/password/i)).toBeNull();
	});

	it("shows the cancelled-error message when error=cancelled", () => {
		render(<AuthPage csrfToken="t" error="cancelled" />);
		expect(screen.getByText(/sign-in cancelled/i)).toBeInTheDocument();
	});

	it("shows the unavailable-error message when error=unavailable", () => {
		render(<AuthPage csrfToken="t" error="unavailable" />);
		expect(screen.getByText(/temporarily unavailable/i)).toBeInTheDocument();
	});

	it("shows the disabled-error message when error=disabled", () => {
		render(<AuthPage csrfToken="t" error="disabled" />);
		expect(screen.getByText(/account disabled/i)).toBeInTheDocument();
	});

	it("shows a notice banner when notice prop is provided", () => {
		render(<AuthPage csrfToken="t" notice="Signed out successfully" />);
		expect(screen.getByText(/signed out successfully/i)).toBeInTheDocument();
	});
});
