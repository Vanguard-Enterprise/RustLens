import type { ChatInputCommandInteraction, Client } from "discord.js";
import {
  ActionRowBuilder,
  ButtonBuilder,
  ButtonStyle,
  EmbedBuilder,
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

export const VIEW_COMMAND_NAME = "view";

const VIEW_USER_SUBCOMMAND = "user";
const VIEW_ALL_SUBCOMMAND = "all";
const VIEW_NOSTEAM_SUBCOMMAND = "nosteam";

const steamIDPattern = /^\d{17}$/;
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
  .addSubcommand((sub) =>
    sub
      .setName(VIEW_USER_SUBCOMMAND)
      .setDescription("View linked accounts for a user or steam identifier")
      .addUserOption((opt) =>
        opt.setName("discord_user").setDescription("Discord user to look up").setRequired(false),
      )
      .addStringOption((opt) =>
        opt.setName("identifier").setDescription("Steam ID or BattleMetrics ID").setRequired(false),
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
  return hours === null || hours === undefined ? "**private**" : `**${hours}** hours`;
}

type RiskLevel = keyof typeof RISK_EMOJIS;

function riskEmoji(level: number) {
  const normalized = Math.max(0, Math.min(10, Math.round(level))) as RiskLevel;
  return RISK_EMOJIS[normalized];
}

function calculateSteamBmHourRisk(profile: SteamProfile | null) {
  const steamHours = profile?.hours.rust;
  const bmHours = profile?.hours.bm;
  if (steamHours === null || steamHours === undefined || bmHours === undefined) {
    return null;
  }

  const difference = Math.max(0, steamHours - bmHours);
  if (difference === 0 || steamHours <= 0) {
    return { level: 0, difference };
  }

  const percentMissingFromSteam = (difference / steamHours) * 100;
  return { level: Math.min(10, Math.round(percentMissingFromSteam / RISK_PERCENT_STEP)), difference };
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

function buildRiskFactorField(profile: SteamProfile | null) {
  const steamBmRisk = calculateSteamBmHourRisk(profile);
  const serversRisk = calculateServersPlayedRisk(profile);
  const bmHoursRisk = calculateBmHoursRisk(profile);

  const factors = [steamBmRisk, serversRisk, bmHoursRisk].filter((factor) => factor !== null);
  const averageRisk = factors.length > 0
    ? factors.reduce((total, factor) => total + factor.level, 0) / factors.length
    : 0;

  const steamBmLine = steamBmRisk
    ? `${riskEmoji(steamBmRisk.level)} Steam → BM hour difference: **${steamBmRisk.difference}** hours.`
    : `${riskEmoji(0)} Steam → BM hour difference: **private**.`;

  const serversLine = serversRisk
    ? `${riskEmoji(serversRisk.level)} Servers played: **${serversRisk.servers}**.`
    : `${riskEmoji(0)} Servers played: **unknown**.`;

  const bmHoursLine = bmHoursRisk
    ? `${riskEmoji(bmHoursRisk.level)} BM hours: **${bmHoursRisk.hours}**.`
    : `${riskEmoji(0)} BM hours: **unknown**.`;

  return {
    name: `Risk Factor ${riskEmoji(averageRisk)}`,
    value: [steamBmLine, serversLine, bmHoursLine].join("\n"),
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

  if (steamIDPattern.test(normalizedIdentifier)) {
    steamId = normalizedIdentifier;
    account = await fetchSteamAccount(normalizedIdentifier);
    const { items } = await backend.listGuildLinksBySteam(workerId, guildId, steamId);
    if (items.length > 0) {
      return { ok: true, steamId, account, discordId: items[0].discordId };
    }

    if (allowDiscordLookup) {
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

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /view.",
      flags: MessageFlags.Ephemeral,
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
    await interaction.reply({
      content: "Provide either a discord user or an identifier.",
      flags: MessageFlags.Ephemeral,
    });
    return;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

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
  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

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

  const lines = items.map((link) => {
    const acct = accountMap.get(link.steamId);
    let line = `<@${link.discordId}> - [${link.steamId}](https://steamcommunity.com/profiles/${link.steamId})`;
    if (acct?.bmId) {
      line += ` - [${acct.bmId}](https://www.battlemetrics.com/players/${acct.bmId})`;
    }
    return `> * ${line}`;
  });

  let description = lines.join("\n");
  if (description.length > 4096) {
    description = `${description.slice(0, 4093)}...`;
  }

  const embed = new EmbedBuilder()
    .setTitle("Links summary")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .setDescription(description);

  await interaction.editReply({ embeds: [embed] });
}

async function handleViewNoSteam(interaction: ChatInputCommandInteraction) {
  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

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
  buildRiskFactorField,
  calculateSteamBmHourRisk,
  calculateServersPlayedRisk,
  calculateBmHoursRisk,
  normalizeDiscordIdentifier,
};
