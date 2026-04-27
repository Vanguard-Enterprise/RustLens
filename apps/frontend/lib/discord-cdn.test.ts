import { describe, expect, it } from "vitest";
import { guildBannerUrl, guildIconUrl, userAvatarUrl } from "./discord-cdn";

describe("guildIconUrl", () => {
  it("returns null when hash is null", () => {
    expect(guildIconUrl("1", null)).toBe(null);
  });
  it("uses .png for non-animated hashes", () => {
    expect(guildIconUrl("1", "abc")).toBe("https://cdn.discordapp.com/icons/1/abc.png?size=256");
  });
  it("uses .gif for animated hashes (a_ prefix)", () => {
    expect(guildIconUrl("1", "a_abc")).toBe("https://cdn.discordapp.com/icons/1/a_abc.gif?size=256");
  });
  it("honours custom size", () => {
    expect(guildIconUrl("1", "abc", 1024)).toBe("https://cdn.discordapp.com/icons/1/abc.png?size=1024");
  });
});

describe("guildBannerUrl", () => {
  it("uses banners path", () => {
    expect(guildBannerUrl("1", "abc")).toBe("https://cdn.discordapp.com/banners/1/abc.png?size=1024");
  });
});

describe("userAvatarUrl", () => {
  it("uses avatars path", () => {
    expect(userAvatarUrl("1", "abc")).toBe("https://cdn.discordapp.com/avatars/1/abc.png?size=128");
  });
});
