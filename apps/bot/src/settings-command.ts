import type {
  ChatInputCommandInteraction,
  Client,
  Guild,
  GuildBasedChannel,
  ModalSubmitInteraction,
  Role,
  StringSelectMenuInteraction,
} from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ChannelType,
  InteractionContextType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
  StringSelectMenuBuilder,
  escapeMarkdown,
} from "discord.js";
import { StringSelectBuilder, ModalV2Builder } from "./builders/index.js";
import { BackendClient, type GuildSettingsPatch } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { fetchGuildSettings, updateGuildSettings } from "./guild-settings.js";
import { createEmbed, getFieldValues, logDiscordCatch } from "./shared.js";

export const SETTINGS_COMMAND_NAME = "settings";

const SETTINGS_SELECT_CUSTOM_ID = "settings:select";
const SETTINGS_CHANNEL_MODAL_PREFIX = "settings:channel:";
const SETTINGS_CATEGORY_MODAL_PREFIX = "settings:category:";
const SETTINGS_ROLE_MODAL_PREFIX = "settings:role:";
const SETTINGS_SELECT_FIELD_CUSTOM_ID = "settings:value";
const SETTINGS_UNSET_VALUE = "__unset__";
const SETTINGS_CREATE_TRACKED_USERS_CATEGORY_VALUE = "__create_tracked_users_category__";
const TRACKED_USERS_CATEGORY_NAME = "Tracked clans";

const SETTINGS_OPTION_WIPE_REACT_LOGS = "wipe_react_logs";
const SETTINGS_OPTION_LINKING_LOGS = "linking_logs";
const SETTINGS_OPTION_TRACKED_SERVER_LOGS = "tracked_server_logs";
const SETTINGS_OPTION_ADMIN_ROLE = "admin_role";
const SETTINGS_OPTION_TRACKED_USERS = "tracked_users";

const guildSettingsCommand = new SlashCommandBuilder()
  .setName(SETTINGS_COMMAND_NAME)
  .setDescription("Manage guild settings")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

const shardID = process.env.SHARD_ID ?? "0";

type SettingsChoice = {
  label: string;
  value: string;
  description: string;
  emoji: string;
};

const settingsChoices: readonly SettingsChoice[] = [
  {
    label: "Wipe React Logs",
    value: SETTINGS_OPTION_WIPE_REACT_LOGS,
    description: "Set the wipe react logs channel",
    emoji: "📜",
  },
  {
    label: "Linking Logs",
    value: SETTINGS_OPTION_LINKING_LOGS,
    description: "Set the account linking logs channel",
    emoji: "🔗",
  },
  {
    label: "Tracked Server Logs",
    value: SETTINGS_OPTION_TRACKED_SERVER_LOGS,
    description: "Set the tracked server logs channel",
    emoji: "📃",
  },
  {
    label: "Admin Role",
    value: SETTINGS_OPTION_ADMIN_ROLE,
    description: "Set the admin role for protected commands",
    emoji: "🗝️",
  },
  {
    label: "Tracked users",
    value: SETTINGS_OPTION_TRACKED_USERS,
    description: "Category where channels get created for tracked users.",
    emoji: "📁",
  },
] as const;

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
    _workerID = `${config.workerId}-settings-${shardID}`;
  }

  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function formatChannelValue(channelId: string) {
  return channelId ? `<#${channelId}>` : "`Not set`";
}

async function formatCategoryValue(guild: Guild, categoryId: string) {
  if (!categoryId) {
    return "`Not set`";
  }

  const category =
    guild.channels.cache.get(categoryId) ??
    (await guild.channels.fetch(categoryId).catch(logDiscordCatch("fetch settings category display", null, {
      guildId: guild.id,
      channelId: categoryId,
    })));

  if (!category || category.type !== ChannelType.GuildCategory) {
    return "`Not set`";
  }

  return `**${escapeMarkdown(category.name)}**`;
}

function formatRoleValue(roleId: string) {
  return roleId ? `<@&${roleId}>` : "`Not set`";
}

async function buildSettingsPayload(guild: Guild) {
  const settings = await fetchGuildSettings(guild.id);
  const trackedUsersCategoryValue = await formatCategoryValue(guild, settings.trackedUsersCategoryId);

  const embed = createEmbed()
    .setTitle("Server Settings")
    .setDescription(
      "You can edit various settings related to your guild in here.\nRun `/server` to configure tracked servers for this guild.",
    )
    .addFields(
      {
        name: "📜 Wipe React Logs",
        value: `Logs for new reactions and reasons.\n${formatChannelValue(settings.wipeReactLogChannelId)}`,
        inline: true,
      },
      {
        name: "🔗 Linking Logs",
        value: `Logs for users linking their accounts.\n${formatChannelValue(settings.linkingLogChannelId)}`,
        inline: true,
      },
      {
        name: "📃 Tracked Server Logs",
        value: `Requires at least one tracked server.\n${formatChannelValue(settings.trackedServerLogChannelId)}`,
        inline: true,
      },
      {
        name: "🗝️ Admin Role",
        value: `Can run administrator-only commands.\n${formatRoleValue(settings.adminRoleId)}`,
        inline: true,
      },
      {
        name: "📁 Tracked users",
        value: `Category where channels get created for tracked users.\n${trackedUsersCategoryValue}`,
        inline: true,
      },
    );

  const components = [
    new ActionRowBuilder<StringSelectMenuBuilder>().addComponents(
      new StringSelectMenuBuilder()
        .setCustomId(SETTINGS_SELECT_CUSTOM_ID)
        .setPlaceholder("Select a setting")
        .setMinValues(1)
        .setMaxValues(1)
        .addOptions(
          ...settingsChoices.map((choice) => ({
            label: choice.label,
            value: choice.value,
            description: choice.description,
            emoji: choice.emoji,
          })),
        ),
    ),
  ];

  return { embeds: [embed], components };
}

async function replyWithSettings(interaction: ChatInputCommandInteraction | ModalSubmitInteraction, guild: Guild) {
  const payload = await buildSettingsPayload(guild);

  if (interaction.deferred || interaction.replied) {
    await interaction.editReply(payload);
    return;
  }

  await interaction.reply({
    ...payload,
    flags: MessageFlags.Ephemeral,
  });
}

async function refreshSettingsMessage(interaction: ModalSubmitInteraction, guild: Guild) {
  const payload = await buildSettingsPayload(guild);

  if (interaction.isFromMessage()) {
    await interaction.update(payload);
    return;
  }

  if (interaction.deferred || interaction.replied) {
    await interaction.editReply(payload);
    return;
  }

  await interaction.reply({
    ...payload,
    flags: MessageFlags.Ephemeral,
  });
}

async function updateWithSettings(interaction: StringSelectMenuInteraction, guild: Guild) {
  await interaction.update(await buildSettingsPayload(guild));
}

function trimOptionLabel(value: string) {
  return value.length <= 100 ? value : `${value.slice(0, 97)}...`;
}

function trimOptionDescription(value: string) {
  return value.length <= 100 ? value : `${value.slice(0, 97)}...`;
}

async function fetchGuildChannelOptions(guild: Guild) {
  await guild.channels.fetch();

  return [...guild.channels.cache.values()]
    .filter((channel): channel is GuildBasedChannel => Boolean(channel))
    .filter((channel) => channel.type === ChannelType.GuildText || channel.type === ChannelType.GuildAnnouncement)
    .sort((left, right) => {
      const leftPosition = "rawPosition" in left ? left.rawPosition : 0;
      const rightPosition = "rawPosition" in right ? right.rawPosition : 0;
      if (leftPosition !== rightPosition) {
        return leftPosition - rightPosition;
      }

      return left.name.localeCompare(right.name);
    });
}

async function fetchGuildCategoryOptions(guild: Guild) {
  await guild.channels.fetch();

  return [...guild.channels.cache.values()]
    .filter((channel): channel is GuildBasedChannel => Boolean(channel))
    .filter((channel) => channel.type === ChannelType.GuildCategory)
    .sort((left, right) => {
      const leftPosition = "rawPosition" in left ? left.rawPosition : 0;
      const rightPosition = "rawPosition" in right ? right.rawPosition : 0;
      if (leftPosition !== rightPosition) {
        return leftPosition - rightPosition;
      }

      return left.name.localeCompare(right.name);
    });
}

async function fetchGuildRoleOptions(guild: Guild) {
  await guild.roles.fetch();

  return [...guild.roles.cache.values()]
    .filter((role): role is Role => Boolean(role))
    .filter((role) => role.id !== guild.id && !role.managed)
    .sort((left, right) => right.position - left.position || left.name.localeCompare(right.name));
}

function buildChannelSettingsModal(
  fieldKey: keyof Pick<
    GuildSettingsPatch,
    "linkingLogChannelId" | "trackedServerLogChannelId" | "wipeReactLogChannelId"
  >,
  label: string,
  channels: GuildBasedChannel[],
  currentChannelId: string,
) {
  const modal = new ModalV2Builder()
    .setTitle("Server Settings")
    .setCustomId(`${SETTINGS_CHANNEL_MODAL_PREFIX}${fieldKey}`)
    .addComponent(
      label,
      new StringSelectBuilder()
        .setCustomId(SETTINGS_SELECT_FIELD_CUSTOM_ID)
        .setRequired(true)
        .setMinValues(1)
        .setMaxValues(1)
        .addOption({
          label: "Not set",
          value: SETTINGS_UNSET_VALUE,
          ...(currentChannelId.length === 0 && { default: true }),
        })
        .addOptions(
          ...channels.slice(0, 24).map((channel) => ({
            label: trimOptionLabel(`#${channel.name}`),
            value: channel.id,
            description: trimOptionDescription(
              `Category: ${
                channel.parent?.type === ChannelType.GuildCategory
                  ? channel.parent.name
                  : "None"
              }`,
            ),
            ...(channel.id === currentChannelId && { default: true }),
          })),
        ),
    );

  return modal;
}

function buildCategorySettingsModal(
  fieldKey: keyof Pick<GuildSettingsPatch, "trackedUsersCategoryId">,
  label: string,
  categories: GuildBasedChannel[],
  currentCategoryId: string,
) {
  const modal = new ModalV2Builder()
    .setTitle("Server Settings")
    .setCustomId(`${SETTINGS_CATEGORY_MODAL_PREFIX}${fieldKey}`)
    .addComponent(
      label,
      new StringSelectBuilder()
        .setCustomId(SETTINGS_SELECT_FIELD_CUSTOM_ID)
        .setRequired(true)
        .setMinValues(1)
        .setMaxValues(1)
        .addOption({
          label: "Not set",
          value: SETTINGS_UNSET_VALUE,
          ...(currentCategoryId.length === 0 && { default: true }),
        })
        .addOption({
          label: "Create One",
          value: SETTINGS_CREATE_TRACKED_USERS_CATEGORY_VALUE,
          description: `Create a ${TRACKED_USERS_CATEGORY_NAME} category`,
        })
        .addOptions(
          ...categories.slice(0, 23).map((category) => ({
            label: trimOptionLabel(category.name),
            value: category.id,
            ...(category.id === currentCategoryId && { default: true }),
          })),
        ),
    );

  return modal;
}

function buildRoleSettingsModal(
  roles: Role[],
  currentRoleId: string,
) {
  const modal = new ModalV2Builder()
    .setTitle("Server Settings")
    .setCustomId(`${SETTINGS_ROLE_MODAL_PREFIX}adminRoleId`)
    .addComponent(
      "Admin Role",
      new StringSelectBuilder()
        .setCustomId(SETTINGS_SELECT_FIELD_CUSTOM_ID)
        .setRequired(true)
        .setMinValues(1)
        .setMaxValues(1)
        .addOption({
          label: "Not set",
          value: SETTINGS_UNSET_VALUE,
          ...(currentRoleId.length === 0 && { default: true }),
        })
        .addOptions(
          ...roles.slice(0, 24).map((role) => ({
            label: trimOptionLabel(role.name),
            value: role.id,
            ...(role.id === currentRoleId && { default: true }),
          })),
        ),
    );

  return modal;
}

function parseSettingsSelection(interaction: ModalSubmitInteraction) {
  const value = getFieldValues(interaction, SETTINGS_SELECT_FIELD_CUSTOM_ID)[0];
  if (!value) {
    return null;
  }

  return value === SETTINGS_UNSET_VALUE ? "" : value;
}

function isValidGuildChannel(channel: GuildBasedChannel | null, channelId: string) {
  return Boolean(channel && channel.id === channelId);
}

function isValidGuildRole(role: Role | null, roleId: string) {
  return Boolean(role && role.id === roleId);
}

async function createTrackedUsersCategory(interaction: ModalSubmitInteraction) {
  const sourceChannel = interaction.channel;
  const permissionOverwrites =
    sourceChannel && "permissionOverwrites" in sourceChannel
      ? [...sourceChannel.permissionOverwrites.cache.values()].map((overwrite) => ({
          id: overwrite.id,
          type: overwrite.type,
          allow: overwrite.allow.bitfield,
          deny: overwrite.deny.bitfield,
        }))
      : undefined;

  return interaction.guild!.channels.create({
    name: TRACKED_USERS_CATEGORY_NAME,
    type: ChannelType.GuildCategory,
    ...(permissionOverwrites && { permissionOverwrites }),
  });
}

export async function ensureSettingsCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = guildSettingsCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /settings command (global)");
}

export async function handleSettingsChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SETTINGS_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This command can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!interaction.memberPermissions?.has(PermissionFlagsBits.Administrator)) {
    await interaction.reply({
      content: "Only administrators can use /settings.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferReply({ flags: MessageFlags.Ephemeral });

  await replyWithSettings(interaction, interaction.guild);
  return true;
}

export async function handleSettingsSelect(interaction: StringSelectMenuInteraction) {
  if (interaction.customId !== SETTINGS_SELECT_CUSTOM_ID) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This menu can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!interaction.memberPermissions?.has(PermissionFlagsBits.Administrator)) {
    await interaction.reply({
      content: "Only administrators can use /settings.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const selected = interaction.values[0];
  const settings = await fetchGuildSettings(interaction.guildId);

  if (selected === SETTINGS_OPTION_ADMIN_ROLE) {
    const roles = await fetchGuildRoleOptions(interaction.guild);
    await interaction.showModal(
      buildRoleSettingsModal(roles, settings.adminRoleId).toJSON() as any,
    );
    return true;
  }

  if (selected === SETTINGS_OPTION_TRACKED_USERS) {
    const categories = await fetchGuildCategoryOptions(interaction.guild);
    await interaction.showModal(
      buildCategorySettingsModal(
        "trackedUsersCategoryId",
        "Tracked users",
        categories,
        settings.trackedUsersCategoryId,
      ).toJSON() as any,
    );
    return true;
  }

  const channels = await fetchGuildChannelOptions(interaction.guild);

  if (selected === SETTINGS_OPTION_WIPE_REACT_LOGS) {
    await interaction.showModal(
      buildChannelSettingsModal(
        "wipeReactLogChannelId",
        "Wipe React Logs",
        channels,
        settings.wipeReactLogChannelId,
      ).toJSON() as any,
    );
    return true;
  }

  if (selected === SETTINGS_OPTION_LINKING_LOGS) {
    await interaction.showModal(
      buildChannelSettingsModal(
        "linkingLogChannelId",
        "Linking Logs",
        channels,
        settings.linkingLogChannelId,
      ).toJSON() as any,
    );
    return true;
  }

  if (selected === SETTINGS_OPTION_TRACKED_SERVER_LOGS) {
    await interaction.showModal(
      buildChannelSettingsModal(
        "trackedServerLogChannelId",
        "Tracked Server Logs",
        channels,
        settings.trackedServerLogChannelId,
      ).toJSON() as any,
    );
    return true;
  }

  await interaction.reply({
    content: "That setting is not supported.",
    flags: MessageFlags.Ephemeral,
  });
  return true;
}

export async function handleSettingsModalSubmit(interaction: ModalSubmitInteraction) {
  if (!interaction.customId.startsWith(SETTINGS_CHANNEL_MODAL_PREFIX) &&
      !interaction.customId.startsWith(SETTINGS_CATEGORY_MODAL_PREFIX) &&
      !interaction.customId.startsWith(SETTINGS_ROLE_MODAL_PREFIX)) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guild) {
    await interaction.reply({
      content: "This modal can only be used in a server.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const selectedValue = parseSettingsSelection(interaction);
  if (selectedValue === null) {
    await interaction.reply({
      content: "Select a value before saving.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (interaction.customId.startsWith(SETTINGS_CHANNEL_MODAL_PREFIX)) {
    const field = interaction.customId.slice(SETTINGS_CHANNEL_MODAL_PREFIX.length);
    const validChannelFields = new Set(["wipeReactLogChannelId", "linkingLogChannelId", "trackedServerLogChannelId"]);
    if (!validChannelFields.has(field)) {
      await interaction.reply({
        content: "That setting is not valid.",
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }
    let channel: GuildBasedChannel | null = null;

    if (selectedValue) {
      channel =
        interaction.guild.channels.cache.get(selectedValue) ??
        (await interaction.guild.channels.fetch(selectedValue).catch(logDiscordCatch("fetch settings selected channel", null, {
          guildId: interaction.guildId,
          channelId: selectedValue,
        })));

      if (!isValidGuildChannel(channel, selectedValue)) {
        await interaction.reply({
          content: "That channel is no longer available.",
          flags: MessageFlags.Ephemeral,
        });
        return true;
      }
    }

    await updateGuildSettings(interaction.guildId, { [field]: selectedValue ?? undefined });
    await refreshSettingsMessage(interaction, interaction.guild);
    return true;
  }

  if (interaction.customId.startsWith(SETTINGS_CATEGORY_MODAL_PREFIX)) {
    const field = interaction.customId.slice(SETTINGS_CATEGORY_MODAL_PREFIX.length);
    if (field !== "trackedUsersCategoryId") {
      await interaction.reply({
        content: "That setting is not valid.",
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }

    let valueToSave = selectedValue ?? "";
    let category: GuildBasedChannel | null = null;
    if (selectedValue === SETTINGS_CREATE_TRACKED_USERS_CATEGORY_VALUE) {
      try {
        category = await createTrackedUsersCategory(interaction);
      } catch (error) {
        console.error("[bot] failed to create tracked users category", error);
        await interaction.reply({
          content: "I could not create the Tracked clans category. Check that I have Manage Channels permission.",
          flags: MessageFlags.Ephemeral,
        });
        return true;
      }
      valueToSave = category.id;
    }

    if (selectedValue) {
      if (!category) {
        category =
          interaction.guild.channels.cache.get(selectedValue) ??
          (await interaction.guild.channels.fetch(selectedValue).catch(logDiscordCatch("fetch settings selected category", null, {
            guildId: interaction.guildId,
            channelId: selectedValue,
          })));
      }

      if (!category || category.id !== valueToSave || category.type !== ChannelType.GuildCategory) {
        await interaction.reply({
          content: "That category is no longer available.",
          flags: MessageFlags.Ephemeral,
        });
        return true;
      }
    }

    await updateGuildSettings(interaction.guildId, { [field]: valueToSave });
    await refreshSettingsMessage(interaction, interaction.guild);
    return true;
  }

  const roleField = interaction.customId.slice(SETTINGS_ROLE_MODAL_PREFIX.length);
  if (roleField !== "adminRoleId") {
    await interaction.reply({
      content: "That setting is not valid.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }
  let role: Role | null = null;

  if (selectedValue) {
    role =
      interaction.guild.roles.cache.get(selectedValue) ??
      (await interaction.guild.roles.fetch(selectedValue).catch(logDiscordCatch("fetch settings selected role", null, {
        guildId: interaction.guildId,
        roleId: selectedValue,
      })));

    if (!isValidGuildRole(role, selectedValue)) {
      await interaction.reply({
        content: "That role is no longer available.",
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }
  }

  await updateGuildSettings(interaction.guildId, { [roleField]: selectedValue ?? undefined });
  await refreshSettingsMessage(interaction, interaction.guild);
  return true;
}
