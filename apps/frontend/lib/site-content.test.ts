import { describe, expect, it } from "vitest";
import { landingFeatures, launchSteps, operatingPillars } from "./site-content";

describe("site content", () => {
	it("covers the four core landing feature areas", () => {
		expect(landingFeatures).toHaveLength(4);
		expect(
			landingFeatures.every(
				(feature) => feature.title.length > 0 && feature.description.length > 0,
			),
		).toBe(true);
	});

	it("defines the staged rollout narrative", () => {
		expect(launchSteps).toHaveLength(3);
		expect(operatingPillars).toHaveLength(3);
	});
});
