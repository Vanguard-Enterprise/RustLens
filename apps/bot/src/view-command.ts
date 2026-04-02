import type { ChatInputCommandInteraction, Client } from "discord.js";
import { EmbedBuilder, MessageFlags, PermissionFlagsBits, SlashCommandBuilder, type GuildMember } from "discord.js";
import { BackendClient, type SteamAccount } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const VIEW_COMMAND_NAME = "view";

const VIEW_USER_SUBCOMMAND = "user";
const VIEW_ALL_SUBCOMMAND = "all";
const VIEW_NOSTEAM_SUBCOMMAND = "nosteam";

const steamIDPattern = /^\d{17}$/;
const VIEW_EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
  vitality: "<:vitality:1444010131728629761>",
  atlas: "<:atlas:1444010442778480700>",
  vital: "<:vital:1444011141587271682>",
  modded: "<:modded:1487586362600456333>",
  official: "<:official:1487583814271242461>",
  build: "<:build:1487584253775450225>",
  rust: "<:rust:1444010153803382957>",
} as const;

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

function buildUserEmbed(steamId: string, account: SteamAccount | null): EmbedBuilder {
  const embed = new EmbedBuilder()
    .setTitle("Player Profile")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (account?.avatarImage) {
    embed.setThumbnail(account.avatarImage);
  }

  let info = `${VIEW_EMOJIS.steam} [${steamId}](https://steamcommunity.com/profiles/${steamId})`;
  if (account?.bmId) {
    info += `\n${VIEW_EMOJIS.battlemetrics} [${account.bmId}](https://www.battlemetrics.com/players/${account.bmId})`;
  }
  embed.addFields({ name: "Information", value: info });

  const hourFields = [
    { name: `${VIEW_EMOJIS.vitality} Vitality`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.atlas} Atlas`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.vital} Vital`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.modded} Modded`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.official} Official`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.build} Build`, value: "**—** hours" },
    { name: "\u{1F50D} First seen", value: "—" },
    { name: `${VIEW_EMOJIS.battlemetrics} BM`, value: "**—** hours" },
    { name: `${VIEW_EMOJIS.rust} Rust`, value: "**—** hours" },
  ];

  for (const field of hourFields) {
    embed.addFields({ name: field.name, value: field.value, inline: true });
  }

  return embed;
}

function buildNoSteamEmbed(userIds: readonly string[]) {
  return new EmbedBuilder()
    .setTitle("Unlinked Users")
    .setDescription(
      userIds.length > 0
        ? userIds.map((userId) => `> * <@${userId}>`).join("\n")
        : "> * None",
    )
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
  const backend = getBackend();
  const workerId = getWorkerID();

  if (discordUser) {
    const { items } = await backend.listGuildLinksByDiscord(workerId, guildId, discordUser.id);
    if (items.length === 0) {
      await interaction.editReply({ content: `No linked accounts found for <@${discordUser.id}>.` });
      return;
    }

    const steamId = items[0].steamId;
    const account = await fetchSteamAccount(steamId);
    const embed = buildUserEmbed(steamId, account);
    embed.setTitle(account?.bmId ? "Player Profile" : "Player Profile");
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  if (identifier) {
    let steamId: string | null = null;
    let account: SteamAccount | null = null;

    if (steamIDPattern.test(identifier)) {
      steamId = identifier;
      account = await fetchSteamAccount(identifier);
    } else {
      account = await fetchSteamAccountByBm(identifier);
      if (account) {
        steamId = account.steamId;
      }
    }

    if (!steamId) {
      await interaction.editReply({ content: "No steam account found for that identifier." });
      return;
    }

    const { items } = await backend.listGuildLinksBySteam(workerId, guildId, steamId);
    if (items.length === 0) {
      await interaction.editReply({ content: "That identifier is not linked to anyone in this guild." });
      return;
    }

    const embed = buildUserEmbed(steamId, account);
    await interaction.editReply({ embeds: [embed] });
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

  const embed = new EmbedBuilder()
    .setTitle("Links summary")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .setDescription(lines.join("\n"));

  await interaction.editReply({ embeds: [embed] });
}

async function handleViewNoSteam(interaction: ChatInputCommandInteraction) {
  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const guildId = interaction.guildId!;
  const backend = getBackend();
  const workerId = getWorkerID();

  const [members, links] = await Promise.all([
    interaction.guild!.members.fetch(),
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
