import { describe, expect, it } from "vitest";
import {
	legalContact,
	legalEffectiveDate,
	privacyPolicy,
	termsOfService,
} from "./legal-content";

describe("legal content", () => {
	it("defines shared legal metadata placeholders", () => {
		expect(legalEffectiveDate).toBeTruthy();
		expect(legalContact.email).toContain("@");
	});

	it("covers the expected RustLens service terms topics", () => {
		expect(termsOfService.sections.length).toBeGreaterThanOrEqual(8);
		expect(termsOfService.sections.map((section) => section.title)).toEqual(
			expect.arrayContaining([
				"Accounts and credential security",
				"Discord bot, linked identities, and community data",
				"Availability, warranties, and liability limits",
				"Contact",
			]),
		);
	});

	it("covers the expected RustLens privacy topics", () => {
		expect(privacyPolicy.sections.length).toBeGreaterThanOrEqual(8);
		expect(privacyPolicy.sections.map((section) => section.title)).toEqual(
			expect.arrayContaining([
				"Information RustLens collects",
				"How RustLens uses the information",
				"Retention",
				"Your choices and requests",
			]),
		);
	});

	it("defines document descriptions for route metadata usage", () => {
		expect(termsOfService.description).toContain("RustLens");
		expect(privacyPolicy.description).toContain("RustLens");
	});
});
