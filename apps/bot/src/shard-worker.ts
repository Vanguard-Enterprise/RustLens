import { ActivityType, Client, GatewayIntentBits, MessageFlags } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import {
  ensureAdminCommand,
  handleAdminChatInput,
} from "./admin-command.js";
import {
  ensureAnnounceCommand,
  handleAnnounceChatInput,
} from "./announce-command.js";
import {
  BAN_COMMAND_NAME,
  ensureBanCommand,
  handleBanChatInput,
  handleBanModalSubmit,
} from "./ban-command.js";
import {
  resolveCommandCategory,
  resolveCustomIdCategory,
} from "./ban-categories.js";
import {
  enforceBan,
  enforceBanForAutocomplete,
} from "./ban-check.js";
import {
  ensureBindCommand,
  handleBindButtonInteraction,
  handleBindChatInput,
  handleBindModalSubmit,
} from "./bind-command.js";
import {
  ensureCheckGameBansCommand,
  handleCheckGameBansChatInput,
} from "./checkgamebans-command.js";
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
  ensureSearchCommand,
  handleSearchAutocomplete,
  handleSearchChatInput,
} from "./search-command.js";
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
  ensureTrackCommand,
  handleTrackAutocomplete,
  handleTrackButtonInteraction,
  handleTrackChatInput,
  handleTrackModalSubmit,
  startTrackGroupPoller,
} from "./track-command.js";
import {
  ensureTransferCommand,
  handleTransferButtonInteraction,
  handleTransferChatInput,
  handleTransferModalSubmit,
} from "./transfer-command.js";
import {
  ensureViewCommand,
  handleViewButtonInteraction,
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
const config = loadConfig();
const backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
const statusWorkerID = `${config.workerId}-status-${shardID}`;
const statusRefreshIntervalMs = 60_000;

if (!token) {
  throw new Error("DISCORD_TOKEN is required for shard worker process");
}

const client = new Client({
  intents: [GatewayIntentBits.Guilds, GatewayIntentBits.GuildMembers, GatewayIntentBits.GuildVoiceStates],
  shards: [shardID],
  shardCount,
});

registerUserGuildSync(client);

async function updateSteamAccountPresence() {
  const response = await backend.steamAccountCount(statusWorkerID);
  const formattedCount = response.count.toLocaleString("en-US");
  await client.user?.setActivity(`${formattedCount} users`, { type: ActivityType.Watching });
}

client.once("clientReady", () => {
  console.log(`[bot] shard ${shardID}/${shardCount} ready as ${client.user?.tag}`);

  updateSteamAccountPresence().catch((error) => {
    console.error("[bot] failed to update steam account presence", error);
  });
  setInterval(() => {
    updateSteamAccountPresence().catch((error) => {
      console.error("[bot] failed to refresh steam account presence", error);
    });
  }, statusRefreshIntervalMs);

  Promise.allSettled([
    ensureAdminCommand(client, shardID),
    ensureBindCommand(client, shardID),
    ensureCheckGameBansCommand(client, shardID),
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
    ensureBanCommand(client, shardID),
    ensureTransferCommand(client, shardID),
    ensureSearchCommand(client, shardID),
    ensureTrackCommand(client, shardID),
    rehydratePersistedWipeReacts(client),
  ]).then((results) => {
    for (const result of results) {
      if (result.status === "rejected") {
        console.error("[bot] command registration failed", result.reason);
      }
    }
    startWipeReactScheduler(client);
    startTrackedServerLogPoller(client);
    startTrackGroupPoller(client);
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
      const category = resolveCommandCategory(
        interaction.commandName,
        interaction.options.getSubcommand(false),
      );
      if (await enforceBanForAutocomplete(interaction, category)) {
        return;
      }

      if (await handleCompareAutocomplete(interaction)) {
        return;
      }

      if (await handleWipeReactAutocomplete(interaction)) {
        return;
      }

      if (await handleServerAutocomplete(interaction)) {
        return;
      }

      if (await handleSearchAutocomplete(interaction)) {
        return;
      }

      if (await handleTrackAutocomplete(interaction)) {
        return;
      }
    }

    if (interaction.isChatInputCommand()) {
      if (interaction.commandName !== BAN_COMMAND_NAME) {
        const category = resolveCommandCategory(
          interaction.commandName,
          interaction.options.getSubcommand(false),
        );
        if (await enforceBan(interaction, category)) {
          return;
        }
      }

      if (await handleBanChatInput(interaction)) {
        return;
      }

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

      if (await handleCheckGameBansChatInput(interaction)) {
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

      if (await handleSearchChatInput(interaction)) {
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

      if (await handleTransferChatInput(interaction)) {
        return;
      }

      if (await handleBindChatInput(interaction)) {
        return;
      }

      if (await handleWipeReactChatInput(interaction)) {
        return;
      }

      if (await handleTrackChatInput(interaction)) {
        return;
      }

      await handleSettingsChatInput(interaction);
      return;
    }

    if (interaction.isStringSelectMenu()) {
      const category = resolveCustomIdCategory(interaction.customId);
      if (category && (await enforceBan(interaction, category))) {
        return;
      }

      await handleSettingsSelect(interaction);
      return;
    }

    if (interaction.isModalSubmit()) {
      if (await handleBanModalSubmit(interaction)) {
        return;
      }

      const modalCategory = resolveCustomIdCategory(interaction.customId);
      if (modalCategory && (await enforceBan(interaction, modalCategory))) {
        return;
      }

      if (await handleBindModalSubmit(interaction)) {
        return;
      }

      if (await handleWipeReactModalSubmit(interaction)) {
        return;
      }

      if (await handleServerModalSubmit(interaction)) {
        return;
      }

      if (await handleTransferModalSubmit(interaction)) {
        return;
      }

      if (await handleTrackModalSubmit(interaction)) {
        return;
      }

      await handleSettingsModalSubmit(interaction);
      return;
    }

    if (interaction.isButton()) {
      const buttonCategory = resolveCustomIdCategory(interaction.customId);
      if (buttonCategory && (await enforceBan(interaction, buttonCategory))) {
        return;
      }

      if (await handleBindButtonInteraction(interaction)) {
        return;
      }

      if (await handlePlayerButtonInteraction(interaction)) {
        return;
      }

      if (await handleViewButtonInteraction(interaction)) {
        return;
      }

      if (await handleTransferButtonInteraction(interaction)) {
        return;
      }

      if (await handleTrackButtonInteraction(interaction)) {
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
