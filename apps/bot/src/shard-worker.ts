import { Client, GatewayIntentBits, MessageFlags } from "discord.js";
import {
  ensureAdminCommand,
  handleAdminChatInput,
} from "./admin-command.js";
import {
  ensureAnnounceCommand,
  handleAnnounceChatInput,
} from "./announce-command.js";
import {
  ensureBindCommand,
  handleBindButtonInteraction,
  handleBindChatInput,
  handleBindModalSubmit,
} from "./bind-command.js";
import {
  ensureCompareCommand,
  handleCompareAutocomplete,
  handleCompareChatInput,
} from "./compare-command.js";
import {
  ensureOnlineCommand,
  handleOnlineChatInput,
} from "./online-command.js";
import {
  ensurePlayerCommand,
  handlePlayerButtonInteraction,
  handlePlayerChatInput,
} from "./player-command.js";
import {
  ensureNicknameCommand,
  handleNicknameChatInput,
} from "./nickname-command.js";
import {
  ensureSetSteamCommand,
  handleSetSteamChatInput,
} from "./setsteam-command.js";
import {
  ensureServerCommand,
  handleServerAutocomplete,
  handleServerChatInput,
  handleServerModalSubmit,
} from "./server-command.js";
import {
  ensureSettingsCommand,
  handleSettingsChatInput,
  handleSettingsModalSubmit,
  handleSettingsSelect,
} from "./settings-command.js";
import {
  ensureSummonCommand,
  handleSummonChatInput,
} from "./summon-command.js";
import {
  ensureViewCommand,
  handleViewChatInput,
} from "./view-command.js";
import {
  ensureWipeReactCommand,
  handleWipeReactAutocomplete,
  handleWipeReactButtonInteraction,
  handleWipeReactChatInput,
  handleWipeReactModalSubmit,
  rehydratePersistedWipeReacts,
  startWipeReactScheduler,
} from "./wipereact-command.js";
import { startTrackedServerLogPoller } from "./tracked-server-logs.js";
import { registerUserGuildSync } from "./user-guilds.js";

const shardID = Number(process.env.SHARD_ID ?? "0");
const shardCount = Number(process.env.SHARD_COUNT ?? "1");
const token = process.env.DISCORD_TOKEN;

if (!token) {
  throw new Error("DISCORD_TOKEN is required for shard worker process");
}

const client = new Client({
  intents: [GatewayIntentBits.Guilds, GatewayIntentBits.GuildMembers, GatewayIntentBits.GuildVoiceStates],
  shards: [shardID],
  shardCount,
});

registerUserGuildSync(client);

client.once("clientReady", () => {
  console.log(`[bot] shard ${shardID}/${shardCount} ready as ${client.user?.tag}`);

  Promise.allSettled([
    ensureAdminCommand(client, shardID),
    ensureBindCommand(client, shardID),
    ensureCompareCommand(client, shardID),
    ensureWipeReactCommand(client, shardID),
    ensureSummonCommand(client, shardID),
    ensureAnnounceCommand(client, shardID),
    ensureOnlineCommand(client, shardID),
    ensurePlayerCommand(client, shardID),
    ensureNicknameCommand(client, shardID),
    ensureSetSteamCommand(client, shardID),
    ensureSettingsCommand(client, shardID),
    ensureViewCommand(client, shardID),
    ensureServerCommand(client, shardID),
    rehydratePersistedWipeReacts(client),
  ]).then((results) => {
    for (const result of results) {
      if (result.status === "rejected") {
        console.error("[bot] command registration failed", result.reason);
      }
    }
    startWipeReactScheduler(client);
    startTrackedServerLogPoller(client);
  });
});

function isIgnorableInteractionReplyError(error: unknown) {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }

  const code = (error as { code?: unknown }).code;
  return code === 10008 || code === 10062;
}

client.on("interactionCreate", async (interaction) => {
  try {
    if (interaction.isAutocomplete()) {
      if (await handleCompareAutocomplete(interaction)) {
        return;
      }

      if (await handleWipeReactAutocomplete(interaction)) {
        return;
      }

      if (await handleServerAutocomplete(interaction)) {
        return;
      }
    }

    if (interaction.isChatInputCommand()) {
      if (await handleAdminChatInput(interaction)) {
        return;
      }

      if (await handleSummonChatInput(interaction)) {
        return;
      }

      if (await handleAnnounceChatInput(interaction)) {
        return;
      }

      if (await handleViewChatInput(interaction)) {
        return;
      }

      if (await handleCompareChatInput(interaction)) {
        return;
      }

      if (await handleOnlineChatInput(interaction)) {
        return;
      }

      if (await handlePlayerChatInput(interaction)) {
        return;
      }

      if (await handleNicknameChatInput(interaction)) {
        return;
      }

      if (await handleSetSteamChatInput(interaction)) {
        return;
      }

      if (await handleServerChatInput(interaction)) {
        return;
      }

      if (await handleBindChatInput(interaction)) {
        return;
      }

      if (await handleWipeReactChatInput(interaction)) {
        return;
      }

      await handleSettingsChatInput(interaction);
      return;
    }

    if (interaction.isStringSelectMenu()) {
      await handleSettingsSelect(interaction);
      return;
    }

    if (interaction.isModalSubmit()) {
      if (await handleBindModalSubmit(interaction)) {
        return;
      }

      if (await handleWipeReactModalSubmit(interaction)) {
        return;
      }

      if (await handleServerModalSubmit(interaction)) {
        return;
      }

      await handleSettingsModalSubmit(interaction);
      return;
    }

    if (interaction.isButton()) {
      if (await handleBindButtonInteraction(interaction)) {
        return;
      }

      if (await handlePlayerButtonInteraction(interaction)) {
        return;
      }

      await handleWipeReactButtonInteraction(interaction);
      return;
    }
  } catch (error) {
    console.error("[bot] interaction handling failed", error);

    if (interaction.isRepliable()) {
      try {
        if (interaction.deferred || interaction.replied) {
          await interaction.editReply({
            content: "Bot interaction failed.",
          });
        } else {
          await interaction.reply({
            content: "Bot interaction failed.",
            flags: MessageFlags.Ephemeral,
          });
        }
      } catch (replyError) {
        if (!isIgnorableInteractionReplyError(replyError)) {
          console.error("[bot] failed to send interaction error reply", replyError);
        }
      }
    }
  }
});

client.login(token).catch((error) => {
  console.error(`[bot] shard ${shardID} login failed`, error);
  process.exit(1);
});

process.on("SIGINT", async () => {
  await client.destroy();
  process.exit(0);
});

process.on("SIGTERM", async () => {
  await client.destroy();
  process.exit(0);
});
