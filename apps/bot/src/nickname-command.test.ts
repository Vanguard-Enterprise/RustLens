import { describe, expect, it } from "vitest";
import { __testables } from "./nickname-command.js";

describe("nickname-command", () => {
  it("does not duplicate the bm suffix on rerun", () => {
    const member = {
      nickname: "Kasper - turn it up",
      user: {
        globalName: "Kasper",
        username: "kasper",
      },
    };

    expect(__testables.buildTargetNickname(member as never, "turn it up")).toBe("Kasper - turn it up");
  });

  it("truncates long nicknames to Discord limits", () => {
    const member = {
      nickname: "VeryLongDiscordNicknameHere",
      user: {
        globalName: "VeryLongDiscordNicknameHere",
        username: "kasper",
      },
    };

    const nickname = __testables.buildTargetNickname(member as never, "VeryLongBattleMetricsNameHere");
    expect(nickname.length).toBeLessThanOrEqual(32);
    expect(nickname).toContain(" - ");
  });

  it("builds the completion embed results fields", () => {
    const embed = __testables.buildCompletionEmbed(
      7,
      2,
      9,
      [
        { discordId: "111", reason: "Bot's role is below the user's role." },
        { discordId: "222", reason: "Bot's role is below the user's role." },
      ],
    ).toJSON();

    expect(embed.title).toBe("Nickname Change Complete");
    expect(embed.fields?.[0]?.name).toBe("👥 Processed: 9");
    expect(embed.fields?.[0]?.value).toBe("\u200b");
    expect(embed.fields?.[1]?.name).toBe("✅ Changed: 7");
    expect(embed.fields?.[2]?.name).toBe("❌ Failed: 2");
    expect(embed.fields?.[2]?.value).toContain("Bot's role is below the user's role. (2)");
    expect(embed.fields?.[2]?.value).not.toContain("<@111>");
  });
});
