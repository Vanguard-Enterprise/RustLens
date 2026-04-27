import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  getOwnerDiscordIDs,
  getOwnerGuildID,
  isAuthorizedOwner,
  isOwnerCommandConfigured,
  resetCachedOwnerGuard,
} from "./owner-guard.js";

const originalEnv = { ...process.env };

function setOwnerEnv(guildId: string | undefined, discordIds: string | undefined) {
  if (guildId === undefined) {
    delete process.env.BOT_OWNER_GUILD_ID;
  } else {
    process.env.BOT_OWNER_GUILD_ID = guildId;
  }
  if (discordIds === undefined) {
    delete process.env.BOT_OWNER_DISCORD_IDS;
  } else {
    process.env.BOT_OWNER_DISCORD_IDS = discordIds;
  }
  resetCachedOwnerGuard();
}

beforeEach(() => {
  resetCachedOwnerGuard();
});

afterEach(() => {
  process.env = { ...originalEnv };
  resetCachedOwnerGuard();
});

describe("owner-guard", () => {
  it("reports configured when both env vars are present", () => {
    setOwnerEnv("123", "111,222");
    expect(isOwnerCommandConfigured()).toBe(true);
    expect(getOwnerGuildID()).toBe("123");
    expect(getOwnerDiscordIDs()).toEqual(["111", "222"]);
  });

  it("reports unconfigured when guild id is missing", () => {
    setOwnerEnv("", "111");
    expect(isOwnerCommandConfigured()).toBe(false);
  });

  it("reports unconfigured when discord ids are empty", () => {
    setOwnerEnv("123", "");
    expect(isOwnerCommandConfigured()).toBe(false);
  });

  it("authorizes only owners interacting from the owner guild", () => {
    setOwnerEnv("guild-1", "owner-1,owner-2");
    expect(isAuthorizedOwner({ guildId: "guild-1", user: { id: "owner-1" } })).toBe(true);
    expect(isAuthorizedOwner({ guildId: "guild-1", user: { id: "outsider" } })).toBe(false);
    expect(isAuthorizedOwner({ guildId: "other-guild", user: { id: "owner-1" } })).toBe(false);
    expect(isAuthorizedOwner({ guildId: null, user: { id: "owner-1" } })).toBe(false);
  });

  it("trims whitespace and ignores empty entries in discord id list", () => {
    setOwnerEnv("g", " owner-a , , owner-b ");
    expect(getOwnerDiscordIDs()).toEqual(["owner-a", "owner-b"]);
  });
});
