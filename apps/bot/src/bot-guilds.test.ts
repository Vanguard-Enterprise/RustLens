import { describe, expect, it } from "vitest";
import { __testables } from "./bot-guilds.js";

const { buildUpsertPayload, chunk, MEMBERS_RESYNC_INTERVAL_MS } = __testables;

describe("bot-guilds buildUpsertPayload", () => {
  it("maps a Discord guild to the upsert payload shape", () => {
    const guild = {
      id: "1",
      name: "Test",
      icon: "abc",
      banner: null,
      ownerId: "200",
      memberCount: 10,
    };
    const owner = {
      id: "200",
      username: "owner",
      globalName: "Owner Display",
      avatar: "def",
    };
    const payload = buildUpsertPayload(guild as any, owner as any);
    expect(payload).toEqual({
      guildId: "1",
      name: "Test",
      iconHash: "abc",
      bannerHash: null,
      ownerDiscordId: "200",
      ownerUsername: "owner",
      ownerGlobalName: "Owner Display",
      ownerAvatarHash: "def",
      approximateMemberCount: 10,
    });
  });

  it("nulls out missing owner data", () => {
    const guild = { id: "1", name: "Test", icon: null, banner: null, ownerId: "200", memberCount: null };
    const payload = buildUpsertPayload(guild as any, null);
    expect(payload.ownerDiscordId).toBe("200");
    expect(payload.ownerUsername).toBe(null);
    expect(payload.ownerGlobalName).toBe(null);
    expect(payload.ownerAvatarHash).toBe(null);
    expect(payload.approximateMemberCount).toBe(null);
  });
});

describe("bot-guilds chunk", () => {
  it("splits arrays into fixed-size chunks", () => {
    expect(chunk(["a", "b", "c", "d", "e"], 2)).toEqual([["a", "b"], ["c", "d"], ["e"]]);
  });
  it("returns a single empty array on empty input", () => {
    expect(chunk([], 1000)).toEqual([]);
  });
});

describe("bot-guilds MEMBERS_RESYNC_INTERVAL_MS", () => {
  it("is at least 1 day", () => {
    expect(MEMBERS_RESYNC_INTERVAL_MS).toBeGreaterThanOrEqual(86_400_000);
  });
});
