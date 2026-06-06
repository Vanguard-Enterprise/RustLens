import { afterEach, describe, expect, it, vi } from "vitest";
import type { ButtonInteraction } from "discord.js";
import { __testables } from "./create.js";

function makeButtonInteraction(
  fetchChannel: (channelId: string) => Promise<unknown>,
) {
  return {
    guild: {
      channels: {
        fetch: fetchChannel,
      },
    },
  } as ButtonInteraction;
}

describe("create invite pending requests", () => {
  afterEach(() => {
    __testables.pendingInvites.clear();
    vi.restoreAllMocks();
  });

  it("keeps blocking while the pending request message still exists", async () => {
    const interaction = makeButtonInteraction(async () => ({
      messages: {
        fetch: vi.fn(async () => ({ id: "message-1" })),
      },
    }));

    await expect(__testables.pendingInviteRequestStillExists(interaction, {
      guildId: "guild-1",
      channelId: "channel-1",
      messageId: "message-1",
      createdAt: Date.now(),
    })).resolves.toBe(true);
  });

  it("clears stale pending state when the request message was deleted", async () => {
    const interaction = makeButtonInteraction(async () => ({
      messages: {
        fetch: vi.fn(async () => {
          throw { code: 10008 };
        }),
      },
    }));

    await expect(__testables.pendingInviteRequestStillExists(interaction, {
      guildId: "guild-1",
      channelId: "channel-1",
      messageId: "message-1",
      createdAt: Date.now(),
    })).resolves.toBe(false);
  });

  it("keeps the block on transient Discord fetch failures", async () => {
    const interaction = makeButtonInteraction(async () => ({
      messages: {
        fetch: vi.fn(async () => {
          throw { code: 50001 };
        }),
      },
    }));

    await expect(__testables.pendingInviteRequestStillExists(interaction, {
      guildId: "guild-1",
      channelId: "channel-1",
      messageId: "message-1",
      createdAt: Date.now(),
    })).resolves.toBe(true);
  });
});
