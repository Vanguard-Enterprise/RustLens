import type { Interaction } from "discord.js";
import { ActivityType, Client, GatewayIntentBits, MessageFlags } from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import {
  ensureAdminCommand,
  handleAdminChatInput,
} from "./commands/admin.js";
import {
  ensureAnnounceCommand,
  handleAnnounceChatInput,
} from "./commands/announce.js";
import {
  ensureAnalyzeCommand,
  handleAnalyzeAutocomplete,
  handleAnalyzeChatInput,
} from "./commands/analyze.js";
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
} from "./commands/bind.js";
import {
  ensureCheckGameBansCommand,
  handleCheckGameBansChatInput,
} from "./commands/checkgamebans.js";
import {
  ensureCreateCommand,
  handleCreateButtonInteraction,
  handleCreateChatInput,
} from "./commands/create.js";
import {
  ensureCompareCommand,
  handleCompareAutocomplete,
  handleCompareChatInput,
} from "./commands/compare.js";
import {
  ensureOnlineCommand,
  handleOnlineChatInput,
} from "./commands/online.js";
import {
  ensureManageCommand,
  handleManageAutocomplete,
  handleManageButtonInteraction,
  handleManageChatInput,
  handleManageModalSubmit,
  MANAGE_COMMAND_NAME,
} from "./commands/manage.js";
import {
  deferPlayerChatInput,
  deferPlayerButton,
  ensurePlayerCommand,
  handlePlayerButtonInteraction,
  handlePlayerChatInput,
  handlePlayerSelectInteraction,
  PLAYER_COMMAND_NAME,
} from "./commands/player.js";
import {
  ensureNicknameCommand,
  handleNicknameChatInput,
} from "./commands/nickname.js";
import {
  ensureSetSteamCommand,
  handleSetSteamChatInput,
  handleSetSteamModalSubmit,
} from "./commands/setsteam.js";
import {
  ensureSearchCommand,
  handleSearchAutocomplete,
  handleSearchChatInput,
} from "./commands/search.js";
import {
  ensureServerCommand,
  handleServerAutocomplete,
  handleServerChatInput,
} from "./commands/server.js";
import {
  ensureSettingsCommand,
  handleSettingsChatInput,
  handleSettingsModalSubmit,
  handleSettingsSelect,
} from "./commands/settings.js";
import {
  ensureSummonCommand,
  handleSummonChatInput,
} from "./commands/summon.js";
import {
  ensureTrackCommand,
  handleTrackAutocomplete,
  handleTrackButtonInteraction,
  handleTrackChatInput,
  handleTrackModalSubmit,
  handleTrackSelectInteraction,
  startTrackGroupPoller,
} from "./commands/track.js";
import {
  ensureTransferCommand,
  handleTransferButtonInteraction,
  handleTransferChatInput,
  handleTransferModalSubmit,
} from "./commands/transfer.js";
import {
  ensureViewCommand,
  handleViewButtonInteraction,
  handleViewChatInput,
} from "./commands/view.js";
import {
  ensureWipeReactCommand,
  handleWipeReactAutocomplete,
  handleWipeReactButtonInteraction,
  handleWipeReactChatInput,
  handleWipeReactModalSubmit,
  rehydratePersistedWipeReacts,
  startWipeReactScheduler,
} from "./commands/wipereact.js";
import { startTrackedServerLogPoller } from "./tracked-server-logs.js";
import { registerUserGuildSync } from "./user-guilds.js";
import { registerBotGuildSync } from "./bot-guilds.js";
import { startShardStatsPusher } from "./shard-stats.js";

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
registerBotGuildSync(client, shardID, shardCount);

client.on("error", (error) => {
  console.error("[bot] Discord client error", error);
});

client.on("shardError", (error, failedShardID) => {
  console.error("[bot] Discord shard error", { shardId: failedShardID, error });
});

async function updateSteamAccountPresence() {
  const response = await backend.steamAccountCount(statusWorkerID);
  const formattedCount = response.count.toLocaleString("en-US");
  await client.user?.setActivity(`${formattedCount} users`, { type: ActivityType.Watching });
}

const shutdownTimers: (NodeJS.Timeout | undefined)[] = [];

client.once("clientReady", () => {
  console.log(`[bot] shard ${shardID}/${shardCount} ready as ${client.user?.tag}`);

  shutdownTimers.push(startShardStatsPusher(client, shardID));

  let presenceRunning = false;
  const presenceTick = () => {
    if (presenceRunning) {
      return;
    }
    presenceRunning = true;
    updateSteamAccountPresence()
      .catch((error) => {
        console.error("[bot] failed to refresh steam account presence", error);
      })
      .finally(() => {
        presenceRunning = false;
      });
  };

  // Stagger initial fetch by shard so N shards don't all hit the backend at once.
  setTimeout(presenceTick, (shardID % 10) * 1_000);
  shutdownTimers.push(setInterval(presenceTick, statusRefreshIntervalMs));

  Promise.allSettled([
    ensureAdminCommand(client, shardID),
    ensureBindCommand(client, shardID),
    ensureCheckGameBansCommand(client, shardID),
    ensureCreateCommand(client, shardID),
    ensureCompareCommand(client, shardID),
    ensureWipeReactCommand(client, shardID),
    ensureSummonCommand(client, shardID),
    ensureAnnounceCommand(client, shardID),
    ensureAnalyzeCommand(client, shardID),
    ensureOnlineCommand(client, shardID),
    ensurePlayerCommand(client, shardID),
    ensureNicknameCommand(client, shardID),
    ensureSetSteamCommand(client, shardID),
    ensureSettingsCommand(client, shardID),
    ensureViewCommand(client, shardID),
    ensureServerCommand(client, shardID),
    ensureManageCommand(client, shardID),
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
    shutdownTimers.push(startWipeReactScheduler(client));
    shutdownTimers.push(startTrackedServerLogPoller(client));
    shutdownTimers.push(startTrackGroupPoller(client));
  });
});

function isUnknownInteractionError(error: unknown) {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }

  return (error as { code?: unknown }).code === 10062;
}

function isAlreadyAcknowledgedInteractionError(error: unknown) {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }

  return (error as { code?: unknown }).code === 40060;
}

function interactionKind(interaction: Interaction) {
  if (interaction.isChatInputCommand()) {
    return `/${interaction.commandName}`;
  }
  if (interaction.isButton()) {
    return `button:${interaction.customId}`;
  }
  if (interaction.isStringSelectMenu()) {
    return `select:${interaction.customId}`;
  }
  if (interaction.isModalSubmit()) {
    return `modal:${interaction.customId}`;
  }
  if (interaction.isAutocomplete()) {
    return `autocomplete:/${interaction.commandName}`;
  }

  return interaction.type;
}

function logUnknownInteraction(interaction: Interaction, error: unknown) {
  console.error("[bot] Discord interaction error: unknown interaction", {
    kind: interactionKind(interaction),
    id: interaction.id,
    ageMs: Date.now() - interaction.createdTimestamp,
    workerId: config.workerId,
    shardId: shardID,
    shardCount,
    pid: process.pid,
    error,
  });
}

function logAlreadyAcknowledgedInteraction(interaction: Interaction, error: unknown) {
  console.error("[bot] Discord interaction error: already acknowledged", {
    kind: interactionKind(interaction),
    id: interaction.id,
    ageMs: Date.now() - interaction.createdTimestamp,
    workerId: config.workerId,
    shardId: shardID,
    shardCount,
    pid: process.pid,
    error,
  });
}

client.on("interactionCreate", async (interaction) => {
  try {
    if (interaction.isAutocomplete()) {
      if (await handleManageAutocomplete(interaction)) {
        return;
      }

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

      if (await handleAnalyzeAutocomplete(interaction)) {
        return;
      }

      if (await handleTrackAutocomplete(interaction)) {
        return;
      }
    }

    if (interaction.isChatInputCommand()) {
      if (interaction.commandName !== MANAGE_COMMAND_NAME) {
        const category = resolveCommandCategory(
          interaction.commandName,
          interaction.options.getSubcommand(false),
        );
        if (await enforceBan(interaction, category)) {
          return;
        }
      }

      if (interaction.commandName === PLAYER_COMMAND_NAME) {
        if (!(await deferPlayerChatInput(interaction))) {
          return;
        }
      }

      if (await handleManageChatInput(interaction)) {
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

      if (await handleAnalyzeChatInput(interaction)) {
        return;
      }

      if (await handleViewChatInput(interaction)) {
        return;
      }

      if (await handleCheckGameBansChatInput(interaction)) {
        return;
      }

      if (await handleCreateChatInput(interaction)) {
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

      if (await handleTrackSelectInteraction(interaction)) {
        return;
      }

      if (await handlePlayerSelectInteraction(interaction)) {
        return;
      }

      await handleSettingsSelect(interaction);
      return;
    }

    if (interaction.isModalSubmit()) {
      if (await handleManageModalSubmit(interaction)) {
        return;
      }

      const modalCategory = resolveCustomIdCategory(interaction.customId);
      if (modalCategory && (await enforceBan(interaction, modalCategory))) {
        return;
      }

      if (await handleBindModalSubmit(interaction)) {
        return;
      }

      if (await handleSetSteamModalSubmit(interaction)) {
        return;
      }

      if (await handleWipeReactModalSubmit(interaction)) {
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
      if (buttonCategory && interaction.customId.startsWith("player:")) {
        if (!(await deferPlayerButton(interaction))) {
          return;
        }
      }
      if (buttonCategory && (await enforceBan(interaction, buttonCategory))) {
        return;
      }

      if (await handleBindButtonInteraction(interaction)) {
        return;
      }

      if (await handleCreateButtonInteraction(interaction)) {
        return;
      }

      if (await handlePlayerButtonInteraction(interaction)) {
        return;
      }

      if (await handleManageButtonInteraction(interaction)) {
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
    if (isUnknownInteractionError(error)) {
      logUnknownInteraction(interaction, error);
      return;
    }

    if (isAlreadyAcknowledgedInteractionError(error)) {
      logAlreadyAcknowledgedInteraction(interaction, error);
      return;
    }

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
        console.error("[bot] failed to send interaction error reply", replyError);
      }
    }
  }
});

client.login(token).catch((error) => {
  console.error(`[bot] shard ${shardID} login failed`, error);
  process.exit(1);
});

async function shutdown() {
  for (const timer of shutdownTimers) {
    if (timer) {
      clearInterval(timer);
    }
  }
  shutdownTimers.length = 0;
  await client.destroy();
  process.exit(0);
}

process.on("SIGINT", () => {
  void shutdown();
});

process.on("SIGTERM", () => {
  void shutdown();
});
