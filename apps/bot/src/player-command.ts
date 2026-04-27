import type { ButtonInteraction, ChatInputCommandInteraction, Client, User } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import {
  BackendClient,
  type SteamAccount,
  type SteamAnalytics,
  type SteamDiscordLink,
} from "./backend-client.js";
import { extractBmID } from "./bm-input.js";
import { loadConfig } from "./config.js";
import {
  RUSTLENS_EMBED_COLOR,
  RUSTLENS_EMBED_ICON_URL,
  buildWebsiteAdminOnlyEmbed,
} from "./shared.js";
import {
  resolveSteamInputErrorMessage,
  resolveSteamProfileInput,
} from "./steam-input.js";
import { buildProfileButtons, buildUserEmbed, fetchSteamProfileForView } from "./view-command.js";
import { isWebsiteAdmin } from "./website-admin.js";

export const PLAYER_COMMAND_NAME = "player";

const PLAYER_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
} as const;

const steamIDPattern = /^\d{17}$/;

const playerCommand = new SlashCommandBuilder()
  .setName(PLAYER_COMMAND_NAME)
  .setDescription("Look up any player by Steam ID or BattleMetrics ID")
  .setContexts(InteractionContextType.Guild, InteractionContextType.BotDM, InteractionContextType.PrivateChannel)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall, ApplicationIntegrationType.UserInstall)
  .addStringOption((opt) =>
    opt
      .setName("identifier")
      .setDescription("Steam ID, BattleMetrics ID/URL, or steamcommunity.com profile URL")
      .setRequired(false),
  )
  .addUserOption((opt) =>
    opt
      .setName("discord_user")
      .setDescription("Discord user to look up (website admins only)")
      .setRequired(false),
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

  const identifier = interaction.options.getString("identifier", false)?.trim() ?? "";
  const discordUser = interaction.options.getUser("discord_user", false);

  if (!identifier && !discordUser) {
    await interaction.reply({
      content: "Provide either an identifier or a discord user.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (discordUser) {
    if (!(await isWebsiteAdmin(interaction.user.id))) {
      await interaction.reply({
        embeds: [buildWebsiteAdminOnlyEmbed()],
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }

    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
    const embed = await buildDiscordLinksEmbed(discordUser);
    await interaction.editReply({ embeds: [embed] });
    return true;
  }

  await interaction.deferReply();

  const lookupIdentifier = await resolveLookupIdentifier(identifier);
  if (!lookupIdentifier.ok) {
    await interaction.editReply({ content: lookupIdentifier.error });
    return true;
  }

  const account = await lookupAccountByIdentifier(lookupIdentifier.identifier);
  if (!account) {
    await interaction.editReply({
      content: "No record found for that Steam ID, BattleMetrics ID/URL, or Steam profile URL.",
    });
    return true;
  }

  const profileResult = await fetchPlayerProfile(account.steamId);
  if (profileResult.privateProfile) {
    await interaction.editReply({ embeds: [buildPrivateProfileEmbed(account.steamId, account.bmId)] });
    return true;
  }

  await interaction.editReply({
    embeds: [buildUserEmbed(account.steamId, profileResult.profile)],
    components: buildProfileButtons(account.steamId),
  });
  return true;
}

async function fetchPlayerProfile(steamId: string) {
  const { backend, workerId } = getBackend();

  try {
    return { privateProfile: false as const, profile: await backend.getSteamProfile(workerId, steamId) };
  } catch (error) {
    if (isBmPrivateProfileError(error)) {
      return { privateProfile: true as const, profile: null };
    }

    return { privateProfile: false as const, profile: await fetchSteamProfileForView(steamId) };
  }
}

function isBmPrivateProfileError(error: unknown) {
  if (!error || typeof error !== "object") {
    return false;
  }

  const status = "status" in error ? error.status : undefined;
  const message = "message" in error && typeof error.message === "string" ? error.message : "";
  return status === 404 && message.includes("battlemetrics profile private");
}

function buildPrivateProfileEmbed(steamId: string, bmId: string | null) {
  const embed = new EmbedBuilder()
    .setTitle("Error")
    .setDescription("BM profile private")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  const info: string[] = [
    `${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
  ];
  if (bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${bmId}](https://www.battlemetrics.com/rcon/players/${bmId})`);
  }

  embed.addFields({ name: "Information", value: info.join("\n") });
  return embed;
}

async function resolveLookupIdentifier(identifier: string) {
  const { backend, workerId } = getBackend();

  try {
    const steamId = await resolveSteamProfileInput(
      identifier,
      (input) => backend.resolveSteamID(workerId, input),
    );
    return { ok: true as const, identifier: steamId ?? identifier.trim() };
  } catch (error) {
    return { ok: false as const, error: resolveSteamInputErrorMessage(error) };
  }
}

async function lookupAccountByIdentifier(identifier: string): Promise<SteamAccount | null> {
  const { backend, workerId } = getBackend();

  if (steamIDPattern.test(identifier)) {
    try {
      return await backend.getSteamAccount(workerId, identifier);
    } catch {
      return null;
    }
  }

  const bmID = extractBmID(identifier);
  if (bmID) {
    try {
      return await backend.getSteamAccountByBmID(workerId, bmID);
    } catch {
      return null;
    }
  }

  return null;
}

export async function handlePlayerButtonInteraction(interaction: ButtonInteraction) {
  if (interaction.customId.startsWith("player:analytics:")) {
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

  if (interaction.customId.startsWith("player:links:")) {
    const steamId = interaction.customId.slice("player:links:".length);
    if (!/^\d{17}$/.test(steamId)) {
      await interaction.reply({
        content: "Invalid links button.",
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }

    await interaction.deferReply({ flags: MessageFlags.Ephemeral });

    const isAdmin = await isWebsiteAdmin(interaction.user.id);
    try {
      const embed = await buildSteamLinksEmbed(steamId, isAdmin);
      await interaction.editReply({ embeds: [embed] });
    } catch (error) {
      console.error("[bot] failed to load steam links", error);
      await interaction.editReply({ content: "Failed to load links for this player." });
    }

    return true;
  }

  return false;
}

async function buildSteamLinksEmbed(steamId: string, isAdmin: boolean): Promise<EmbedBuilder> {
  const { backend, workerId } = getBackend();

  const [account, linksResponse] = await Promise.all([
    backend.getSteamAccount(workerId, steamId).catch(() => null),
    backend.getLinksBySteamID(workerId, steamId).catch(() => ({ items: [] as SteamDiscordLink[] })),
  ]);

  const links = linksResponse.items;
  const { strong, soft } = partitionLinksByDiscord(links);

  const title = account?.bmName ? `Links - ${account.bmName}` : "Links";
  const embed = new EmbedBuilder()
    .setTitle(title)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (account?.avatarImage) {
    embed.setThumbnail(account.avatarImage);
  }

  const info: string[] = [
    `${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
  ];
  if (account?.bmId) {
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${account.bmId}](https://www.battlemetrics.com/rcon/players/${account.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  embed.addFields({
    name: "Strong links",
    value: strong.length > 0
      ? strong.map((id) => `> * <@${id}>`).join("\n")
      : "> * None",
  });

  if (isAdmin) {
    embed.addFields({
      name: "Soft links",
      value: soft.length > 0
        ? soft.map((id) => `> * <@${id}>`).join("\n")
        : "> * None",
    });
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid\n> * A **soft link** is a link that is usually valid, but can also be invalid",
    });
  } else {
    embed.addFields({
      name: "What do these mean?",
      value: "> * A **strong link** is a link that is certainly valid",
    });
  }

  return embed;
}

function partitionLinksByDiscord(links: readonly SteamDiscordLink[]) {
  const strongDiscordIds = new Set<string>();
  for (const link of links) {
    if (link.type === "hard") {
      strongDiscordIds.add(link.discordId);
    }
  }
  const softDiscordIds = new Set<string>();
  for (const link of links) {
    if (link.type === "soft" && !strongDiscordIds.has(link.discordId)) {
      softDiscordIds.add(link.discordId);
    }
  }
  return {
    strong: [...strongDiscordIds],
    soft: [...softDiscordIds],
  };
}

async function ensureSteamAccountWithBm(steamId: string): Promise<SteamAccount | null> {
  const { backend, workerId } = getBackend();
  try {
    const account = await backend.getSteamAccount(workerId, steamId);
    if (account.bmId) {
      return account;
    }
    // Try the same fallback /player uses — the backend lazily backfills bm_id
    // through the steam profile endpoint.
    try {
      await backend.getSteamProfile(workerId, steamId);
    } catch {
      // ignore — return what we have
    }
    try {
      return await backend.getSteamAccount(workerId, steamId);
    } catch {
      return account;
    }
  } catch {
    return null;
  }
}

async function buildDiscordLinksEmbed(discordUser: User): Promise<EmbedBuilder> {
  const { backend, workerId } = getBackend();

  const linksResponse = await backend
    .getLinksByDiscordID(workerId, discordUser.id)
    .catch(() => ({ items: [] as SteamDiscordLink[] }));

  const links = linksResponse.items;
  const { strongSteamIds, softSteamIds } = partitionLinksBySteam(links);

  const [strongAccounts, softAccounts] = await Promise.all([
    Promise.all(strongSteamIds.map((sid) => ensureSteamAccountWithBm(sid))),
    Promise.all(softSteamIds.map((sid) => ensureSteamAccountWithBm(sid))),
  ]);

  const discordName = discordUser.displayName ?? discordUser.username ?? discordUser.tag;
  const embed = new EmbedBuilder()
    .setTitle(`Links - ${discordName}`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  const avatarUrl = discordUser.displayAvatarURL({ size: 256 });
  if (avatarUrl) {
    embed.setThumbnail(avatarUrl);
  }

  embed.addFields({ name: "Information", value: `<@${discordUser.id}>` });
  embed.addFields({
    name: "Strong links",
    value: formatSteamAccountList(strongSteamIds, strongAccounts),
  });
  embed.addFields({
    name: "Soft links",
    value: formatSteamAccountList(softSteamIds, softAccounts),
  });
  embed.addFields({
    name: "What do these mean?",
    value: "> * A **strong link** is a link that is certainly valid\n> * A **soft link** is a link that is usually valid, but can also be invalid",
  });

  return embed;
}

function partitionLinksBySteam(links: readonly SteamDiscordLink[]) {
  const strongSteamIds = new Set<string>();
  for (const link of links) {
    if (link.type === "hard") {
      strongSteamIds.add(link.steamId);
    }
  }
  const softSteamIds = new Set<string>();
  for (const link of links) {
    if (link.type === "soft" && !strongSteamIds.has(link.steamId)) {
      softSteamIds.add(link.steamId);
    }
  }
  return {
    strongSteamIds: [...strongSteamIds],
    softSteamIds: [...softSteamIds],
  };
}

function formatSteamAccountList(
  steamIds: readonly string[],
  accounts: ReadonlyArray<SteamAccount | null>,
): string {
  if (steamIds.length === 0) {
    return "> * None";
  }

  const blocks: string[] = [];
  for (let i = 0; i < steamIds.length; i++) {
    const steamId = steamIds[i];
    const account = accounts[i];
    const lines: string[] = [
      `> *  ${PLAYER_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`,
    ];
    if (account?.bmId) {
      lines.push(`> *   ${PLAYER_EMOJIS.battlemetrics} [${account.bmId}](https://www.battlemetrics.com/rcon/players/${account.bmId})`);
    }
    blocks.push(lines.join("\n"));
  }
  return blocks.join("\n");
}

function formatGapDurationDays(fromUnix: number, toUnix: number) {
  const durationSeconds = Math.max(0, toUnix - fromUnix);
  const durationDays = Math.max(1, Math.round(durationSeconds / 86400));
  return `${durationDays} day${durationDays === 1 ? "" : "s"}`;
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
    info.push(`${PLAYER_EMOJIS.battlemetrics} [${analytics.bmId}](https://www.battlemetrics.com/rcon/players/${analytics.bmId})`);
  }
  embed.addFields({ name: "Information", value: info.join("\n") });

  if (analytics.gaps.length > 0) {
    const sortedGaps = [...analytics.gaps]
      .sort((a, b) => b.toUnix - a.toUnix)
      .slice(0, 10);
    const gapLines = sortedGaps.map((gap) =>
      `<t:${gap.fromUnix}:R> - <t:${gap.toUnix}:R> (${formatGapDurationDays(gap.fromUnix, gap.toUnix)})`
    );
    embed.addFields({ name: "**Did not play:**", value: gapLines.join("\n") });
  }

  if (analytics.totalGames !== null && analytics.topGames.length > 0) {
    const hasPlaytime = analytics.topGames.some((game) => game.hours > 0);
    const value = hasPlaytime
      ? analytics.topGames.map((game) => `> * ${game.name} **${game.hours}** hours`).join("\n")
      : "*Playtime is private*";
    embed.addFields({
      name: `Games owned: ${analytics.totalGames}`,
      value,
    });
  }

  if (analytics.topServers.length > 0) {
    const serverLines = analytics.topServers.map((server) => `> * ${server.name} **${server.hours}** hours`);
    embed.addFields({
      name: `Servers played: ${analytics.totalServers}`,
      value: serverLines.join("\n"),
    });
  }

  return embed;
}

export const __testables = {
  buildPrivateProfileEmbed,
  buildAnalyticsEmbed,
  isBmPrivateProfileError,
};
