import { afterEach, describe, expect, test, vi } from "vitest";
import type { BackendClient, GuildSettings } from "./backend-client.js";
import {
  __setBackendForTests,
  invalidateGuildLimitedCache,
  isGuildLimited,
} from "./guild-settings.js";

function makeSettings(guildId: string, limited: boolean): GuildSettings {
  return {
    guildId,
    wipeReactLogChannelId: "",
    linkingLogChannelId: "",
    trackedServerLogChannelId: "",
    trackedUsersCategoryId: "",
    adminRoleId: "",
    alertRoleId: "",
    limited,
  };
}

afterEach(() => {
  __setBackendForTests(null);
});

describe("isGuildLimited", () => {
  test("caches the backend value and does not refetch within the TTL", async () => {
    const guildId = "guild-cache";
    invalidateGuildLimitedCache(guildId);
    const guildSettings = vi.fn().mockResolvedValue(makeSettings(guildId, true));
    __setBackendForTests({ guildSettings } as unknown as BackendClient);

    expect(await isGuildLimited(guildId)).toBe(true);
    expect(await isGuildLimited(guildId)).toBe(true);
    expect(guildSettings).toHaveBeenCalledTimes(1);
  });

  test("invalidateGuildLimitedCache forces a refetch", async () => {
    const guildId = "guild-invalidate";
    invalidateGuildLimitedCache(guildId);
    const guildSettings = vi.fn().mockResolvedValue(makeSettings(guildId, false));
    __setBackendForTests({ guildSettings } as unknown as BackendClient);

    expect(await isGuildLimited(guildId)).toBe(false);
    expect(guildSettings).toHaveBeenCalledTimes(1);

    invalidateGuildLimitedCache(guildId);
    expect(await isGuildLimited(guildId)).toBe(false);
    expect(guildSettings).toHaveBeenCalledTimes(2);
  });

  test("fails open (returns false) when the backend throws", async () => {
    const guildId = "guild-error";
    invalidateGuildLimitedCache(guildId);
    const guildSettings = vi.fn().mockRejectedValue(new Error("backend down"));
    __setBackendForTests({ guildSettings } as unknown as BackendClient);

    expect(await isGuildLimited(guildId)).toBe(false);

    // Error path must not cache; a subsequent call should hit the backend again.
    expect(await isGuildLimited(guildId)).toBe(false);
    expect(guildSettings).toHaveBeenCalledTimes(2);
  });
});
