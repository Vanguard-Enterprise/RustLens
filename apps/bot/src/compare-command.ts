import type {
  AutocompleteInteraction,
  ChatInputCommandInteraction,
  Client,
  GuildMember,
} from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import {
  BackendClient,
  type SteamAccount,
  type TrackedServer,
  type WipeReact,
} from "./backend-client.js";
import { loadConfig } from "./config.js";
import { paginateSections, replyPaginatedEmbeds, type PaginatedSection } from "./embed-paginator.js";
import { getGuildMembers } from "./guild-member-cache.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { buildFooter, RUSTLENS_EMBED_COLOR } from "./shared.js";

export const COMPARE_COMMAND_NAME = "compare";

const COMPARE_SERVER_OPTION_NAME = "server";
const COMPARE_WIPEREACT_OPTION_NAME = "wipereact";

const compareCommand = new SlashCommandBuilder()
  .setName(COMPARE_COMMAND_NAME)
  .setDescription("Compare guild voice channels against a wipe react or tracked server")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
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
    .setFooter(buildFooter());
}

function userLines(userIds: readonly string[]): string[] {
  return userIds.map((userId) => `> * <@${userId}>`);
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

function buildWipeReactComparisonEmbeds(
  wipeReact: WipeReact,
  acceptedNotInVoice: readonly string[],
  lateNotInVoice: readonly string[],
  declinedInVoice: readonly string[],
): EmbedBuilder[] {
  const outOfSyncCount = new Set([
    ...acceptedNotInVoice,
    ...lateNotInVoice,
    ...declinedInVoice,
  ]).size;
  const description = `**Compared Against**\n${wipeReact.name} | ${formatWipeReactDate(wipeReact)} | \`${wipeReact.messageId}\`\n\n${outOfSyncCount} users found out of sync`;

  const sections: PaginatedSection[] = [
    { name: "✅ Accepted - Not in Voice", lines: userLines(acceptedNotInVoice) },
    { name: "⏰ Late - Not in Voice", lines: userLines(lateNotInVoice) },
    { name: "❌ Declined - In Voice", lines: userLines(declinedInVoice) },
  ];

  return paginateSections(sections, () => buildBaseEmbed("Wipe React Comparison", description));
}

function buildWipeReactServerComparisonEmbeds(
  wipeReact: WipeReact,
  serverName: string,
  acceptedNotOnServer: readonly string[],
  lateNotOnServer: readonly string[],
  declinedOnServer: readonly string[],
): EmbedBuilder[] {
  const outOfSyncCount = new Set([
    ...acceptedNotOnServer,
    ...lateNotOnServer,
    ...declinedOnServer,
  ]).size;
  const description = `**Compared Against**\n${wipeReact.name} | ${formatWipeReactDate(wipeReact)} | \`${wipeReact.messageId}\`\n${serverName}\n\n${outOfSyncCount} users found out of sync`;

  const sections: PaginatedSection[] = [
    { name: "✅ Accepted - Not on Server", lines: userLines(acceptedNotOnServer) },
    { name: "⏰ Late - Not on Server", lines: userLines(lateNotOnServer) },
    { name: "❌ Declined - On Server", lines: userLines(declinedOnServer) },
  ];

  return paginateSections(sections, () => buildBaseEmbed("Wipe React vs Server Comparison", description));
}

function buildServerComparisonEmbeds(
  serverName: string,
  needToJoinServer: readonly string[],
  needToJoinVoice: readonly string[],
): EmbedBuilder[] {
  const outOfSyncCount = new Set([...needToJoinServer, ...needToJoinVoice]).size;
  const description = `**Compared Against**\n${serverName}\n\n${outOfSyncCount} users out of sync`;

  const sections: PaginatedSection[] = [
    { name: "🎮 Need to Join Server", lines: userLines(needToJoinServer) },
    { name: "🎙️ Need to Join Voice", lines: userLines(needToJoinVoice) },
  ];

  return paginateSections(sections, () => buildBaseEmbed("Server Comparison ", description));
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

function getGuildVoiceMembers(interaction: ChatInputCommandInteraction) {
  const guild = interaction.guild;
  if (!guild) {
    return null;
  }

  const voiceMembers = new Map<string, GuildMember>();
  for (const voiceState of guild.voiceStates.cache.values()) {
    const member = voiceState.member;
    if (!member || member.user.bot || !voiceState.channelId) {
      continue;
    }

    voiceMembers.set(member.id, member);
  }

  return [...voiceMembers.values()];
}

async function fetchComparisonMembers(interaction: ChatInputCommandInteraction) {
  const guild = interaction.guild;
  if (!guild) {
    return null;
  }

  const voiceMembers = getGuildVoiceMembers(interaction);
  if (voiceMembers && voiceMembers.length > 0) {
    return voiceMembers;
  }

  const members = await getGuildMembers(guild);
  return [...members.values()].filter((guildMember) => !guildMember.user.bot);
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

  const embeds = buildWipeReactComparisonEmbeds(wipeReact, acceptedNotInVoice, lateNotInVoice, declinedInVoice);
  await replyPaginatedEmbeds(interaction, embeds);
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

  const embeds = buildServerComparisonEmbeds(serverName, needToJoinServer, needToJoinVoice);
  await replyPaginatedEmbeds(interaction, embeds);
  return true;
}

async function handleWipeReactServerComparison(
  interaction: ChatInputCommandInteraction,
  wipeReactMessageId: string,
  serverId: string,
) {
  const backend = getBackend();
  const workerId = getWorkerID();
  const [wipeReact, trackedServers, playerStates, mappings] = await Promise.all([
    resolveSelectedWipeReact(interaction.guildId!, wipeReactMessageId),
    listTrackedServers(),
    backend.listOnlinePlayerStatesByServer(workerId, serverId),
    resolveGuildMappings(interaction.guildId!),
  ]);

  if (!wipeReact) {
    await interaction.editReply({
      content: "No passed wipe react was found for this server.",
      embeds: [],
    });
    return true;
  }

  const server = trackedServers.find((item) => item.bmId === serverId) ?? null;
  const serverName = server?.name ?? playerStates.items[0]?.serverName ?? serverId;
  const onlineBmIds = new Set(playerStates.items.map((item) => item.playerBmId));
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

  const acceptedNotOnServer = uniqueSorted(
    wipeReact.acceptedUserIds.filter((userId) => !onlineDiscordIds.has(userId)),
  );
  const lateNotOnServer = uniqueSorted(
    wipeReact.lateUserIds.filter((userId) => !onlineDiscordIds.has(userId)),
  );
  const declinedSet = new Set(wipeReact.declinedUserIds);
  const declinedOnServer = uniqueSorted(
    [...onlineDiscordIds].filter((userId) => declinedSet.has(userId)),
  );

  const embeds = buildWipeReactServerComparisonEmbeds(
    wipeReact,
    serverName,
    acceptedNotOnServer,
    lateNotOnServer,
    declinedOnServer,
  );
  await replyPaginatedEmbeds(interaction, embeds);
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

  await interaction.deferReply();

  if (wipeReactMessageId && serverId) {
    return handleWipeReactServerComparison(interaction, wipeReactMessageId, serverId);
  }

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

  const voiceMembers = getGuildVoiceMembers(interaction);
  if (!voiceMembers) {
    await interaction.editReply({
      content: "RustLens could not load members for this server.",
    });
    return true;
  }

  return handleServerComparison(interaction, voiceMembers, serverId);
}

export const __testables = {
  buildServerComparisonEmbeds,
  buildWipeReactComparisonEmbeds,
  buildWipeReactServerComparisonEmbeds,
  filterChoices,
  formatWipeReactChoiceName,
};
