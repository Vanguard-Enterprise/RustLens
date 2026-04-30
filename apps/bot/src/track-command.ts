import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  Guild,
  ModalSubmitInteraction,
  StringSelectMenuInteraction,
  TextChannel,
} from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  AttachmentBuilder,
  ButtonBuilder,
  ButtonStyle,
  ChannelType,
  EmbedBuilder,
  InteractionContextType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
  StringSelectMenuBuilder,
} from "discord.js";
import {
  BackendClient,
  type TrackGroup,
  type TrackGroupAnalyticsDay,
  type TrackGroupAnalyticsResponse,
  type TrackGroupOnlineRow,
} from "./backend-client.js";
import { extractBmID } from "./bm-input.js";
import { CheckboxGroupBuilder, ModalV2Builder, TextInputBuilder } from "./builders/index.js";
import { TextInputStyle } from "./builders/types.js";
import { loadConfig } from "./config.js";
import { PAGE_NEXT_EMOJI, PAGE_PREVIOUS_EMOJI } from "./embed-paginator.js";
import { fetchGuildSettings, hasGuildAdminAccess } from "./guild-settings.js";
import { buildNextBestOfflineFieldValue, computeNextBestOfflineWindow } from "./next-best-offline.js";
import { getFieldValues, RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";
import { renderTrackAnalyticsChart } from "./track-analytics-chart.js";
import { isWebsiteAdmin } from "./website-admin.js";

export const TRACK_COMMAND_NAME = "track";

const TRACK_CREATE_SUBCOMMAND = "create";
const TRACK_DELETE_SUBCOMMAND = "delete";
const TRACK_ADD_SUBCOMMAND = "add";
const TRACK_REMOVE_SUBCOMMAND = "remove";
const TRACK_GROUP_OPTION = "group_name";
const TRACK_DELETE_GROUP_OPTION = "track_name";
const TRACK_SERVER_OPTION = "server";
const TRACK_PLAYER_OPTION = "player";

const TRACK_BUTTON_ADD_PREFIX = "track:add:";
const TRACK_BUTTON_REMOVE_PREFIX = "track:remove:";
const TRACK_BUTTON_VIEW_PREFIX = "track:view:";
const TRACK_BUTTON_NOTIFY_PREFIX = "track:notify:";
const TRACK_BUTTON_ANALYTICS_PREFIX = "track:analytics:";
const TRACK_BUTTON_ANALYTICS_PAGE_PREFIX = "track:analyticspage:";
const TRACK_SELECT_PLAYER_PREFIX = "track_select:player:";

const TRACK_MODAL_ADD_PREFIX = "track_modal:add:";
const TRACK_MODAL_REMOVE_PREFIX = "track_modal:remove:";
const TRACK_MODAL_NOTIFY_PREFIX = "track_modal:notify:";

const TRACK_FIELD_IDS = "track_ids";
const TRACK_FIELD_PLAYER_NAME = "track_player_name";
const TRACK_FIELD_NOTIFY_OPTIONS = "track_notify_options";
const TRACK_NOTIFY_SEND_UPDATES = "send_updates";

const TRACK_TEAM_NAME_MAX = 12;
const TRACK_EMBED_COLOR = 0xde811d;
const JOIN_COLOR = 1233708;
const LEAVE_COLOR = 13832761;
const TRACK_ONLINE_EMOJI = "<:Online:1497993468981805076>";
const TRACK_OFFLINE_EMOJI = "<:Offline:1497993467660599476>";
const TRACK_ADD_EMOJI = { id: "1498006708549587155", name: "user_add" };
const TRACK_REMOVE_EMOJI = { id: "1498006709866856458", name: "user_remove" };
const TRACK_NOTIFICATION_EMOJI = { id: "1498033713764761852", name: "notification" };
const TRACK_EMBED_DESCRIPTION_MAX = 3900;
const TRACK_MESSAGE_TEXT_MAX = 5800;
type TrackViewMode = "online" | "offline";
type TrackMemberInput = { steamId: string; bmId: string };
type PlayerNameMatchResult =
  | { status: "none" }
  | { status: "unique"; member: TrackMemberInput }
  | { status: "ambiguous"; matches: PlayerNameMatch[] };
type PlayerNameMatch = TrackMemberInput & { bmName: string };
const EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
  discord: "<:discord:1496989877831729284>",
} as const;

const POLL_INTERVAL_MS = 30_000;

const trackCommand = new SlashCommandBuilder()
  .setName(TRACK_COMMAND_NAME)
  .setDescription("Manage tracking channels for groups of users on tracked servers")
  .setDefaultMemberPermissions(null)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((sub) =>
    sub
      .setName(TRACK_CREATE_SUBCOMMAND)
      .setDescription("Create a tracking channel")
      .addStringOption((opt) =>
        opt
          .setName(TRACK_GROUP_OPTION)
          .setDescription(`Team name (max ${TRACK_TEAM_NAME_MAX} chars). Spaces become dashes in the channel name.`)
          .setRequired(true)
          .setMaxLength(TRACK_TEAM_NAME_MAX),
      )
      .addStringOption((opt) =>
        opt
          .setName(TRACK_SERVER_OPTION)
          .setDescription("Tracked server")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((sub) =>
    sub
      .setName(TRACK_DELETE_SUBCOMMAND)
      .setDescription("Delete a tracking channel")
      .addStringOption((opt) =>
        opt
          .setName(TRACK_DELETE_GROUP_OPTION)
          .setDescription("Track to delete")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((sub) =>
    sub
      .setName(TRACK_ADD_SUBCOMMAND)
      .setDescription("Add a player to this track channel")
      .addStringOption((opt) =>
        opt
          .setName(TRACK_PLAYER_OPTION)
          .setDescription("Steam ID, BattleMetrics ID/URL, or player name")
          .setRequired(true),
      ),
  )
  .addSubcommand((sub) =>
    sub
      .setName(TRACK_REMOVE_SUBCOMMAND)
      .setDescription("Remove a player from this track channel")
      .addStringOption((opt) =>
        opt
          .setName(TRACK_PLAYER_OPTION)
          .setDescription("Steam ID, BattleMetrics ID/URL, or player name")
          .setRequired(true)
          .setAutocomplete(true),
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
    _workerID = `${config.workerId}-track`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

const STEAM_ID_PATTERN = /^\d{17}$/;
const pendingInitialPosts = new Set<number>();
const trackViewModes = new Map<number, TrackViewMode>();

function sanitizeGroupNameForChannel(name: string) {
  const slug = name
    .trim()
    .replace(/\s+/g, "-")
    .replace(/[^a-zA-Z0-9-_]/g, "")
    .toLowerCase();
  return slug.length > 0 ? slug.slice(0, 90) : "tracked";
}

function buildChannelName(groupName: string, onlineCount: number) {
  return `${sanitizeGroupNameForChannel(groupName)}-${onlineCount}`.slice(0, 100);
}

function buildChannelTopic(groupName: string) {
  return `Channel with information about ${groupName} clan.`.slice(0, 1024);
}

function bmLink(row: TrackGroupOnlineRow) {
  const label = row.bmName.trim().length > 0 ? row.bmName : (row.bmId || row.steamId);
  if (!row.bmId) {
    return `[${label}](https://steamcommunity.com/profiles/${row.steamId})`;
  }
  return `[${label}](https://www.battlemetrics.com/rcon/players/${row.bmId})`;
}

function buildOnlineLine(row: TrackGroupOnlineRow) {
  const stamp = row.onlineUnix > 0 ? ` - <t:${row.onlineUnix}:R>` : "";
  return `> * ${bmLink(row)}${stamp}`;
}

function buildOfflineLine(row: TrackGroupOnlineRow) {
  const stamp = row.onlineUnix > 0 ? ` - <t:${row.onlineUnix}:R>` : "";
  return `> * ${bmLink(row)}${stamp}`;
}

function chunkDescriptionLines(lines: string[]) {
  const chunks: string[] = [];
  let current = "";
  let totalLength = 0;
  let included = 0;

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    const safeLine = line.length > TRACK_EMBED_DESCRIPTION_MAX
      ? `${line.slice(0, TRACK_EMBED_DESCRIPTION_MAX - 3)}...`
      : line;
    const next = current.length === 0 ? safeLine : `${current}\n${safeLine}`;
    const addedLength = current.length === 0 ? safeLine.length : safeLine.length + 1;
    const needsMoreLine = index < lines.length - 1;
    if (totalLength + addedLength + (needsMoreLine ? 32 : 0) > TRACK_MESSAGE_TEXT_MAX) {
      break;
    }
    if (next.length > TRACK_EMBED_DESCRIPTION_MAX && current.length > 0) {
      chunks.push(current);
      current = safeLine;
    } else {
      current = next;
    }
    totalLength += addedLength;
    included += 1;
  }

  const hidden = lines.length - included;
  if (hidden > 0) {
    const moreLine = `+${hidden} more`;
    const next = current.length === 0 ? moreLine : `${current}\n${moreLine}`;
    if (next.length <= TRACK_EMBED_DESCRIPTION_MAX && totalLength + moreLine.length + 1 <= TRACK_MESSAGE_TEXT_MAX) {
      current = next;
    } else if (current.length > 0) {
      chunks.push(current);
      current = moreLine;
    }
  }

  if (current.length > 0) {
    chunks.push(current);
  }
  return chunks;
}

export function buildTrackEmbeds(
  group: { name: string; serverName: string },
  rows: TrackGroupOnlineRow[],
  mode: TrackViewMode = "online",
) {
  const online = rows.filter((row) => row.isOnline);
  const offline = rows.filter((row) => !row.isOnline);
  const visible = mode === "online" ? online : offline;
  const emoji = mode === "online" ? TRACK_ONLINE_EMOJI : TRACK_OFFLINE_EMOJI;
  const lineBuilder = mode === "online" ? buildOnlineLine : buildOfflineLine;

  const lines = visible.length > 0 ? visible.map(lineBuilder) : ["> * None"];

  return chunkDescriptionLines(lines).map((description, index) =>
    new EmbedBuilder()
      .setTitle(index === 0 ? `${emoji} ${visible.length} - ${group.name} - ${group.serverName}` : `${emoji} ${visible.length} - ${group.name} - ${group.serverName} cont.`)
      .setDescription(description)
      .setColor(TRACK_EMBED_COLOR)
      .setTimestamp()
      .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL }),
  );
}

function oppositeTrackViewMode(mode: TrackViewMode): TrackViewMode {
  return mode === "online" ? "offline" : "online";
}

export function buildTrackButtons(groupId: number, mode: TrackViewMode = "online") {
  const targetMode = oppositeTrackViewMode(mode);
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_ADD_PREFIX}${groupId}`)
        .setEmoji(TRACK_ADD_EMOJI)
        .setLabel("Add")
        .setStyle(ButtonStyle.Success),
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_REMOVE_PREFIX}${groupId}`)
        .setEmoji(TRACK_REMOVE_EMOJI)
        .setLabel("Remove")
        .setStyle(ButtonStyle.Danger),
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_VIEW_PREFIX}${groupId}:${targetMode}`)
        .setEmoji(targetMode === "online" ? TRACK_ONLINE_EMOJI : TRACK_OFFLINE_EMOJI)
        .setStyle(ButtonStyle.Secondary),
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_NOTIFY_PREFIX}${groupId}`)
        .setEmoji(TRACK_NOTIFICATION_EMOJI)
        .setStyle(ButtonStyle.Secondary),
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_ANALYTICS_PREFIX}${groupId}`)
        .setEmoji("📈")
        .setStyle(ButtonStyle.Secondary),
    ),
  ];
}

function buildErrorEmbed(description: string) {
  return new EmbedBuilder()
    .setTitle("Error")
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildStatusEmbed(successful: number, unsuccessful: number, failedIds: string[]) {
  const lines = [`> * Successfully tracked: ${successful}`, `> * Unsuccessfully tracked: ${unsuccessful}`];
  if (failedIds.length > 0) {
    lines.push("");
    lines.push(failedIds.map((id) => `> - \`${id}\``).join("\n"));
  }
  return new EmbedBuilder()
    .setTitle("Tracking Status")
    .setDescription(lines.join("\n"))
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function buildPlayerSelectEmbed(playerName: string) {
  return buildErrorEmbed(`Multiple players matched **${playerName}** on this server. Select the correct player below.`);
}

function truncateOptionLabel(value: string) {
  return value.length <= 100 ? value : `${value.slice(0, 97)}...`;
}

function encodeTrackMemberValue(member: TrackMemberInput) {
  return `${member.steamId}:${member.bmId}`;
}

function decodeTrackMemberValue(value: string): TrackMemberInput | null {
  const [steamId, bmId] = value.split(":");
  if (!steamId || !bmId || !STEAM_ID_PATTERN.test(steamId)) {
    return null;
  }
  return { steamId, bmId };
}

function isTrackIdentifierInput(value: string) {
  return STEAM_ID_PATTERN.test(value) || Boolean(extractBmID(value));
}

function extractTrailingParenthesizedID(value: string) {
  const match = /\((\d{6,17})\)\s*$/.exec(value.trim());
  return match?.[1] ?? null;
}

function normalizePlayerNameQuery(value: string) {
  return value.replace(/\s*\(\d{6,17}\)\s*$/, "").trim().toLocaleLowerCase();
}

function buildPlayerSelectComponents(action: "add" | "remove", groupId: number, matches: readonly PlayerNameMatch[]) {
  return [
    new ActionRowBuilder<StringSelectMenuBuilder>().addComponents(
      new StringSelectMenuBuilder()
        .setCustomId(`${TRACK_SELECT_PLAYER_PREFIX}${action}:${groupId}`)
        .setPlaceholder("Select a player")
        .addOptions(
          matches.slice(0, 25).map((match) => ({
            label: truncateOptionLabel(`${match.bmName || match.steamId} (${match.bmId})`),
            value: encodeTrackMemberValue(match),
          })),
        ),
    ),
  ];
}

function buildRemoveStatusEmbed(removedCount: number) {
  return new EmbedBuilder()
    .setTitle("Tracking Status")
    .setDescription(`> * Removed: ${removedCount}`)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });
}

function refreshTrackGroupInBackground(guild: Guild, guildId: string, groupId: number, context: string) {
  void refreshTrackGroupById(guild, guildId, groupId).catch((error) => {
    console.error(`[bot] failed to refresh track group ${groupId} after ${context}`, error);
  });
}

function parseIdentifiers(raw: string) {
  return raw
    .split(/[,\s]+/)
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
}

export async function ensureTrackCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = trackCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /track command (global)");
}

export async function handleTrackAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== TRACK_COMMAND_NAME) {
    return false;
  }

  const focused = interaction.options.getFocused(true);
  try {
    const query = focused.value.trim().toLocaleLowerCase();

    if (focused.name === TRACK_DELETE_GROUP_OPTION) {
      const { items } = interaction.guildId
        ? await getBackend().listTrackGroups(getWorkerID(), interaction.guildId)
        : { items: [] };
      const filtered = items
        .filter((item) => query.length === 0 || item.name.toLocaleLowerCase().includes(query))
        .slice(0, 25)
        .map((item) => ({
          name: item.name.slice(0, 100),
          value: String(item.id),
        }));
      await interaction.respond(filtered);
      return true;
    }

    if (focused.name === TRACK_PLAYER_OPTION) {
      const subcommand = interaction.options.getSubcommand(false);
      const group = interaction.guildId && interaction.channelId
        ? await findTrackGroupForChannel(interaction.guildId, interaction.channelId)
        : null;
      if (!group) {
        await interaction.respond([]);
        return true;
      }

      if (subcommand === TRACK_REMOVE_SUBCOMMAND) {
        const { items } = await getBackend().listTrackGroupOnlineState(getWorkerID(), group.guildId, group.id);
        const filtered = items
          .filter((item) => {
            const haystack = `${item.bmName} ${item.bmId} ${item.steamId}`.toLocaleLowerCase();
            return query.length === 0 || haystack.includes(query);
          })
          .slice(0, 25)
          .map((item) => ({
            name: truncateOptionLabel(`${item.bmName || item.steamId} (${item.bmId || item.steamId})`),
            value: encodeTrackMemberValue({ steamId: item.steamId, bmId: item.bmId }),
          }));
        await interaction.respond(filtered);
        return true;
      }

      if (subcommand === TRACK_ADD_SUBCOMMAND) {
        if (query.length === 0) {
          await interaction.respond([]);
          return true;
        }
        const { items } = await getBackend().searchPlayerStatesByName(getWorkerID(), group.serverBmId, focused.value.trim(), 25, true);
        await interaction.respond(items.slice(0, 25).map((item) => ({
          name: truncateOptionLabel(`${item.bmName || item.steamId} (${item.playerBmId})`),
          value: encodeTrackMemberValue({ steamId: item.steamId, bmId: item.playerBmId }),
        })));
        return true;
      }

      await interaction.respond([]);
      return true;
    }

    if (focused.name !== TRACK_SERVER_OPTION) {
      await interaction.respond([]);
      return true;
    }

    const { items } = await getBackend().listTrackedServers(getWorkerID());
    const filtered = items
      .filter((item) =>
        query.length === 0 ||
        item.name.toLocaleLowerCase().includes(query) ||
        item.bmId.includes(query),
      )
      .slice(0, 25)
      .map((item) => ({
        name: item.name.slice(0, 100),
        value: item.bmId,
      }));
    await interaction.respond(filtered);
  } catch {
    await interaction.respond([]);
  }
  return true;
}

export async function handleTrackChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== TRACK_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);
  if (subcommand === TRACK_ADD_SUBCOMMAND || subcommand === TRACK_REMOVE_SUBCOMMAND) {
    return handleTrackMemberChatInput(interaction, subcommand);
  }

  if (subcommand === TRACK_DELETE_SUBCOMMAND) {
    if (!(await hasGuildAdminAccess(interaction))) {
      await interaction.reply({
        embeds: [buildErrorEmbed("Only administrators or the configured admin role can use /track delete.")],
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }
    return handleTrackDeleteChatInput(interaction);
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      embeds: [buildErrorEmbed("Only administrators or the configured admin role can use /track create.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const groupName = (interaction.options.getString(TRACK_GROUP_OPTION, true) ?? "").trim();
  const serverBmId = (interaction.options.getString(TRACK_SERVER_OPTION, true) ?? "").trim();

  if (groupName.length === 0 || groupName.length > TRACK_TEAM_NAME_MAX) {
    await interaction.reply({
      embeds: [buildErrorEmbed(`Team name must be 1-${TRACK_TEAM_NAME_MAX} characters.`)],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const settings = await fetchGuildSettings(interaction.guildId).catch(() => null);
  if (!settings?.trackedUsersCategoryId) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("Configure the Tracked users category in /settings before using /track.")],
    });
    return true;
  }

  const category = await interaction.guild.channels
    .fetch(settings.trackedUsersCategoryId)
    .catch(() => null);
  if (!category || category.type !== ChannelType.GuildCategory) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("The configured Tracked users category is missing. Update /settings.")],
    });
    return true;
  }

  const trackedServers = await getBackend().listTrackedServers(getWorkerID()).catch(() => null);
  const server = trackedServers?.items.find((item) => item.bmId === serverBmId);
  if (!server) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("That tracked server could not be found.")],
    });
    return true;
  }

  const channel = await interaction.guild.channels.create({
    name: buildChannelName(groupName, 0),
    type: ChannelType.GuildText,
    parent: category.id,
    topic: buildChannelTopic(groupName),
    reason: `/track group ${groupName}`,
  });

  let group: TrackGroup;
  try {
    group = await getBackend().createTrackGroup(getWorkerID(), interaction.guildId, {
      name: groupName,
      serverBmId,
      channelId: channel.id,
      messageId: "",
      sendUpdates: false,
      onlineCount: 0,
      createdBy: interaction.user.id,
    });
  } catch (error) {
    await channel.delete("track group create failed").catch(() => null);
    const status = (error as { status?: number }).status;
    const description = status === 409
      ? `A track group named **${groupName}** already exists in this guild.`
      : "RustLens could not create that track group.";
    await interaction.editReply({ embeds: [buildErrorEmbed(description)] });
    return true;
  }

  const embeds = buildTrackEmbeds({ name: group.name, serverName: server.name }, []);
  const components = buildTrackButtons(group.id);
  pendingInitialPosts.add(group.id);
  try {
    const message = await channel.send({ embeds, components });
    await message.pin().catch(() => null);

    await getBackend().updateTrackGroup(getWorkerID(), interaction.guildId, group.id, {
      messageId: message.id,
    });
  } finally {
    pendingInitialPosts.delete(group.id);
  }

  await interaction.editReply({
    embeds: [
      new EmbedBuilder()
        .setTitle("Track Group Created")
        .setDescription(`Created <#${channel.id}> for **${group.name}** on **${server.name}**.`)
        .setColor(RUSTLENS_EMBED_COLOR)
        .setTimestamp()
        .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL }),
    ],
  });

  return true;
}

async function handleTrackDeleteChatInput(interaction: ChatInputCommandInteraction) {
  if (!interaction.inGuild() || !interaction.guild || !interaction.guildId) {
    return true;
  }

  const groupId = Number(interaction.options.getString(TRACK_DELETE_GROUP_OPTION, true));
  if (!Number.isInteger(groupId) || groupId <= 0) {
    await interaction.reply({
      embeds: [buildErrorEmbed("Choose a valid track to delete.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  let group: TrackGroup;
  try {
    group = await getBackend().getTrackGroup(getWorkerID(), interaction.guildId, groupId);
  } catch {
    await interaction.editReply({ embeds: [buildErrorEmbed("That track group could not be found.")] });
    return true;
  }

  const channel = await interaction.guild.channels.fetch(group.channelId).catch(() => null);
  await getBackend().deleteTrackGroup(getWorkerID(), interaction.guildId, group.id);
  previousSnapshots.delete(group.id);

  await interaction.editReply({
    embeds: [
      new EmbedBuilder()
        .setTitle("Track Group Deleted")
        .setDescription(`Deleted **${group.name}**.`)
        .setColor(RUSTLENS_EMBED_COLOR)
        .setTimestamp()
        .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL }),
      ],
  });

  if (channel) {
    await channel.delete(`/track delete ${group.name}`).catch(() => null);
  }
  return true;
}

async function handleTrackMemberChatInput(
  interaction: ChatInputCommandInteraction,
  subcommand: typeof TRACK_ADD_SUBCOMMAND | typeof TRACK_REMOVE_SUBCOMMAND,
) {
  if (!interaction.inGuild() || !interaction.guild || !interaction.guildId || !interaction.channelId) {
    return true;
  }

  if (!canViewCurrentChannel(interaction)) {
    await interaction.reply({
      embeds: [buildErrorEmbed("You need access to view this track channel to use this command here.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  const group = await findTrackGroupForChannel(interaction.guildId, interaction.channelId).catch(() => null);
  if (!group) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("Run this command inside a track channel.")],
    });
    return true;
  }

  const input = interaction.options.getString(TRACK_PLAYER_OPTION, true).trim();
  const selectedMember = decodeTrackMemberValue(input);

  if (subcommand === TRACK_ADD_SUBCOMMAND) {
    let resolved: TrackMemberInput[] = [];
    let failed: string[] = [];
    if (selectedMember) {
      resolved = [selectedMember];
    } else if (isTrackIdentifierInput(input)) {
      const byIdentifier = await resolveIdentifiersToMembers([input]);
      resolved = byIdentifier.resolved;
      failed = byIdentifier.failed;
    } else {
      const matched = await resolvePlayerNameToMember(group.serverBmId, input).catch((): PlayerNameMatchResult => ({ status: "none" }));
      if (matched.status === "ambiguous") {
        await interaction.editReply({
          embeds: [buildPlayerSelectEmbed(input)],
          components: buildPlayerSelectComponents("add", group.id, matched.matches),
        });
        return true;
      }
      if (matched.status === "unique") {
        resolved = [matched.member];
      } else {
        failed = [`${input} (no match on this server)`];
      }
    }

    if (resolved.length > 0) {
      await getBackend().addTrackGroupMembers(getWorkerID(), interaction.guildId, group.id, resolved);
      refreshTrackGroupInBackground(interaction.guild, interaction.guildId, group.id, "command add");
    }
    await interaction.editReply({ embeds: [buildStatusEmbed(resolved.length, failed.length, failed)] });
    return true;
  }

  let identifiers = selectedMember ? [selectedMember.steamId] : isTrackIdentifierInput(input) ? [input] : [];
  if (!selectedMember && !isTrackIdentifierInput(input)) {
    const matched = await resolvePlayerNameWithinTrack(interaction.guildId, group.id, input).catch((): PlayerNameMatchResult => ({ status: "none" }));
    if (matched.status === "ambiguous") {
      await interaction.editReply({
        embeds: [buildPlayerSelectEmbed(input)],
        components: buildPlayerSelectComponents("remove", group.id, matched.matches),
      });
      return true;
    }
    if (matched.status === "unique") {
      identifiers = [matched.member.steamId];
    } else {
      await interaction.editReply({ embeds: [buildErrorEmbed(`${input} did not match a tracked player in this channel.`)] });
      return true;
    }
  }

  try {
    const removed = await getBackend().removeTrackGroupMembers(getWorkerID(), interaction.guildId, group.id, identifiers);
    await interaction.editReply({ embeds: [buildRemoveStatusEmbed(removed.count)] });
    if (removed.count > 0) {
      refreshTrackGroupInBackground(interaction.guild, interaction.guildId, group.id, "command remove");
    }
  } catch (error) {
    console.error(`[bot] failed to remove track group members for group ${group.id}`, error);
    await interaction.editReply({ embeds: [buildErrorEmbed("RustLens could not remove that player. Try again shortly.")] });
  }
  return true;
}

export async function handleTrackButtonInteraction(interaction: ButtonInteraction) {
  if (interaction.customId.startsWith(TRACK_BUTTON_ADD_PREFIX)) {
    const groupId = Number(interaction.customId.slice(TRACK_BUTTON_ADD_PREFIX.length));
    if (!Number.isInteger(groupId) || groupId <= 0) {
      return false;
    }
    await interaction.showModal(buildAddRemoveModal(true, groupId).toJSON() as any);
    return true;
  }

  if (interaction.customId.startsWith(TRACK_BUTTON_REMOVE_PREFIX)) {
    const groupId = Number(interaction.customId.slice(TRACK_BUTTON_REMOVE_PREFIX.length));
    if (!Number.isInteger(groupId) || groupId <= 0) {
      return false;
    }
    await interaction.showModal(buildAddRemoveModal(false, groupId).toJSON() as any);
    return true;
  }

  if (interaction.customId.startsWith(TRACK_BUTTON_VIEW_PREFIX)) {
    const rest = interaction.customId.slice(TRACK_BUTTON_VIEW_PREFIX.length);
    const [groupRaw, modeRaw] = rest.split(":");
    const groupId = Number(groupRaw);
    if (!Number.isInteger(groupId) || groupId <= 0 || !interaction.inGuild() || !interaction.guildId || !interaction.guild) {
      return false;
    }
    const mode: TrackViewMode = modeRaw === "offline" ? "offline" : "online";
    trackViewModes.set(groupId, mode);
    await interaction.deferUpdate();
    await refreshTrackGroupById(interaction.guild, interaction.guildId, groupId, mode).catch((error) => {
      console.error(`[bot] failed to toggle track group ${groupId} view`, error);
    });
    return true;
  }

  if (interaction.customId.startsWith(TRACK_BUTTON_NOTIFY_PREFIX)) {
    const groupId = Number(interaction.customId.slice(TRACK_BUTTON_NOTIFY_PREFIX.length));
    if (!Number.isInteger(groupId) || groupId <= 0 || !interaction.inGuild() || !interaction.guildId) {
      return false;
    }

    let group: TrackGroup;
    try {
      group = await getBackend().getTrackGroup(getWorkerID(), interaction.guildId, groupId);
    } catch {
      await interaction.reply({
        embeds: [buildErrorEmbed("That track group could not be found.")],
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }

    await interaction.showModal(buildNotifyModal(group).toJSON() as any);
    return true;
  }

  if (interaction.customId.startsWith(TRACK_BUTTON_ANALYTICS_PREFIX)) {
    const groupId = Number(interaction.customId.slice(TRACK_BUTTON_ANALYTICS_PREFIX.length));
    if (!Number.isInteger(groupId) || groupId <= 0 || !interaction.inGuild() || !interaction.guildId) {
      return false;
    }
    return handleAnalyticsButton(interaction, groupId, 0);
  }

  if (interaction.customId.startsWith(TRACK_BUTTON_ANALYTICS_PAGE_PREFIX)) {
    const rest = interaction.customId.slice(TRACK_BUTTON_ANALYTICS_PAGE_PREFIX.length);
    const [groupRaw, pageRaw] = rest.split(":");
    const groupId = Number(groupRaw);
    const targetPage = Number(pageRaw);
    if (!Number.isInteger(groupId) || groupId <= 0 || !Number.isInteger(targetPage) || targetPage < 0 || !interaction.inGuild() || !interaction.guildId) {
      return false;
    }
    return handleAnalyticsPage(interaction, groupId, targetPage);
  }

  return false;
}

function buildAddRemoveModal(isAdd: boolean, groupId: number) {
  const customId = `${isAdd ? TRACK_MODAL_ADD_PREFIX : TRACK_MODAL_REMOVE_PREFIX}${groupId}`;
  const title = isAdd ? "Add tracked users" : "Remove tracked users";
  const placeholder = "76561198000000000, 918543318";
  const modal = new ModalV2Builder()
    .setTitle(title)
    .setCustomId(customId)
    .addComponent(
      "Steam IDs, BattleMetrics IDs, or BM URLs",
      new TextInputBuilder()
        .setCustomId(TRACK_FIELD_IDS)
        .setStyle(TextInputStyle.Paragraph)
        .setRequired(false)
        .setMaxLength(2000)
        .setPlaceholder(placeholder),
    );

  modal.addComponent(
    "Player name on this tracked server",
    new TextInputBuilder()
      .setCustomId(TRACK_FIELD_PLAYER_NAME)
      .setStyle(TextInputStyle.Short)
      .setRequired(false)
      .setMaxLength(100)
      .setPlaceholder("One exact or partial player name"),
  );

  return modal;
}

async function findTrackGroupForChannel(guildId: string, channelId: string) {
  const { items } = await getBackend().listTrackGroups(getWorkerID(), guildId);
  return items.find((group) => group.channelId === channelId) ?? null;
}

function canViewCurrentChannel(interaction: ChatInputCommandInteraction) {
  if (!interaction.channel || !("permissionsFor" in interaction.channel)) {
    return true;
  }

  return interaction.channel.permissionsFor(interaction.user)?.has(PermissionFlagsBits.ViewChannel) ?? false;
}

function buildNotifyModal(group: TrackGroup) {
  return new ModalV2Builder()
    .setTitle("Notification settings")
    .setCustomId(`${TRACK_MODAL_NOTIFY_PREFIX}${group.id}`)
    .addComponent(
      "Notification options",
      new CheckboxGroupBuilder()
        .setCustomId(TRACK_FIELD_NOTIFY_OPTIONS)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(1)
        .addOptions({
          label: "Send updates",
          value: TRACK_NOTIFY_SEND_UPDATES,
          description: "Post a join/leave embed in this channel when state changes.",
          default: group.sendUpdates,
        }),
    );
}

export async function handleTrackModalSubmit(interaction: ModalSubmitInteraction) {
  if (
    !interaction.customId.startsWith(TRACK_MODAL_ADD_PREFIX) &&
    !interaction.customId.startsWith(TRACK_MODAL_REMOVE_PREFIX) &&
    !interaction.customId.startsWith(TRACK_MODAL_NOTIFY_PREFIX)
  ) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId || !interaction.guild) {
    await interaction.reply({
      content: "This modal can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (interaction.customId.startsWith(TRACK_MODAL_NOTIFY_PREFIX)) {
    const groupId = Number(interaction.customId.slice(TRACK_MODAL_NOTIFY_PREFIX.length));
    if (!Number.isInteger(groupId) || groupId <= 0) {
      return true;
    }
    const selected = getFieldValues(interaction, TRACK_FIELD_NOTIFY_OPTIONS);
    const sendUpdates = selected.includes(TRACK_NOTIFY_SEND_UPDATES);
    await getBackend().updateTrackGroup(getWorkerID(), interaction.guildId, groupId, { sendUpdates });
    await interaction.reply({
      content: `Notifications ${sendUpdates ? "enabled" : "disabled"} for this track group.`,
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const isAdd = interaction.customId.startsWith(TRACK_MODAL_ADD_PREFIX);
  const groupId = Number(
    interaction.customId.slice(
      (isAdd ? TRACK_MODAL_ADD_PREFIX : TRACK_MODAL_REMOVE_PREFIX).length,
    ),
  );
  if (!Number.isInteger(groupId) || groupId <= 0) {
    return true;
  }

  const raw = getFieldValues(interaction, TRACK_FIELD_IDS).join(",");
  const playerName = getFieldValues(interaction, TRACK_FIELD_PLAYER_NAME).join(" ").trim();
  const identifiers = parseIdentifiers(raw);
  if (identifiers.length === 0 && playerName.length === 0) {
    await interaction.reply({
      embeds: [buildErrorEmbed("Provide at least one Steam ID, BattleMetrics ID, or player name.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (isAdd) {
    const { resolved, failed } = await resolveIdentifiersToMembers(identifiers);
    if (playerName.length > 0) {
      const group = await getBackend().getTrackGroup(getWorkerID(), interaction.guildId, groupId).catch(() => null);
      if (!group) {
        await interaction.editReply({ embeds: [buildErrorEmbed("That track group could not be found.")] });
        return true;
      }

      const matched = await resolvePlayerNameToMember(group.serverBmId, playerName).catch((): PlayerNameMatchResult => ({ status: "none" }));
      if (matched.status === "ambiguous") {
        await interaction.editReply({
          embeds: [buildPlayerSelectEmbed(playerName)],
          components: buildPlayerSelectComponents("add", groupId, matched.matches),
        });
        return true;
      }
      if (matched.status === "none") {
        failed.push(`${playerName} (no unique match on this server)`);
      } else if (!resolved.some((member) => member.steamId === matched.member.steamId)) {
        resolved.push(matched.member);
      }
    }
    if (resolved.length > 0) {
      await getBackend().addTrackGroupMembers(getWorkerID(), interaction.guildId, groupId, resolved);
      refreshTrackGroupInBackground(interaction.guild, interaction.guildId, groupId, "modal add");
    }
    await interaction.editReply({
      embeds: [buildStatusEmbed(resolved.length, failed.length, failed)],
    });
    return true;
  }

  if (playerName.length > 0) {
    const group = await getBackend().getTrackGroup(getWorkerID(), interaction.guildId, groupId).catch(() => null);
    if (!group) {
      await interaction.editReply({ embeds: [buildErrorEmbed("That track group could not be found.")] });
      return true;
    }

    const matched = await resolvePlayerNameToMember(group.serverBmId, playerName).catch((): PlayerNameMatchResult => ({ status: "none" }));
    if (matched.status === "ambiguous") {
      await interaction.editReply({
        embeds: [buildPlayerSelectEmbed(playerName)],
        components: buildPlayerSelectComponents("remove", groupId, matched.matches),
      });
      return true;
    }
    if (matched.status === "none") {
      if (identifiers.length === 0) {
        await interaction.editReply({ embeds: [buildErrorEmbed(`${playerName} did not uniquely match a player on this server.`)] });
        return true;
      }
    } else if (!identifiers.includes(matched.member.steamId)) {
      identifiers.push(matched.member.steamId);
    }
  }

  let removed: { count: number };
  try {
    removed = await getBackend().removeTrackGroupMembers(
      getWorkerID(),
      interaction.guildId,
      groupId,
      identifiers,
    );
  } catch (error) {
    console.error(`[bot] failed to remove track group members for group ${groupId}`, error);
    await interaction.editReply({
      embeds: [buildErrorEmbed("RustLens could not remove those members. Try again shortly.")],
    });
    return true;
  }

  await interaction.editReply({
    embeds: [buildRemoveStatusEmbed(removed.count)],
  });
  if (removed.count > 0) {
    refreshTrackGroupInBackground(interaction.guild, interaction.guildId, groupId, "modal remove");
  }
  return true;
}

export async function handleTrackSelectInteraction(interaction: StringSelectMenuInteraction) {
  if (!interaction.customId.startsWith(TRACK_SELECT_PLAYER_PREFIX)) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId || !interaction.guild) {
    await interaction.reply({
      content: "This select can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const rest = interaction.customId.slice(TRACK_SELECT_PLAYER_PREFIX.length);
  const [actionRaw, groupRaw] = rest.split(":");
  const action = actionRaw === "remove" ? "remove" : actionRaw === "add" ? "add" : null;
  const groupId = Number(groupRaw);
  const selected = interaction.values[0]?.split(":") ?? [];
  const [steamId, bmId] = selected;
  if (!action || !Number.isInteger(groupId) || groupId <= 0 || !steamId || !bmId) {
    return true;
  }

  await interaction.deferUpdate();

  if (action === "add") {
    await getBackend().addTrackGroupMembers(getWorkerID(), interaction.guildId, groupId, [{ steamId, bmId }]);
    await interaction.editReply({
      embeds: [buildStatusEmbed(1, 0, [])],
      components: [],
    });
    refreshTrackGroupInBackground(interaction.guild, interaction.guildId, groupId, "select add");
    return true;
  }

  const removed = await getBackend().removeTrackGroupMembers(getWorkerID(), interaction.guildId, groupId, [steamId]);
  await interaction.editReply({
    embeds: [buildRemoveStatusEmbed(removed.count)],
    components: [],
  });
  if (removed.count > 0) {
    refreshTrackGroupInBackground(interaction.guild, interaction.guildId, groupId, "select remove");
  }
  return true;
}

async function resolveIdentifiersToMembers(identifiers: string[]) {
  const resolved: TrackMemberInput[] = [];
  const failed: string[] = [];
  const seenSteam = new Set<string>();

  for (const raw of identifiers) {
    const identifier = raw.trim();
    if (identifier.length === 0) {
      continue;
    }

    try {
      if (STEAM_ID_PATTERN.test(identifier)) {
        const member = await resolveSteamID(identifier);
        if (!member) {
          failed.push(identifier);
          continue;
        }
        if (seenSteam.has(member.steamId)) continue;
        seenSteam.add(member.steamId);
        resolved.push(member);
        continue;
      }

      const bmID = extractBmID(identifier);
      if (bmID) {
        const member = await resolveBmID(bmID);
        if (!member) {
          failed.push(identifier);
          continue;
        }
        if (seenSteam.has(member.steamId)) continue;
        seenSteam.add(member.steamId);
        resolved.push(member);
        continue;
      }

      failed.push(identifier);
    } catch {
      failed.push(identifier);
    }
  }

  return { resolved, failed };
}

async function resolvePlayerNameToMember(serverBmId: string, playerName: string): Promise<PlayerNameMatchResult> {
  const response = await getBackend().searchPlayerStatesByName(getWorkerID(), serverBmId, playerName, 20, true);
  const parenthesizedId = extractTrailingParenthesizedID(playerName);
  const normalizedName = normalizePlayerNameQuery(playerName);
  const allMatches = response.items
    .filter((item) => !parenthesizedId || item.playerBmId === parenthesizedId || item.steamId === parenthesizedId)
    .map((item) => ({
      steamId: item.steamId,
      bmId: item.playerBmId,
      bmName: item.bmName || item.steamId,
    }));
  const exactMatches = allMatches.filter((item) => item.bmName.toLocaleLowerCase() === normalizedName);
  const matches = exactMatches.length > 0 ? exactMatches : allMatches;
  if (matches.length === 0) {
    return { status: "none" };
  }
  if (matches.length === 1) {
    const match = matches[0];
    return { status: "unique", member: { steamId: match.steamId, bmId: match.bmId } };
  }

  return { status: "ambiguous", matches: matches.slice(0, 25) };
}

async function resolvePlayerNameWithinTrack(guildId: string, groupId: number, playerName: string): Promise<PlayerNameMatchResult> {
  const response = await getBackend().listTrackGroupOnlineState(getWorkerID(), guildId, groupId);
  const parenthesizedId = extractTrailingParenthesizedID(playerName);
  const normalizedName = normalizePlayerNameQuery(playerName);
  const allMatches = response.items
    .filter((item) => {
      if (parenthesizedId && (item.bmId === parenthesizedId || item.steamId === parenthesizedId)) {
        return true;
      }
      const haystack = `${item.bmName} ${item.bmId} ${item.steamId}`.toLocaleLowerCase();
      return haystack.includes(normalizedName);
    })
    .map((item) => ({
      steamId: item.steamId,
      bmId: item.bmId,
      bmName: item.bmName || item.steamId,
    }));
  const exactMatches = allMatches.filter((item) => item.bmName.toLocaleLowerCase() === normalizedName);
  const matches = exactMatches.length > 0 ? exactMatches : allMatches;
  if (matches.length === 0) {
    return { status: "none" };
  }
  if (matches.length === 1) {
    const match = matches[0];
    return { status: "unique", member: { steamId: match.steamId, bmId: match.bmId } };
  }

  return { status: "ambiguous", matches: matches.slice(0, 25) };
}

async function resolveSteamID(steamId: string): Promise<{ steamId: string; bmId: string } | null> {
  const backend = getBackend();
  const workerId = getWorkerID();
  try {
    const account = await backend.getSteamAccount(workerId, steamId);
    if (account.bmId) {
      return { steamId: account.steamId, bmId: account.bmId };
    }
  } catch {
    // fall through to profile fetch
  }

  // Trigger the lazy profile/BM lookup like /player does so the steam_account row gets a bm_id.
  try {
    await backend.getSteamProfile(workerId, steamId);
  } catch {
    // ignore — try once more below
  }

  try {
    const account = await backend.getSteamAccount(workerId, steamId);
    return account.bmId ? { steamId: account.steamId, bmId: account.bmId } : null;
  } catch {
    return null;
  }
}

async function resolveBmID(bmId: string): Promise<{ steamId: string; bmId: string } | null> {
  try {
    const account = await getBackend().getSteamAccountByBmID(getWorkerID(), bmId);
    if (account.steamId) {
      return { steamId: account.steamId, bmId: account.bmId ?? bmId };
    }
  } catch {
    // ignore
  }
  return null;
}

// ---------------------------------------------------------------------------
// Polling: keep the pinned embed and channel name in sync, post update embeds.
// ---------------------------------------------------------------------------

type Snapshot = Map<string, boolean>; // steamId -> isOnline

const previousSnapshots = new Map<number, Snapshot>();

export function startTrackGroupPoller(client: Client): NodeJS.Timeout {
  let running = false;

  const tick = async () => {
    if (running) return;
    running = true;
    try {
      await pollAllGroups(client);
    } catch (error) {
      console.error("[bot] track group poller failed", error);
    } finally {
      running = false;
    }
  };

  const timer = setInterval(tick, POLL_INTERVAL_MS);
  void tick();
  return timer;
}

async function pollAllGroups(client: Client) {
  const guilds = [...client.guilds.cache.values()];
  for (const guild of guilds) {
    try {
      await pollGuildGroups(guild);
    } catch (error) {
      console.error(`[bot] track group poll failed for guild ${guild.id}`, error);
    }
  }
}

async function pollGuildGroups(guild: Guild) {
  const { items: groups } = await getBackend()
    .listTrackGroups(getWorkerID(), guild.id)
    .catch(() => ({ items: [] as TrackGroup[] }));

  if (groups.length === 0) {
    return;
  }

  const trackedServers = await getBackend()
    .listTrackedServers(getWorkerID())
    .catch(() => ({ items: [] as { bmId: string; name: string }[] }));
  const serverNameByBmId = new Map(trackedServers.items.map((item) => [item.bmId, item.name]));

  for (const group of groups) {
    try {
      await refreshTrackGroup(guild, group, serverNameByBmId.get(group.serverBmId) ?? group.serverBmId);
    } catch (error) {
      console.error(`[bot] failed to refresh track group ${group.id}`, error);
    }
  }
}

async function refreshTrackGroup(guild: Guild, group: TrackGroup, serverName: string, requestedMode?: TrackViewMode) {
  if (pendingInitialPosts.has(group.id)) {
    return;
  }

  const channel = await guild.channels.fetch(group.channelId).catch(() => null);
  if (!channel || channel.type !== ChannelType.GuildText) {
    await getBackend().deleteTrackGroup(getWorkerID(), guild.id, group.id).catch(() => null);
    previousSnapshots.delete(group.id);
    return;
  }
  const textChannel = channel as TextChannel;

  let message = group.messageId
    ? await textChannel.messages.fetch(group.messageId).catch(() => null)
    : null;

  const mode = requestedMode ?? trackViewModes.get(group.id) ?? "online";
  trackViewModes.set(group.id, mode);

  const { items: rows } = await getBackend()
    .listTrackGroupOnlineState(getWorkerID(), guild.id, group.id)
    .catch(() => ({ items: [] as TrackGroupOnlineRow[] }));

  const onlineRows = rows.filter((row) => row.isOnline);
  const onlineCount = onlineRows.length;
  const embeds = buildTrackEmbeds({ name: group.name, serverName }, rows, mode);
  const components = buildTrackButtons(group.id, mode);

  if (!message) {
    const existing = await findExistingTrackMessage(textChannel, group, serverName);
    if (existing) {
      await existing.edit({ embeds, components }).catch((error) => {
        console.error(`[bot] failed to edit existing track message ${group.id}`, error);
      });
      await getBackend().updateTrackGroup(getWorkerID(), guild.id, group.id, {
        messageId: existing.id,
      });
      message = existing;
    }
  }

  if (!message) {
    const sent = await textChannel.send({ embeds, components }).catch(() => null);
    if (!sent) {
      return;
    }
    await sent.pin().catch(() => null);
    await getBackend().updateTrackGroup(getWorkerID(), guild.id, group.id, {
      messageId: sent.id,
    });
    message = sent;
  } else {
    await message.edit({ embeds, components }).catch((error) => {
      console.error(`[bot] failed to edit track message ${group.id}`, error);
    });
  }

  // Update channel name + persisted online_count if it changed.
  if (group.onlineCount !== onlineCount) {
    const desiredName = buildChannelName(group.name, onlineCount);
    if (textChannel.name !== desiredName) {
      await textChannel.setName(desiredName).catch(() => null);
    }
    await getBackend().updateTrackGroup(getWorkerID(), guild.id, group.id, {
      onlineCount,
    });
  }

  const desiredTopic = buildChannelTopic(group.name);
  if (textChannel.topic !== desiredTopic) {
    await textChannel.setTopic(desiredTopic).catch(() => null);
  }

  // Post change updates if enabled.
  const previous = previousSnapshots.get(group.id);
  const current: Snapshot = new Map(rows.map((row) => [row.steamId, row.isOnline]));

  if (previous && group.sendUpdates) {
    const changes: { row: TrackGroupOnlineRow; isOnline: boolean }[] = [];
    for (const row of rows) {
      const prev = previous.get(row.steamId);
      if (prev !== undefined && prev !== row.isOnline) {
        changes.push({ row, isOnline: row.isOnline });
      }
    }
    for (const change of changes) {
      const updateEmbed = buildJoinLeaveEmbed(change.row, change.isOnline, serverName);
      await textChannel.send({ embeds: [updateEmbed] }).catch(() => null);
    }
  }

  previousSnapshots.set(group.id, current);
}

async function refreshTrackGroupById(guild: Guild, guildId: string, groupId: number, mode?: TrackViewMode) {
  const group = await getBackend().getTrackGroup(getWorkerID(), guildId, groupId);
  const trackedServers = await getBackend()
    .listTrackedServers(getWorkerID())
    .catch(() => ({ items: [] as { bmId: string; name: string }[] }));
  const serverName = trackedServers.items.find((item) => item.bmId === group.serverBmId)?.name ?? group.serverBmId;
  await refreshTrackGroup(guild, group, serverName, mode);
}

async function findExistingTrackMessage(textChannel: TextChannel, group: TrackGroup, serverName: string) {
  const expectedTitle = `${group.name} - ${serverName}`;
  const messages = await textChannel.messages.fetch({ limit: 25 }).catch(() => null);
  return messages?.find((message) =>
    message.author.id === textChannel.client.user?.id &&
    message.embeds.some((embed) => embed.title === expectedTitle),
  ) ?? null;
}

function buildJoinLeaveEmbed(row: TrackGroupOnlineRow, isOnline: boolean, serverName: string) {
  const action = isOnline ? "joined" : "left";
  const color = isOnline ? JOIN_COLOR : LEAVE_COLOR;
  const label = row.bmName.trim().length > 0 ? row.bmName : (row.bmId || row.steamId);
  const lines = [
    `${EMOJIS.steam} [${row.steamId}](https://steamcommunity.com/profiles/${row.steamId})`,
  ];
  if (row.bmId) {
    lines.push(`${EMOJIS.battlemetrics} [${row.bmId}](https://www.battlemetrics.com/rcon/players/${row.bmId})`);
  }
  if (row.discordId) {
    lines.push(`${EMOJIS.discord} <@${row.discordId}>`);
  }

  const embed = new EmbedBuilder()
    .setTitle(`${label} has ${action} ${serverName}`)
    .setColor(color)
    .addFields({ name: "Information", value: lines.join("\n") })
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  if (row.avatarImage) {
    embed.setThumbnail(row.avatarImage);
  }

  return embed;
}

// ---------------------------------------------------------------------------
// Analytics button: website-admin gated chart image.
// ---------------------------------------------------------------------------

const ANALYTICS_EMBED_COLOR = 14582045;
const ANALYTICS_FIELD_VALUE_MAX = 1024;
const ANALYTICS_EMBED_BODY_MAX = 5800;

async function handleAnalyticsButton(
  interaction: ButtonInteraction,
  groupId: number,
  targetPage: number,
) {
  if (!(await isWebsiteAdmin(interaction.user.id))) {
    await interaction.reply({
      embeds: [buildErrorEmbed("Only website admins can view analytics.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply();

  let analytics: TrackGroupAnalyticsResponse;
  try {
    analytics = await getBackend().getTrackGroupAnalytics(getWorkerID(), interaction.guildId!, groupId);
  } catch (error) {
    console.error("[bot] failed to load track group analytics", error);
    await interaction.editReply({
      embeds: [buildErrorEmbed("RustLens could not load analytics for this track group.")],
    });
    return true;
  }

  if (analytics.analyzed === 0) {
    await interaction.editReply({
      embeds: [
        buildErrorEmbed(
          analytics.members === 0
            ? "Add at least one tracked user before viewing analytics."
            : "None of this group's members have BattleMetrics session data yet.",
        ),
      ],
    });
    return true;
  }

  const hasData = analytics.days.some((day) => day.hours.some((hour) => (hour.minutes ?? hour.avg) > 0));
  if (!hasData) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("Not enough activity in the last 200 sessions to compute analytics.")],
    });
    return true;
  }

  try {
    const image = await renderTrackAnalyticsChart(analytics);
    const file = new AttachmentBuilder(image, { name: "track-analytics.png" });
    await interaction.editReply({
      embeds: [buildAnalyticsChartEmbed(analytics)],
      files: [file],
      components: [],
    });
  } catch (error) {
    console.error("[bot] failed to render track group analytics chart", error);
    await interaction.editReply({
      embeds: [buildErrorEmbed("RustLens could not render the analytics chart.")],
    });
  }
  return true;
}

async function handleAnalyticsPage(
  interaction: ButtonInteraction,
  groupId: number,
  targetPage: number,
) {
  if (!(await isWebsiteAdmin(interaction.user.id))) {
    await interaction.reply({
      embeds: [buildErrorEmbed("Only website admins can view analytics.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferUpdate();

  let analytics: TrackGroupAnalyticsResponse;
  try {
    analytics = await getBackend().getTrackGroupAnalytics(getWorkerID(), interaction.guildId!, groupId);
  } catch (error) {
    console.error("[bot] failed to load track group analytics", error);
    await interaction.editReply({
      embeds: [buildErrorEmbed("RustLens could not refresh analytics for this track group.")],
      components: [],
    });
    return true;
  }

  const hasData = analytics.days.some((day) => day.hours.some((hour) => (hour.minutes ?? hour.avg) > 0));
  if (!hasData) {
    await interaction.editReply({
      embeds: [buildErrorEmbed("Not enough activity to compute analytics.")],
      components: [],
    });
    return true;
  }

  const image = await renderTrackAnalyticsChart(analytics);
  const file = new AttachmentBuilder(image, { name: "track-analytics.png" });
  await interaction.editReply({ embeds: [buildAnalyticsChartEmbed(analytics)], files: [file], components: [] });
  return true;
}

function buildAnalyticsChartEmbed(analytics: TrackGroupAnalyticsResponse) {
  const embed = new EmbedBuilder()
    .setTitle(`Analytics - ${analytics.groupName}`)
    .setDescription(`**${analytics.serverName}**`)
    .setColor(ANALYTICS_EMBED_COLOR)
    .setImage("attachment://track-analytics.png")
    .setTimestamp()
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL });

  const window = computeNextBestOfflineWindow(analytics);
  if (window) {
    embed.addFields({
      name: "Next best offline time",
      value: buildNextBestOfflineFieldValue(window),
    });
  }
  return embed;
}

type AnalyticsField = { name: string; value: string; inline: boolean };

function formatAvg(value: number) {
  if (value >= 10) {
    return value.toFixed(0);
  }
  return value.toFixed(2).replace(/\.?0+$/, "");
}

function averageForBestWindow(day: TrackGroupAnalyticsDay) {
  const windowHours = day.hours.filter((hour) => hour.hourUnix >= day.bestStart && hour.hourUnix < day.bestEnd);
  if (windowHours.length === 0) {
    return 0;
  }
  return windowHours.reduce((sum, hour) => sum + hour.avg, 0) / windowHours.length;
}

function buildWeekdayFieldValue(day: TrackGroupAnalyticsDay) {
  const lines: string[] = [];
  lines.push("Best time to go for an offline:");
  lines.push(`> * <t:${day.bestStart}:t> - <t:${day.bestEnd}:t>`);
  lines.push("Average people online:");
  lines.push(`> * **${formatAvg(averageForBestWindow(day))}**`);
  return lines.join("\n").slice(0, ANALYTICS_FIELD_VALUE_MAX);
}

function buildAnalyticsFields(analytics: TrackGroupAnalyticsResponse): AnalyticsField[] {
  return analytics.days.map((day) => ({
    name: day.weekday,
    value: buildWeekdayFieldValue(day),
    inline: true,
  }));
}

function buildAnalyticsPages(
  analytics: TrackGroupAnalyticsResponse,
  bodyMax = ANALYTICS_EMBED_BODY_MAX,
): EmbedBuilder[] {
  const fields = buildAnalyticsFields(analytics);
  const title = `Analytics - ${analytics.groupName}`;
  const titleLength = title.length;

  const pages: EmbedBuilder[] = [];
  let currentFields: AnalyticsField[] = [];
  let currentLength = titleLength;

  const flush = () => {
    if (currentFields.length === 0) {
      return;
    }
    pages.push(
      new EmbedBuilder()
        .setTitle(title)
        .setColor(ANALYTICS_EMBED_COLOR)
        .addFields(currentFields)
        .setTimestamp()
        .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL }),
    );
    currentFields = [];
    currentLength = titleLength;
  };

  for (const field of fields) {
    const fieldLength = field.name.length + field.value.length;
    if (currentFields.length > 0 && currentLength + fieldLength > bodyMax) {
      flush();
    }
    currentFields.push(field);
    currentLength += fieldLength;
  }
  flush();

  return pages;
}

function buildAnalyticsPagerComponents(groupId: number, currentPage: number, totalPages: number) {
  const previousPage = (currentPage - 1 + totalPages) % totalPages;
  const nextPage = (currentPage + 1) % totalPages;
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_ANALYTICS_PAGE_PREFIX}${groupId}:${previousPage}`)
        .setEmoji(PAGE_PREVIOUS_EMOJI)
        .setStyle(ButtonStyle.Secondary),
      new ButtonBuilder()
        .setCustomId(`${TRACK_BUTTON_ANALYTICS_PAGE_PREFIX}${groupId}:${nextPage}`)
        .setEmoji(PAGE_NEXT_EMOJI)
        .setStyle(ButtonStyle.Secondary),
    ),
  ];
}

export const __testables = {
  buildChannelName,
  buildAddRemoveModal,
  buildPlayerSelectComponents,
  buildTrackButtons,
  parseIdentifiers,
  buildTrackEmbeds,
  sanitizeGroupNameForChannel,
  buildAnalyticsPages,
  buildWeekdayFieldValue,
};
