import { describe, expect, test, vi } from "vitest";
import type { CommandUsagePayload, CommandUsageTarget } from "./backend-client.js";
import {
  buildCommandUsagePayload,
  recordCommandUsage,
  __setBackendForTests,
  __resetForTests,
} from "./command-usage.js";
import { rememberParentCorrelation } from "./correlation.js";

function fakeChatInput(opts: {
  command: string;
  subcommand?: string;
  options?: Array<{ name: string; value: unknown }>;
  guildId?: string;
  channelId?: string;
  userId?: string;
}) {
  const getOpts = opts.options ?? [];
  return {
    isChatInputCommand: () => true,
    isButton: () => false,
    isModalSubmit: () => false,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    commandName: opts.command,
    options: {
      getSubcommand: (_: boolean) => opts.subcommand ?? null,
      data: getOpts.map((o) => ({ name: o.name, value: o.value, type: 3 })),
    },
    guildId:  opts.guildId  ?? null,
    channelId: opts.channelId ?? null,
    user: { id: opts.userId ?? "111" },
  };
}

function fakeButton(customId: string) {
  return {
    isChatInputCommand: () => false,
    isButton: () => true,
    isModalSubmit: () => false,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    customId,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
  };
}

function fakeModal(customId: string, fields: Record<string, string>) {
  return {
    isChatInputCommand: () => false,
    isButton: () => false,
    isModalSubmit: () => true,
    isStringSelectMenu: () => false,
    isAutocomplete: () => false,
    customId,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
    fields: {
      fields: new Map(Object.entries(fields).map(([k, v]) => [k, { customId: k, value: v }])),
    },
  };
}

function fakeStringSelect(customId: string, values: string[]) {
  return {
    isChatInputCommand: () => false,
    isButton: () => false,
    isModalSubmit: () => false,
    isStringSelectMenu: () => true,
    isAutocomplete: () => false,
    customId,
    values,
    guildId:  "g1",
    channelId: "c1",
    user:     { id: "111" },
  };
}

describe("buildCommandUsagePayload", () => {
  test("chat input with subcommand + options", () => {
    const p = buildCommandUsagePayload(
      fakeChatInput({
        command: "track",
        subcommand: "create",
        options: [
          { name: "group_name", value: "raid" },
          { name: "server",     value: "12345" },
        ],
        guildId: "g1",
        channelId: "c1",
      }) as never,
      "success",
      null,
      142,
    );
    expect(p).not.toBeNull();
    expect(p!.interactionType).toBe("command");
    expect(p!.command).toBe("track");
    expect(p!.subcommand).toBe("create");
    expect(p!.target).toBe("raid");
    expect(p!.options).toEqual({ group_name: "raid", server: "12345" });
    expect(p!.latencyMs).toBe(142);
    expect(p!.outcome).toBe("success");
  });

  test("button splits customId on first colon", () => {
    const p = buildCommandUsagePayload(
      fakeButton("track:add:abc123") as never,
      "success",
      null,
      10,
    );
    expect(p!.interactionType).toBe("button");
    expect(p!.command).toBe("track");
    expect(p!.target).toBe("add:abc123");
    expect(p!.options).toEqual({ customId: "track:add:abc123" });
  });

  test("button with no colon uses full customId as command", () => {
    const p = buildCommandUsagePayload(
      fakeButton("standalone-button") as never,
      "success",
      null,
      10,
    );
    expect(p!.command).toBe("standalone-button");
    expect(p!.target).toBeNull();
  });

  test("modal extracts field values", () => {
    const p = buildCommandUsagePayload(
      fakeModal("wipereact:edit:42", { title: "Raid", duration: "60" }) as never,
      "success",
      null,
      20,
    );
    expect(p!.interactionType).toBe("modal");
    expect(p!.command).toBe("wipereact");
    expect(p!.target).toBe("edit:42");
    expect(p!.options).toEqual({ title: "Raid", duration: "60" });
  });

  test("string select captures values", () => {
    const p = buildCommandUsagePayload(
      fakeStringSelect("settings:tracked_servers", ["s1", "s2"]) as never,
      "success",
      null,
      5,
    );
    expect(p!.interactionType).toBe("string_select");
    expect(p!.command).toBe("settings");
    expect(p!.target).toBe("tracked_servers");
    expect(p!.options).toEqual({ customId: "settings:tracked_servers", values: ["s1", "s2"] });
  });

  test("autocomplete returns null", () => {
    const interaction = {
      isChatInputCommand: () => false,
      isButton: () => false,
      isModalSubmit: () => false,
      isStringSelectMenu: () => false,
      isAutocomplete: () => true,
      user: { id: "111" },
    };
    expect(buildCommandUsagePayload(interaction as never, "success", null, 1)).toBeNull();
  });

  test("chat input mints a stable correlationId", () => {
    const interaction = fakeChatInput({ command: "bind", subcommand: "c4" });
    const p1 = buildCommandUsagePayload(interaction as never, "success", null, 1);
    const p2 = buildCommandUsagePayload(interaction as never, "success", null, 2);
    expect(p1!.correlationId).toBeTruthy();
    // Same interaction → same correlation. Two calls in a row must agree so
    // any customId minted earlier in the handler is recoverable when the row
    // is recorded later in the `finally` block.
    expect(p1!.correlationId).toBe(p2!.correlationId);
    expect(p1!.parentCorrelationId).toBeFalsy();
  });

  test("modal follow-up carries parentCorrelationId stashed by the dispatcher", () => {
    const modal = fakeModal("bind_c4_modal", { bind_c4_bind: "Z" });
    // Dispatcher would have stripped #abc12345 off the raw customId and
    // stashed the correlation against the interaction via this helper.
    rememberParentCorrelation(modal as never, "abc12345");
    const p = buildCommandUsagePayload(modal as never, "success", null, 5);
    expect(p!.parentCorrelationId).toBe("abc12345");
    // Modal itself still gets its OWN correlation so any further nested
    // follow-up could chain through.
    expect(p!.correlationId).toBeTruthy();
    expect(p!.correlationId).not.toBe("abc12345");
  });

  test("classifies guild interactions via guildId regardless of channel", () => {
    const p = buildCommandUsagePayload(
      fakeChatInput({ command: "player", guildId: "g1", channelId: "c1" }) as never,
      "success",
      null,
      1,
    );
    expect(p!.channelKind).toBe("guild");
    expect(p!.channelName).toBeNull();
  });

  test("classifies a 1:1 DM as 'dm' with no name", () => {
    // discord.js ChannelType.DM = 1
    const dm = {
      ...fakeChatInput({ command: "ping", channelId: "c1" }),
      guildId: null,
      channel: { type: 1, name: null },
    };
    const p = buildCommandUsagePayload(dm as never, "success", null, 1);
    expect(p!.channelKind).toBe("dm");
    expect(p!.channelName).toBeNull();
  });

  test("classifies a group DM as 'group_dm' and forwards its name", () => {
    // discord.js ChannelType.GroupDM = 3
    const gc = {
      ...fakeChatInput({ command: "ping", channelId: "c1" }),
      guildId: null,
      channel: { type: 3, name: "Raid Squad" },
    };
    const p = buildCommandUsagePayload(gc as never, "success", null, 1);
    expect(p!.channelKind).toBe("group_dm");
    expect(p!.channelName).toBe("Raid Squad");
  });

  test("falls back to 'unknown' when guildId is null and channel is missing", () => {
    const orphan = {
      ...fakeChatInput({ command: "ping" }),
      guildId: null,
      channel: null,
    };
    const p = buildCommandUsagePayload(orphan as never, "success", null, 1);
    expect(p!.channelKind).toBe("unknown");
    expect(p!.channelName).toBeNull();
  });

  test("error outcome carries errorCode (capped to 256)", () => {
    const long = "x".repeat(500);
    const p = buildCommandUsagePayload(
      fakeChatInput({ command: "player" }) as never,
      "error",
      long,
      10,
    );
    expect(p!.outcome).toBe("error");
    expect(p!.errorCode!.length).toBe(256);
  });
});

describe("recordCommandUsage", () => {
  test("fire and forget — does not throw on backend failure", () => {
    __resetForTests();
    const spy = vi.fn().mockRejectedValue(new Error("boom"));
    __setBackendForTests({ recordCommandUsage: spy } as never);

    expect(() =>
      recordCommandUsage(
        fakeChatInput({ command: "player" }) as never,
        "success",
        null,
        Date.now() - 50,
      ),
    ).not.toThrow();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  test("does not call backend when interaction has no user id", () => {
    __resetForTests();
    const spy = vi.fn().mockResolvedValue(undefined);
    __setBackendForTests({ recordCommandUsage: spy } as never);

    // chat input fake but with empty user id
    const interaction = {
      isChatInputCommand: () => true,
      isButton: () => false,
      isModalSubmit: () => false,
      isStringSelectMenu: () => false,
      isAutocomplete: () => false,
      commandName: "player",
      options: {
        getSubcommand: () => null,
        data: [],
      },
      guildId:  null,
      channelId: null,
      user: { id: "" },
    };

    recordCommandUsage(interaction as never, "success", null, Date.now());

    expect(spy).not.toHaveBeenCalled();
  });

  test("targets propagate to payload when provided", async () => {
    __resetForTests();
    const captured: CommandUsagePayload[] = [];
    __setBackendForTests({
      recordCommandUsage: async (_w: string, p: CommandUsagePayload) => { captured.push(p); },
    } as never);

    const interaction = fakeChatInput({
      command: "announce",
      subcommand: "send",
      guildId: "g1",
      channelId: "c1",
      userId: "222",
    });

    const targets: CommandUsageTarget[] = [
      {
        kind: "role",
        snowflake: "role-111",
        displayName: "@Raiders",
        avatarHash: null,
        relation: "scope",
      },
      {
        kind: "channel",
        snowflake: "chan-999",
        displayName: "#announcements",
        avatarHash: null,
        relation: "recipient",
      },
    ];

    recordCommandUsage(interaction as never, "success", null, Date.now(), targets);

    // Allow the fire-and-forget promise to settle
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(captured).toHaveLength(1);
    expect(captured[0].targets).toHaveLength(2);
    expect(captured[0].targets![0]).toMatchObject({ kind: "role", relation: "scope" });

    __resetForTests();
  });
});
