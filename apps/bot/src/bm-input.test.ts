import { describe, expect, it } from "vitest";
import { extractBmID } from "./bm-input.js";

describe("extractBmID", () => {
  it("returns bare numeric BM IDs unchanged", () => {
    expect(extractBmID("918543318")).toBe("918543318");
    expect(extractBmID("  918543318  ")).toBe("918543318");
  });

  it("extracts the ID from the public player URL", () => {
    expect(extractBmID("https://www.battlemetrics.com/players/918543318")).toBe("918543318");
    expect(extractBmID("https://battlemetrics.com/players/918543318")).toBe("918543318");
    expect(extractBmID("http://www.battlemetrics.com/players/918543318/")).toBe("918543318");
    expect(extractBmID("https://www.battlemetrics.com/players/918543318?foo=bar")).toBe("918543318");
  });

  it("extracts the ID from the rcon player URL", () => {
    expect(extractBmID("https://www.battlemetrics.com/rcon/players/918543318")).toBe("918543318");
    expect(extractBmID("https://www.battlemetrics.com/rcon/players/918543318/sessions")).toBe("918543318");
  });

  it("rejects non-BM input", () => {
    expect(extractBmID("76561198127315384")).toBeNull(); // 17-digit Steam ID exceeds BM 16-digit max
    expect(extractBmID("hello")).toBeNull();
    expect(extractBmID("https://steamcommunity.com/profiles/76561198127315384")).toBeNull();
    expect(extractBmID("https://www.battlemetrics.com/servers/rust/16741517")).toBeNull();
    expect(extractBmID("")).toBeNull();
  });

  it("rejects bare-digit inputs that are too short to be a real BM ID", () => {
    expect(extractBmID("0")).toBeNull();
    expect(extractBmID("1")).toBeNull();
    expect(extractBmID("12345")).toBeNull();
  });
});
