import type { ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, InteractionContextType, MessageFlags, PermissionFlagsBits, SlashCommandBuilder } from "discord.js";
import { BackendClient, type GuildRecentGameBan } from "../backend-client.js";
import { loadConfig } from "../config.js";
import { getGuildMembers } from "../guild-member-cache.js";
import { hasGuildAdminAccess } from "../guild-settings.js";
import { buildProOnlyFeatureEmbed, createEmbed } from "../shared.js";
import { hasProFeatureAccess } from "../website-admin.js";

export const CHECK_GAME_BANS_COMMAND_NAME = "checkgamebans";
const CHECK_GAME_BANS_METHOD_OPTION = "method";
const CHECK_GAME_BANS_SCOPE_OPTION = "scope";
const CHECK_GAME_BANS_TIMEFRAME_OPTION = "timeframe";
const CHECK_GAME_BANS_METHOD_LIGHT = "light";
const CHECK_GAME_BANS_METHOD_DEEP = "deep";
const CHECK_GAME_BANS_TIMEFRAME_30D = "30d";
const CHECK_GAME_BANS_TIMEFRAME_90D = "90d";
const CHECK_GAME_BANS_TIMEFRAME_180D = "180d";
const CHECK_GAME_BANS_TIMEFRAME_ALL = "all";
type CheckGameBansMethod = typeof CHECK_GAME_BANS_METHOD_LIGHT | typeof CHECK_GAME_BANS_METHOD_DEEP;
type CheckGameBansTimeframe =
  | typeof CHECK_GAME_BANS_TIMEFRAME_30D
  | typeof CHECK_GAME_BANS_TIMEFRAME_90D
  | typeof CHECK_GAME_BANS_TIMEFRAME_180D
  | typeof CHECK_GAME_BANS_TIMEFRAME_ALL;

const CHECK_GAME_BANS_TIMEFRAME_LABELS: Record<CheckGameBansTimeframe, string> = {
  [CHECK_GAME_BANS_TIMEFRAME_30D]: "30 days",
  [CHECK_GAME_BANS_TIMEFRAME_90D]: "90 days",
  [CHECK_GAME_BANS_TIMEFRAME_180D]: "180 days",
  [CHECK_GAME_BANS_TIMEFRAME_ALL]: "all time",
};

const checkGameBansCommand = new SlashCommandBuilder()
  .setName(CHECK_GAME_BANS_COMMAND_NAME)
  .setDescription("Check linked guild Steam accounts for recent game bans")
  .addStringOption((opt) =>
    opt
      .setName(CHECK_GAME_BANS_METHOD_OPTION)
      .setDescription("How broadly to check linked Steam accounts")
      .setRequired(true)
      .addChoices(
        { name: "Light", value: CHECK_GAME_BANS_METHOD_LIGHT },
        { name: "Deep", value: CHECK_GAME_BANS_METHOD_DEEP },
      ),
  )
  .addStringOption((opt) =>
    opt
      .setName(CHECK_GAME_BANS_TIMEFRAME_OPTION)
      .setDescription("Only show game bans within this timeframe")
      .setRequired(false)
      .addChoices(
        { name: "30d", value: CHECK_GAME_BANS_TIMEFRAME_30D },
        { name: "90d", value: CHECK_GAME_BANS_TIMEFRAME_90D },
        { name: "180d", value: CHECK_GAME_BANS_TIMEFRAME_180D },
        { name: "All time", value: CHECK_GAME_BANS_TIMEFRAME_ALL },
      ),
  )
  .addRoleOption((opt) =>
    opt
      .setName(CHECK_GAME_BANS_SCOPE_OPTION)
      .setDescription("Only check members with this role")
      .setRequired(false),
  )
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

let _backend: BackendClient | undefined;
let _workerID: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerID = `${config.workerId}-checkgamebans`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildGameBanLine(item: GuildRecentGameBan) {
  return `> * <@${item.discordId}> - **[${item.steamId}](https://steamcommunity.com/profiles/${item.steamId})** - <t:${item.gameBannedUnix}:R>`;
}

function buildGameBanSection(title: string, items: GuildRecentGameBan[]) {
  if (items.length === 0) {
    return `**${title}**\n> None`;
  }
  return `**${title}**\n${items.map(buildGameBanLine).join("\n")}`;
}

function buildGameBansEmbed(
  items: GuildRecentGameBan[],
  timeframe: CheckGameBansTimeframe = CHECK_GAME_BANS_TIMEFRAME_90D,
  groupBySource = false,
) {
  const timeframeLabel = CHECK_GAME_BANS_TIMEFRAME_LABELS[timeframe];
  const emptyTimeframeText = timeframe === CHECK_GAME_BANS_TIMEFRAME_ALL ? "all time" : `the last ${timeframeLabel}`;
  const directLinks = items.filter((item) => item.source === "direct_guild_link");
  const linkedAccounts = items.filter((item) => item.source === "linked_account");
  const description =
    items.length === 0
      ? `No linked users have a game ban in ${emptyTimeframeText}.`
      : groupBySource
        ? [
            buildGameBanSection("Direct guild links", directLinks),
            buildGameBanSection("Other linked accounts", linkedAccounts),
          ].join("\n\n")
        : items.map(buildGameBanLine).join("\n");

  return createEmbed()
    .setTitle(`Gamebans - ${timeframeLabel}`)
    .setDescription(description);
}

function buildDeepScopeTooLargeEmbed() {
  return createEmbed()
    .setTitle("Deep Check Scope Too Large")
    .setDescription("More than 100 people in the current scope need a deep lookup. Try setting a smaller scope or use light mode.");
}

export async function ensureCheckGameBansCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = checkGameBansCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /checkgamebans command (global)");
}

export async function handleCheckGameBansChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== CHECK_GAME_BANS_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }
  const guild = interaction.guild;
  if (!guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await interaction.reply({
      content: "Only the configured admin role or administrators can use /checkgamebans.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const method = interaction.options.getString(CHECK_GAME_BANS_METHOD_OPTION, true) as CheckGameBansMethod;
  const timeframe =
    (interaction.options.getString(CHECK_GAME_BANS_TIMEFRAME_OPTION, false) as CheckGameBansTimeframe | null) ??
    CHECK_GAME_BANS_TIMEFRAME_90D;
  const scope = interaction.options.getRole(CHECK_GAME_BANS_SCOPE_OPTION, false);
  if (method === CHECK_GAME_BANS_METHOD_DEEP && !(await hasProFeatureAccess(interaction.user.id))) {
    await interaction.reply({
      embeds: [buildProOnlyFeatureEmbed()],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  try {
    let scopeDiscordIds: string[] = [];
    if (scope) {
      const members = await getGuildMembers(guild);
      scopeDiscordIds = members
        .filter((member) => !member.user.bot && member.roles.cache.has(scope.id))
        .map((member) => member.id);

      if (scopeDiscordIds.length === 0) {
        await interaction.editReply({ embeds: [buildGameBansEmbed([], timeframe, method === CHECK_GAME_BANS_METHOD_DEEP)] });
        return true;
      }
    }

    const response = await getBackend().listGuildRecentGameBans(getWorkerID(), interaction.guildId, method, timeframe, scopeDiscordIds);
    await interaction.editReply({ embeds: [buildGameBansEmbed(response.items, timeframe, method === CHECK_GAME_BANS_METHOD_DEEP)] });
  } catch (error) {
    if ((error as { status?: number }).status === 422) {
      await interaction.editReply({ embeds: [buildDeepScopeTooLargeEmbed()] });
      return true;
    }
    console.error("[bot] /checkgamebans backend call failed", error);
    await interaction.editReply({ content: "Failed to check game bans." });
  }

  return true;
}

export const __testables = {
  buildGameBanLine,
  buildGameBansEmbed,
  buildDeepScopeTooLargeEmbed,
};
