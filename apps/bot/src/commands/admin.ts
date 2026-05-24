import type { ChatInputCommandInteraction, Client } from "discord.js";
import { ApplicationIntegrationType, EmbedBuilder, InteractionContextType, MessageFlags, SlashCommandBuilder } from "discord.js";
import { BackendClient } from "../backend-client.js";
import type { CommandUsageTarget } from "../backend-client.js";
import { loadConfig } from "../config.js";
import { fetchGuildSettings, hasGuildAdminAccess, resolveConfiguredGuildChannel } from "../guild-settings.js";
import { logger } from "../logger.js";
import { createEmbed, formatDiscordUserName, logDiscordCatch } from "../shared.js";
import { resolveSteamInputErrorMessage } from "../steam-input.js";

export const ADMIN_COMMAND_NAME = "admin";

export type AdminHandlerResult = {
  handled: true;
  targets: CommandUsageTarget[];
};

const ADMIN_SET_STEAM_SUBCOMMAND_NAME = "setsteam";
const ADMIN_REMOVE_STEAM_SUBCOMMAND_NAME = "removesteam";
const ADMIN_USER_OPTION_NAME = "user";
const ADMIN_STEAM_ID_OPTION_NAME = "steam_id";

const shardID = process.env.SHARD_ID ?? "0";

const adminCommand = new SlashCommandBuilder()
  .setName(ADMIN_COMMAND_NAME)
  .setDescription("Guild administrator commands")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(ADMIN_SET_STEAM_SUBCOMMAND_NAME)
      .setDescription("Set a guild Steam link for a user")
      .addUserOption((option) =>
        option
          .setName(ADMIN_USER_OPTION_NAME)
          .setDescription("User to link")
          .setRequired(true),
      )
      .addStringOption((option) =>
        option
          .setName(ADMIN_STEAM_ID_OPTION_NAME)
          .setDescription("17-digit Steam ID or steamcommunity.com profile URL")
          .setRequired(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(ADMIN_REMOVE_STEAM_SUBCOMMAND_NAME)
      .setDescription("Remove guild Steam links for a user")
      .addUserOption((option) =>
        option
          .setName(ADMIN_USER_OPTION_NAME)
          .setDescription("User to unlink")
          .setRequired(true),
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
    _workerID = `${config.workerId}-admin-${shardID}`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildAdminSteamEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

function buildAdminSetSteamLogEmbed(
  interaction: ChatInputCommandInteraction,
  targetUserId: string,
  steamId: string,
  bmId: string | null,
  replacedSteamIds: string[],
) {
  const embed = createEmbed()
    .setTitle("Steam account linked by admin")
    .setAuthor({
      name: formatDiscordUserName(interaction.user),
      iconURL: interaction.user.displayAvatarURL({ size: 128 }),
    })
    .addFields(
      { name: "User", value: `<@${targetUserId}>`, inline: true },
      { name: "Steam ID", value: `\`${steamId}\``, inline: true },
      { name: "Admin", value: `<@${interaction.user.id}>`, inline: true },
    );

  if (bmId) {
    embed.addFields({ name: "BM ID", value: `\`${bmId}\``, inline: true });
  }

  if (replacedSteamIds.length > 0) {
    embed.addFields({
      name: "Replaced",
      value: replacedSteamIds.map((id) => `\`${id}\``).join(", "),
      inline: false,
    });
  }

  return embed;
}

function buildAdminRemoveSteamLogEmbed(
  interaction: ChatInputCommandInteraction,
  targetUserId: string,
  removedSteamIds: string[],
) {
  return createEmbed()
    .setTitle("Steam account removed by admin")
    .setAuthor({
      name: formatDiscordUserName(interaction.user),
      iconURL: interaction.user.displayAvatarURL({ size: 128 }),
    })
    .addFields(
      { name: "User", value: `<@${targetUserId}>`, inline: true },
      { name: "Admin", value: `<@${interaction.user.id}>`, inline: true },
      {
        name: "Removed Steam ID" + (removedSteamIds.length === 1 ? "" : "s"),
        value: removedSteamIds.map((id) => `\`${id}\``).join(", "),
        inline: false,
      },
    );
}

async function sendAdminSteamLog(
  interaction: ChatInputCommandInteraction,
  embed: EmbedBuilder,
  action: string,
) {
  if (!interaction.guildId || !interaction.guild) {
    return;
  }

  const settings = await fetchGuildSettings(interaction.guildId).catch(logDiscordCatch(`fetch /admin ${action} linking log settings`, null, {
    guildId: interaction.guildId,
    discordId: interaction.user.id,
  }));
  if (!settings?.linkingLogChannelId) {
    return;
  }

  const channel = await resolveConfiguredGuildChannel(interaction.guild, settings.linkingLogChannelId);
  if (!channel?.isTextBased() || !("send" in channel)) {
    return;
  }

  await channel.send({ embeds: [embed] }).catch(logDiscordCatch(`send /admin ${action} linking log`, undefined, {
    guildId: interaction.guildId,
    channelId: channel.id,
    discordId: interaction.user.id,
  }));
}

async function replyEphemeralEmbed(
  interaction: ChatInputCommandInteraction,
  embed: EmbedBuilder,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ embeds: [embed] });
    return;
  }

  await interaction.reply({
    embeds: [embed],
    flags: MessageFlags.Ephemeral,
  });
}

async function deferEphemeral(interaction: ChatInputCommandInteraction) {
  if (interaction.deferred || interaction.replied) {
    return;
  }

  await interaction.deferReply({
    flags: MessageFlags.Ephemeral,
  });
}

async function removeGuildSteamLinks(guildID: string, discordID: string) {
  const backend = getBackend();
  const workerID = getWorkerID();
  const existing = await backend.listGuildLinksByDiscord(workerID, guildID, discordID);

  for (const link of existing.items) {
    await backend.deleteGuildLink(workerID, guildID, discordID, link.steamId);
  }

  return existing.items;
}

export async function ensureAdminCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = adminCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  logger.info("ensured /admin command (global)");
}

export async function handleAdminChatInput(interaction: ChatInputCommandInteraction): Promise<false | true | AdminHandlerResult> {
  if (interaction.commandName !== ADMIN_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeralEmbed(
      interaction,
      buildAdminSteamEmbed("Error", "Admin commands can only be used inside a server."),
    );
    return true;
  }

  await deferEphemeral(interaction);

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeralEmbed(
      interaction,
      buildAdminSteamEmbed("Error", "Only the configured admin role or administrators can use /admin."),
    );
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);

  if (subcommand === ADMIN_SET_STEAM_SUBCOMMAND_NAME) {
    const targetUser = interaction.options.getUser(ADMIN_USER_OPTION_NAME, true);

    if (targetUser.bot) {
      await replyEphemeralEmbed(
        interaction,
        buildAdminSteamEmbed("Error", "You cannot link a Steam ID to a bot account."),
      );
      return true;
    }

    const steamInput = interaction.options.getString(ADMIN_STEAM_ID_OPTION_NAME, true).trim();

    let steamID: string;
    try {
      const resolved = await getBackend().resolveSteamID(getWorkerID(), steamInput);
      steamID = resolved.steamId;
    } catch (error) {
      await replyEphemeralEmbed(
        interaction,
        buildAdminSteamEmbed("Error", resolveSteamInputErrorMessage(error)),
      );
      return true;
    }

    try {
      await getBackend().createSoftLink(getWorkerID(), steamID, targetUser.id, "bot_command");
    } catch (error) {
      const status = error instanceof Error ? (error as any).status : undefined;
      if (status !== 409) {
        await replyEphemeralEmbed(
          interaction,
          buildAdminSteamEmbed("Error", `RustLens could not save the Steam ID for <@${targetUser.id}>.`),
        );
        return true;
      }
    }

    let removed: { steamId: string }[] = [];
    try {
      removed = await removeGuildSteamLinks(interaction.guildId, targetUser.id);
      await getBackend().createGuildLink(getWorkerID(), interaction.guildId, targetUser.id, steamID);
    } catch {
      await replyEphemeralEmbed(
        interaction,
        buildAdminSteamEmbed("Error", `RustLens could not set the Steam ID for <@${targetUser.id}> in this guild.`),
      );
      return true;
    }

    const steamAccount = await getBackend().getSteamAccount(getWorkerID(), steamID).catch(() => null);
    await sendAdminSteamLog(
      interaction,
      buildAdminSetSteamLogEmbed(
        interaction,
        targetUser.id,
        steamID,
        steamAccount?.bmId ?? null,
        removed.map((link) => link.steamId).filter((id) => id !== steamID),
      ),
      "setsteam",
    );

    await replyEphemeralEmbed(
      interaction,
      buildAdminSteamEmbed("Steam ID updated", `Linked <@${targetUser.id}> to Steam ID \`${steamID}\` in this guild.`),
    );
    return {
      handled: true,
      targets: [
        {
          kind:        "user",
          snowflake:   targetUser.id,
          displayName: targetUser.globalName ?? targetUser.username,
          avatarHash:  targetUser.avatar ?? null,
          relation:    "subject",
        },
        { kind: "steam", snowflake: steamID, relation: "subject" },
      ],
    };
  }

  if (subcommand === ADMIN_REMOVE_STEAM_SUBCOMMAND_NAME) {
    const targetUser = interaction.options.getUser(ADMIN_USER_OPTION_NAME, true);

    let removed: { steamId: string }[] = [];
    try {
      removed = await removeGuildSteamLinks(interaction.guildId, targetUser.id);
    } catch {
      await replyEphemeralEmbed(
        interaction,
        buildAdminSteamEmbed("Error", `RustLens could not remove the Steam ID for <@${targetUser.id}>.`),
      );
      return true;
    }

    if (removed.length === 0) {
      await replyEphemeralEmbed(
        interaction,
        buildAdminSteamEmbed("Nothing to remove", `<@${targetUser.id}> does not have a Steam ID linked in this guild.`),
      );
      return true;
    }

    await sendAdminSteamLog(
      interaction,
      buildAdminRemoveSteamLogEmbed(
        interaction,
        targetUser.id,
        removed.map((link) => link.steamId),
      ),
      "removesteam",
    );

    await replyEphemeralEmbed(
      interaction,
      buildAdminSteamEmbed("Steam ID removed", `Removed ${removed.length} guild Steam link${removed.length === 1 ? "" : "s"} for <@${targetUser.id}>.`),
    );
    return {
      handled: true,
      targets: [
        {
          kind:        "user",
          snowflake:   targetUser.id,
          displayName: targetUser.globalName ?? targetUser.username,
          avatarHash:  targetUser.avatar ?? null,
          relation:    "subject",
        },
      ],
    };
  }

  await replyEphemeralEmbed(
    interaction,
    buildAdminSteamEmbed("Error", `Unknown /${ADMIN_COMMAND_NAME} subcommand: ${subcommand}`),
  );
  return true;
}
