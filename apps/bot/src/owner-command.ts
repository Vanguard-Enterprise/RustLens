import type { AutocompleteInteraction, ChatInputCommandInteraction, Client, ModalSubmitInteraction } from "discord.js";
import { ActionRowBuilder, EmbedBuilder, MessageFlags, ModalBuilder, SlashCommandBuilder, TextInputBuilder, TextInputStyle } from "discord.js";
import { BackendClient, type OwnerProxy, type OwnerStatsResponse, type ShardStatSnapshot } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getOwnerGuildID, isAuthorizedOwner, isOwnerCommandConfigured } from "./owner-guard.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const OWNER_COMMAND_NAME = "owner";
export const OWNER_STATS_SUBCOMMAND_NAME = "stats";

const OWNER_STATS_VIEW_SUBCOMMAND_NAME = "view";
const OWNER_PROXY_GROUP_NAME = "proxy";
const OWNER_PROXY_ADD_SUBCOMMAND_NAME = "add";
const OWNER_PROXY_LIST_SUBCOMMAND_NAME = "list";
const OWNER_PROXY_REMOVE_SUBCOMMAND_NAME = "remove";
const OWNER_PROXY_OPTION_NAME = "proxy";
const OWNER_PROXY_ADD_MODAL_CUSTOM_ID = "owner_proxy_add_modal";
const OWNER_PROXY_ADD_FIELD_CUSTOM_ID = "owner_proxy_add_endpoints";

const ownerCommand = new SlashCommandBuilder()
  .setName(OWNER_COMMAND_NAME)
  .setDescription("Owner-only RustLens utilities")
  .setDMPermission(false)
  .addSubcommandGroup((group) =>
    group
      .setName(OWNER_STATS_SUBCOMMAND_NAME)
      .setDescription("Show aggregated bot statistics")
      .addSubcommand((subcommand) =>
        subcommand
          .setName(OWNER_STATS_VIEW_SUBCOMMAND_NAME)
          .setDescription("Show aggregated bot statistics"),
      ),
  )
  .addSubcommandGroup((group) =>
    group
      .setName(OWNER_PROXY_GROUP_NAME)
      .setDescription("Manage outbound proxies")
      .addSubcommand((subcommand) =>
        subcommand
          .setName(OWNER_PROXY_ADD_SUBCOMMAND_NAME)
          .setDescription("Add encrypted outbound proxies"),
      )
      .addSubcommand((subcommand) =>
        subcommand
          .setName(OWNER_PROXY_LIST_SUBCOMMAND_NAME)
          .setDescription("List configured outbound proxies"),
      )
      .addSubcommand((subcommand) =>
        subcommand
          .setName(OWNER_PROXY_REMOVE_SUBCOMMAND_NAME)
          .setDescription("Remove an outbound proxy")
          .addStringOption((option) =>
            option
              .setName(OWNER_PROXY_OPTION_NAME)
              .setDescription("Proxy to remove")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      ),
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
  const guild = await client.guilds.fetch(guildId);

  const globalCommands = await client.application.commands.fetch();
  const existingGlobal = globalCommands.find((command) => command.name === data.name);
  if (existingGlobal) {
    await client.application.commands.delete(existingGlobal.id);
  }

  const commands = await guild.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);
  if (existing) {
    await guild.commands.delete(existing.id);
  }
  await guild.commands.create(data);

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

function proxyChoiceLabel(proxy: OwnerProxy) {
  const host = proxy.endpoint.replace(/^https?:\/\//, "");
  return host.length <= 100 ? host : `${host.slice(0, 97)}...`;
}

function buildOwnerProxyListEmbed(proxies: OwnerProxy[]) {
  const description = proxies.length === 0
    ? "No outbound proxies configured."
    : proxies.map((proxy, index) => `${index + 1}. \`${proxyChoiceLabel(proxy)}\``).join("\n");

  return new EmbedBuilder()
    .setTitle("Outbound Proxies")
    .setDescription(description.length <= 4096 ? description : `${description.slice(0, 4093)}...`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildOwnerProxyAddModal() {
  return new ModalBuilder()
    .setCustomId(OWNER_PROXY_ADD_MODAL_CUSTOM_ID)
    .setTitle("Add Outbound Proxies")
    .addComponents(
      new ActionRowBuilder<TextInputBuilder>().addComponents(
        new TextInputBuilder()
          .setCustomId(OWNER_PROXY_ADD_FIELD_CUSTOM_ID)
          .setLabel("Proxy URLs")
          .setStyle(TextInputStyle.Paragraph)
          .setRequired(true)
          .setPlaceholder("http://username:password@45.127.250.10:5619,http://username:password@82.22.249.124:6961"),
      ),
    );
}

export function parseOwnerProxyInput(input: string) {
  return Array.from(
    new Set(
      input
        .split(/[\s,]+/)
        .map((value) => value.trim())
        .filter((value) => value.length > 0),
    ),
  );
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

export async function handleOwnerAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== OWNER_COMMAND_NAME) {
    return false;
  }

  if (!isOwnerCommandConfigured() || !isAuthorizedOwner(interaction)) {
    await interaction.respond([]);
    return true;
  }

  const group = interaction.options.getSubcommandGroup(false);
  const subcommand = interaction.options.getSubcommand(false);
  if (group !== OWNER_PROXY_GROUP_NAME || subcommand !== OWNER_PROXY_REMOVE_SUBCOMMAND_NAME) {
    await interaction.respond([]);
    return true;
  }

  try {
    const focused = interaction.options.getFocused().toLowerCase();
    const { items } = await getBackend().listOwnerProxies(getWorkerID());
    const choices = items
      .filter((proxy) => proxyChoiceLabel(proxy).toLowerCase().includes(focused))
      .slice(0, 25)
      .map((proxy) => ({ name: proxyChoiceLabel(proxy), value: String(proxy.id) }));
    await interaction.respond(choices);
  } catch (error) {
    console.error("[bot] /owner proxy autocomplete failed", error);
    await interaction.respond([]);
  }

  return true;
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

  const group = interaction.options.getSubcommandGroup(false);
  const subcommand = interaction.options.getSubcommand(true);
  if (group === OWNER_PROXY_GROUP_NAME) {
    if (subcommand === OWNER_PROXY_ADD_SUBCOMMAND_NAME) {
      await interaction.showModal(buildOwnerProxyAddModal());
      return true;
    }

    await interaction.deferReply({ flags: MessageFlags.Ephemeral });

    try {
      if (subcommand === OWNER_PROXY_LIST_SUBCOMMAND_NAME) {
        const { items } = await getBackend().listOwnerProxies(getWorkerID());
        await interaction.editReply({ embeds: [buildOwnerProxyListEmbed(items)] });
        return true;
      }

      if (subcommand === OWNER_PROXY_REMOVE_SUBCOMMAND_NAME) {
        const rawID = interaction.options.getString(OWNER_PROXY_OPTION_NAME, true);
        const proxyID = Number(rawID);
        if (!Number.isInteger(proxyID) || proxyID <= 0) {
          await interaction.editReply({ content: "Select a proxy from autocomplete." });
          return true;
        }
        await getBackend().deleteOwnerProxy(getWorkerID(), proxyID);
        await interaction.editReply({ content: "Removed proxy." });
        return true;
      }
    } catch (error) {
      console.error("[bot] /owner proxy failed", error);
      await interaction.editReply({ content: "Failed to manage proxies." });
      return true;
    }
  }

  if (group !== OWNER_STATS_SUBCOMMAND_NAME || subcommand !== OWNER_STATS_VIEW_SUBCOMMAND_NAME) {
    await replyOwnerOnly(interaction, `Unknown /owner subcommand: ${group ? `${group} ` : ""}${subcommand}`);
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

export async function handleOwnerModalSubmit(interaction: ModalSubmitInteraction) {
  if (interaction.customId !== OWNER_PROXY_ADD_MODAL_CUSTOM_ID) {
    return false;
  }

  if (!interaction.inGuild()) {
    await interaction.reply({ content: "This command can only be used in a server.", flags: MessageFlags.Ephemeral });
    return true;
  }

  if (!isOwnerCommandConfigured()) {
    await interaction.reply({ content: "BOT_OWNER_GUILD_ID and BOT_OWNER_DISCORD_IDS must be configured before /owner can be used.", flags: MessageFlags.Ephemeral });
    return true;
  }

  if (!isAuthorizedOwner(interaction)) {
    await interaction.reply({ content: "Only the bot owners can use /owner commands.", flags: MessageFlags.Ephemeral });
    return true;
  }

  const endpoints = parseOwnerProxyInput(interaction.fields.getTextInputValue(OWNER_PROXY_ADD_FIELD_CUSTOM_ID));
  if (endpoints.length === 0) {
    await interaction.reply({ content: "Enter at least one proxy URL.", flags: MessageFlags.Ephemeral });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const added: OwnerProxy[] = [];
  const failed: string[] = [];
  for (const endpoint of endpoints) {
    try {
      added.push(await getBackend().createOwnerProxy(getWorkerID(), endpoint));
    } catch (error) {
      const status = typeof error === "object" && error !== null && "status" in error
        ? (error as { status?: unknown }).status
        : undefined;
      failed.push(status === 409 ? `${endpoint} (already exists)` : endpoint);
    }
  }

  const lines = [`Added ${added.length}/${endpoints.length} proxies.`];
  if (added.length > 0) {
    lines.push(`Saved: ${added.map((proxy) => `\`${proxyChoiceLabel(proxy)}\``).join(", ")}`);
  }
  if (failed.length > 0) {
    const detail = failed.map((endpoint) => `\`${endpoint.replace(/^https?:\/\//, "")}\``).join(", ");
    lines.push(`Failed: ${detail.length <= 1500 ? detail : `${detail.slice(0, 1497)}...`}`);
  }

  await interaction.editReply({ content: lines.join("\n") });
  return true;
}
