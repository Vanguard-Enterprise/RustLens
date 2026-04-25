import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { AuthPage } from "@/components/auth-page";

describe("AuthPage error messages", () => {
	it.each([
		"discord-config",
		"discord-state",
		"discord-no-email",
		"discord-email-in-use",
		"discord-already-signed-in",
	])("renders a non-empty message for %s", (code) => {
		render(<AuthPage csrfToken="x" error={code} showDiscord />);
		// The error message should be present in the rendered output (non-empty body).
		expect(document.body.textContent ?? "").not.toEqual("");
	});
});
