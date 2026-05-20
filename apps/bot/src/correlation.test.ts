import { describe, expect, test } from "vitest";
import {
  getFollowupCorrelationId,
  getOrAssignCorrelationId,
  getParentCorrelation,
  mintCustomId,
  parseCustomId,
  rememberParentCorrelation,
  withCorrelation,
} from "./correlation.js";

function fakeInteraction(): Record<string, unknown> {
  return { user: { id: "111" } };
}

describe("parseCustomId", () => {
  test("returns the bare id and null correlation when no delimiter present", () => {
    expect(parseCustomId("bind_c4_modal")).toEqual({
      base:        "bind_c4_modal",
      correlation: null,
    });
  });

  test("splits on the last # so existing : segments in the base survive", () => {
    expect(parseCustomId("bind_fullkit_continue:DRAFT-7#abc12345")).toEqual({
      base:        "bind_fullkit_continue:DRAFT-7",
      correlation: "abc12345",
    });
  });

  test("treats an empty correlation as null", () => {
    expect(parseCustomId("bind_c4_modal#")).toEqual({
      base:        "bind_c4_modal",
      correlation: null,
    });
  });
});

describe("withCorrelation", () => {
  test("appends the correlation when present", () => {
    expect(withCorrelation("bind_c4_modal", "abc12345")).toBe("bind_c4_modal#abc12345");
  });

  test("returns the base unchanged when correlation is empty/null", () => {
    expect(withCorrelation("bind_c4_modal", null)).toBe("bind_c4_modal");
    expect(withCorrelation("bind_c4_modal", "")).toBe("bind_c4_modal");
  });
});

describe("getOrAssignCorrelationId", () => {
  test("returns the same id for the same interaction", () => {
    const i = fakeInteraction();
    const a = getOrAssignCorrelationId(i as never);
    const b = getOrAssignCorrelationId(i as never);
    expect(a).toBe(b);
    expect(a).toMatch(/^[a-f0-9]+$/);
  });

  test("different interactions get different ids", () => {
    expect(getOrAssignCorrelationId(fakeInteraction() as never))
      .not.toBe(getOrAssignCorrelationId(fakeInteraction() as never));
  });
});

describe("mintCustomId", () => {
  test("produces base#<corr> tied to the interaction", () => {
    const i = fakeInteraction();
    const minted = mintCustomId(i as never, "bind_c4_modal");
    const corr = getOrAssignCorrelationId(i as never);
    expect(minted).toBe(`bind_c4_modal#${corr}`);
  });
});

describe("parent correlation tracking", () => {
  test("remember + get round-trips", () => {
    const i = fakeInteraction();
    rememberParentCorrelation(i as never, "parent-abc");
    expect(getParentCorrelation(i as never)).toBe("parent-abc");
  });

  test("getFollowupCorrelationId prefers parent over a fresh mint", () => {
    const i = fakeInteraction();
    rememberParentCorrelation(i as never, "parent-xyz");
    // No fresh id should have been assigned yet — and even if we asked, it
    // would not be returned by getFollowupCorrelationId.
    expect(getFollowupCorrelationId(i as never)).toBe("parent-xyz");
  });

  test("getFollowupCorrelationId falls back to a fresh id when no parent", () => {
    const i = fakeInteraction();
    const id = getFollowupCorrelationId(i as never);
    expect(id).toBeTruthy();
    expect(id).toBe(getOrAssignCorrelationId(i as never));
  });
});
