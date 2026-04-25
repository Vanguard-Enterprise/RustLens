import type { ButtonInteraction, ChatInputCommandInteraction, Client } from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ButtonBuilder,
  ButtonStyle,
  EmbedBuilder,
  InteractionContextType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
  type GuildMember,
} from "discord.js";
import { BackendClient, type SteamAccount, type SteamProfile } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import {
  resolveSteamInputErrorMessage,
  resolveSteamProfileInput,
} from "./steam-input.js";

export const VIEW_COMMAND_NAME = "view";

const VIEW_USER_SUBCOMMAND = "user";
const VIEW_ALL_SUBCOMMAND = "all";
const VIEW_NOSTEAM_SUBCOMMAND = "nosteam";
const VIEW_ALL_PAGE_SIZE = 20;
const VIEW_ALL_PAGE_CUSTOM_ID_PREFIX = "view:all:page:";

const discordMentionPattern = /^<@!?(\d{16,20})>$/;
const VIEW_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
  discord: "<:discord:1496989877831729284>",
  vitality: "<:vitality:1444010131728629761>",
  atlas: "<:atlas:1444010442778480700>",
  vital: "<:vital:1444011141587271682>",
  modded: "<:modded:1487586362600456333>",
  official: "<:official:1487583814271242461>",
  build: "<:build:1487584253775450225>",
  rust: "<:rust:1444010153803382957>",
} as const;

const RISK_EMOJIS = {
  0: "<:risk_0:1487595959809609758>",
  1: "<:risk_1:1487595708881178734>",
  2: "<:risk_2:1487595710030417970>",
  3: "<:risk_3:1487595707627081758>",
  4: "<:risk_4:1487595713553502359>",
  5: "<:risk_5:1487595720956448949>",
  6: "<:risk_6:1487595718431739954>",
  7: "<:risk_7:1487595715952902254>",
  8: "<:risk_8:1487595711213076530>",
  9: "<:risk_9:1487595711972245549>",
  10: "<:risk_10:1487595719790432447>",
} as const;

const RISK_PERCENT_STEP = 9.0909090909;

const viewCommand = new SlashCommandBuilder()
  .setName(VIEW_COMMAND_NAME)
  .setDescription("View linked steam/discord accounts in this guild")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((sub) =>
    sub
      .setName(VIEW_USER_SUBCOMMAND)
      .setDescription("View linked accounts for a user or steam identifier")
      .addUserOption((opt) =>
        opt.setName("discord_user").setDescription("Discord user to look up").setRequired(false),
      )
      .addStringOption((opt) =>
        opt.setName("identifier").setDescription("Steam ID, BattleMetrics ID, or steamcommunity.com profile URL").setRequired(false),
      ),
  )
  .addSubcommand((sub) =>
    sub.setName(VIEW_ALL_SUBCOMMAND).setDescription("List all linked users in this guild"),
  )
  .addSubcommand((sub) =>
    sub.setName(VIEW_NOSTEAM_SUBCOMMAND).setDescription("List guild users without a linked Steam account"),
  );

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-view`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

async function fetchSteamAccount(steamId: string): Promise<SteamAccount | null> {
  try {
    return await getBackend().getSteamAccount(getWorkerID(), steamId);
  } catch {
    return null;
  }
}

async function fetchSteamAccountByBm(bmId: string): Promise<SteamAccount | null> {
  try {
    return await getBackend().getSteamAccountByBmID(getWorkerID(), bmId);
  } catch {
    return null;
  }
}

async function fetchSteamProfile(steamId: string): Promise<SteamProfile | null> {
  try {
    return await getBackend().getSteamProfile(getWorkerID(), steamId);
  } catch {
    return null;
  }
}

export async function fetchSteamProfileForView(steamId: string) {
  return fetchSteamProfile(steamId);
}

function normalizeDiscordIdentifier(identifier: string) {
  const trimmed = identifier.trim();
  const mentionMatch = discordMentionPattern.exec(trimmed);
  if (mentionMatch) {
    return mentionMatch[1];
  }

  return null;
}

function formatHours(hours: number | null | undefined) {
  return hours === null || hours === undefined ? "**—** hours" : `**${hours}** hours`;
}

function formatRustHours(hours: number | null | undefined) {
  if (hours === null || hours === undefined || hours === 0) {
    return "**private**";
  }
  return `**${hours}** hours`;
}

type RiskLevel = keyof typeof RISK_EMOJIS;

function riskEmoji(level: number) {
  const normalized = Math.max(0, Math.min(10, Math.round(level))) as RiskLevel;
  return RISK_EMOJIS[normalized];
}

function calculateSteamBmHourRisk(profile: SteamProfile | null) {
  const steamHours = profile?.hours.rust;
  const bmHours = profile?.hours.bm;
  if (steamHours === null || steamHours === undefined || steamHours === 0 || bmHours === undefined) {
    return null;
  }

  const difference = Math.max(0, steamHours - bmHours);
  if (difference === 0) {
    return { level: 0, difference };
  }

  const percentMissingFromSteam = (difference / steamHours) * 100;
  return { level: Math.min(10, Math.round(percentMissingFromSteam / RISK_PERCENT_STEP)), difference };
}

const SECONDS_PER_DAY = 86400;
const DAYS_PER_MONTH = 30;
const RECENT_BREAK_DAYS = 10;
const BREAK_WINDOW_DAYS = 365;

function calculateRecentBreaksRisk(profile: SteamProfile | null, now: Date = new Date()) {
  const gaps = profile?.nameGaps;
  if (!gaps) {
    return null;
  }

  const nowUnix = Math.floor(now.getTime() / 1000);
  const windowStart = nowUnix - BREAK_WINDOW_DAYS * SECONDS_PER_DAY;
  const recentCutoff = nowUnix - RECENT_BREAK_DAYS * SECONDS_PER_DAY;

  let totalRisk = 0;
  let breakCount = 0;

  for (const gap of gaps) {
    if (gap.toUnix < windowStart) {
      continue;
    }
    const durationDays = (gap.toUnix - gap.fromUnix) / SECONDS_PER_DAY;
    const months = Math.floor(durationDays / DAYS_PER_MONTH);
    if (months < 2) {
      continue;
    }
    breakCount += 1;
    const isRecent = gap.toUnix >= recentCutoff;
    totalRisk += isRecent ? months * 2 : months - 1;
  }

  return {
    level: Math.max(0, Math.min(10, totalRisk)),
    breaks: breakCount,
  };
}

function calculateServersPlayedRisk(profile: SteamProfile | null) {
  const servers = profile?.serversPlayed;
  if (servers === null || servers === undefined) {
    return null;
  }

  if (servers > 30) {
    return { level: 0, servers };
  }

  const level = Math.max(0, Math.min(10, Math.floor((30 - servers) / 3)));
  return { level, servers };
}

function calculateBmHoursRisk(profile: SteamProfile | null) {
  const bmHours = profile?.hours.bm;
  if (bmHours === null || bmHours === undefined) {
    return null;
  }

  if (bmHours < 25) {
    return { level: 10, hours: bmHours };
  }

  const level = Math.max(0, Math.min(10, 11 - Math.floor(bmHours / 25)));
  return { level, hours: bmHours };
}

function calculateAccountCreatedRisk(profile: SteamProfile | null, now: Date = new Date()) {
  const firstSeenUnix = profile?.firstSeenUnix;
  if (firstSeenUnix === null || firstSeenUnix === undefined) {
    return null;
  }

  const nowUnix = Math.floor(now.getTime() / 1000);
  const ageDays = Math.max(0, (nowUnix - firstSeenUnix) / SECONDS_PER_DAY);
  const level = Math.max(0, Math.min(10, 11 - Math.ceil(ageDays / 10)));
  return { level, firstSeenUnix };
}

function buildRiskFactorField(profile: SteamProfile | null, now: Date = new Date()) {
  const steamBmRisk = calculateSteamBmHourRisk(profile);
  const recentBreaksRisk = calculateRecentBreaksRisk(profile, now);
  const serversRisk = calculateServersPlayedRisk(profile);
  const bmHoursRisk = calculateBmHoursRisk(profile);
  const accountCreatedRisk = calculateAccountCreatedRisk(profile, now);

  const factors = [steamBmRisk, recentBreaksRisk, serversRisk, bmHoursRisk, accountCreatedRisk]
    .filter((factor) => factor !== null);
  const averageRisk = factors.length > 0
    ? factors.reduce((total, factor) => total + factor.level, 0) / factors.length
    : 0;

  const steamBmLine = steamBmRisk
    ? `${riskEmoji(steamBmRisk.level)} Steam → BM hour difference: **${steamBmRisk.difference}** hours`
    : `${riskEmoji(0)} Steam → BM hour difference: **private**`;

  const recentBreaksLine = recentBreaksRisk
    ? `${riskEmoji(recentBreaksRisk.level)} Recent breaks > 2 months: **${recentBreaksRisk.breaks}**`
    : `${riskEmoji(0)} Recent breaks > 2 months: **unknown**`;

  const serversLine = serversRisk
    ? `${riskEmoji(serversRisk.level)} Servers played: **${serversRisk.servers}**`
    : `${riskEmoji(0)} Servers played: **unknown**`;

  const bmHoursLine = bmHoursRisk
    ? `${riskEmoji(bmHoursRisk.level)} BM hours: **${bmHoursRisk.hours}**`
    : `${riskEmoji(0)} BM hours: **unknown**`;

  const accountCreatedLine = accountCreatedRisk
    ? `${riskEmoji(accountCreatedRisk.level)} Account created: <t:${accountCreatedRisk.firstSeenUnix}:R>`
    : `${riskEmoji(0)} Account created: **unknown**`;

  return {
    name: `Risk Factor ${riskEmoji(averageRisk)}`,
    value: [steamBmLine, recentBreaksLine, accountCreatedLine, serversLine, bmHoursLine].join("\n"),
  };
}

export function buildProfileButtons(steamId: string) {
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`player:analytics:${steamId}`)
        .setEmoji("📈")
        .setStyle(ButtonStyle.Secondary),
      new ButtonBuilder()
        .setCustomId(`player:links:${steamId}`)
        .setEmoji("🔗")
        .setStyle(ButtonStyle.Secondary),
    ),
  ];
}

type ViewAllPageData = {
  embed: EmbedBuilder;
  totalPages: number;
  page: number;
};

function buildViewAllPageButtons(ownerId: string, page: number, totalPages: number) {
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`${VIEW_ALL_PAGE_CUSTOM_ID_PREFIX}${page - 1}:${ownerId}`)
        .setEmoji("⬅️")
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(page <= 0),
      new ButtonBuilder()
        .setCustomId(`${VIEW_ALL_PAGE_CUSTOM_ID_PREFIX}${page + 1}:${ownerId}`)
        .setEmoji("➡️")
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(page >= totalPages - 1),
    ),
  ];
}

function buildViewAllPage(items: ReadonlyArray<{ discordId: string; steamId: string }>, accountMap: ReadonlyMap<string, SteamAccount>, page: number): ViewAllPageData {
  const totalPages = Math.max(1, Math.ceil(items.length / VIEW_ALL_PAGE_SIZE));
  const normalizedPage = Math.max(0, Math.min(page, totalPages - 1));
  const start = normalizedPage * VIEW_ALL_PAGE_SIZE;
  const pageItems = items.slice(start, start + VIEW_ALL_PAGE_SIZE);
  const description = pageItems.map((link) => {
    const acct = accountMap.get(link.steamId);
    let line = `<@${link.discordId}> - [${link.steamId}](https://steamcommunity.com/profiles/${link.steamId})`;
    if (acct?.bmId) {
      line += ` - [${acct.bmId}](https://www.battlemetrics.com/players/${acct.bmId})`;
    }
    return `> * ${line}`;
  }).join("\n");

  const embed = new EmbedBuilder()
    .setTitle("Links summary")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: `RustLens.com • Page ${normalizedPage + 1}/${totalPages}`, iconURL: RUSTLENS_EMBED_ICON_URL })
    .setDescription(description);

  return { embed, totalPages, page: normalizedPage };
}

function parseViewAllPageCustomId(customId: string) {
  if (!customId.startsWith(VIEW_ALL_PAGE_CUSTOM_ID_PREFIX)) {
    return null;
  }

  const remainder = customId.slice(VIEW_ALL_PAGE_CUSTOM_ID_PREFIX.length);
  const separatorIndex = remainder.indexOf(":");
  if (separatorIndex === -1) {
    return null;
  }

  const pageValue = Number(remainder.slice(0, separatorIndex));
  const ownerId = remainder.slice(separatorIndex + 1);
  if (!Number.isInteger(pageValue) || ownerId.length === 0) {
    return null;
  }

  return { page: pageValue, ownerId };
}

export function buildUserEmbed(
  steamId: string,
  profile: SteamProfile | null,
  discordId?: string,
  includeRiskFactor = true,
): EmbedBuilder {
  const embed = new EmbedBuilder()
    .setTitle(profile?.bmName || "Player Profile")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (profile?.avatarImage) {
    embed.setThumbnail(profile.avatarImage);
  }

  let info = `${VIEW_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`;
  if (profile?.bmId) {
    info += `\n${VIEW_EMOJIS.battlemetrics} [${profile.bmId}](https://www.battlemetrics.com/players/${profile.bmId})`;
  }
  if (discordId) {
    info += `\n${VIEW_EMOJIS.discord} <@${discordId}>`;
  }
  embed.addFields({ name: "Information", value: info });

  const hourFields = [
    { name: `${VIEW_EMOJIS.vitality} Vitality`, value: formatHours(profile?.hours.vitality), inline: true },
    { name: `${VIEW_EMOJIS.atlas} Atlas`, value: formatHours(profile?.hours.atlas), inline: true },
    { name: `${VIEW_EMOJIS.vital} Vital`, value: formatHours(profile?.hours.vital), inline: true },
    { name: `${VIEW_EMOJIS.modded} Modded`, value: formatHours(profile?.hours.modded), inline: true },
    { name: `${VIEW_EMOJIS.official} Official`, value: formatHours(profile?.hours.official), inline: true },
    { name: `${VIEW_EMOJIS.build} Build`, value: formatHours(profile?.hours.build), inline: true },
    {
      name: "\u{1F50D} First seen",
      value: profile?.firstSeenUnix ? `<t:${profile.firstSeenUnix}:R>` : "—",
      inline: true,
    },
    { name: `${VIEW_EMOJIS.battlemetrics} BM`, value: formatHours(profile?.hours.bm), inline: true },
    { name: `${VIEW_EMOJIS.rust} Rust`, value: formatRustHours(profile?.hours.rust), inline: true },
  ];

  for (const field of hourFields) {
    embed.addFields(field);
  }

  if (includeRiskFactor) {
    embed.addFields(buildRiskFactorField(profile));
  }

  return embed;
}

export type ResolvedPlayerProfile =
  | {
    ok: true;
    steamId: string;
    account: SteamAccount | null;
    discordId: string;
  }
  | {
    ok: false;
    error: string;
  };

export async function resolvePlayerProfileForGuild(
  guildId: string,
  identifier: string,
  options?: { allowDiscordLookup?: boolean },
): Promise<ResolvedPlayerProfile> {
  const backend = getBackend();
  const workerId = getWorkerID();
  const normalizedIdentifier = identifier.trim();
  const discordId = normalizeDiscordIdentifier(normalizedIdentifier);
  const allowDiscordLookup = options?.allowDiscordLookup ?? true;

  let steamProfileIdentifier: string | null = null;
  try {
    steamProfileIdentifier = await resolveSteamProfileInput(
      normalizedIdentifier,
      (input) => backend.resolveSteamID(workerId, input),
    );
  } catch (error) {
    return { ok: false, error: resolveSteamInputErrorMessage(error) };
  }

  if (allowDiscordLookup && discordId) {
    const { items } = await backend.listGuildLinksByDiscord(workerId, guildId, discordId);
    if (items.length === 0) {
      return { ok: false, error: `No linked accounts found for <@${discordId}>.` };
    }

    const steamId = items[0].steamId;
    const account = await fetchSteamAccount(steamId);
    return { ok: true, steamId, account, discordId };
  }

  let steamId: string | null = null;
  let account: SteamAccount | null = null;

  if (steamProfileIdentifier) {
    steamId = steamProfileIdentifier;
    account = await fetchSteamAccount(steamProfileIdentifier);
    const { items } = await backend.listGuildLinksBySteam(workerId, guildId, steamId);
    if (items.length > 0) {
      return { ok: true, steamId, account, discordId: items[0].discordId };
    }

    if (allowDiscordLookup && /^\d{16,20}$/.test(normalizedIdentifier)) {
      const { items: discordItems } = await backend.listGuildLinksByDiscord(workerId, guildId, normalizedIdentifier);
      if (discordItems.length > 0) {
        const linkedSteamId = discordItems[0].steamId;
        const linkedAccount = await fetchSteamAccount(linkedSteamId);
        return { ok: true, steamId: linkedSteamId, account: linkedAccount, discordId: normalizedIdentifier };
      }
    }

    return { ok: false, error: "That identifier is not linked to anyone in this guild." };
  } else if (allowDiscordLookup && /^\d{16,20}$/.test(normalizedIdentifier)) {
    const { items } = await backend.listGuildLinksByDiscord(workerId, guildId, normalizedIdentifier);
    if (items.length === 0) {
      return { ok: false, error: `No linked accounts found for <@${normalizedIdentifier}>.` };
    }

    const linkedSteamId = items[0].steamId;
    const linkedAccount = await fetchSteamAccount(linkedSteamId);
    return { ok: true, steamId: linkedSteamId, account: linkedAccount, discordId: normalizedIdentifier };
  } else {
    account = await fetchSteamAccountByBm(normalizedIdentifier);
    if (account) {
      steamId = account.steamId;
    }
  }

  if (!steamId) {
    return { ok: false, error: "No steam account found for that identifier." };
  }

  const { items } = await backend.listGuildLinksBySteam(workerId, guildId, steamId);
  if (items.length === 0) {
    return { ok: false, error: "That identifier is not linked to anyone in this guild." };
  }

  return { ok: true, steamId, account, discordId: items[0].discordId };
}

function buildNoSteamEmbed(userIds: readonly string[]) {
  let description = userIds.length > 0
    ? userIds.map((userId) => `> * <@${userId}>`).join("\n")
    : "> * None";
  if (description.length > 4096) {
    description = `${description.slice(0, 4093)}...`;
  }
  return new EmbedBuilder()
    .setTitle("Unlinked Users")
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function sortMembers(left: GuildMember, right: GuildMember) {
  const leftName = left.displayName.toLocaleLowerCase();
  const rightName = right.displayName.toLocaleLowerCase();
  if (leftName !== rightName) {
    return leftName.localeCompare(rightName);
  }

  return left.id.localeCompare(right.id);
}

export async function ensureViewCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = viewCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /view command (global)");
}

export async function handleViewChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== VIEW_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.editReply({
      content: "Only the configured admin role or administrators can use /view.",
    });
    return true;
  }

  const subcommand = interaction.options.getSubcommand();

  if (subcommand === VIEW_USER_SUBCOMMAND) {
    await handleViewUser(interaction);
    return true;
  }

  if (subcommand === VIEW_ALL_SUBCOMMAND) {
    await handleViewAll(interaction);
    return true;
  }

  if (subcommand === VIEW_NOSTEAM_SUBCOMMAND) {
    await handleViewNoSteam(interaction);
    return true;
  }

  return true;
}

async function handleViewUser(interaction: ChatInputCommandInteraction) {
  const discordUser = interaction.options.getUser("discord_user");
  const identifier = interaction.options.getString("identifier");

  if (!discordUser && !identifier) {
    await interaction.editReply({
      content: "Provide either a discord user or an identifier.",
    });
    return;
  }

  if (!interaction.deferred && !interaction.replied) {
    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  }

  const guildId = interaction.guildId!;

  if (discordUser) {
    const result = await resolvePlayerProfileForGuild(guildId, discordUser.id);
    if (!result.ok) {
      await interaction.editReply({ content: result.error });
      return;
    }

    const profile = await fetchSteamProfile(result.steamId);
    await interaction.editReply({ embeds: [buildUserEmbed(result.steamId, profile, result.discordId, false)] });
    return;
  }

  if (identifier) {
    const result = await resolvePlayerProfileForGuild(guildId, identifier);
    if (!result.ok) {
      await interaction.editReply({ content: result.error });
      return;
    }

    const profile = await fetchSteamProfile(result.steamId);
    await interaction.editReply({ embeds: [buildUserEmbed(result.steamId, profile, result.discordId, false)] });
  }
}

async function handleViewAll(interaction: ChatInputCommandInteraction) {
  if (!interaction.deferred && !interaction.replied) {
    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  }

  const guildId = interaction.guildId!;
  const backend = getBackend();
  const workerId = getWorkerID();

  const { items } = await backend.listGuildLinks(workerId, guildId);

  if (items.length === 0) {
    await interaction.editReply({ content: "No linked accounts in this guild." });
    return;
  }

  const steamIds = [...new Set(items.map((l) => l.steamId))];
  const accountMap = new Map<string, SteamAccount>();

  await Promise.all(
    steamIds.map(async (sid) => {
      const acct = await fetchSteamAccount(sid);
      if (acct) {
        accountMap.set(sid, acct);
      }
    }),
  );

  const pageData = buildViewAllPage(items, accountMap, 0);
  await interaction.editReply({
    embeds: [pageData.embed],
    components: buildViewAllPageButtons(interaction.user.id, pageData.page, pageData.totalPages),
  });
}

export async function handleViewButtonInteraction(interaction: ButtonInteraction) {
  const parsed = parseViewAllPageCustomId(interaction.customId);
  if (!parsed) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This button can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (interaction.user.id !== parsed.ownerId) {
    await interaction.reply({
      content: "Only the user who ran /view all can use these buttons.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /view.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferUpdate();

  const backend = getBackend();
  const workerId = getWorkerID();
  const { items } = await backend.listGuildLinks(workerId, interaction.guildId);

  if (items.length === 0) {
    await interaction.editReply({
      content: "No linked accounts in this guild.",
      embeds: [],
      components: [],
    });
    return true;
  }

  const steamIds = [...new Set(items.map((link) => link.steamId))];
  const accountMap = new Map<string, SteamAccount>();

  await Promise.all(
    steamIds.map(async (sid) => {
      const acct = await fetchSteamAccount(sid);
      if (acct) {
        accountMap.set(sid, acct);
      }
    }),
  );

  const pageData = buildViewAllPage(items, accountMap, parsed.page);
  await interaction.editReply({
    content: null,
    embeds: [pageData.embed],
    components: buildViewAllPageButtons(parsed.ownerId, pageData.page, pageData.totalPages),
  });
  return true;
}

async function handleViewNoSteam(interaction: ChatInputCommandInteraction) {
  if (!interaction.deferred && !interaction.replied) {
    await interaction.deferReply({ flags: MessageFlags.Ephemeral });
  }

  const guildId = interaction.guildId!;
  const backend = getBackend();
  const workerId = getWorkerID();

  const [members, links] = await Promise.all([
    getGuildMembers(interaction.guild!),
    backend.listGuildLinks(workerId, guildId),
  ]);

  const linkedDiscordIds = new Set(links.items.map((link) => link.discordId));
  const missingUserIds = [...members.values()]
    .filter((member) => !member.user.bot)
    .sort(sortMembers)
    .filter((member) => !linkedDiscordIds.has(member.id))
    .map((member) => member.id);

  await interaction.editReply({
    embeds: [buildNoSteamEmbed(missingUserIds)],
  });
}

export const __testables = {
  buildUserEmbed,
  buildProfileButtons,
  buildViewAllPage,
  buildViewAllPageButtons,
  buildRiskFactorField,
  calculateSteamBmHourRisk,
  calculateServersPlayedRisk,
  calculateBmHoursRisk,
  calculateAccountCreatedRisk,
  calculateRecentBreaksRisk,
  normalizeDiscordIdentifier,
  parseViewAllPageCustomId,
};
