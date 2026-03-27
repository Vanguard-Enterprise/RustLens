import { HEALTH_ENDPOINT } from "./health";
import { describe, expect, it } from "vitest";

describe("health endpoint contract", () => {
  it("uses backend health endpoint under /api", () => {
    expect(HEALTH_ENDPOINT).toBe("/api/healthz");
  });
});