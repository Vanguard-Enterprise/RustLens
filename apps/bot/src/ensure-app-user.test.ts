import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  __resetEnsureAppUserCacheForTests,
  ensureAppUser,
  __setBackendForTests,
} from "./ensure-app-user.js";

type FakeInteraction = {
  user: { id: string; username: string; globalName: string | null; avatar: string | null };
};

function makeInteraction(id = "111222333444555666"): FakeInteraction {
  return {
    user: {
      id,
      username:   "alice",
      globalName: "Alice",
      avatar:     "deadbeef",
    },
  };
}

describe("ensureAppUser", () => {
  let backendSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    backendSpy = vi.fn().mockResolvedValue({ userId: "usr_x", created: true });
    __setBackendForTests({
      // Only the method we exercise needs a real impl.
      ensureAppUser: backendSpy,
    } as unknown as Parameters<typeof __setBackendForTests>[0]);
    __resetEnsureAppUserCacheForTests();
  });

  afterEach(() => {
    __setBackendForTests(null);
  });

  it("posts to the backend on first call for a user", async () => {
    ensureAppUser(makeInteraction() as never);
    // Allow microtask queue to flush so the .catch chain runs.
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledOnce();
    const [, payload] = backendSpy.mock.calls[0];
    expect(payload).toEqual({
      discordId:  "111222333444555666",
      username:   "alice",
      globalName: "Alice",
      avatarHash: "deadbeef",
    });
  });

  it("dedupes a second call within the TTL window", async () => {
    ensureAppUser(makeInteraction() as never);
    ensureAppUser(makeInteraction() as never);
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledOnce();
  });

  it("fires again for a different user", async () => {
    ensureAppUser(makeInteraction("111") as never);
    ensureAppUser(makeInteraction("222") as never);
    await Promise.resolve();
    expect(backendSpy).toHaveBeenCalledTimes(2);
  });

  it("swallows backend errors without throwing", async () => {
    backendSpy.mockRejectedValueOnce(new Error("boom"));
    // Must not throw.
    expect(() => ensureAppUser(makeInteraction() as never)).not.toThrow();
    await Promise.resolve();
    await Promise.resolve();
  });
});
