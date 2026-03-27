import type {
  ChatInputCommandInteraction,
  Client,
  Guild,
  GuildBasedChannel,
  ModalSubmitInteraction,
  Role,
} from "discord.js";
import {
  ChannelType,
  GuildSystemChannelFlags,
  MessageFlags,
  OverwriteType,
  PermissionFlagsBits,
  SlashCommandBuilder,
  TextChannel,
} from "discord.js";
import { BackendClient } from "./backend-client.js";
import { CheckboxGroupBuilder, ModalV2Builder } from "./builders/index.js";
import { loadConfig } from "./config.js";

export const SETUP_COMMAND_NAME = "setup";
export const ADMIN_COMMAND_NAME = "admin";

const SETUP_START_SUBCOMMAND_NAME = "start";
const ADMIN_RESET_SUBCOMMAND_NAME = "reset_setup";
const SETUP_MODAL_CUSTOM_ID = "setup_start_modal";
const SETUP_CHANNELS_CUSTOM_ID = "setup_channels";
const SETUP_ROLES_CUSTOM_ID = "setup_roles";
const SETUP_ALL_ROLES_VALUE = "all_roles";
const SETUP_ALL_CHANNELS_VALUE = "all_channels";

const shardID = process.env.SHARD_ID ?? "0";
const config = loadConfig();
const backend = new BackendClient(
  config.backendUrl,
  config.jwtSecret,
  config.jwtIssuer,
  config.jwtAudience,
);
const setupWorkerID = `${config.workerId}-setup-${shardID}`;

type SetupRoleKey =
  | "keyrole"
  | "leader"
  | "co_leader"
  | "bot"
  | "member"
  | "builder"
  | "electrician"
  | "monument"
  | "pvp";

type SetupChannelKey =
  | "voice_calls"
  | "announcements"
  | "wipe_react"
  | "invite"
  | "general"
  | "pics";

type SetupRoleConfig = {
  key: SetupRoleKey;
  modalLabel: string;
  roleName: string;
  hoist: boolean;
  color?: number;
  permissions: bigint[];
};

type SetupChannelConfig = {
  key: SetupChannelKey;
  modalLabel: string;
};

type SetupCategoryPlan = {
  name: string;
};

type SetupChannelPlan = {
  name: string;
  type: ChannelType.GuildText | ChannelType.GuildVoice;
  parentName?: string;
  permissionOverwrites: SetupRoleOverwrite[];
};

type SetupRoleOverwrite = {
    id: string;
    type: OverwriteType.Role;
    allow?: bigint[];
    deny?: bigint[];
};

type SetupPlan = {
  categories: SetupCategoryPlan[];
  channels: SetupChannelPlan[];
};

type ExistingChannelSummary = {
  id: string;
  name: string;
  type: ChannelType;
};

type SetupBootstrapPlan = {
  bootstrapChannelId: string | null;
  blockingConflicts: string[];
  error: string | null;
};

type SetupChannelCreationResult = {
  createdChannels: GuildBasedChannel[];
  bootstrapChannelId: string | null;
  bootstrapOriginalName: string | null;
};

const leaderPermissions = [
  PermissionFlagsBits.BanMembers,
  PermissionFlagsBits.KickMembers,
  PermissionFlagsBits.ModerateMembers,
  PermissionFlagsBits.ManageMessages,
  PermissionFlagsBits.MentionEveryone,
  PermissionFlagsBits.MoveMembers,
] as const;

const setupRoleConfigs: readonly SetupRoleConfig[] = [
  {
    key: "keyrole",
    modalLabel: "Keyrole",
    roleName: "🗝️",
    hoist: false,
    permissions: [PermissionFlagsBits.Administrator],
  },
  {
    key: "leader",
    modalLabel: "Leader",
    roleName: "Leader",
    hoist: true,
    color: 0xf4e7a1,
    permissions: [...leaderPermissions],
  },
  {
    key: "co_leader",
    modalLabel: "Co-Leader",
    roleName: "Co Leader",
    hoist: true,
    color: 0xa9d8a4,
    permissions: [...leaderPermissions],
  },
  {
    key: "bot",
    modalLabel: "Bot",
    roleName: "Bot",
    hoist: true,
    color: 0x9fc6ff,
    permissions: [],
  },
  {
    key: "member",
    modalLabel: "Member",
    roleName: "member",
    hoist: true,
    color: 0x8edee8,
    permissions: [],
  },
  {
    key: "builder",
    modalLabel: "Builder",
    roleName: "Builder",
    hoist: false,
    permissions: [],
  },
  {
    key: "electrician",
    modalLabel: "Electrician",
    roleName: "Electrician",
    hoist: false,
    permissions: [],
  },
  {
    key: "monument",
    modalLabel: "Monument",
    roleName: "Monument",
    hoist: false,
    permissions: [],
  },
  {
    key: "pvp",
    modalLabel: "PvP",
    roleName: "PvP",
    hoist: false,
    permissions: [],
  },
] as const;

const setupChannelConfigs: readonly SetupChannelConfig[] = [
  { key: "voice_calls", modalLabel: "Voice Calls" },
  { key: "announcements", modalLabel: "Announcements" },
  { key: "wipe_react", modalLabel: "Wipe react" },
  { key: "invite", modalLabel: "Invite" },
  { key: "general", modalLabel: "General" },
  { key: "pics", modalLabel: "Pics" },
] as const;

const roleConfigByKey = new Map<SetupRoleKey, SetupRoleConfig>(
  setupRoleConfigs.map((config) => [config.key, config]),
);
const channelConfigByKey = new Map<SetupChannelKey, SetupChannelConfig>(
  setupChannelConfigs.map((config) => [config.key, config]),
);
const memberChildRoleKeys = new Set<SetupRoleKey>([
  "builder",
  "electrician",
  "monument",
  "pvp",
]);
const threadPermissionBits = [
  PermissionFlagsBits.CreatePublicThreads,
  PermissionFlagsBits.CreatePrivateThreads,
  PermissionFlagsBits.SendMessagesInThreads,
] as const;
const textWritePermissionBits = [
  PermissionFlagsBits.SendMessages,
  PermissionFlagsBits.AddReactions,
  ...threadPermissionBits,
] as const;
const voicePermissionBits = [
  PermissionFlagsBits.ViewChannel,
  PermissionFlagsBits.Connect,
  PermissionFlagsBits.Speak,
] as const;
const orderedSetupCategoryNames = ["Important", "General", "Voice Channels"] as const;
const joinMessageUnsuppressFlags =
  GuildSystemChannelFlags.SuppressJoinNotifications |
  GuildSystemChannelFlags.SuppressJoinNotificationReplies;
const setupAfkTimeoutSeconds = 600;

const setupCommand = new SlashCommandBuilder()
  .setName(SETUP_COMMAND_NAME)
  .setDescription("RustLens setup commands")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(SETUP_START_SUBCOMMAND_NAME)
      .setDescription("Create the configured RustLens roles"),
  );

const adminCommand = new SlashCommandBuilder()
  .setName(ADMIN_COMMAND_NAME)
  .setDescription("RustLens admin commands")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(ADMIN_RESET_SUBCOMMAND_NAME)
      .setDescription("Reset setup state so the guild can be configured again"),
  );

function buildSetupStartModal() {
  return new ModalV2Builder()
    .setTitle("Setup Roles")
    .setCustomId(SETUP_MODAL_CUSTOM_ID)
    .addComponent(
      "Roles to create",
      new CheckboxGroupBuilder()
        .setCustomId(SETUP_ROLES_CUSTOM_ID)
        .setRequired(true)
        .setMinValues(1)
        .setMaxValues(setupRoleConfigs.length + 1)
        .addOptions(
          {
            label: "All",
            value: SETUP_ALL_ROLES_VALUE,
          },
          ...setupRoleConfigs.map((config) => ({
            label: config.modalLabel,
            value: config.key,
          })),
        ),
    )
    .addComponent(
      "Channels to create",
      new CheckboxGroupBuilder()
        .setCustomId(SETUP_CHANNELS_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(setupChannelConfigs.length + 1)
        .addOptions(
          {
            label: "All",
            value: SETUP_ALL_CHANNELS_VALUE,
          },
          ...setupChannelConfigs.map((config) => ({
            label: config.modalLabel,
            value: config.key,
          })),
        ),
    );
}

function normalizeModalFieldValue(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((entry): entry is string => typeof entry === "string");
  }

  if (typeof value === "string") {
    if (value.length === 0) {
      return [];
    }

    return value
      .split(",")
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0);
  }

  return [];
}

function extractFieldValues(
  field: { value?: unknown; values?: unknown } | null | undefined,
) {
  if (!field) {
    return [];
  }

  if ("values" in field) {
    return normalizeModalFieldValue(field.values);
  }

  if ("value" in field) {
    return normalizeModalFieldValue(field.value);
  }

  return [];
}

function getFieldValues(interaction: ModalSubmitInteraction, customId: string) {
  const accessorField = interaction.fields.getField(customId) as
    | { value?: unknown; values?: unknown }
    | null
    | undefined;
  const accessorValues = extractFieldValues(accessorField);
  if (accessorValues.length > 0) {
    return accessorValues;
  }

  const mapField = interaction.fields.fields.get(customId) as
    | { value?: unknown; values?: unknown }
    | null
    | undefined;
  return extractFieldValues(mapField);
}

function getSelectedRoleKeys(values: string[]): SetupRoleKey[] {
  if (values.includes(SETUP_ALL_ROLES_VALUE)) {
    return setupRoleConfigs.map((config) => config.key);
  }

  const selected = new Set<SetupRoleKey>();

  for (const value of values) {
    if (roleConfigByKey.has(value as SetupRoleKey)) {
      selected.add(value as SetupRoleKey);
    }
  }

  if (selected.size > 0) {
    selected.add("bot");
  }

  return setupRoleConfigs
    .filter((config) => selected.has(config.key))
    .map((config) => config.key);
}

function getSelectedChannelKeys(values: string[]): SetupChannelKey[] {
  if (values.includes(SETUP_ALL_CHANNELS_VALUE)) {
    return setupChannelConfigs.map((config) => config.key);
  }

  const selected = new Set<SetupChannelKey>();

  for (const value of values) {
    if (channelConfigByKey.has(value as SetupChannelKey)) {
      selected.add(value as SetupChannelKey);
    }
  }

  return setupChannelConfigs
    .filter((config) => selected.has(config.key))
    .map((config) => config.key);
}

function validateSetupSelections(roleKeys: SetupRoleKey[], channelKeys: SetupChannelKey[]) {
  if (roleKeys.length === 0) {
    return "Select at least one role.";
  }

  const includesMemberChildRole = roleKeys.some((key) => memberChildRoleKeys.has(key));
  if (includesMemberChildRole && !roleKeys.includes("member")) {
    return "Builder, Electrician, Monument, and PvP require Member to be selected too.";
  }

  if (channelKeys.length > 0) {
    const missingCoreRoles = [
      !roleKeys.includes("leader") && "Leader",
      !roleKeys.includes("co_leader") && "Co-Leader",
      !roleKeys.includes("member") && "Member",
    ].filter((value): value is string => Boolean(value));

    if (missingCoreRoles.length > 0) {
      return `Channel creation requires these roles too: ${missingCoreRoles.join(", ")}.`;
    }
  }

  return null;
}

function formatRoleList(keys: SetupRoleKey[]) {
  return keys
    .map((key) => roleConfigByKey.get(key)?.roleName ?? key)
    .join(", ");
}

function formatChannelList(keys: SetupChannelKey[]) {
  return keys
    .map((key) => channelConfigByKey.get(key)?.modalLabel ?? key)
    .join(", ");
}

function hasAdministratorPermission(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
) {
  return interaction.memberPermissions?.has(PermissionFlagsBits.Administrator) ?? false;
}

async function ensureManageableGuild(guild: Guild) {
  const me = guild.members.me ?? await guild.members.fetchMe();

  if (!me.permissions.has(PermissionFlagsBits.ManageRoles)) {
    throw new Error(
      "The bot needs the Manage Roles permission before setup can run. Server ownership does not bypass bot permissions.",
    );
  }

  if (me.roles.highest.comparePositionTo(guild.roles.everyone) <= 0) {
    throw new Error(
      "Move the bot's highest role above the roles it needs to create. Server ownership does not bypass role hierarchy for the bot.",
    );
  }

  return me;
}

function getRoleConfigs(keys: SetupRoleKey[]) {
  return keys.flatMap((key) => {
    const config = roleConfigByKey.get(key);
    return config ? [config] : [];
  });
}

function getSelectedRoleMap(createdRoles: Role[]) {
  const roleMap = new Map<SetupRoleKey, Role>();

  for (const config of setupRoleConfigs) {
    const role = createdRoles.find((entry) => entry.name === config.roleName);
    if (role) {
      roleMap.set(config.key, role);
    }
  }

  return roleMap;
}

function roleOverwrite(
  role: Role,
  allow: readonly bigint[] = [],
  deny: readonly bigint[] = [],
): SetupRoleOverwrite {
  return {
    id: role.id,
    type: OverwriteType.Role as const,
    allow: [...allow],
    deny: [...deny],
  };
}

function everyoneOverwrite(
  roleId: string,
  deny: readonly bigint[] = [],
): SetupRoleOverwrite {
  return {
    id: roleId,
    type: OverwriteType.Role as const,
    deny: [...deny],
  };
}

function buildSetupPlan(
  channelKeys: SetupChannelKey[],
  roleMap: Map<SetupRoleKey, Role>,
): SetupPlan {
  const categories = new Map<string, SetupCategoryPlan>();
  const channels: SetupChannelPlan[] = [];
  const everyoneId = roleMap.get("leader")?.guild.roles.everyone.id ?? "";

  const leaderRole = roleMap.get("leader");
  const coLeaderRole = roleMap.get("co_leader");
  const memberRole = roleMap.get("member");
  const builderRole = roleMap.get("builder");

  if (!leaderRole || !coLeaderRole || !memberRole) {
    throw new Error("Channel setup requires Leader, Co-Leader, and Member roles.");
  }

  const privateTextOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    roleOverwrite(leaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
    roleOverwrite(coLeaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
  ];

  const memberTextOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    roleOverwrite(memberRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
    roleOverwrite(leaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
    roleOverwrite(coLeaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
  ];

  const restrictedAnnouncementOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    roleOverwrite(
      memberRole,
      [PermissionFlagsBits.ViewChannel],
      textWritePermissionBits,
    ),
    roleOverwrite(leaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
    roleOverwrite(coLeaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
  ];

  const builderTextOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    ...(builderRole
      ? [roleOverwrite(builderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits])]
      : []),
    roleOverwrite(leaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
    roleOverwrite(coLeaderRole, [PermissionFlagsBits.ViewChannel, ...textWritePermissionBits]),
  ];

  const memberVoiceOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    roleOverwrite(memberRole, voicePermissionBits),
    roleOverwrite(leaderRole, voicePermissionBits),
    roleOverwrite(coLeaderRole, voicePermissionBits),
  ];

  const builderVoiceOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    ...(builderRole ? [roleOverwrite(builderRole, voicePermissionBits)] : []),
    roleOverwrite(leaderRole, voicePermissionBits),
    roleOverwrite(coLeaderRole, voicePermissionBits),
  ];

  const privateVoiceOverwrites = [
    everyoneOverwrite(everyoneId, [PermissionFlagsBits.ViewChannel]),
    roleOverwrite(leaderRole, voicePermissionBits),
    roleOverwrite(coLeaderRole, voicePermissionBits),
  ];

  if (channelKeys.includes("general")) {
    channels.push(
      {
        name: "join",
        type: ChannelType.GuildText,
        permissionOverwrites: privateTextOverwrites,
      },
      {
        name: "public",
        type: ChannelType.GuildText,
        permissionOverwrites: memberTextOverwrites,
      },
    );

    categories.set("General", { name: "General" });
    channels.push(
      {
        name: "general",
        type: ChannelType.GuildText,
        parentName: "General",
        permissionOverwrites: memberTextOverwrites,
      },
      {
        name: "logs",
        type: ChannelType.GuildText,
        parentName: "General",
        permissionOverwrites: privateTextOverwrites,
      },
      {
        name: "priv",
        type: ChannelType.GuildText,
        parentName: "General",
        permissionOverwrites: privateTextOverwrites,
      },
    );

    if (builderRole) {
      channels.push({
        name: "build",
        type: ChannelType.GuildText,
        parentName: "General",
        permissionOverwrites: builderTextOverwrites,
      });
    }
  }

  if (channelKeys.includes("pics")) {
    categories.set("General", { name: "General" });
    channels.push({
      name: "pics",
      type: ChannelType.GuildText,
      parentName: "General",
      permissionOverwrites: memberTextOverwrites,
    });
  }

  if (channelKeys.includes("announcements")) {
    categories.set("Important", { name: "Important" });
    channels.push({
      name: "announcements",
      type: ChannelType.GuildText,
      parentName: "Important",
      permissionOverwrites: restrictedAnnouncementOverwrites,
    });
  }

  if (channelKeys.includes("wipe_react")) {
    categories.set("Important", { name: "Important" });
    channels.push({
      name: "wipe react",
      type: ChannelType.GuildText,
      parentName: "Important",
      permissionOverwrites: restrictedAnnouncementOverwrites,
    });
  }

  if (channelKeys.includes("invite")) {
    categories.set("Important", { name: "Important" });
    channels.push({
      name: "click for invite",
      type: ChannelType.GuildText,
      parentName: "Important",
      permissionOverwrites: restrictedAnnouncementOverwrites,
    });
  }

  if (
    channelKeys.includes("announcements") ||
    channelKeys.includes("wipe_react") ||
    channelKeys.includes("invite")
  ) {
    categories.set("Important", { name: "Important" });
    channels.push({
      name: "wipe log",
      type: ChannelType.GuildText,
      parentName: "Important",
      permissionOverwrites: restrictedAnnouncementOverwrites,
    });
  }

  if (channelKeys.includes("voice_calls")) {
    channels.push({
      name: "public vc",
      type: ChannelType.GuildVoice,
      permissionOverwrites: memberVoiceOverwrites,
    });

    categories.set("Voice Channels", { name: "Voice Channels" });
    for (let index = 1; index <= 10; index += 1) {
      channels.push({
        name: `${index}`,
        type: ChannelType.GuildVoice,
        parentName: "Voice Channels",
        permissionOverwrites: memberVoiceOverwrites,
      });
    }

    if (builderRole) {
      channels.push({
        name: "builder call",
        type: ChannelType.GuildVoice,
        parentName: "Voice Channels",
        permissionOverwrites: builderVoiceOverwrites,
      });
    }

    channels.push({
      name: "private vc",
      type: ChannelType.GuildVoice,
      parentName: "Voice Channels",
      permissionOverwrites: privateVoiceOverwrites,
    });

    channels.push({
      name: "afk",
      type: ChannelType.GuildVoice,
      parentName: "Voice Channels",
      permissionOverwrites: memberVoiceOverwrites,
    });
  }

  return {
    categories: [...categories.values()].sort(
      (left, right) =>
        orderedSetupCategoryNames.indexOf(left.name as (typeof orderedSetupCategoryNames)[number]) -
        orderedSetupCategoryNames.indexOf(right.name as (typeof orderedSetupCategoryNames)[number]),
    ),
    channels,
  };
}

async function configureGuildChannelSettings(
  guild: Guild,
  createdChannels: readonly GuildBasedChannel[],
  actorTag: string,
) {
  const joinChannel = createdChannels.find(
    (channel): channel is TextChannel =>
      channel.type === ChannelType.GuildText && channel.name === "join",
  );
  if (!joinChannel) {
    const afkChannel = createdChannels.find(
      (channel): channel is import("discord.js").VoiceChannel =>
        channel.type === ChannelType.GuildVoice && channel.name === "afk",
    );
    if (afkChannel) {
      await guild.edit({
        afkChannel,
        afkTimeout: setupAfkTimeoutSeconds,
        reason: `RustLens setup by ${actorTag}`,
      });
    }
    return;
  }

  const updatedFlags = guild.systemChannelFlags.bitfield & ~joinMessageUnsuppressFlags;
  const afkChannel = createdChannels.find(
    (channel): channel is import("discord.js").VoiceChannel =>
      channel.type === ChannelType.GuildVoice && channel.name === "afk",
  );
  await guild.edit({
    systemChannel: joinChannel,
    systemChannelFlags: updatedFlags,
    ...(afkChannel
      ? {
          afkChannel,
          afkTimeout: setupAfkTimeoutSeconds,
        }
      : {}),
    reason: `RustLens setup by ${actorTag}`,
  });
}

function getTargetChannelNames(
  channelKeys: SetupChannelKey[],
  roleKeys: readonly SetupRoleKey[] = setupRoleConfigs.map((config) => config.key),
) {
  const names: string[] = [];
  const hasBuilderRole = roleKeys.includes("builder");

  if (channelKeys.includes("general")) {
    names.push("join", "public", "General", "general", "logs", "priv");
    if (hasBuilderRole) {
      names.push("build");
    }
  }

  if (channelKeys.includes("pics")) {
    names.push("General", "pics");
  }

  if (channelKeys.includes("announcements")) {
    names.push("Important", "announcements");
  }

  if (channelKeys.includes("wipe_react")) {
    names.push("Important", "wipe react");
  }

  if (channelKeys.includes("invite")) {
    names.push("Important", "click for invite");
  }

  if (
    channelKeys.includes("announcements") ||
    channelKeys.includes("wipe_react") ||
    channelKeys.includes("invite")
  ) {
    names.push("Important", "wipe log");
  }

  if (channelKeys.includes("voice_calls")) {
    names.push("public vc", "Voice Channels", "private vc", "afk");
    if (hasBuilderRole) {
      names.push("builder call");
    }
    for (let index = 1; index <= 10; index += 1) {
      names.push(`${index}`);
    }
  }

  return [...new Set(names)];
}

function findConflictingRoles(guild: Guild, roleKeys: SetupRoleKey[]) {
  const conflicts: string[] = [];

  for (const config of getRoleConfigs(roleKeys)) {
    if (guild.roles.cache.some((role) => role.name === config.roleName)) {
      conflicts.push(config.roleName);
    }
  }

  return conflicts;
}

function findConflictingChannels(
  guild: Guild,
  channelKeys: SetupChannelKey[],
  roleKeys: readonly SetupRoleKey[] = setupRoleConfigs.map((config) => config.key),
  ignoredChannelIds: readonly string[] = [],
) {
  const names = new Set(getTargetChannelNames(channelKeys, roleKeys));
  const ignoredIds = new Set(ignoredChannelIds);

  return guild.channels.cache
    .filter((channel) => names.has(channel.name) && !ignoredIds.has(channel.id))
    .map((channel) => channel.name);
}

function isCountedGuildChannel(type: ChannelType) {
  return (
    type !== ChannelType.PublicThread &&
    type !== ChannelType.PrivateThread &&
    type !== ChannelType.AnnouncementThread
  );
}

function planSetupBootstrap(
  existingChannels: readonly ExistingChannelSummary[],
  currentChannelId: string | null,
  roleKeys: readonly SetupRoleKey[],
  channelKeys: SetupChannelKey[],
): SetupBootstrapPlan {
  if (channelKeys.length === 0) {
    return {
      bootstrapChannelId: null,
      blockingConflicts: [],
      error: null,
    };
  }

  const countedChannels = existingChannels.filter((channel) =>
    isCountedGuildChannel(channel.type),
  );
  if (countedChannels.length !== 1) {
    return {
      bootstrapChannelId: null,
      blockingConflicts: [],
      error:
        "Channel setup can only run when the server has exactly one existing channel. Delete the extras first.",
    };
  }

  const bootstrapChannel = countedChannels[0];
  if (bootstrapChannel.type === ChannelType.GuildCategory) {
    return {
      bootstrapChannelId: null,
      blockingConflicts: [],
      error:
        "Channel setup must be started from a normal server channel, not a category.",
    };
  }

  if (!currentChannelId || bootstrapChannel.id !== currentChannelId) {
    return {
      bootstrapChannelId: null,
      blockingConflicts: [],
      error:
        "Run /setup start from the server's only channel so RustLens can replace it.",
    };
  }

  const targetNames = new Set(getTargetChannelNames(channelKeys, roleKeys));
  const blockingConflicts = countedChannels
    .filter(
      (channel) =>
        channel.id !== bootstrapChannel.id && targetNames.has(channel.name),
    )
    .map((channel) => channel.name);

  return {
    bootstrapChannelId: bootstrapChannel.id,
    blockingConflicts,
    error: null,
  };
}

async function createSetupRoles(guild: Guild, roleKeys: SetupRoleKey[], actorTag: string) {
  const me = await ensureManageableGuild(guild);
  const createdRoles: Role[] = [];
  const targetConfigs = getRoleConfigs(roleKeys);
  const highestPosition = me.roles.highest.position;

  try {
    for (const [index, config] of targetConfigs.entries()) {
      const role = await guild.roles.create({
        name: config.roleName,
        hoist: config.hoist,
        mentionable: false,
        permissions: config.permissions,
        position: Math.max(1, highestPosition - (index + 1)),
        ...(config.color !== undefined && {
          colors: { primaryColor: config.color },
        }),
        reason: `RustLens setup by ${actorTag}`,
      });

      createdRoles.push(role);
    }

    const botRole = createdRoles.find((role) => role.name === roleConfigByKey.get("bot")?.roleName);
    if (botRole) {
      await me.roles.add(botRole, `RustLens setup by ${actorTag}`);

      const members = await guild.members.fetch();
      const otherBots = members.filter(
        (member) => member.user.bot && member.id !== me.id,
      );

      await Promise.allSettled(
        otherBots.map((member) =>
          member.roles
            .add(botRole, `RustLens setup by ${actorTag}`)
            .catch((error) => {
              console.error(
                `[bot] failed to assign Bot role to ${member.user.tag}`,
                error,
              );
            }),
        ),
      );
    }

    return createdRoles;
  } catch (error) {
    await Promise.allSettled(
      createdRoles.map((role) =>
        role.delete(`Rolling back failed RustLens setup by ${actorTag}`),
      ),
    );
    throw error;
  }
}

async function createSetupChannels(
  guild: Guild,
  channelKeys: SetupChannelKey[],
  createdRoles: Role[],
  actorTag: string,
  bootstrapChannelId?: string | null,
): Promise<SetupChannelCreationResult> {
  const roleMap = getSelectedRoleMap(createdRoles);
  const plan = buildSetupPlan(channelKeys, roleMap);
  const createdChannels: GuildBasedChannel[] = [];
  const categoryMap = new Map<string, GuildBasedChannel>();
  const reason = `RustLens setup by ${actorTag}`;
  let bootstrapOriginalName: string | null = null;

  try {
    if (bootstrapChannelId) {
      const bootstrapChannel = guild.channels.cache.get(bootstrapChannelId);
      if (!bootstrapChannel) {
        throw new Error("The setup channel disappeared before setup could continue.");
      }

      bootstrapOriginalName = bootstrapChannel.name;
      await guild.channels.edit(bootstrapChannelId, {
        name: `rustlens-bootstrap-${Date.now().toString(36)}`,
        reason,
      });
    }

    for (const category of plan.categories) {
      const createdCategory = await guild.channels.create({
        name: category.name,
        type: ChannelType.GuildCategory,
        reason,
      });
      categoryMap.set(category.name, createdCategory);
      createdChannels.push(createdCategory);
    }

    for (const channel of plan.channels) {
      const createdChannel = await guild.channels.create({
        name: channel.name,
        type: channel.type,
        parent: channel.parentName ? categoryMap.get(channel.parentName)?.id : undefined,
        permissionOverwrites: channel.permissionOverwrites,
        reason,
      });
      createdChannels.push(createdChannel);
    }

    return {
      createdChannels,
      bootstrapChannelId: bootstrapChannelId ?? null,
      bootstrapOriginalName,
    };
  } catch (error) {
    await Promise.allSettled(
      createdChannels
        .slice()
        .reverse()
        .map((channel) =>
          channel.delete(`Rolling back failed RustLens setup by ${actorTag}`),
        ),
    );

    if (bootstrapChannelId && bootstrapOriginalName) {
      await guild.channels
        .edit(bootstrapChannelId, {
          name: bootstrapOriginalName,
          reason: `Restoring RustLens bootstrap channel after failed setup by ${actorTag}`,
        })
        .catch(() => undefined);
    }

    throw error;
  }
}

async function deleteSetupRoles(guild: Guild, roleKeys: SetupRoleKey[], actorTag: string) {
  await ensureManageableGuild(guild);

  const deletedRoleNames: string[] = [];
  const existingRoles = getRoleConfigs(roleKeys)
    .map((config) =>
      guild.roles.cache.find((role) => role.name === config.roleName),
    )
    .filter((role): role is Role => Boolean(role))
    .reverse();

  for (const role of existingRoles) {
    deletedRoleNames.push(role.name);
    await role.delete(`RustLens setup reset by ${actorTag}`);
  }

  return deletedRoleNames.reverse();
}

async function deleteSetupChannels(
  guild: Guild,
  channelKeys: SetupChannelKey[],
  roleKeys: SetupRoleKey[],
  actorTag: string,
) {
  if (channelKeys.length === 0) {
    return [];
  }

  const roleMap = new Map<SetupRoleKey, Role>();
  for (const config of getRoleConfigs(roleKeys)) {
    const role = guild.roles.cache.find((entry) => entry.name === config.roleName);
    if (role) {
      roleMap.set(config.key, role);
    }
  }

  const plan = buildSetupPlan(channelKeys, roleMap);
  const targetNames = new Set([
    ...plan.channels.map((channel) => channel.name),
    ...plan.categories.map((category) => category.name),
  ]);
  const deletedChannelNames: string[] = [];
  const existingChannels = [...guild.channels.cache.values()]
    .filter((channel) => targetNames.has(channel.name))
    .sort((left, right) => {
      const leftWeight = left.type === ChannelType.GuildCategory ? 1 : 0;
      const rightWeight = right.type === ChannelType.GuildCategory ? 1 : 0;
      return leftWeight - rightWeight;
    })
    .reverse();

  for (const channel of existingChannels) {
    deletedChannelNames.push(channel.name);
    await channel.delete(`RustLens setup reset by ${actorTag}`);
  }

  return deletedChannelNames.reverse();
}

function formatSetupError(error: unknown) {
  if (typeof error === "object" && error !== null && "code" in error) {
    const code = (error as { code?: unknown }).code;
    if (code === 50013) {
      return "Discord rejected the role change with Missing Permissions. The bot itself must have Manage Roles and its highest role must stay above the roles it is creating or deleting.";
    }
  }

  if (error instanceof Error) {
    return error.message;
  }

  return "Setup failed while creating roles.";
}

function setupBlockedMessage(status: "missing" | "pending" | "configured") {
  if (status === "pending") {
    return "Setup is already in progress for this server.";
  }

  if (status === "configured") {
    return "Setup has already been completed for this server. Use /admin reset_setup before running it again.";
  }

  return null;
}

async function replyEphemeral(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
  content: string,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content });
    return;
  }

  await interaction.reply({
    content,
    flags: MessageFlags.Ephemeral,
  });
}

async function deferEphemeral(
  interaction: ChatInputCommandInteraction | ModalSubmitInteraction,
) {
  if (interaction.deferred || interaction.replied) {
    return;
  }

  await interaction.deferReply({
    flags: MessageFlags.Ephemeral,
  });
}

export async function ensureSetupCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const commandData = [setupCommand.toJSON(), adminCommand.toJSON()];
  const commands = await client.application.commands.fetch();

  for (const data of commandData) {
    const existing = commands.find((command) => command.name === data.name);

    if (existing) {
      await client.application.commands.edit(existing.id, data);
      continue;
    }

    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /setup and /admin commands (global)");
}

export async function handleSetupChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== SETUP_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Setup can only be used inside a server.");
    return true;
  }

  if (!hasAdministratorPermission(interaction)) {
    await replyEphemeral(interaction, "Only administrators can use /setup.");
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);
  if (subcommand !== SETUP_START_SUBCOMMAND_NAME) {
    await replyEphemeral(
      interaction,
      `Unknown /${SETUP_COMMAND_NAME} subcommand: ${subcommand}`,
    );
    return true;
  }

  const state = await backend.guildSetup(setupWorkerID, interaction.guildId);
  const blockedMessage = setupBlockedMessage(state.status);
  if (blockedMessage) {
    await replyEphemeral(interaction, blockedMessage);
    return true;
  }

  await interaction.showModal(buildSetupStartModal().toJSON() as any);
  return true;
}

export async function handleAdminChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== ADMIN_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Admin commands can only be used inside a server.");
    return true;
  }

  const guild = interaction.guild;
  if (!guild) {
    await replyEphemeral(interaction, "Guild context is unavailable for this command.");
    return true;
  }

  if (!hasAdministratorPermission(interaction)) {
    await replyEphemeral(interaction, "Only administrators can use /admin.");
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);
  if (subcommand !== ADMIN_RESET_SUBCOMMAND_NAME) {
    await replyEphemeral(
      interaction,
      `Unknown /${ADMIN_COMMAND_NAME} subcommand: ${subcommand}`,
    );
    return true;
  }

  await deferEphemeral(interaction);

  const state = await backend.guildSetup(setupWorkerID, interaction.guildId);
  if (state.status === "missing") {
    await replyEphemeral(interaction, "Setup has not been completed for this server.");
    return true;
  }

  const roleKeys = getSelectedRoleKeys(state.selectedRoles);
  const channelKeys = getSelectedChannelKeys(state.selectedChannels);
  const deletedChannels = await deleteSetupChannels(
    guild,
    channelKeys,
    roleKeys,
    interaction.user.tag,
  );
  const deletedRoles = await deleteSetupRoles(
    guild,
    roleKeys,
    interaction.user.tag,
  );
  await backend.resetGuildSetup(setupWorkerID, interaction.guildId);
  return true;
}

export async function handleSetupModalSubmit(interaction: ModalSubmitInteraction) {
  if (interaction.customId !== SETUP_MODAL_CUSTOM_ID) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Setup can only be submitted inside a server.");
    return true;
  }

  const guild = interaction.guild;
  if (!guild) {
    await replyEphemeral(interaction, "Guild context is unavailable for this setup.");
    return true;
  }

  if (!hasAdministratorPermission(interaction)) {
    await replyEphemeral(interaction, "Only administrators can submit setup.");
    return true;
  }

  const selectedRoleKeys = getSelectedRoleKeys(
    getFieldValues(interaction, SETUP_ROLES_CUSTOM_ID),
  );
  const selectedChannelKeys = getSelectedChannelKeys(
    getFieldValues(interaction, SETUP_CHANNELS_CUSTOM_ID),
  );
  const validationError = validateSetupSelections(
    selectedRoleKeys,
    selectedChannelKeys,
  );
  if (validationError) {
    await replyEphemeral(interaction, validationError);
    return true;
  }

  const conflictingRoles = findConflictingRoles(guild, selectedRoleKeys);
  if (conflictingRoles.length > 0) {
    await replyEphemeral(
      interaction,
      `Setup cannot continue because these roles already exist: ${conflictingRoles.join(", ")}.`,
    );
    return true;
  }

  const bootstrapPlan = planSetupBootstrap(
    guild.channels.cache.map((channel) => ({
      id: channel.id,
      name: channel.name,
      type: channel.type,
    })),
    interaction.channelId,
    selectedRoleKeys,
    selectedChannelKeys,
  );
  if (bootstrapPlan.error) {
    await replyEphemeral(interaction, bootstrapPlan.error);
    return true;
  }

  if (bootstrapPlan.blockingConflicts.length > 0) {
    await replyEphemeral(
      interaction,
      `Setup cannot continue because these channels already exist: ${bootstrapPlan.blockingConflicts.join(", ")}.`,
    );
    return true;
  }

  await deferEphemeral(interaction);

  const claim = await backend.claimGuildSetup(setupWorkerID, interaction.guildId);
  if (!claim.claimed) {
    const blockedMessage = setupBlockedMessage(claim.status) ?? "Setup is not available right now.";
    await replyEphemeral(interaction, blockedMessage);
    return true;
  }

  let createdRoles: Role[] = [];
  let createdChannels: GuildBasedChannel[] = [];
  let bootstrapChannelId: string | null = null;
  let bootstrapOriginalName: string | null = null;

  try {
    createdRoles = await createSetupRoles(
      guild,
      selectedRoleKeys,
      interaction.user.tag,
    );
    const channelCreation = await createSetupChannels(
      guild,
      selectedChannelKeys,
      createdRoles,
      interaction.user.tag,
      bootstrapPlan.bootstrapChannelId,
    );
    createdChannels = channelCreation.createdChannels;
    bootstrapChannelId = channelCreation.bootstrapChannelId;
    bootstrapOriginalName = channelCreation.bootstrapOriginalName;
    await configureGuildChannelSettings(
      guild,
      createdChannels,
      interaction.user.tag,
    ).catch((error) => {
      console.error("[bot] failed to configure guild channel settings", error);
    });
    await backend.completeGuildSetup(
      setupWorkerID,
      interaction.guildId,
      interaction.user.id,
      selectedRoleKeys,
      selectedChannelKeys,
    );

    await replyEphemeral(
      interaction,
      `Setup completed. Created roles: ${formatRoleList(selectedRoleKeys)}. Created channels: ${selectedChannelKeys.length > 0 ? formatChannelList(selectedChannelKeys) : "(none)"}.`,
    );

    if (bootstrapChannelId) {
      const bootstrapChannel = guild.channels.cache.get(bootstrapChannelId);
      if (bootstrapChannel) {
        await bootstrapChannel
          .delete(`RustLens setup by ${interaction.user.tag}`)
          .catch((deleteError) => {
            console.error("[bot] failed to delete bootstrap channel after setup", deleteError);
          });
      }
    }
  } catch (error) {
    if (createdChannels.length > 0) {
      await Promise.allSettled(
        createdChannels
          .slice()
          .reverse()
          .map((channel) =>
            channel.delete(`Rolling back failed RustLens setup by ${interaction.user.tag}`),
          ),
      );
    }

    if (bootstrapChannelId && bootstrapOriginalName) {
      await guild.channels
        .edit(bootstrapChannelId, {
          name: bootstrapOriginalName,
          reason: `Restoring RustLens bootstrap channel after failed setup by ${interaction.user.tag}`,
        })
        .catch(() => undefined);
    }

    if (createdRoles.length > 0) {
      await Promise.allSettled(
        createdRoles.map((role) =>
          role.delete(`Rolling back failed RustLens setup by ${interaction.user.tag}`),
        ),
      );
    }

    await backend.resetGuildSetup(setupWorkerID, interaction.guildId).catch(() => undefined);

    const message = formatSetupError(error);
    await replyEphemeral(interaction, message);
  }

  return true;
}

export const __testables = {
  buildSetupStartModal,
  extractFieldValues,
  getSelectedChannelKeys,
  getTargetChannelNames,
  normalizeModalFieldValue,
  planSetupBootstrap,
  getSelectedRoleKeys,
  validateSetupSelections,
};
