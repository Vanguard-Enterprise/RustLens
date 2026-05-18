import { describe, expect, it } from "vitest";
import { effectiveRoleFilter } from "../role-filter";

describe("effectiveRoleFilter", () => {
	it("returns all admin role keys when URL specifies none", () => {
		expect(effectiveRoleFilter([], ["a", "b", "c"])).toEqual(["a", "b", "c"]);
	});

	it("returns the intersection when URL specifies a subset", () => {
		expect(effectiveRoleFilter(["a", "b"], ["a", "c"])).toEqual(["a"]);
	});

	it("preserves URL order in the intersection", () => {
		expect(effectiveRoleFilter(["b", "a"], ["a", "b", "c"])).toEqual(["b", "a"]);
	});

	it("falls back to all admin role keys when URL has only non-admin roles", () => {
		expect(effectiveRoleFilter(["x"], ["a", "b"])).toEqual(["a", "b"]);
	});

	it("returns an empty array when both inputs are empty", () => {
		expect(effectiveRoleFilter([], [])).toEqual([]);
	});

	it("returns admin keys (fallback) when URL is empty even if admin keys is non-empty", () => {
		// Sanity check: the empty-URL branch fires before the intersection branch.
		expect(effectiveRoleFilter([], ["only"])).toEqual(["only"]);
	});
});
