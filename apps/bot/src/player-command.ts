import type { ButtonInteraction, ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient, type SteamAnalytics } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import { buildProfileButtons, buildUserEmbed, resolvePlayerProfileForGuild, fetchSteamProfileForView } from "./view-command.js";

export const PLAYER_COMMAND_NAME = "player";

const PLAYER_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
} as const;

const playerCommand = new SlashCommandBuilder()
  .setName(PLAYER_COMMAND_NAME)
  .setDescription("Show a linked player's profile in this guild")
  .setDMPermission(false)
  .addStringOption((opt) =>
    opt
      .setName("identifier")
      .setDescription("Steam ID or BattleMetrics ID")
      .setRequired(true),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-player`;
  }
  return { backend: _backend, workerId: _workerID! };
}

export async function ensurePlayerCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = playerCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /player command (global)");
}

export async function handlePlayerChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== PLAYER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const identifier = interaction.options.getString("identifier", true);
  await interaction.deferReply();

  const result = await resolvePlayerProfileForGuild(interaction.guildId!, identifier, { allowDiscordLookup: false });
  if (!result.ok) {
    await interaction.editReply({ content: result.error });
    return true;
  }

  const profile = await fetchSteamProfileForView(result.steamId);
  await interaction.editReply({ embeds: [buildUserEmbed(result.steamId, profile)], components: buildProfileButtons(result.steamId) });
  return true;
}

export async function handlePlayerButtonInteraction(interaction: ButtonInteraction) {
  if (!interaction.customId.startsWith("player:analytics:")) {
    return false;
  }

  const steamId = interaction.customId.slice("player:analytics:".length);
  if (!/^\d{17}$/.test(steamId)) {
    await interaction.reply({
      content: "Invalid analytics button.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  try {
    const { backend, workerId } = getBackend();
    const analytics = await backend.getSteamAnalytics(workerId, steamId);
    await interaction.editReply({ embeds: [buildAnalyticsEmbed(analytics)] });
  } catch (error) {
    console.error("[bot] failed to load player analytics", error);
    await interaction.editReply({ content: "Failed to load analytics for this player." });
  }

  return true;
}

export function buildAnalyticsEmbed(analytics: SteamAnalytics) {
  const title = analytics.bmName ? `Analytics - ${analytics.bmName}` : "Analytics";
  const embed = new EmbedBuilder()
    .setTitle(title)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (analytics.avatarImage) {
    embed.setThumbnail(analytics.avatarImage);
  }

  const info: string[] = [];
  info.push(`${PLAYER_EMOJIS.steam} [${analytics.steamId}](https://steamcommunity.com/profiles/${analytics.steamId})`);
  if (analytics.bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${analytics.bmId}](https://www.battlemetrics.com/players/${analytics.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  if (analytics.gaps.length > 0) {
    const gapLines = analytics.gaps.map((gap) => `<t:${gap.fromUnix}:R> - <t:${gap.toUnix}:R>`);
    embed.addFields({ name: "**Did not play:**", value: gapLines.join("\n") });
  }

  if (analytics.totalGames !== null && analytics.topGames.length > 0) {
    const gameLines = analytics.topGames.map((game) => `${game.name} **${game.hours}** hours`);
    embed.addFields({
      name: `Games owned: ${analytics.totalGames}`,
      value: gameLines.join("\n"),
    });
  }

  if (analytics.topServers.length > 0) {
    const serverLines = analytics.topServers.map((server) => `${server.name} **${server.hours}** hours`);
    embed.addFields({
      name: `Servers played: ${analytics.totalServers}`,
      value: serverLines.join("\n"),
    });
  }

  return embed;
}

export const __testables = {
  buildAnalyticsEmbed,
};
