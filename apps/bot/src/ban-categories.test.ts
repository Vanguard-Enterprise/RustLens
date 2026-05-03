import { describe, expect, it } from "vitest";
import { resolveCommandCategory, resolveCustomIdCategory } from "./ban-categories.js";

describe("ban category resolution", () => {
  it("maps create invite interactions to coordination", () => {
    expect(resolveCommandCategory("create", "invite")).toBe("coordination");
    expect(resolveCustomIdCategory("ci_req:123:no_required_role")).toBe("coordination");
    expect(resolveCustomIdCategory("ci_acc:123")).toBe("coordination");
  });
});
