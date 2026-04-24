import type {
  APIEmbedField,
  AutocompleteInteraction,
  ChatInputCommandInteraction,
  Client,
  GuildMember,
} from "discord.js";
import { EmbedBuilder, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import {
  BackendClient,
  type SteamAccount,
  type TrackedServer,
  type WipeReact,
} from "./backend-client.js";
import { loadConfig } from "./config.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { RUSTLENS_EMBED_COLOR, RUSTLENS_EMBED_ICON_URL } from "./shared.js";

export const COMPARE_COMMAND_NAME = "compare";

const COMPARE_SERVER_OPTION_NAME = "server";
const COMPARE_WIPEREACT_OPTION_NAME = "wipereact";
const FOOTER_TEXT = "RustLens.com";

const compareCommand = new SlashCommandBuilder()
  .setName(COMPARE_COMMAND_NAME)
  .setDescription("Compare your current voice channel against a wipe react or tracked server")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .addStringOption((option) =>
    option
      .setName(COMPARE_SERVER_OPTION_NAME)
      .setDescription("Tracked server to compare against")
      .setRequired(false)
      .setAutocomplete(true),
  )
  .addStringOption((option) =>
    option
      .setName(COMPARE_WIPEREACT_OPTION_NAME)
      .setDescription("Past wipe react to compare against")
      .setRequired(false)
      .setAutocomplete(true),
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
    _workerID = `${config.workerId}-compare`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildBaseEmbed(title: string, description: string) {
  return new EmbedBuilder()
    .setTitle(title)
    .setDescription(description)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter({ text: FOOTER_TEXT, iconURL: RUSTLENS_EMBED_ICON_URL });
}

function formatUserLines(userIds: readonly string[]) {
  return userIds.map((userId) => `> * <@${userId}>`).join("\n");
}

function uniqueSorted(values: Iterable<string>) {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right));
}

function formatWipeReactDate(wipeReact: Pick<WipeReact, "scheduledUnix" | "timeZone">) {
  return new Intl.DateTimeFormat("en-GB", {
    timeZone: wipeReact.timeZone,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(wipeReact.scheduledUnix * 1000));
}

function formatWipeReactChoiceName(wipeReact: WipeReact) {
  const label = `${wipeReact.name} | ${formatWipeReactDate(wipeReact)}`;
  return label.length <= 100 ? label : `${label.slice(0, 97)}...`;
}

function filterChoices<T>(
  items: readonly T[],
  focusedValue: string,
  getName: (item: T) => string,
  getValue: (item: T) => string,
) {
  const needle = focusedValue.trim().toLocaleLowerCase();
  return items
    .filter((item) => {
      if (needle.length === 0) {
        return true;
      }

      return getName(item).toLocaleLowerCase().includes(needle) || getValue(item).toLocaleLowerCase().includes(needle);
    })
    .slice(0, 25)
    .map((item) => ({
      name: getName(item),
      value: getValue(item),
    }));
}

function buildWipeReactComparisonEmbed(
  wipeReact: WipeReact,
  acceptedNotInVoice: readonly string[],
  lateNotInVoice: readonly string[],
  declinedInVoice: readonly string[],
) {
  const outOfSyncCount = new Set([
    ...acceptedNotInVoice,
    ...lateNotInVoice,
    ...declinedInVoice,
  ]).size;

  const embed = buildBaseEmbed(
    "Wipe React Comparison",
    `**Compared Against**\n${wipeReact.name} | ${formatWipeReactDate(wipeReact)} | \`${wipeReact.messageId}\`\n\n${outOfSyncCount} users found out of sync`,
  );

  const fields: APIEmbedField[] = [];
  if (acceptedNotInVoice.length > 0) {
    fields.push({
      name: "✅ Accepted - Not in Voice",
      value: formatUserLines(acceptedNotInVoice),
      inline: false,
    });
  }
  if (lateNotInVoice.length > 0) {
    fields.push({
      name: "⏰ Late - Not in Voice",
      value: formatUserLines(lateNotInVoice),
      inline: false,
    });
  }
  if (declinedInVoice.length > 0) {
    fields.push({
      name: "❌ Declined - In Voice",
      value: formatUserLines(declinedInVoice),
      inline: false,
    });
  }

  if (fields.length > 0) {
    embed.addFields(fields);
  }

  return embed;
}

function buildServerComparisonEmbed(
  serverName: string,
  needToJoinServer: readonly string[],
  needToJoinVoice: readonly string[],
) {
  const outOfSyncCount = new Set([...needToJoinServer, ...needToJoinVoice]).size;
  const embed = buildBaseEmbed(
    "Server Comparison ",
    `**Compared Against**\n${serverName}\n\n${outOfSyncCount} users out of sync`,
  );

  const fields: APIEmbedField[] = [];
  if (needToJoinServer.length > 0) {
    fields.push({
      name: "🎮 Need to Join Server",
      value: formatUserLines(needToJoinServer),
      inline: false,
    });
  }
  if (needToJoinVoice.length > 0) {
    fields.push({
      name: "🎙️ Need to Join Voice",
      value: formatUserLines(needToJoinVoice),
      inline: false,
    });
  }

  if (fields.length > 0) {
    embed.addFields(fields);
  }

  return embed;
}

async function listTrackedServers() {
  const response = await getBackend().listTrackedServers(getWorkerID());
  return response.items;
}

async function listWipeReacts(guildId: string) {
  const response = await getBackend().listWipeReacts(getWorkerID(), guildId);
  return response.items;
}

async function listPassedWipeReacts(guildId: string, nowUnix = Math.floor(Date.now() / 1000)) {
  return (await listWipeReacts(guildId)).filter((item) => item.scheduledUnix <= nowUnix);
}

async function resolveSelectedWipeReact(guildId: string, messageId: string | null) {
  if (messageId) {
    return getBackend().getWipeReact(getWorkerID(), guildId, messageId);
  }

  const mostRecentPassed = (await listPassedWipeReacts(guildId))[0] ?? null;
  return mostRecentPassed;
}

async function fetchComparisonMembers(interaction: ChatInputCommandInteraction) {
  const guild = interaction.guild;
  if (!guild) {
    return null;
  }

  const member = await guild.members.fetch(interaction.user.id).catch(() => null);
  const voiceChannel = member?.voice.channel;
  if (voiceChannel) {
    return [...voiceChannel.members.values()].filter((voiceMember) => !voiceMember.user.bot);
  }

  const members = await getGuildMembers(guild);
  return [...members.values()].filter((guildMember) => !guildMember.user.bot);
}

async function fetchInvokerVoiceMembers(interaction: ChatInputCommandInteraction) {
  const guild = interaction.guild;
  if (!guild) {
    return null;
  }

  const member = await guild.members.fetch(interaction.user.id).catch(() => null);
  const voiceChannel = member?.voice.channel;
  if (!voiceChannel) {
    return [] as GuildMember[];
  }

  return [...voiceChannel.members.values()].filter((voiceMember) => !voiceMember.user.bot);
}

type GuildMappings = {
  discordToBmIds: Map<string, Set<string>>;
  bmToDiscordIds: Map<string, Set<string>>;
};

function addMappedValue(map: Map<string, Set<string>>, key: string, value: string) {
  const current = map.get(key) ?? new Set<string>();
  current.add(value);
  map.set(key, current);
}

async function resolveGuildMappings(guildId: string) {
  const backend = getBackend();
  const workerId = getWorkerID();
  const { items: guildLinks } = await backend.listGuildLinks(workerId, guildId);
  const steamIds = uniqueSorted(guildLinks.map((item) => item.steamId));
  const accounts = await Promise.all(
    steamIds.map(async (steamId) => {
      try {
        const account = await backend.getSteamAccount(workerId, steamId);
        return [steamId, account] as const;
      } catch {
        return [steamId, null] as const;
      }
    }),
  );

  const steamAccounts = new Map<string, SteamAccount | null>(accounts);
  const mappings: GuildMappings = {
    discordToBmIds: new Map(),
    bmToDiscordIds: new Map(),
  };

  for (const link of guildLinks) {
    const account = steamAccounts.get(link.steamId);
    const bmId = account?.bmId?.trim() ?? "";
    if (!bmId) {
      continue;
    }

    addMappedValue(mappings.discordToBmIds, link.discordId, bmId);
    addMappedValue(mappings.bmToDiscordIds, bmId, link.discordId);
  }

  return mappings;
}

function memberIds(members: readonly GuildMember[]) {
  return members.map((member) => member.id);
}

async function handleWipeReactComparison(
  interaction: ChatInputCommandInteraction,
  voiceMembers: readonly GuildMember[],
  wipeReactMessageId: string | null,
) {
  const wipeReact = await resolveSelectedWipeReact(interaction.guildId!, wipeReactMessageId);
  if (!wipeReact) {
    await interaction.editReply({
      content: "No passed wipe react was found for this server.",
      embeds: [],
    });
    return true;
  }

  const voiceUserIds = new Set(memberIds(voiceMembers));
  const acceptedNotInVoice = uniqueSorted(
    wipeReact.acceptedUserIds.filter((userId) => !voiceUserIds.has(userId)),
  );
  const lateNotInVoice = uniqueSorted(
    wipeReact.lateUserIds.filter((userId) => !voiceUserIds.has(userId)),
  );
  const declinedSet = new Set(wipeReact.declinedUserIds);
  const declinedInVoice = uniqueSorted(
    [...voiceUserIds].filter((userId) => declinedSet.has(userId)),
  );

  await interaction.editReply({
    embeds: [buildWipeReactComparisonEmbed(wipeReact, acceptedNotInVoice, lateNotInVoice, declinedInVoice)],
  });
  return true;
}

async function handleServerComparison(
  interaction: ChatInputCommandInteraction,
  voiceMembers: readonly GuildMember[],
  serverId: string,
) {
  const backend = getBackend();
  const workerId = getWorkerID();
  const [trackedServers, playerStates, mappings] = await Promise.all([
    listTrackedServers(),
    backend.listOnlinePlayerStatesByServer(workerId, serverId),
    resolveGuildMappings(interaction.guildId!),
  ]);

  const server = trackedServers.find((item) => item.bmId === serverId) ?? null;
  const serverName = server?.name ?? playerStates.items[0]?.serverName ?? serverId;
  const onlineBmIds = new Set(playerStates.items.map((item) => item.playerBmId));
  const voiceUserIds = memberIds(voiceMembers);
  const voiceSet = new Set(voiceUserIds);

  const needToJoinServer = uniqueSorted(
    voiceUserIds.filter((discordId) => {
      const bmIds = mappings.discordToBmIds.get(discordId);
      if (!bmIds || bmIds.size === 0) {
        return false;
      }

      return [...bmIds].every((bmId) => !onlineBmIds.has(bmId));
    }),
  );

  const onlineDiscordIds = new Set<string>();
  for (const bmId of onlineBmIds) {
    const discordIds = mappings.bmToDiscordIds.get(bmId);
    if (!discordIds) {
      continue;
    }

    for (const discordId of discordIds) {
      onlineDiscordIds.add(discordId);
    }
  }

  const needToJoinVoice = uniqueSorted(
    [...onlineDiscordIds].filter((discordId) => !voiceSet.has(discordId)),
  );

  await interaction.editReply({
    embeds: [buildServerComparisonEmbed(serverName, needToJoinServer, needToJoinVoice)],
  });
  return true;
}

export async function ensureCompareCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = compareCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /compare command (global)");
}

export async function handleCompareAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== COMPARE_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.respond([]);
    return true;
  }

  const focused = interaction.options.getFocused(true);

  if (focused.name === COMPARE_SERVER_OPTION_NAME) {
    const trackedServers = await listTrackedServers().catch(() => []);
    await interaction.respond(
      filterChoices(
        trackedServers,
        String(focused.value ?? ""),
        (item: TrackedServer) => item.name,
        (item: TrackedServer) => item.bmId,
      ),
    );
    return true;
  }

  if (focused.name === COMPARE_WIPEREACT_OPTION_NAME) {
    const wipeReacts = await listPassedWipeReacts(interaction.guildId).catch(() => []);
    await interaction.respond(
      filterChoices(
        wipeReacts,
        String(focused.value ?? ""),
        (item) => formatWipeReactChoiceName(item),
        (item) => item.messageId,
      ),
    );
    return true;
  }

  return false;
}

export async function handleCompareChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== COMPARE_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /compare.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const serverId = interaction.options.getString(COMPARE_SERVER_OPTION_NAME);
  const wipeReactMessageId = interaction.options.getString(COMPARE_WIPEREACT_OPTION_NAME);

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  if (wipeReactMessageId || !serverId) {
    const comparisonMembers = await fetchComparisonMembers(interaction);
    if (!comparisonMembers) {
      await interaction.editReply({
        content: "RustLens could not load members for this server.",
      });
      return true;
    }
    return handleWipeReactComparison(interaction, comparisonMembers, wipeReactMessageId);
  }

  const voiceMembers = await fetchInvokerVoiceMembers(interaction);
  if (!voiceMembers) {
    await interaction.editReply({
      content: "RustLens could not load members for this server.",
    });
    return true;
  }

  return handleServerComparison(interaction, voiceMembers, serverId);
}

export const __testables = {
  buildServerComparisonEmbed,
  buildWipeReactComparisonEmbed,
  filterChoices,
  formatWipeReactChoiceName,
};
