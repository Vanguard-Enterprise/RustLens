import type { ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient, type OwnerStatsResponse, type ShardStatSnapshot } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getOwnerGuildID, isAuthorizedOwner, isOwnerCommandConfigured } from "./owner-guard.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const OWNER_COMMAND_NAME = "owner";
export const OWNER_STATS_SUBCOMMAND_NAME = "stats";

const ownerCommand = new SlashCommandBuilder()
  .setName(OWNER_COMMAND_NAME)
  .setDescription("Owner-only RustLens utilities")
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(OWNER_STATS_SUBCOMMAND_NAME)
      .setDescription("Show aggregated bot statistics"),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _workerID = `${config.workerId}-owner-cmd`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

export async function ensureOwnerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  if (!isOwnerCommandConfigured()) {
    console.warn("[bot] skipping /owner command registration because BOT_OWNER_GUILD_ID or BOT_OWNER_DISCORD_IDS is missing");
    return;
  }

  const guildId = getOwnerGuildID();
  const data = ownerCommand.toJSON();

  const globalCommands = await client.application.commands.fetch();
  const existingGlobal = globalCommands.find((command) => command.name === data.name);
  if (existingGlobal) {
    await client.application.commands.delete(existingGlobal.id);
  }

  const commands = await client.application.commands.fetch({ guildId });
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data, guildId);
  } else {
    await client.application.commands.create(data, guildId);
  }

  console.log(`[bot] ensured /owner command in guild ${guildId}`);
}

function formatNumber(value: number) {
  return value.toLocaleString("en-US");
}

function formatBytes(bytes: number) {
  if (bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let size = bytes;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const rounded = size >= 100 ? size.toFixed(0) : size.toFixed(1);
  return `${rounded} ${units[unitIndex]}`;
}

function formatUptime(uptimeSec: number) {
  if (uptimeSec <= 0) {
    return "—";
  }
  const days = Math.floor(uptimeSec / 86_400);
  const hours = Math.floor((uptimeSec % 86_400) / 3600);
  const minutes = Math.floor((uptimeSec % 3600) / 60);

  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  return `${minutes}m`;
}

function summarizeShards(shards: ShardStatSnapshot[], targetShards: number) {
  if (shards.length === 0) {
    return `Shards: 0/${targetShards} reporting`;
  }

  const ready = shards.filter((shard) => shard.status === "ready");
  const degraded = shards.filter((shard) => shard.status !== "ready");

  const avgPing = ready.length > 0
    ? Math.round(ready.reduce((sum, shard) => sum + shard.pingMs, 0) / ready.length)
    : 0;

  const lines: string[] = [];
  lines.push(`Shards: ${ready.length}/${targetShards} ready · ⌀ ping ${avgPing}ms`);

  if (degraded.length > 0) {
    const detail = degraded
      .map((shard) => `shard ${shard.shardId} ${shard.status}`)
      .join(", ");
    lines.push(`⚠ ${detail}`);
  }

  return lines.join("\n");
}

export function buildOwnerStatsEmbed(stats: OwnerStatsResponse) {
  const allShards = stats.workers.flatMap((worker) => worker.shards);
  const totalRSS = stats.workers.reduce((sum, worker) => sum + worker.rssBytes, 0);
  const maxUptime = stats.workers.reduce((max, worker) => Math.max(max, worker.uptimeSec), 0);

  const embed = new EmbedBuilder()
    .setTitle("Bot Statistics")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields(
      {
        name: "📊 Servers",
        value: formatNumber(stats.guildCount),
        inline: true,
      },
      {
        name: "👥 Total Members",
        value: formatNumber(stats.memberCount),
        inline: true,
      },
      {
        name: "⚙️ Configured Guilds",
        value: formatNumber(stats.configuredGuilds),
        inline: true,
      },
      {
        name: "🔗 Linked Steam IDs",
        value: formatNumber(stats.steamAccountCount),
        inline: true,
      },
      {
        name: "🔒 Hard Links",
        value: formatNumber(stats.hardLinkCount),
        inline: true,
      },
      {
        name: "👤 Unique Linked Users",
        value: formatNumber(stats.uniqueLinkedUsers),
        inline: true,
      },
      {
        name: "🛰️ Tracked Servers",
        value: formatNumber(stats.trackedServerCount),
        inline: true,
      },
      {
        name: "🟢 Online Tracked",
        value: formatNumber(stats.onlineTrackedPlayers),
        inline: true,
      },
      {
        name: "⏰ Active Wipe Reacts",
        value: formatNumber(stats.activeWipeReacts),
        inline: true,
      },
      {
        name: "Workers",
        value: [
          summarizeShards(allShards, stats.targetShards),
          `Uptime: ${formatUptime(maxUptime)} · Memory: ${formatBytes(totalRSS)}`,
        ].join("\n"),
        inline: false,
      },
    );

  return embed;
}

async function replyOwnerOnly(interaction: ChatInputCommandInteraction, content: string) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content });
    return;
  }
  await interaction.reply({ content, flags: MessageFlags.Ephemeral });
}

export async function handleOwnerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== OWNER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyOwnerOnly(interaction, "This command can only be used in a server.");
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await replyOwnerOnly(
      interaction,
      "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /owner can be used.",
    );
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await replyOwnerOnly(interaction, "Only the bot owners can use /owner commands.");
    return true;
  }

  const subcommand = interaction.options.getSubcommand(true);
  if (subcommand !== OWNER_STATS_SUBCOMMAND_NAME) {
    await replyOwnerOnly(interaction, `Unknown /owner subcommand: ${subcommand}`);
    return true;
  }

  await interaction.deferReply();

  try {
    const stats = await getBackend().ownerStats(getWorkerID());
    const embed = buildOwnerStatsEmbed(stats);
    await interaction.editReply({ embeds: [embed] });
  } catch (error) {
    console.error("[bot] /owner stats failed", error);
    await interaction.editReply({ content: "Failed to load bot statistics." });
  }

  return true;
}
