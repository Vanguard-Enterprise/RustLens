import { randomUUID } from "node:crypto";
import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  Guild,
  GuildBasedChannel,
  ModalSubmitInteraction,
  Message,
  Role,
} from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ButtonBuilder,
  ButtonStyle,
  ChannelType,
  InteractionContextType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
  TextChannel,
} from "discord.js";
import {
  BackendClient,
  type WipeReact,
  type WipeReactReactionType,
  type WipeReactSchedule,
} from "./backend-client.js";
import {
  CheckboxGroupBuilder,
  ModalV2Builder,
  StringSelectBuilder,
  TextInputBuilder,
  TextInputStyle,
} from "./builders/index.js";
import { loadConfig } from "./config.js";
import { isGuildTooLargeForDms, replyDmLimitError } from "./dm-limit.js";
import { getGuildMembers } from "./guild-member-cache.js";
import {
  fetchGuildSettings,
  hasGuildAdminAccess,
  resolveConfiguredGuildChannel,
} from "./guild-settings.js";
import {
  RUSTLENS_EMBED_COLOR,
  RUSTLENS_EMBED_ICON_URL,
  type ModalComponentNode,
  getFieldValues,
  logDiscordCatch,
  logDiscordError,
  replyEphemeral,
} from "./shared.js";

export const WIPEREACT_COMMAND_NAME = "wipereact";

const WIPEREACT_CREATE_SUBCOMMAND_NAME = "create";
const WIPEREACT_DELETE_SUBCOMMAND_NAME = "delete";
const WIPEREACT_NOT_REACTED_SUBCOMMAND_NAME = "notreacted";
const WIPEREACT_REACT_GROUP_NAME = "react";
const WIPEREACT_REACT_ADD_SUBCOMMAND_NAME = "add";
const WIPEREACT_REACT_REMOVE_SUBCOMMAND_NAME = "remove";
const WIPEREACT_CREATE_MODAL_CUSTOM_ID = "wipereact_create_modal";
const WIPEREACT_EDIT_MODAL_PREFIX = "wipereact_edit_modal:";
const WIPEREACT_REASON_MODAL_PREFIX = "wipereact_reason_modal:";
const WIPEREACT_NAME_CUSTOM_ID = "wipereact_name";
const WIPEREACT_DESCRIPTION_CUSTOM_ID = "wipereact_description";
const WIPEREACT_TIME_CUSTOM_ID = "wipereact_time";
const WIPEREACT_TIMEZONE_CUSTOM_ID = "wipereact_timezone";
const WIPEREACT_OPTIONS_CUSTOM_ID = "wipereact_options";
const WIPEREACT_REASON_TEXT_CUSTOM_ID = "wipereact_reason_text";
const WIPEREACT_ACCEPT_BUTTON_CUSTOM_ID = "wipereact_accept";
const WIPEREACT_DECLINE_BUTTON_PREFIX = "wipereact_decline:";
const WIPEREACT_LATE_BUTTON_PREFIX = "wipereact_late:";
const WIPEREACT_EDIT_BUTTON_CUSTOM_ID = "wipereact_edit";
const WIPEREACT_EDIT_BUTTON_PREFIX = "wipereact_edit:";
const WIPEREACT_TARGET_OPTION_NAME = "wipereact";
const WIPEREACT_MESSAGE_OPTION_NAME = "message";
const WIPEREACT_SCOPE_OPTION_NAME = "scope";
const WIPEREACT_USER_OPTION_NAME = "user";
const WIPEREACT_TYPE_OPTION_NAME = "type";
const WIPEREACT_OPTION_ASK_LATE_REASON = "ask_late_reason";
const WIPEREACT_OPTION_ASK_DECLINED_REASON = "ask_declined_reason";
const WIPEREACT_SCHEDULE_GROUP_NAME = "schedule";
const WIPEREACT_SCHEDULE_CREATE_SUBCOMMAND_NAME = "create";
const WIPEREACT_SCHEDULE_DELETE_SUBCOMMAND_NAME = "delete";
const WIPEREACT_SCHEDULE_EDIT_SUBCOMMAND_NAME = "edit";
const WIPEREACT_SCHEDULE_MODAL_CUSTOM_ID = "wipereact_schedule_modal";
const WIPEREACT_SCHEDULE_DAYS_CUSTOM_ID = "wipereact_schedule_days";
const WIPEREACT_SCHEDULE_TIME_CUSTOM_ID = "wipereact_schedule_time";
const WIPEREACT_SCHEDULE_REPEAT_CUSTOM_ID = "wipereact_schedule_repeat";
const WIPEREACT_SCHEDULE_TIMEZONE_CUSTOM_ID = "wipereact_schedule_timezone";
const WIPEREACT_SCHEDULE_POST_BEFORE_CUSTOM_ID = "wipereact_schedule_post_before";
const WIPEREACT_SCHEDULE_TARGET_OPTION_NAME = "schedule";
const WIPEREACT_SCHEDULE_EDIT_MODAL_PREFIX = "wipereact_schedule_edit_modal:";
const WIPEREACT_SCHEDULE_STEP_TWO_MODAL_PREFIX = "wipereact_schedule_step2:";
const WIPEREACT_SCHEDULE_CONTINUE_BUTTON_PREFIX = "wipereact_schedule_continue:";
const WIPEREACT_SCHEDULE_CONFIRM_BUTTON_PREFIX = "wipereact_schedule_confirm:";
const WIPEREACT_SCHEDULE_DENY_BUTTON_PREFIX = "wipereact_schedule_deny:";
const WIPEREACT_SCHEDULE_NAME_CUSTOM_ID = "wipereact_schedule_name";
const WIPEREACT_SCHEDULE_DESCRIPTION_CUSTOM_ID = "wipereact_schedule_description";
const WIPEREACT_SCHEDULE_OPTIONS_CUSTOM_ID = "wipereact_schedule_options";
const WIPEREACT_SCHEDULE_OPTION_ASK_LATE_REASON = "ask_late_reason";
const WIPEREACT_SCHEDULE_OPTION_ASK_DECLINED_REASON = "ask_declined_reason";
const SCHEDULE_DRAFT_TTL_MS = 15 * 60 * 1000;
const SCHEDULE_FORCE_WIPE_VALUE = "force_wipe";
const SCHEDULE_DAY_OPTIONS = [
  { label: "Monday", value: "monday" },
  { label: "Tuesday", value: "tuesday" },
  { label: "Wednesday", value: "wednesday" },
  { label: "Thursday", value: "thursday" },
  { label: "Friday", value: "friday" },
  { label: "Saturday", value: "saturday" },
  { label: "Sunday", value: "sunday" },
  { label: "Force Wipe", value: SCHEDULE_FORCE_WIPE_VALUE },
] as const;
const SCHEDULE_REPEAT_OPTIONS = [
  { label: "Weekly", value: "weekly" },
  { label: "Bi-Weekly", value: "biweekly" },
  { label: "Monthly", value: "monthly" },
] as const;
const SCHEDULE_POST_BEFORE_OPTIONS = [
  { label: "1 day before", value: "1d" },
  { label: "2 days before", value: "2d" },
  { label: "3 days before", value: "3d" },
  { label: "4 days before", value: "4d" },
  { label: "1 week before", value: "1w" },
  { label: "2 weeks before", value: "2w" },
] as const;
const WIPE_REACT_ACCEPTED_FIELD_PREFIX = "✅ Accepted";
const WIPE_REACT_DECLINED_FIELD_PREFIX = "❌ Declined";
const WIPE_REACT_LATE_FIELD_PREFIX = "🕑 Late";
const WIPE_REACT_TIMEZONE_FIELD_NAME = "Timezone";
const WIPE_REACT_EMPTY_FIELD_VALUE = "-";
const LOG_ACCEPTED_COLOR = 0x67c587;
const LOG_DECLINED_COLOR = 0xd96b6b;
const LOG_LATE_COLOR = 0xe3a24f;
const LOG_REMOVED_COLOR = 0x99a1ab;
const DEFAULT_WIPEREACT_TIMEZONE = "Europe/London";


type WipeReactAction = WipeReactReactionType;

type WipeReactConfig = {
  name: string;
  description: string;
  timeZone: string;
  scheduledUnix: number;
  askLateReason: boolean;
  askDeclinedReason: boolean;
};

type WipeReactMessageState = WipeReactConfig & {
  creatorId: string | null;
  acceptedUserIds: string[];
  declinedUserIds: string[];
  lateUserIds: string[];
  embedTimestamp: string;
};

type ParsedActionButton =
  | {
      type: "accepted";
      requiresReason: false;
    }
  | {
      type: "declined" | "late";
      requiresReason: boolean;
    };

type ScheduleDraft = {
  userId: string;
  guildId: string;
  config: ScheduleWipeReactConfig;
  editScheduleId: number | null;
  createdAt: number;
};

const scheduleDrafts = new Map<string, ScheduleDraft>();

function pruneExpiredScheduleDrafts(now = Date.now()) {
  for (const [draftId, draft] of scheduleDrafts) {
    if (now - draft.createdAt > SCHEDULE_DRAFT_TTL_MS) {
      scheduleDrafts.delete(draftId);
    }
  }
}

const MAX_TIMEOUT_MS = 2_147_483_647;

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _wipeReactWorkerID: string | undefined;
const wipeReactExpiryTimers = new Map<string, ReturnType<typeof setTimeout>>();

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(
      config.backendUrl,
      config.jwtSecret,
      config.jwtIssuer,
      config.jwtAudience,
    );
    _wipeReactWorkerID = `${config.workerId}-wipereact-${shardID}`;
  }
  return _backend;
}

function getWipeReactWorkerID() {
  getBackend();
  return _wipeReactWorkerID!;
}

const wipeReactCommand = new SlashCommandBuilder()
  .setName(WIPEREACT_COMMAND_NAME)
  .setDescription("RustLens wipe react commands")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(WIPEREACT_CREATE_SUBCOMMAND_NAME)
      .setDescription("Create a wipe react message"),
  )
  .addSubcommandGroup((group) =>
    group
      .setName(WIPEREACT_SCHEDULE_GROUP_NAME)
      .setDescription("Manage wipe react schedules")
      .addSubcommand((subcommand) =>
        subcommand
          .setName(WIPEREACT_SCHEDULE_CREATE_SUBCOMMAND_NAME)
          .setDescription("Create a scheduled wipe react"),
      )
      .addSubcommand((subcommand) =>
        subcommand
          .setName(WIPEREACT_SCHEDULE_DELETE_SUBCOMMAND_NAME)
          .setDescription("Delete a wipe react schedule")
          .addStringOption((option) =>
            option
              .setName(WIPEREACT_SCHEDULE_TARGET_OPTION_NAME)
              .setDescription("Select a schedule")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      )
      .addSubcommand((subcommand) =>
        subcommand
          .setName(WIPEREACT_SCHEDULE_EDIT_SUBCOMMAND_NAME)
          .setDescription("Edit a wipe react schedule")
          .addStringOption((option) =>
            option
              .setName(WIPEREACT_SCHEDULE_TARGET_OPTION_NAME)
              .setDescription("Select a schedule")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(WIPEREACT_DELETE_SUBCOMMAND_NAME)
      .setDescription("Delete an active wipe react")
      .addStringOption((option) =>
        option
          .setName(WIPEREACT_TARGET_OPTION_NAME)
          .setDescription("Select an active wipe react")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(WIPEREACT_NOT_REACTED_SUBCOMMAND_NAME)
      .setDescription("List members missing a response for a wipe react")
      .addStringOption((option) =>
        option
          .setName(WIPEREACT_TARGET_OPTION_NAME)
          .setDescription("Select an active wipe react")
          .setRequired(false)
          .setAutocomplete(true),
      )
      .addStringOption((option) =>
        option
          .setName(WIPEREACT_MESSAGE_OPTION_NAME)
          .setDescription("DMs everyone who has not reacted")
          .setRequired(false),
      )
      .addRoleOption((option) =>
        option
          .setName(WIPEREACT_SCOPE_OPTION_NAME)
          .setDescription("Role to check")
          .setRequired(false),
      ),
  )
  .addSubcommandGroup((group) =>
    group
      .setName(WIPEREACT_REACT_GROUP_NAME)
      .setDescription("Manage wipe react responses")
      .addSubcommand((subcommand) =>
        subcommand
          .setName(WIPEREACT_REACT_ADD_SUBCOMMAND_NAME)
          .setDescription("Add or replace a user's wipe react response")
          .addUserOption((option) =>
            option
              .setName(WIPEREACT_USER_OPTION_NAME)
              .setDescription("User to update")
              .setRequired(true),
          )
          .addStringOption((option) =>
            option
              .setName(WIPEREACT_TYPE_OPTION_NAME)
              .setDescription("Response type")
              .setRequired(true)
              .addChoices(
                { name: "accept", value: "accepted" },
                { name: "decline", value: "declined" },
                { name: "late", value: "late" },
              ),
          )
          .addStringOption((option) =>
            option
              .setName(WIPEREACT_TARGET_OPTION_NAME)
              .setDescription("Select an active wipe react")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      )
      .addSubcommand((subcommand) =>
        subcommand
          .setName(WIPEREACT_REACT_REMOVE_SUBCOMMAND_NAME)
          .setDescription("Remove a user's wipe react response")
          .addUserOption((option) =>
            option
              .setName(WIPEREACT_USER_OPTION_NAME)
              .setDescription("User to update")
              .setRequired(true),
          )
          .addStringOption((option) =>
            option
              .setName(WIPEREACT_TARGET_OPTION_NAME)
              .setDescription("Select an active wipe react")
              .setRequired(true)
              .setAutocomplete(true),
          ),
      ),
  );


async function deferEphemeral(
  interaction: ButtonInteraction | ModalSubmitInteraction,
) {
  if (interaction.deferred || interaction.replied) {
    return;
  }

  await interaction.deferReply({
    flags: MessageFlags.Ephemeral,
  });
}

function padNumber(value: number) {
  return value.toString().padStart(2, "0");
}

function formatTimestampForInput(unixSeconds: number, timeZone = DEFAULT_WIPEREACT_TIMEZONE) {
  const parts = getTimeZoneParts(new Date(unixSeconds * 1000), timeZone);
  return [
    `${parts.day}.${parts.month}.${parts.year}`,
    `${padNumber(parts.hour)}:${padNumber(parts.minute)}`,
  ].join(" ");
}

function resolveTimeZone(input: string | undefined) {
  const trimmed = input?.trim() ?? "";
  const timeZone = trimmed.length > 0 ? trimmed : DEFAULT_WIPEREACT_TIMEZONE;

  try {
    new Intl.DateTimeFormat("en-GB", { timeZone }).format(new Date());
  } catch {
    return {
      timeZone: null,
      error: "Timezone must be a valid IANA timezone such as Europe/London.",
    } as const;
  }

  return {
    timeZone,
    error: null,
  } as const;
}

function getTimeZoneParts(date: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).formatToParts(date);

  const partMap = new Map(parts.map((part) => [part.type, part.value]));
  return {
    year: Number(partMap.get("year")),
    month: Number(partMap.get("month")),
    day: Number(partMap.get("day")),
    hour: Number(partMap.get("hour")),
    minute: Number(partMap.get("minute")),
    second: Number(partMap.get("second")),
  };
}

function getTimeZoneOffsetMilliseconds(date: Date, timeZone: string) {
  const parts = getTimeZoneParts(date, timeZone);
  return (
    Date.UTC(
      parts.year,
      parts.month - 1,
      parts.day,
      parts.hour,
      parts.minute,
      parts.second,
      0,
    ) - date.getTime()
  );
}

function zonedDateTimeToUtc(
  year: number,
  month: number,
  day: number,
  hour: number,
  minute: number,
  timeZone: string,
) {
  const utcGuess = Date.UTC(year, month - 1, day, hour, minute, 0, 0);
  let offset = getTimeZoneOffsetMilliseconds(new Date(utcGuess), timeZone);
  let result = utcGuess - offset;
  const adjustedOffset = getTimeZoneOffsetMilliseconds(new Date(result), timeZone);
  if (adjustedOffset !== offset) {
    offset = adjustedOffset;
    result = utcGuess - offset;
  }

  return new Date(result);
}

function parseFlexibleDateTime(
  input: string,
  timeZone: string,
  now = new Date(),
) {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return {
      date: null,
      error: "Time & Date is required.",
    } as const;
  }

  const normalized = trimmed.replace(/\s+/g, " ");
  const parts = normalized.split(" ");
  if (parts.length > 2) {
    return {
      date: null,
      error: "Use a single date and optional time, for example 01.02.2026 18:30.",
    } as const;
  }

  let datePart = parts.length === 2 ? parts[0] : "";
  let timePart = parts.length === 2 ? parts[1] : "";
  if (parts.length === 1) {
    const token = parts[0];
    if (token.includes(":")) {
      timePart = token;
    } else if (token.includes(".")) {
      datePart = token;
    } else {
      const numericSegments = token.split(/[.-]/).filter((segment) => segment.length > 0);
      const looksLikeTime =
        numericSegments.length === 2 &&
        numericSegments.every((segment) => /^\d{1,2}$/.test(segment));

      if (looksLikeTime) {
        timePart = token;
      } else {
        datePart = token;
      }
    }
  }

  const nowParts = getTimeZoneParts(now, timeZone);
  let year = nowParts.year;
  let month = nowParts.month;
  let day = nowParts.day;

  if (datePart.length > 0) {
    const dateSegments = datePart.split(/[.-]/).filter((segment) => segment.length > 0);
    if (dateSegments.length < 2 || dateSegments.length > 3) {
      return {
        date: null,
        error: "Date must use day.month or day.month.year.",
      } as const;
    }

    day = Number(dateSegments[0]);
    month = Number(dateSegments[1]);
    if (!Number.isInteger(day) || !Number.isInteger(month)) {
      return {
        date: null,
        error: "Date must contain valid numbers.",
      } as const;
    }

    if (dateSegments.length === 3) {
      const rawYear = Number(dateSegments[2]);
      if (!Number.isInteger(rawYear)) {
        return {
          date: null,
          error: "Year must be a valid number.",
        } as const;
      }

      year = dateSegments[2].length === 2 ? 2000 + rawYear : rawYear;
    }
  }

  let hours = 0;
  let minutes = 0;

  if (timePart.length > 0) {
    const timeSegments = timePart.split(/[:.-]/).filter((segment) => segment.length > 0);
    if (timeSegments.length !== 2) {
      return {
        date: null,
        error: "Time must use hour and minute, for example 18:30.",
      } as const;
    }

    hours = Number(timeSegments[0]);
    minutes = Number(timeSegments[1]);
    if (!Number.isInteger(hours) || !Number.isInteger(minutes)) {
      return {
        date: null,
        error: "Time must contain valid numbers.",
      } as const;
    }
  }

  const parsed = zonedDateTimeToUtc(year, month, day, hours, minutes, timeZone);
  const parsedParts = getTimeZoneParts(parsed, timeZone);
  if (
    Number.isNaN(parsed.getTime()) ||
    parsedParts.year !== year ||
    parsedParts.month !== month ||
    parsedParts.day !== day ||
    parsedParts.hour !== hours ||
    parsedParts.minute !== minutes
  ) {
    return {
      date: null,
      error: "Time & Date is not a valid calendar date.",
    } as const;
  }

  return {
    date: parsed,
    error: null,
  } as const;
}

function parseUserIds(value: string | undefined) {
  if (!value) {
    return [];
  }

  const matches = value.matchAll(/<@!?(\d+)>/g);
  return [...new Set([...matches].map((match) => match[1]))];
}

const WIPE_REACT_FIELD_VALUE_MAX = 1024;

function formatUserMentions(userIds: readonly string[]) {
  if (userIds.length === 0) {
    return WIPE_REACT_EMPTY_FIELD_VALUE;
  }

  const mentions = userIds.map((userId) => `<@${userId}>`);
  const buffer: string[] = [];
  let length = 0;

  for (let i = 0; i < mentions.length; i += 1) {
    const mention = mentions[i];
    const sep = buffer.length > 0 ? 1 : 0;
    const remaining = mentions.length - i;
    const overflowSuffix = remaining > 1 ? `\n+${remaining} more` : "";
    if (length + sep + mention.length + overflowSuffix.length > WIPE_REACT_FIELD_VALUE_MAX) {
      const omitted = mentions.length - buffer.length;
      buffer.push(`+${omitted} more`);
      break;
    }
    buffer.push(mention);
    length += sep + mention.length;
  }

  return buffer.join("\n");
}

function buildStatusField(prefix: string, userIds: readonly string[]) {
  return {
    name: `${prefix} (${userIds.length})`,
    value: formatUserMentions(userIds),
    inline: true,
  };
}

function buildWipeReactEmbed(state: WipeReactMessageState) {
  return {
    title: state.name,
    ...(state.description.length > 0 ? { description: state.description } : {}),
    color: RUSTLENS_EMBED_COLOR,
    timestamp: state.embedTimestamp,
    footer: {
      text: "RustLens",
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    fields: [
      {
        name: WIPE_REACT_TIMEZONE_FIELD_NAME,
        value: state.timeZone,
      },
      {
        name: "Date & Time",
        value: `<t:${state.scheduledUnix}:F>\n<t:${state.scheduledUnix}:R>`,
      },
      buildStatusField(WIPE_REACT_ACCEPTED_FIELD_PREFIX, state.acceptedUserIds),
      buildStatusField(WIPE_REACT_DECLINED_FIELD_PREFIX, state.declinedUserIds),
      buildStatusField(WIPE_REACT_LATE_FIELD_PREFIX, state.lateUserIds),
    ],
  };
}

function createWipeReactButtons(
  config: Pick<WipeReactMessageState, "askDeclinedReason" | "askLateReason" | "creatorId">,
  disabled: boolean,
) {
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(WIPEREACT_ACCEPT_BUTTON_CUSTOM_ID)
        .setLabel("Accept")
        .setEmoji("✅")
        .setStyle(ButtonStyle.Success)
        .setDisabled(disabled),
      new ButtonBuilder()
        .setCustomId(`${WIPEREACT_DECLINE_BUTTON_PREFIX}${config.askDeclinedReason ? "1" : "0"}`)
        .setLabel("Decline")
        .setEmoji("❌")
        .setStyle(ButtonStyle.Danger)
        .setDisabled(disabled),
      new ButtonBuilder()
        .setCustomId(`${WIPEREACT_LATE_BUTTON_PREFIX}${config.askLateReason ? "1" : "0"}`)
        .setLabel("Late")
        .setEmoji("🕑")
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(disabled),
      new ButtonBuilder()
        .setCustomId(
          config.creatorId
            ? `${WIPEREACT_EDIT_BUTTON_PREFIX}${config.creatorId}`
            : WIPEREACT_EDIT_BUTTON_CUSTOM_ID,
        )
        .setEmoji("⚙️")
        .setStyle(ButtonStyle.Secondary)
        .setDisabled(disabled),
    ),
  ];
}

function parseScheduledUnix(value: string | undefined) {
  if (!value) {
    return null;
  }

  const match = value.match(/<t:(\d+):F>/);
  return match ? Number(match[1]) : null;
}

function getMessageButtonCustomId(message: Message, prefix: string) {
  for (const row of message.components as Array<{ components?: Array<{ customId?: string }> }>) {
    for (const component of row.components ?? []) {
      const customId = "customId" in component ? component.customId : undefined;
      if (typeof customId === "string" && customId.startsWith(prefix)) {
        return customId;
      }
    }
  }

  return null;
}

function parseReasonToggle(message: Message, prefix: string) {
  const customId = getMessageButtonCustomId(message, prefix);
  if (!customId) {
    return false;
  }

  return customId.split(":")[1] === "1";
}

function parseCreatorId(message: Message) {
  const customId = getMessageButtonCustomId(message, WIPEREACT_EDIT_BUTTON_PREFIX);
  if (!customId) {
    return null;
  }

  const creatorId = customId.slice(WIPEREACT_EDIT_BUTTON_PREFIX.length);
  return creatorId.length > 0 ? creatorId : null;
}

function parseWipeReactMessage(message: Message): WipeReactMessageState | null {
  const [embed] = message.embeds;
  if (!embed?.title) {
    return null;
  }

  const timeZoneField = embed.fields.find(
    (field) => field.name === WIPE_REACT_TIMEZONE_FIELD_NAME,
  );
  const dateField = embed.fields.find((field) => field.name === "Date & Time");
  const acceptedField = embed.fields.find((field) =>
    field.name.startsWith(WIPE_REACT_ACCEPTED_FIELD_PREFIX),
  );
  const declinedField = embed.fields.find((field) =>
    field.name.startsWith(WIPE_REACT_DECLINED_FIELD_PREFIX),
  );
  const lateField = embed.fields.find((field) =>
    field.name.startsWith(WIPE_REACT_LATE_FIELD_PREFIX),
  );

  const scheduledUnix = parseScheduledUnix(dateField?.value);
  if (!scheduledUnix || !acceptedField || !declinedField || !lateField) {
    return null;
  }

  return {
    name: embed.title,
    description: embed.description ?? "",
    timeZone: timeZoneField?.value ?? DEFAULT_WIPEREACT_TIMEZONE,
    scheduledUnix,
    creatorId: parseCreatorId(message),
    askDeclinedReason: parseReasonToggle(message, WIPEREACT_DECLINE_BUTTON_PREFIX),
    askLateReason: parseReasonToggle(message, WIPEREACT_LATE_BUTTON_PREFIX),
    acceptedUserIds: parseUserIds(acceptedField.value),
    declinedUserIds: parseUserIds(declinedField.value),
    lateUserIds: parseUserIds(lateField.value),
    embedTimestamp: embed.timestamp ?? new Date().toISOString(),
  };
}

function buildScheduleWipeReactModal(
  customId = WIPEREACT_SCHEDULE_MODAL_CUSTOM_ID,
  defaults?: Partial<Pick<WipeReactSchedule, "days" | "includesForceWipe" | "time" | "timeZone" | "repeat" | "postBefore">>,
) {
  return new ModalV2Builder()
    .setTitle("Scheduled Wipe React")
    .setCustomId(customId)
    .addComponent(
      "Schedule",
      new CheckboxGroupBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_DAYS_CUSTOM_ID)
        .setRequired(true)
        .setMinValues(1)
        .setMaxValues(8)
        .addOptions(
          ...SCHEDULE_DAY_OPTIONS.map(({ label, value }) => ({
            label,
            value,
            ...(value === SCHEDULE_FORCE_WIPE_VALUE
              ? defaults?.includesForceWipe && { default: true as const }
              : defaults?.days?.includes(value) && { default: true as const }),
          })),
        ),
    )
    .addComponent(
      "Time (HH:MM)",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_TIME_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(false)
        .setMaxLength(5)
        .setPlaceholder("18:30")
        .setValue(defaults?.time ?? ""),
    )
    .addComponent(
      "Timezone",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_TIMEZONE_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(false)
        .setMaxLength(100)
        .setPlaceholder("Defaults to Europe/London")
        .setValue(defaults?.timeZone ?? ""),
    )
    .addComponent(
      "Repeat",
      new StringSelectBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_REPEAT_CUSTOM_ID)
        .setRequired(false)
        .setPlaceholder("How often to post")
        .setMinValues(0)
        .setMaxValues(1)
        .addOptions(
          ...SCHEDULE_REPEAT_OPTIONS.map(({ label, value }) => ({
            label,
            value,
            ...(defaults?.repeat === value && { default: true as const }),
          })),
        ),
    )
    .addComponent(
      "When to post",
      new StringSelectBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_POST_BEFORE_CUSTOM_ID)
        .setPlaceholder("How early to post the wipe react")
        .setMinValues(1)
        .setMaxValues(1)
        .addOptions(
          ...SCHEDULE_POST_BEFORE_OPTIONS.map(({ label, value }) => ({
            label,
            value,
            ...(defaults?.postBefore === value && { default: true as const }),
          })),
        ),
    );
}

type ScheduleWipeReactConfig = {
  days: string[];
  includesForceWipe: boolean;
  time: string | null;
  timeZone: string;
  repeat: string | null;
  postBefore: string;
  name: string;
  description: string;
  askLateReason: boolean;
  askDeclinedReason: boolean;
};

function parseScheduleTime(value: string): { hours: number; minutes: number } | null {
  const match = value.trim().match(/^(\d{1,2}):(\d{2})$/);
  if (!match) {
    return null;
  }

  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours < 0 || hours > 23 || minutes < 0 || minutes > 59) {
    return null;
  }

  return { hours, minutes };
}

function validateScheduleModalSubmission(
  interaction: ModalSubmitInteraction,
): { config: ScheduleWipeReactConfig; error: null } | { config: null; error: string } {
  const selectedDays = getFieldValues(interaction, WIPEREACT_SCHEDULE_DAYS_CUSTOM_ID);
  if (selectedDays.length === 0) {
    return { config: null, error: "Select at least one schedule day." };
  }

  const validDayValues: Set<string> = new Set(SCHEDULE_DAY_OPTIONS.map((option) => option.value));
  for (const day of selectedDays) {
    if (!validDayValues.has(day)) {
      return { config: null, error: `Invalid schedule day: ${day}` };
    }
  }

  const includesForceWipe = selectedDays.includes(SCHEDULE_FORCE_WIPE_VALUE);
  const regularDays = selectedDays.filter((day) => day !== SCHEDULE_FORCE_WIPE_VALUE);
  const hasRegularDays = regularDays.length > 0;

  const rawTime = getFieldValues(interaction, WIPEREACT_SCHEDULE_TIME_CUSTOM_ID)[0]?.trim() ?? "";
  let time: string | null = null;
  if (hasRegularDays) {
    if (rawTime.length === 0) {
      return { config: null, error: "Time is required when a regular day is selected." };
    }
    const parsed = parseScheduleTime(rawTime);
    if (!parsed) {
      return { config: null, error: "Time must be in HH:MM format (24-hour)." };
    }
    time = `${String(parsed.hours).padStart(2, "0")}:${String(parsed.minutes).padStart(2, "0")}`;
  }

  const timeZoneResult = resolveTimeZone(
    getFieldValues(interaction, WIPEREACT_SCHEDULE_TIMEZONE_CUSTOM_ID)[0],
  );
  if (timeZoneResult.error || !timeZoneResult.timeZone) {
    return { config: null, error: timeZoneResult.error ?? "Timezone is invalid." };
  }

  const repeatValues = getFieldValues(interaction, WIPEREACT_SCHEDULE_REPEAT_CUSTOM_ID);
  let repeat: string | null = null;
  if (hasRegularDays) {
    if (repeatValues.length === 0) {
      return { config: null, error: "Repeat is required when a regular day is selected." };
    }
    const validRepeatValues: Set<string> = new Set(SCHEDULE_REPEAT_OPTIONS.map((option) => option.value));
    if (!validRepeatValues.has(repeatValues[0])) {
      return { config: null, error: "Invalid repeat value." };
    }
    repeat = repeatValues[0];
  }

  const postBeforeValues = getFieldValues(interaction, WIPEREACT_SCHEDULE_POST_BEFORE_CUSTOM_ID);
  if (postBeforeValues.length === 0) {
    return { config: null, error: "When to post is required." };
  }
  const validPostBeforeValues: Set<string> = new Set(SCHEDULE_POST_BEFORE_OPTIONS.map((option) => option.value));
  if (!validPostBeforeValues.has(postBeforeValues[0])) {
    return { config: null, error: "Invalid post-before value." };
  }

  return {
    config: {
      days: regularDays,
      includesForceWipe,
      time,
      timeZone: timeZoneResult.timeZone,
      repeat,
      postBefore: postBeforeValues[0],
      name: "",
      description: "",
      askLateReason: false,
      askDeclinedReason: false,
    },
    error: null,
  };
}

function formatScheduleSummary(config: ScheduleWipeReactConfig) {
  const lines: string[] = [];

  if (config.days.length > 0) {
    const dayLabels = config.days.map(
      (day) => SCHEDULE_DAY_OPTIONS.find((option) => option.value === day)?.label ?? day,
    );
    lines.push(`**Days:** ${dayLabels.join(", ")}`);
    lines.push(`**Time:** ${config.time}`);
    lines.push(`**Timezone:** ${config.timeZone}`);
    const repeatLabel = SCHEDULE_REPEAT_OPTIONS.find((option) => option.value === config.repeat)?.label ?? config.repeat;
    lines.push(`**Repeat:** ${repeatLabel}`);
  }

  if (config.includesForceWipe) {
    lines.push(`**Force Wipe:** First Thursday of the month at 19:00 Europe/London`);
  }

  const postBeforeLabel = SCHEDULE_POST_BEFORE_OPTIONS.find(
    (option) => option.value === config.postBefore,
  )?.label ?? config.postBefore;
  lines.push(`**Post:** ${postBeforeLabel}`);

  return lines.join("\n");
}

function buildSchedulePayload(config: ScheduleWipeReactConfig, createdBy: string, channelId = "") {
  return {
    days: config.days,
    includesForceWipe: config.includesForceWipe,
    time: config.time ?? "",
    timeZone: config.timeZone,
    repeat: config.repeat ?? "",
    postBefore: config.postBefore,
    name: config.name,
    description: config.description,
    askLateReason: config.askLateReason,
    askDeclinedReason: config.askDeclinedReason,
    channelId,
    createdBy,
  };
}

function buildScheduleStepTwoModal(
  draftId: string,
  defaults?: { name?: string; description?: string; askLateReason?: boolean; askDeclinedReason?: boolean },
) {
  return new ModalV2Builder()
    .setTitle("Scheduled Wipe React")
    .setCustomId(`${WIPEREACT_SCHEDULE_STEP_TWO_MODAL_PREFIX}${draftId}`)
    .addComponent(
      "Title",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_NAME_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(100)
        .setValue(defaults?.name ?? ""),
    )
    .addComponent(
      "Description",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_DESCRIPTION_CUSTOM_ID)
        .setStyle(TextInputStyle.Paragraph)
        .setRequired(false)
        .setMaxLength(250)
        .setValue(defaults?.description ?? ""),
    )
    .addComponent(
      "Options",
      new CheckboxGroupBuilder()
        .setCustomId(WIPEREACT_SCHEDULE_OPTIONS_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(2)
        .addOptions(
          {
            label: "Ask for late reason?",
            value: WIPEREACT_SCHEDULE_OPTION_ASK_LATE_REASON,
            ...(defaults?.askLateReason && { default: true }),
          },
          {
            label: "Ask for declined reason?",
            value: WIPEREACT_SCHEDULE_OPTION_ASK_DECLINED_REASON,
            ...(defaults?.askDeclinedReason && { default: true }),
          },
        ),
    );
}

function buildScheduleConfirmEmbed(config: ScheduleWipeReactConfig) {
  return {
    title: `Schedule: ${config.name}`,
    ...(config.description.length > 0 ? { description: config.description } : {}),
    color: RUSTLENS_EMBED_COLOR,
    fields: [
      { name: "Schedule", value: formatScheduleSummary(config) },
      { name: "Ask late reason", value: config.askLateReason ? "Yes" : "No", inline: true },
      { name: "Ask declined reason", value: config.askDeclinedReason ? "Yes" : "No", inline: true },
    ],
    footer: {
      text: "RustLens",
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    timestamp: new Date().toISOString(),
  };
}

function createScheduleConfirmButtons(draftId: string, disabled = false) {
  return [
    new ActionRowBuilder<ButtonBuilder>().addComponents(
      new ButtonBuilder()
        .setCustomId(`${WIPEREACT_SCHEDULE_CONFIRM_BUTTON_PREFIX}${draftId}`)
        .setLabel("Confirm")
        .setEmoji("✅")
        .setStyle(ButtonStyle.Success)
        .setDisabled(disabled),
      new ButtonBuilder()
        .setCustomId(`${WIPEREACT_SCHEDULE_DENY_BUTTON_PREFIX}${draftId}`)
        .setLabel("Cancel")
        .setEmoji("❌")
        .setStyle(ButtonStyle.Danger)
        .setDisabled(disabled),
    ),
  ];
}

function formatScheduleChoiceName(schedule: WipeReactSchedule) {
  const parts: string[] = [];
  if (schedule.days.length > 0) {
    const dayLabels = schedule.days.map(
      (day) => SCHEDULE_DAY_OPTIONS.find((option) => option.value === day)?.label ?? day,
    );
    parts.push(dayLabels.join(", "));
  }
  if (schedule.includesForceWipe) {
    parts.push("Force Wipe");
  }
  if (schedule.time) {
    parts.push(schedule.time);
  }
  const label = `#${schedule.id} | ${parts.join(" | ")}`;
  return label.length <= 100 ? label : `${label.slice(0, 97)}...`;
}

function buildWipeReactModal(
  customId: string,
  defaults?: Partial<WipeReactConfig>,
) {
  return new ModalV2Builder()
    .setTitle("Wipe React")
    .setCustomId(customId)
    .addComponent(
      "Name",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_NAME_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setMaxLength(100)
        .setValue(defaults?.name ?? ""),
    )
    .addComponent(
      "Description",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_DESCRIPTION_CUSTOM_ID)
        .setStyle(TextInputStyle.Paragraph)
        .setRequired(false)
        .setMaxLength(250)
        .setValue(defaults?.description ?? ""),
    )
    .addComponent(
      "Time & Date",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_TIME_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(true)
        .setPlaceholder("DD.MM.YY HH:MM")
        .setValue(
          defaults?.scheduledUnix
            ? formatTimestampForInput(
                defaults.scheduledUnix,
                defaults.timeZone ?? DEFAULT_WIPEREACT_TIMEZONE,
              )
            : "",
        ),
    )
    .addComponent(
      "Timezone",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_TIMEZONE_CUSTOM_ID)
        .setStyle(TextInputStyle.Short)
        .setRequired(false)
        .setMaxLength(100)
        .setPlaceholder("Defaults to Europe/London")
        .setValue(defaults?.timeZone ?? ""),
    )
    .addComponent(
      "Anything else?",
      new CheckboxGroupBuilder()
        .setCustomId(WIPEREACT_OPTIONS_CUSTOM_ID)
        .setRequired(false)
        .setMinValues(0)
        .setMaxValues(2)
        .addOptions(
          {
            label: "Ask for late reason?",
            value: WIPEREACT_OPTION_ASK_LATE_REASON,
            ...(defaults?.askLateReason && { default: true }),
          },
          {
            label: "Ask for declined reason?",
            value: WIPEREACT_OPTION_ASK_DECLINED_REASON,
            ...(defaults?.askDeclinedReason && { default: true }),
          },
        ),
    );
}

function buildReasonModal(
  action: Extract<WipeReactAction, "declined" | "late">,
  channelId: string,
  messageId: string,
) {
  const title = action === "declined" ? "Decline Reason" : "Late Reason";
  return new ModalV2Builder()
    .setTitle(title)
    .setCustomId(`${WIPEREACT_REASON_MODAL_PREFIX}${action}:${channelId}:${messageId}`)
    .addComponent(
      "Reason",
      new TextInputBuilder()
        .setCustomId(WIPEREACT_REASON_TEXT_CUSTOM_ID)
        .setStyle(TextInputStyle.Paragraph)
        .setRequired(true)
        .setMaxLength(1000),
    );
}

function buildWipeReactStateFromModal(
  interaction: ModalSubmitInteraction,
  embedTimestamp: string,
  creatorId: string | null,
) {
  const timeZoneResult = resolveTimeZone(
    getFieldValues(interaction, WIPEREACT_TIMEZONE_CUSTOM_ID)[0],
  );
  if (timeZoneResult.error || !timeZoneResult.timeZone) {
    return {
      state: null,
      error: timeZoneResult.error ?? "Timezone is invalid.",
    } as const;
  }

  const name = getFieldValues(interaction, WIPEREACT_NAME_CUSTOM_ID)[0]?.trim() ?? "";
  if (name.length === 0) {
    return {
      state: null,
      error: "Name is required.",
    } as const;
  }

  const description =
    getFieldValues(interaction, WIPEREACT_DESCRIPTION_CUSTOM_ID)[0]?.trim() ?? "";
  const timeInput = getFieldValues(interaction, WIPEREACT_TIME_CUSTOM_ID)[0] ?? "";
  const parsedDate = parseFlexibleDateTime(timeInput, timeZoneResult.timeZone);
  if (parsedDate.error || !parsedDate.date) {
    return {
      state: null,
      error: parsedDate.error ?? "Time & Date is invalid.",
    } as const;
  }

  if (parsedDate.date.getTime() <= Date.now()) {
    return {
      state: null,
      error: "You cannot create or edit a wipe react in the past.",
    } as const;
  }

  const options = new Set(getFieldValues(interaction, WIPEREACT_OPTIONS_CUSTOM_ID));

  return {
    state: {
      name,
      description,
      timeZone: timeZoneResult.timeZone,
      scheduledUnix: Math.floor(parsedDate.date.getTime() / 1000),
      creatorId,
      askLateReason: options.has(WIPEREACT_OPTION_ASK_LATE_REASON),
      askDeclinedReason: options.has(WIPEREACT_OPTION_ASK_DECLINED_REASON),
      acceptedUserIds: [],
      declinedUserIds: [],
      lateUserIds: [],
      embedTimestamp,
    } satisfies WipeReactMessageState,
    error: null,
  } as const;
}

function parseActionButton(customId: string): ParsedActionButton | null {
  if (customId === WIPEREACT_ACCEPT_BUTTON_CUSTOM_ID) {
    return {
      type: "accepted",
      requiresReason: false,
    };
  }

  if (customId.startsWith(WIPEREACT_DECLINE_BUTTON_PREFIX)) {
    return {
      type: "declined",
      requiresReason: customId.slice(WIPEREACT_DECLINE_BUTTON_PREFIX.length) === "1",
    };
  }

  if (customId.startsWith(WIPEREACT_LATE_BUTTON_PREFIX)) {
    return {
      type: "late",
      requiresReason: customId.slice(WIPEREACT_LATE_BUTTON_PREFIX.length) === "1",
    };
  }

  return null;
}

function getUserReactionType(state: WipeReactMessageState, userId: string): WipeReactAction | null {
  if (state.acceptedUserIds.includes(userId)) {
    return "accepted";
  }
  if (state.declinedUserIds.includes(userId)) {
    return "declined";
  }
  if (state.lateUserIds.includes(userId)) {
    return "late";
  }
  return null;
}

function applyUserReaction(
  state: WipeReactMessageState,
  userId: string,
  action: WipeReactAction,
) {
  const acceptedUserIds = state.acceptedUserIds.filter((entry) => entry !== userId);
  const declinedUserIds = state.declinedUserIds.filter((entry) => entry !== userId);
  const lateUserIds = state.lateUserIds.filter((entry) => entry !== userId);

  const previousAction = getUserReactionType(state, userId);

  if (action === "accepted") {
    acceptedUserIds.push(userId);
  } else if (action === "declined") {
    declinedUserIds.push(userId);
  } else {
    lateUserIds.push(userId);
  }

  return {
    state: {
      ...state,
      acceptedUserIds,
      declinedUserIds,
      lateUserIds,
    },
    changed: previousAction !== action,
  } as const;
}

function actionLabel(action: WipeReactAction) {
  switch (action) {
    case "accepted":
      return "Accepted";
    case "declined":
      return "Declined";
    case "late":
      return "Late";
  }
}

function actionEmoji(action: WipeReactAction) {
  switch (action) {
    case "accepted":
      return "✅";
    case "declined":
      return "❌";
    case "late":
      return "🕑";
  }
}

function actionLogColor(action: WipeReactAction) {
  switch (action) {
    case "accepted":
      return LOG_ACCEPTED_COLOR;
    case "declined":
      return LOG_DECLINED_COLOR;
    case "late":
      return LOG_LATE_COLOR;
  }
}

function buildReactionLogEmbed(
  state: WipeReactMessageState,
  action: WipeReactAction,
  user: { tag: string; id: string; displayAvatarURL: (options?: { size?: number }) => string },
  reason?: string,
) {
  return {
    color: actionLogColor(action),
    author: {
      name: user.tag,
      icon_url: user.displayAvatarURL({ size: 128 }),
    },
    fields: [
      {
        name: "Reacted with",
        value: `${actionLabel(action)}\n${actionEmoji(action)}`,
      },
      {
        name: "Discord",
        value: `<@${user.id}>`,
      },
      ...(reason
        ? [
            {
              name: "Reason",
              value: reason,
            },
          ]
        : []),
    ],
    footer: {
      text: `RustLens | ${state.name}`,
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    timestamp: new Date().toISOString(),
  };
}

function buildReactionRemovalLogEmbed(
  state: WipeReactMessageState,
  action: WipeReactAction,
  user: { tag: string; id: string; displayAvatarURL: (options?: { size?: number }) => string },
  reason: string,
) {
  return {
    color: LOG_REMOVED_COLOR,
    author: {
      name: user.tag,
      icon_url: user.displayAvatarURL({ size: 128 }),
    },
    fields: [
      {
        name: "Removed reaction",
        value: `${actionLabel(action)}\n${actionEmoji(action)}`,
      },
      {
        name: "Discord",
        value: `<@${user.id}>`,
      },
      {
        name: "Reason",
        value: reason,
      },
    ],
    footer: {
      text: `RustLens | ${state.name}`,
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    timestamp: new Date().toISOString(),
  };
}

function buildMissingReactantsEmbed(
  state: WipeReactMessageState,
  missingUserIds: readonly string[],
  messageStatus?: { sent: number; failed: number },
) {
  const fields: { name: string; value: string }[] = [];

  if (messageStatus) {
    fields.push({
      name: "Message status",
      value:
        `> * Successfully sent to **${messageStatus.sent}** member${messageStatus.sent === 1 ? "" : "s"}\n` +
        `> * Failed to send to **${messageStatus.failed}** member${messageStatus.failed === 1 ? "" : "s"}`,
    });
  }

  fields.push({
    name: "Users",
    value:
      missingUserIds.length > 0
        ? missingUserIds.map((userId) => `> * <@${userId}>`).join("\n")
        : "> * None",
  });

  return {
    title: "Missing reactants",
    color: RUSTLENS_EMBED_COLOR,
    footer: {
      text: "RustLens.com",
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    timestamp: new Date().toISOString(),
    thumbnail: {
      url: "",
    },
    fields,
  };
}

function buildNotReactedDmEmbed(message: string, wipeReactURL: string) {
  return {
    title: "You have not reacted to the wipe react",
    description: `${message}\n\n${wipeReactURL}`,
    timestamp: new Date().toISOString(),
    color: RUSTLENS_EMBED_COLOR,
    footer: {
      text: "RustLens.com",
      icon_url: RUSTLENS_EMBED_ICON_URL,
    },
    image: {
      url: "",
    },
    thumbnail: {
      url: "",
    },
    fields: [],
  };
}

function buildPersistedWipeReact(
  guildId: string,
  channelId: string,
  messageId: string,
  state: WipeReactMessageState,
): WipeReact {
  return {
    guildId,
    channelId,
    messageId,
    creatorId: state.creatorId ?? "",
    name: state.name,
    description: state.description,
    timeZone: state.timeZone,
    scheduledUnix: state.scheduledUnix,
    askLateReason: state.askLateReason,
    askDeclinedReason: state.askDeclinedReason,
    acceptedUserIds: [...state.acceptedUserIds],
    declinedUserIds: [...state.declinedUserIds],
    lateUserIds: [...state.lateUserIds],
    embedTimestamp: state.embedTimestamp,
  };
}

function toWipeReactState(record: WipeReact): WipeReactMessageState {
  return {
    name: record.name,
    description: record.description,
    timeZone: record.timeZone,
    scheduledUnix: record.scheduledUnix,
    creatorId: record.creatorId,
    askLateReason: record.askLateReason,
    askDeclinedReason: record.askDeclinedReason,
    acceptedUserIds: record.acceptedUserIds,
    declinedUserIds: record.declinedUserIds,
    lateUserIds: record.lateUserIds,
    embedTimestamp: record.embedTimestamp,
  };
}

async function listActiveWipeReacts(guildId: string) {
  const response = await getBackend().listActiveWipeReacts(getWipeReactWorkerID(), guildId);
  return response.items;
}

async function getPersistedWipeReact(guildId: string, messageId: string) {
  return getBackend().getWipeReact(getWipeReactWorkerID(), guildId, messageId);
}

async function resolveLogsChannel(interaction: {
  guildId: string | null;
  guild: Guild | null;
}) {
  if (!interaction.guildId || !interaction.guild) {
    return null;
  }

  const settings = await fetchGuildSettings(interaction.guildId);
  if (!settings.wipeReactLogChannelId) {
    return null;
  }

  const configured = await resolveConfiguredGuildChannel(
    interaction.guild,
    settings.wipeReactLogChannelId,
  );
  return configured?.type === ChannelType.GuildText || configured?.type === ChannelType.GuildAnnouncement
    ? (configured as TextChannel)
    : null;
}

async function fetchWipeReactMessage(
  guild: Guild,
  channelId: string,
  messageId: string,
) {
  const channel = guild.channels.cache.get(channelId) ?? (await guild.channels.fetch(channelId));
  if (!channel?.isTextBased() || !("messages" in channel)) {
    return null;
  }

  return channel.messages.fetch(messageId).catch(logDiscordCatch("fetch wipe react message", null, {
    guildId: guild.id,
    channelId,
    messageId,
  }));
}

async function syncPersistedWipeReactMessage(guild: Guild, record: WipeReact) {
  const message = await fetchWipeReactMessage(guild, record.channelId, record.messageId);
  if (!message) {
    await getBackend().deleteWipeReact(getWipeReactWorkerID(), guild.id, record.messageId).catch(() => undefined);
    const timer = wipeReactExpiryTimers.get(record.messageId);
    if (timer) {
      clearTimeout(timer);
      wipeReactExpiryTimers.delete(record.messageId);
    }
    return null;
  }

  const disabled = record.scheduledUnix <= Math.floor(Date.now() / 1000);
  const updatedMessage = await message.edit({
    embeds: [buildWipeReactEmbed(toWipeReactState(record))],
    components: createWipeReactButtons(toWipeReactState(record), disabled),
  });

  if (disabled) {
    const timer = wipeReactExpiryTimers.get(record.messageId);
    if (timer) {
      clearTimeout(timer);
      wipeReactExpiryTimers.delete(record.messageId);
    }
  } else {
    scheduleWipeReactExpiry(updatedMessage, record.scheduledUnix);
  }

  return updatedMessage;
}

function formatWipeReactChoiceName(state: WipeReactMessageState) {
  const dateLabel = new Intl.DateTimeFormat("en-GB", {
    timeZone: state.timeZone,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(state.scheduledUnix * 1000));
  const label = `${state.name} | ${dateLabel}`;
  return label.length <= 100 ? label : `${label.slice(0, 97)}...`;
}

function pickDefaultNotReactedWipeReact(records: readonly WipeReact[]) {
  if (records.length === 0) {
    return null;
  }

  const nowUnix = Math.floor(Date.now() / 1000);
  const nextRecord = [...records]
    .filter((record) => record.scheduledUnix >= nowUnix)
    .sort((left, right) => left.scheduledUnix - right.scheduledUnix)[0];

  return nextRecord ?? [...records].sort((left, right) => right.scheduledUnix - left.scheduledUnix)[0] ?? null;
}

function extractTargetFromCustomId(customId: string, prefix: string) {
  if (!customId.startsWith(prefix)) {
    return null;
  }

  const parts = customId.slice(prefix.length).split(":");
  return parts.every((entry) => entry.length > 0) ? parts : null;
}

async function disableExpiredWipeReact(message: Message, state: WipeReactMessageState) {
  await message.edit({
    embeds: [buildWipeReactEmbed(state)],
    components: createWipeReactButtons(state, true),
  });
}

async function disableScheduledWipeReact(message: Message) {
  if (!message.channel.isTextBased() || !("messages" in message.channel)) {
    return;
  }

  const latestMessage = await message.channel.messages.fetch(message.id).catch(logDiscordCatch("fetch latest wipe react message", null, {
    guildId: message.guildId,
    channelId: message.channelId,
    messageId: message.id,
  }));
  if (!latestMessage) {
    return;
  }

  if (!latestMessage.guildId) {
    return;
  }

  const record = await getPersistedWipeReact(latestMessage.guildId, latestMessage.id).catch(() => null);
  if (!record) {
    const legacyState = parseWipeReactMessage(latestMessage);
    if (!legacyState) {
      return;
    }
    const nowUnix = Math.floor(Date.now() / 1000);
    if (legacyState.scheduledUnix > nowUnix) {
      scheduleWipeReactExpiry(latestMessage, legacyState.scheduledUnix);
      return;
    }
    await disableExpiredWipeReact(latestMessage, legacyState).catch(logDiscordCatch("disable legacy expired wipe react", undefined, {
      guildId: latestMessage.guildId,
      channelId: latestMessage.channelId,
      messageId: latestMessage.id,
    }));
    return;
  }

  const nowUnix = Math.floor(Date.now() / 1000);
  if (record.scheduledUnix > nowUnix) {
    scheduleWipeReactExpiry(latestMessage, record.scheduledUnix);
    return;
  }

  await disableExpiredWipeReact(latestMessage, toWipeReactState(record)).catch(logDiscordCatch("disable expired wipe react", undefined, {
    guildId: latestMessage.guildId,
    channelId: latestMessage.channelId,
    messageId: latestMessage.id,
  }));
}

function scheduleWipeReactExpiry(message: Message, scheduledUnix: number) {
  const existingTimer = wipeReactExpiryTimers.get(message.id);
  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  const delayMs = Math.max(scheduledUnix * 1000 - Date.now(), 0);
  if (delayMs > MAX_TIMEOUT_MS) {
    // setTimeout caps at ~24.8 days; re-arm at the cap instead of firing the
    // disable handler early.
    const rearmTimer = setTimeout(() => {
      wipeReactExpiryTimers.delete(message.id);
      scheduleWipeReactExpiry(message, scheduledUnix);
    }, MAX_TIMEOUT_MS);
    wipeReactExpiryTimers.set(message.id, rearmTimer);
    return;
  }

  const timer = setTimeout(() => {
    wipeReactExpiryTimers.delete(message.id);
    void disableScheduledWipeReact(message);
  }, delayMs);

  wipeReactExpiryTimers.set(message.id, timer);
}

async function processWipeReactAction(
  interaction: ButtonInteraction | ModalSubmitInteraction,
  message: Message,
  action: WipeReactAction,
  reason?: string,
) {
  if (!interaction.guildId) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  const record = await getPersistedWipeReact(interaction.guildId, message.id).catch(() => null);
  if (!record) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  const state = toWipeReactState(record);
  if (record.scheduledUnix <= Math.floor(Date.now() / 1000)) {
    await disableExpiredWipeReact(message, state).catch(logDiscordCatch("disable expired wipe react during action", undefined, {
      guildId: interaction.guildId,
      channelId: message.channelId,
      messageId: message.id,
    }));
    await replyEphemeral(interaction, "This wipe react has already closed.");
    return true;
  }

  const logsChannel = await resolveLogsChannel(interaction);
  if (!logsChannel) {
    await replyEphemeral(
      interaction,
      "RustLens needs Wipe React Logs configured before wipe reacts can be used. Run /settings and set Wipe React Logs.",
    );
    return true;
  }

  if (getUserReactionType(state, interaction.user.id) === action) {
    const removal = await getBackend().removeWipeReactReaction(
      getWipeReactWorkerID(),
      interaction.guildId,
      message.id,
      interaction.user.id,
    );
    if (!removal.changed || !removal.removedType) {
      await replyEphemeral(interaction, "You do not have a reaction on this wipe react.");
      return true;
    }

    await logsChannel.send({
      embeds: [
        buildReactionRemovalLogEmbed(
          toWipeReactState(removal.wipeReact),
          removal.removedType,
          interaction.user,
          `Removed by clicking ${actionLabel(action)} again.`,
        ),
      ],
    });

    await syncPersistedWipeReactMessage(message.guild!, removal.wipeReact);

    await replyEphemeral(interaction, `${actionEmoji(action)} Removed your ${actionLabel(action).toLowerCase()} reaction.`);
    return true;
  }

  const updated = await getBackend().setWipeReactReaction(
    getWipeReactWorkerID(),
    interaction.guildId,
    message.id,
    interaction.user.id,
    action,
  );
  if (!updated.changed) {
    await replyEphemeral(
      interaction,
      `${actionEmoji(action)} You are already marked as ${actionLabel(action).toLowerCase()}.`,
    );
    return true;
  }

  await logsChannel.send({
    embeds: [buildReactionLogEmbed(toWipeReactState(updated.wipeReact), action, interaction.user, reason?.trim())],
  });

  await syncPersistedWipeReactMessage(message.guild!, updated.wipeReact);

  await replyEphemeral(
    interaction,
    `${actionEmoji(action)} You are now marked as ${actionLabel(action).toLowerCase()}.`,
  );
  return true;
}

export async function ensureWipeReactCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = wipeReactCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /wipereact command (global)");
}

export async function rehydratePersistedWipeReacts(client: Client) {
  const guilds = [...client.guilds.cache.values()];
  await Promise.all(
    guilds.map(async (guild) => {
      const records = await listActiveWipeReacts(guild.id).catch((error) => {
        console.error("[bot] failed to load persisted wipe reacts", { guildId: guild.id, error });
        return null;
      });
      if (!records) {
        return;
      }

      await Promise.all(
        records.map(async (record) => {
          const message = await fetchWipeReactMessage(guild, record.channelId, record.messageId);
          if (!message) {
            await getBackend().deleteWipeReact(getWipeReactWorkerID(), guild.id, record.messageId).catch(() => undefined);
            return;
          }

          if (record.scheduledUnix <= Math.floor(Date.now() / 1000)) {
            await disableExpiredWipeReact(message, toWipeReactState(record)).catch(logDiscordCatch("disable expired wipe react during rehydrate", undefined, {
              guildId: guild.id,
              channelId: message.channelId,
              messageId: message.id,
            }));
            return;
          }

          scheduleWipeReactExpiry(message, record.scheduledUnix);
        }),
      );
    }),
  );
}

export async function handleWipeReactChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== WIPEREACT_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Wipe react can only be used inside a server.");
    return true;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeral(interaction, "Only the configured admin role or administrators can use /wipereact.");
    return true;
  }

  const guild = interaction.guild;
  if (!guild) {
    await replyEphemeral(interaction, "Wipe react can only be used inside a server.");
    return true;
  }

  const subcommandGroup = interaction.options.getSubcommandGroup(false);
  const subcommand = interaction.options.getSubcommand(false);
  if (!subcommandGroup && subcommand === WIPEREACT_CREATE_SUBCOMMAND_NAME) {
    const logsChannel = await resolveLogsChannel(interaction);
    if (!logsChannel) {
      await replyEphemeral(
        interaction,
        "RustLens could not find Wipe React Logs. Run /settings and set Wipe React Logs before creating a wipe react.",
      );
      return true;
    }

    await interaction.showModal(buildWipeReactModal(WIPEREACT_CREATE_MODAL_CUSTOM_ID).toJSON() as any);
    return true;
  }

  if (subcommandGroup === WIPEREACT_SCHEDULE_GROUP_NAME) {
    if (subcommand === WIPEREACT_SCHEDULE_CREATE_SUBCOMMAND_NAME) {
      await interaction.showModal(buildScheduleWipeReactModal().toJSON() as any);
      return true;
    }

    if (subcommand === WIPEREACT_SCHEDULE_DELETE_SUBCOMMAND_NAME) {
      const scheduleId = Number(interaction.options.getString(WIPEREACT_SCHEDULE_TARGET_OPTION_NAME, true));
      if (!Number.isInteger(scheduleId) || scheduleId <= 0) {
        await replyEphemeral(interaction, "That schedule could not be found.");
        return true;
      }

      try {
        await getBackend().deleteWipeReactSchedule(getWipeReactWorkerID(), interaction.guildId, scheduleId);
        await replyEphemeral(interaction, "Schedule deleted.");
      } catch {
        await replyEphemeral(interaction, "That schedule could not be found.");
      }
      return true;
    }

    if (subcommand === WIPEREACT_SCHEDULE_EDIT_SUBCOMMAND_NAME) {
      const scheduleId = Number(interaction.options.getString(WIPEREACT_SCHEDULE_TARGET_OPTION_NAME, true));
      if (!Number.isInteger(scheduleId) || scheduleId <= 0) {
        await replyEphemeral(interaction, "That schedule could not be found.");
        return true;
      }

      let schedule: WipeReactSchedule;
      try {
        schedule = await getBackend().getWipeReactSchedule(getWipeReactWorkerID(), interaction.guildId, scheduleId);
      } catch {
        await replyEphemeral(interaction, "That schedule could not be found.");
        return true;
      }

      await interaction.showModal(
        buildScheduleWipeReactModal(
          `${WIPEREACT_SCHEDULE_EDIT_MODAL_PREFIX}${scheduleId}`,
          schedule,
        ).toJSON() as any,
      );
      return true;
    }
  }

  if (subcommand === WIPEREACT_DELETE_SUBCOMMAND_NAME) {
    const messageId = interaction.options.getString(WIPEREACT_TARGET_OPTION_NAME, true);
    const record = await getPersistedWipeReact(interaction.guildId, messageId).catch(() => null);
    if (!record) {
      await replyEphemeral(interaction, "That wipe react could not be found.");
      return true;
    }

    const message = await fetchWipeReactMessage(guild, record.channelId, record.messageId);
    if (message) {
      try {
        await message.delete();
      } catch (error) {
        logDiscordError("delete wipe react message", error, {
          guildId: interaction.guildId,
          channelId: record.channelId,
          messageId: record.messageId,
        });
        await replyEphemeral(interaction, "RustLens could not delete that wipe react.");
        return true;
      }
    }

    const timer = wipeReactExpiryTimers.get(record.messageId);
    if (timer) {
      clearTimeout(timer);
      wipeReactExpiryTimers.delete(record.messageId);
    }

    await getBackend().deleteWipeReact(getWipeReactWorkerID(), interaction.guildId, record.messageId).catch(() => undefined);
    await replyEphemeral(interaction, message ? "Wipe react deleted." : "Wipe react state deleted. The Discord message was already missing.");
    return true;
  }

  if (subcommand === WIPEREACT_NOT_REACTED_SUBCOMMAND_NAME) {
    const scope = interaction.options.getRole(WIPEREACT_SCOPE_OPTION_NAME, false);
    if (scope && !("members" in scope)) {
      await replyEphemeral(interaction, "That scope role is not available in this server.");
      return true;
    }

    await interaction.deferReply({ flags: MessageFlags.Ephemeral });

    const messageText = interaction.options.getString(WIPEREACT_MESSAGE_OPTION_NAME)?.trim() ?? "";
    if (messageText.length > 0 && isGuildTooLargeForDms(guild)) {
      await replyDmLimitError(interaction, guild);
      return true;
    }

    const selectedMessageId = interaction.options.getString(WIPEREACT_TARGET_OPTION_NAME);
    const record = selectedMessageId
      ? await getPersistedWipeReact(interaction.guildId, selectedMessageId).catch(() => null)
      : pickDefaultNotReactedWipeReact(await listActiveWipeReacts(interaction.guildId).catch(() => []));
    if (!record) {
      await replyEphemeral(interaction, "That wipe react could not be found.");
      return true;
    }

    const wipeReactMessage = await fetchWipeReactMessage(guild, record.channelId, record.messageId);
    if (!wipeReactMessage) {
      await getBackend().deleteWipeReact(getWipeReactWorkerID(), interaction.guildId, record.messageId).catch(() => undefined);
      await replyEphemeral(interaction, "That wipe react message no longer exists. RustLens removed its persisted state.");
      return true;
    }

    let members;
    try {
      members = await getGuildMembers(guild);
    } catch (error) {
      logDiscordError("fetch guild members for wipe react missing", error, {
        guildId: interaction.guildId,
      });
      await replyEphemeral(
        interaction,
        "RustLens could not load the members for that role. Make sure the bot has the Guild Members intent enabled.",
      );
      return true;
    }

    const reactedUserIds = new Set([
      ...record.acceptedUserIds,
      ...record.declinedUserIds,
      ...record.lateUserIds,
    ]);
    const missingMembers = [...members.values()]
      .filter((member) => !member.user.bot)
      .filter((member) => scope ? member.roles.cache.has(scope.id) : true)
      .filter((member) => !reactedUserIds.has(member.id))
      .sort((left, right) => left.displayName.localeCompare(right.displayName))
    const missingUserIds = missingMembers.map((member) => member.id);

    let messageStatus: { sent: number; failed: number } | undefined;
    if (messageText.length > 0 && missingMembers.length > 0) {
      let sent = 0;
      let failed = 0;
      const recipients = missingMembers.filter((member) => member.id !== interaction.user.id);
      const dmEmbed = buildNotReactedDmEmbed(messageText, wipeReactMessage.url);
      // Cap fanout to avoid Discord per-route DM rate limits on large guilds.
      const DM_CONCURRENCY = 5;
      let cursor = 0;
      const sendOne = async () => {
        while (true) {
          const idx = cursor;
          cursor += 1;
          if (idx >= recipients.length) {
            return;
          }
          try {
            await recipients[idx].send({ embeds: [dmEmbed], components: [] });
            sent += 1;
          } catch (error) {
            logDiscordError("send wipe react missing DM", error, {
              guildId: interaction.guildId,
              discordId: recipients[idx].id,
              messageId: wipeReactMessage.id,
            });
            failed += 1;
          }
        }
      };
      await Promise.all(
        Array.from({ length: Math.min(DM_CONCURRENCY, recipients.length) }, () => sendOne()),
      );

      messageStatus = { sent, failed };
    }

    await interaction.editReply({
      embeds: [buildMissingReactantsEmbed(toWipeReactState(record), missingUserIds, messageStatus)],
      components: [],
    });
    return true;
  }

  if (subcommandGroup === WIPEREACT_REACT_GROUP_NAME) {
    const messageId = interaction.options.getString(WIPEREACT_TARGET_OPTION_NAME, true);
    const record = await getPersistedWipeReact(interaction.guildId, messageId).catch(() => null);
    if (!record) {
      await replyEphemeral(interaction, "That wipe react could not be found.");
      return true;
    }

    const message = await fetchWipeReactMessage(guild, record.channelId, record.messageId);
    if (!message) {
      await getBackend().deleteWipeReact(getWipeReactWorkerID(), interaction.guildId, record.messageId).catch(() => undefined);
      await replyEphemeral(interaction, "That wipe react message no longer exists. RustLens removed its persisted state.");
      return true;
    }

    const logsChannel = await resolveLogsChannel(interaction);
    if (!logsChannel) {
      await replyEphemeral(
        interaction,
        "RustLens needs Wipe React Logs configured before wipe reacts can be managed. Run /settings and set Wipe React Logs.",
      );
      return true;
    }

    if (subcommand === WIPEREACT_REACT_ADD_SUBCOMMAND_NAME) {
      const targetUser = interaction.options.getUser(WIPEREACT_USER_OPTION_NAME, true);
      const rawAction = interaction.options.getString(WIPEREACT_TYPE_OPTION_NAME, true);
      if (rawAction !== "accepted" && rawAction !== "declined" && rawAction !== "late") {
        await replyEphemeral(interaction, "Invalid reaction type.");
        return true;
      }
      const action: WipeReactAction = rawAction;
      const update = await getBackend().setWipeReactReaction(
        getWipeReactWorkerID(),
        interaction.guildId,
        record.messageId,
        targetUser.id,
        action,
      );
      if (!update.changed) {
        await replyEphemeral(
          interaction,
          `${actionEmoji(action)} <@${targetUser.id}> is already marked as ${actionLabel(action).toLowerCase()}.`,
        );
        return true;
      }

      await logsChannel.send({
        embeds: [
          buildReactionLogEmbed(
            toWipeReactState(update.wipeReact),
            action,
            targetUser,
            `Set by <@${interaction.user.id}> via /wipereact react add.`,
          ),
        ],
      });

      const synced = await syncPersistedWipeReactMessage(guild, update.wipeReact);
      if (!synced) {
        await replyEphemeral(interaction, "That wipe react message no longer exists. RustLens removed its persisted state.");
        return true;
      }

      await replyEphemeral(
        interaction,
        `${actionEmoji(action)} <@${targetUser.id}> is now marked as ${actionLabel(action).toLowerCase()}.`,
      );
      return true;
    }

    if (subcommand === WIPEREACT_REACT_REMOVE_SUBCOMMAND_NAME) {
      const targetUser = interaction.options.getUser(WIPEREACT_USER_OPTION_NAME, true);
      const removal = await getBackend().removeWipeReactReaction(
        getWipeReactWorkerID(),
        interaction.guildId,
        record.messageId,
        targetUser.id,
      );
      if (!removal.changed || !removal.removedType) {
        await replyEphemeral(interaction, `<@${targetUser.id}> does not have a reaction on that wipe react.`);
        return true;
      }

      await logsChannel.send({
        embeds: [
          buildReactionRemovalLogEmbed(
            toWipeReactState(removal.wipeReact),
            removal.removedType,
            targetUser,
            `Removed by <@${interaction.user.id}> via /wipereact react remove.`,
          ),
        ],
      });

      const synced = await syncPersistedWipeReactMessage(guild, removal.wipeReact);
      if (!synced) {
        await replyEphemeral(interaction, "That wipe react message no longer exists. RustLens removed its persisted state.");
        return true;
      }

      await replyEphemeral(
        interaction,
        `${actionEmoji(removal.removedType)} Removed <@${targetUser.id}> from ${actionLabel(removal.removedType).toLowerCase()}.`,
      );
      return true;
    }
  }

  await replyEphemeral(
    interaction,
    `Unknown /${WIPEREACT_COMMAND_NAME} command: ${subcommandGroup ? `${subcommandGroup} ${subcommand}` : subcommand}`,
  );
  return true;
}

export async function handleWipeReactAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== WIPEREACT_COMMAND_NAME || !interaction.inGuild()) {
    return false;
  }

  const focused = interaction.options.getFocused(true);

  if (focused.name === WIPEREACT_SCHEDULE_TARGET_OPTION_NAME) {
    const schedules = await getBackend().listWipeReactSchedules(getWipeReactWorkerID(), interaction.guildId).catch(() => null);
    if (!schedules) {
      await interaction.respond([]);
      return true;
    }

    const query = focused.value.toLowerCase();
    const choices = schedules.items
      .filter((schedule) => {
        const label = formatScheduleChoiceName(schedule);
        return label.toLowerCase().includes(query);
      })
      .slice(0, 25)
      .map((schedule) => ({
        name: formatScheduleChoiceName(schedule),
        value: String(schedule.id),
      }));

    await interaction.respond(choices);
    return true;
  }

  if (focused.name !== WIPEREACT_TARGET_OPTION_NAME) {
    return false;
  }

  const query = focused.value.toLowerCase();
  const records = await listActiveWipeReacts(interaction.guildId).catch(() => null);
  if (!records) {
    await interaction.respond([]);
    return true;
  }

  const choices = records
    .filter((record) => record.name.toLowerCase().includes(query))
    .sort((left, right) => right.scheduledUnix - left.scheduledUnix)
    .slice(0, 25)
    .map((record) => ({
      name: formatWipeReactChoiceName(toWipeReactState(record)),
      value: record.messageId,
    }));

  await interaction.respond(choices);
  return true;
}

export async function handleWipeReactButtonInteraction(interaction: ButtonInteraction) {
  const action = parseActionButton(interaction.customId);
  if (action) {
    if (!interaction.guildId) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    if (!action.requiresReason) {
      await deferEphemeral(interaction);
    }

    const record = await getPersistedWipeReact(interaction.guildId, interaction.message.id).catch(() => null);
    if (!record) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    scheduleWipeReactExpiry(interaction.message, record.scheduledUnix);

    if (record.scheduledUnix <= Math.floor(Date.now() / 1000)) {
      await deferEphemeral(interaction);
      await disableExpiredWipeReact(interaction.message, toWipeReactState(record)).catch(logDiscordCatch("disable expired wipe react button", undefined, {
        guildId: interaction.guildId,
        channelId: interaction.message.channelId,
        messageId: interaction.message.id,
      }));
      await replyEphemeral(interaction, "This wipe react has already closed.");
      return true;
    }

    if (getUserReactionType(toWipeReactState(record), interaction.user.id) === action.type) {
      await deferEphemeral(interaction);
      return processWipeReactAction(interaction, interaction.message, action.type);
    }

    if (action.requiresReason) {
      await interaction.showModal(
        buildReasonModal(action.type, interaction.channelId, interaction.message.id).toJSON() as any,
      );
      return true;
    }

    return processWipeReactAction(interaction, interaction.message, action.type);
  }

  if (interaction.customId.startsWith(WIPEREACT_SCHEDULE_CONTINUE_BUTTON_PREFIX)) {
    const draftId = interaction.customId.slice(WIPEREACT_SCHEDULE_CONTINUE_BUTTON_PREFIX.length);
    pruneExpiredScheduleDrafts();
    const draft = scheduleDrafts.get(draftId);
    if (!draft || draft.userId !== interaction.user.id || draft.guildId !== interaction.guildId) {
      await replyEphemeral(interaction, "That schedule draft expired. Start again with /wipereact schedule create.");
      return true;
    }

    await interaction.showModal(
      buildScheduleStepTwoModal(draftId, {
        name: draft.config.name,
        description: draft.config.description,
        askLateReason: draft.config.askLateReason,
        askDeclinedReason: draft.config.askDeclinedReason,
      }).toJSON() as any,
    );
    return true;
  }

  if (interaction.customId.startsWith(WIPEREACT_SCHEDULE_CONFIRM_BUTTON_PREFIX)) {
    const draftId = interaction.customId.slice(WIPEREACT_SCHEDULE_CONFIRM_BUTTON_PREFIX.length);
    pruneExpiredScheduleDrafts();
    const draft = scheduleDrafts.get(draftId);
    if (!draft || draft.userId !== interaction.user.id || draft.guildId !== interaction.guildId) {
      await replyEphemeral(interaction, "That schedule draft expired. Start again with /wipereact schedule create.");
      return true;
    }

    scheduleDrafts.delete(draftId);
    await interaction.update({
      components: createScheduleConfirmButtons(draftId, true),
    });

    try {
      if (draft.editScheduleId !== null) {
        await getBackend().updateWipeReactSchedule(
          getWipeReactWorkerID(),
          draft.guildId,
          draft.editScheduleId,
          buildSchedulePayload(draft.config, interaction.user.id, interaction.channelId),
        );
        await interaction.followUp({
          content: `Schedule updated\n-# ID: ${draft.editScheduleId}`,
          flags: MessageFlags.Ephemeral,
        });
      } else {
        const schedule = await getBackend().createWipeReactSchedule(
          getWipeReactWorkerID(),
          draft.guildId,
          buildSchedulePayload(draft.config, interaction.user.id, interaction.channelId),
        );
        await interaction.followUp({
          content: `Schedule created\n-# ID: ${schedule.id}`,
          flags: MessageFlags.Ephemeral,
        });
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("409")) {
        await interaction.followUp({
          content: "You can only have up to 3 schedules per server.",
          flags: MessageFlags.Ephemeral,
        });
        return true;
      }
      await interaction.followUp({
        content: "Failed to save the schedule. Please try again.",
        flags: MessageFlags.Ephemeral,
      });
    }
    return true;
  }

  if (interaction.customId.startsWith(WIPEREACT_SCHEDULE_DENY_BUTTON_PREFIX)) {
    const draftId = interaction.customId.slice(WIPEREACT_SCHEDULE_DENY_BUTTON_PREFIX.length);
    scheduleDrafts.delete(draftId);
    await interaction.update({
      components: createScheduleConfirmButtons(draftId, true),
    });
    await interaction.followUp({
      content: "Schedule cancelled.",
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (
    interaction.customId !== WIPEREACT_EDIT_BUTTON_CUSTOM_ID &&
    !interaction.customId.startsWith(WIPEREACT_EDIT_BUTTON_PREFIX)
  ) {
    return false;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeral(interaction, "Only the configured admin role or administrators can edit a wipe react.");
    return true;
  }

  if (!interaction.guildId) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  const record = await getPersistedWipeReact(interaction.guildId, interaction.message.id).catch(() => null);
  if (!record) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  if (record.scheduledUnix <= Math.floor(Date.now() / 1000)) {
    await deferEphemeral(interaction);
    await disableExpiredWipeReact(interaction.message, toWipeReactState(record)).catch(logDiscordCatch("disable expired wipe react edit button", undefined, {
      guildId: interaction.guildId,
      channelId: interaction.message.channelId,
      messageId: interaction.message.id,
    }));
    await replyEphemeral(interaction, "This wipe react has already closed.");
    return true;
  }

  await interaction.showModal(
    buildWipeReactModal(
      `${WIPEREACT_EDIT_MODAL_PREFIX}${interaction.channelId}:${interaction.message.id}`,
      toWipeReactState(record),
    ).toJSON() as any,
  );
  return true;
}

export async function handleWipeReactModalSubmit(interaction: ModalSubmitInteraction) {
  if (interaction.customId === WIPEREACT_SCHEDULE_MODAL_CUSTOM_ID) {
    if (!interaction.inGuild()) {
      await replyEphemeral(interaction, "Scheduled wipe react can only be used inside a server.");
      return true;
    }

    const result = validateScheduleModalSubmission(interaction);
    if (result.error || !result.config) {
      await replyEphemeral(interaction, result.error ?? "Invalid schedule configuration.");
      return true;
    }

    pruneExpiredScheduleDrafts();
    const draftId = randomUUID();
    scheduleDrafts.set(draftId, {
      userId: interaction.user.id,
      guildId: interaction.guildId,
      config: result.config,
      editScheduleId: null,
      createdAt: Date.now(),
    });

    await replyEphemeral(
      interaction,
      "Step 1 saved. Click continue to set the title, description, and options.",
      [
        new ActionRowBuilder<ButtonBuilder>().addComponents(
          new ButtonBuilder()
            .setCustomId(`${WIPEREACT_SCHEDULE_CONTINUE_BUTTON_PREFIX}${draftId}`)
            .setLabel("Continue")
            .setEmoji("⚙️")
            .setStyle(ButtonStyle.Success),
        ),
      ],
    );
    return true;
  }

  if (interaction.customId.startsWith(WIPEREACT_SCHEDULE_EDIT_MODAL_PREFIX)) {
    if (!interaction.inGuild()) {
      await replyEphemeral(interaction, "Scheduled wipe react can only be used inside a server.");
      return true;
    }

    const scheduleId = Number(interaction.customId.slice(WIPEREACT_SCHEDULE_EDIT_MODAL_PREFIX.length));
    if (!Number.isInteger(scheduleId) || scheduleId <= 0) {
      await replyEphemeral(interaction, "That schedule could not be found.");
      return true;
    }

    const result = validateScheduleModalSubmission(interaction);
    if (result.error || !result.config) {
      await replyEphemeral(interaction, result.error ?? "Invalid schedule configuration.");
      return true;
    }

    let existing: WipeReactSchedule | null = null;
    try {
      existing = await getBackend().getWipeReactSchedule(getWipeReactWorkerID(), interaction.guildId, scheduleId);
    } catch {
      // Will use empty defaults
    }

    pruneExpiredScheduleDrafts();
    const draftId = randomUUID();
    scheduleDrafts.set(draftId, {
      userId: interaction.user.id,
      guildId: interaction.guildId,
      config: {
        ...result.config,
        name: existing?.name ?? "",
        description: existing?.description ?? "",
        askLateReason: existing?.askLateReason ?? false,
        askDeclinedReason: existing?.askDeclinedReason ?? false,
      },
      editScheduleId: scheduleId,
      createdAt: Date.now(),
    });

    await replyEphemeral(
      interaction,
      "Step 1 saved. Click continue to update the title, description, and options.",
      [
        new ActionRowBuilder<ButtonBuilder>().addComponents(
          new ButtonBuilder()
            .setCustomId(`${WIPEREACT_SCHEDULE_CONTINUE_BUTTON_PREFIX}${draftId}`)
            .setLabel("Continue")
            .setEmoji("⚙️")
            .setStyle(ButtonStyle.Success),
        ),
      ],
    );
    return true;
  }

  if (interaction.customId.startsWith(WIPEREACT_SCHEDULE_STEP_TWO_MODAL_PREFIX)) {
    const draftId = interaction.customId.slice(WIPEREACT_SCHEDULE_STEP_TWO_MODAL_PREFIX.length);
    pruneExpiredScheduleDrafts();
    const draft = scheduleDrafts.get(draftId);
    if (!draft || draft.userId !== interaction.user.id || draft.guildId !== interaction.guildId) {
      await replyEphemeral(interaction, "That schedule draft expired. Start again with /wipereact schedule create.");
      return true;
    }

    const name = getFieldValues(interaction, WIPEREACT_SCHEDULE_NAME_CUSTOM_ID)[0]?.trim() ?? "";
    if (name.length === 0) {
      await replyEphemeral(interaction, "Title is required.");
      return true;
    }

    const description = getFieldValues(interaction, WIPEREACT_SCHEDULE_DESCRIPTION_CUSTOM_ID)[0]?.trim() ?? "";
    const options = new Set(getFieldValues(interaction, WIPEREACT_SCHEDULE_OPTIONS_CUSTOM_ID));

    draft.config.name = name;
    draft.config.description = description;
    draft.config.askLateReason = options.has(WIPEREACT_SCHEDULE_OPTION_ASK_LATE_REASON);
    draft.config.askDeclinedReason = options.has(WIPEREACT_SCHEDULE_OPTION_ASK_DECLINED_REASON);

    await interaction.reply({
      embeds: [buildScheduleConfirmEmbed(draft.config)],
      components: createScheduleConfirmButtons(draftId),
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (interaction.customId === WIPEREACT_CREATE_MODAL_CUSTOM_ID) {
    if (!interaction.inGuild()) {
      await replyEphemeral(interaction, "Wipe react can only be created inside a server.");
      return true;
    }

    const stateResult = buildWipeReactStateFromModal(
      interaction,
      new Date().toISOString(),
      interaction.user.id,
    );
    if (stateResult.error || !stateResult.state) {
      await replyEphemeral(interaction, stateResult.error ?? "Wipe react is invalid.");
      return true;
    }

    const logsChannel = await resolveLogsChannel(interaction);
    if (!logsChannel) {
      await replyEphemeral(
        interaction,
        "RustLens could not find Wipe React Logs. Run /settings and set Wipe React Logs before creating a wipe react.",
      );
      return true;
    }

    await interaction.reply({
      embeds: [buildWipeReactEmbed(stateResult.state)],
      components: createWipeReactButtons(stateResult.state, false),
    });
    const createdMessage = await interaction.fetchReply();
    try {
      await getBackend().createWipeReact(
        getWipeReactWorkerID(),
        interaction.guildId,
        buildPersistedWipeReact(
          interaction.guildId,
          createdMessage.channelId,
          createdMessage.id,
          stateResult.state,
        ),
      );
    } catch {
      await createdMessage.delete().catch(logDiscordCatch("delete unpersisted wipe react message", undefined, {
        guildId: interaction.guildId,
        channelId: createdMessage.channelId,
        messageId: createdMessage.id,
      }));
      await interaction.followUp({
        content: "RustLens could not persist that wipe react.",
        flags: MessageFlags.Ephemeral,
      });
      return true;
    }
    scheduleWipeReactExpiry(createdMessage, stateResult.state.scheduledUnix);
    return true;
  }

  const editTarget = extractTargetFromCustomId(
    interaction.customId,
    WIPEREACT_EDIT_MODAL_PREFIX,
  );
  if (editTarget) {
    const [channelId, messageId] = editTarget;
    if (!interaction.guildId || !interaction.guild) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    const message = await fetchWipeReactMessage(interaction.guild, channelId, messageId);
    if (!message) {
      await getBackend().deleteWipeReact(getWipeReactWorkerID(), interaction.guildId, messageId).catch(() => undefined);
      await replyEphemeral(interaction, "The wipe react message could not be found.");
      return true;
    }

    const existing = await getPersistedWipeReact(interaction.guildId, messageId).catch(() => null);
    if (!existing) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    if (existing.scheduledUnix <= Math.floor(Date.now() / 1000)) {
      await disableExpiredWipeReact(message, toWipeReactState(existing)).catch(logDiscordCatch("disable expired wipe react during edit", undefined, {
        guildId: interaction.guildId,
        channelId: message.channelId,
        messageId: message.id,
      }));
      await replyEphemeral(interaction, "This wipe react has already closed.");
      return true;
    }

    const updated = buildWipeReactStateFromModal(
      interaction,
      existing.embedTimestamp,
      existing.creatorId,
    );
    if (updated.error || !updated.state) {
      await replyEphemeral(interaction, updated.error ?? "Wipe react is invalid.");
      return true;
    }

    const updatedRecord = await getBackend().updateWipeReact(
      getWipeReactWorkerID(),
      interaction.guildId,
      messageId,
      buildPersistedWipeReact(
        interaction.guildId,
        existing.channelId,
        existing.messageId,
        {
          ...updated.state,
          acceptedUserIds: existing.acceptedUserIds,
          declinedUserIds: existing.declinedUserIds,
          lateUserIds: existing.lateUserIds,
        },
      ),
    );

    const synced = await syncPersistedWipeReactMessage(interaction.guild, updatedRecord);
    if (!synced) {
      await replyEphemeral(interaction, "That wipe react message no longer exists. RustLens removed its persisted state.");
      return true;
    }
    await replyEphemeral(interaction, "Wipe react updated.");
    return true;
  }

  const reasonTarget = extractTargetFromCustomId(
    interaction.customId,
    WIPEREACT_REASON_MODAL_PREFIX,
  );
  if (!reasonTarget) {
    return false;
  }

  const [action, channelId, messageId] = reasonTarget;
  if (action !== "declined" && action !== "late") {
    await replyEphemeral(interaction, "That wipe react reason action is invalid.");
    return true;
  }

  if (!interaction.guild) {
    await replyEphemeral(interaction, "The wipe react message could not be found.");
    return true;
  }

  const message = await fetchWipeReactMessage(interaction.guild, channelId, messageId);
  if (!message) {
    await replyEphemeral(interaction, "The wipe react message could not be found.");
    return true;
  }

  await deferEphemeral(interaction);
  const reason = getFieldValues(interaction, WIPEREACT_REASON_TEXT_CUSTOM_ID)[0]?.trim() ?? "";
  if (reason.length === 0) {
    await replyEphemeral(interaction, "Reason is required.");
    return true;
  }

  return processWipeReactAction(interaction, message, action, reason);
}

export function startWipeReactScheduler(client: Client): NodeJS.Timeout {
  const SCHEDULER_INTERVAL_MS = 60_000;
  const workerID = getWipeReactWorkerID();
  let running = false;

  async function tick() {
    if (running) {
      return;
    }
    running = true;
    try {
      const guilds = [...client.guilds.cache.values()];

      for (const guild of guilds) {
        try {
          while (true) {
            const response = await getBackend().claimNextWipeReactSchedulePost(workerID, guild.id);
            if (!response.item) {
              break;
            }

            await processSchedulePost(guild, response.item).catch((error) => {
              console.error("[bot] schedule tick failed", {
                guildId: guild.id,
                postId: response.item?.id,
                scheduleId: response.item?.scheduleId,
                error,
              });
            });
          }
        } catch (error) {
          console.error("[bot] failed to poll scheduled wipe reacts for guild", {
            guildId: guild.id,
            error,
          });
        }
      }
    } finally {
      running = false;
    }
  }

  async function processSchedulePost(guild: Guild, post: import("./backend-client.js").WipeReactSchedulePost) {
    const channel = guild.channels.cache.get(post.channelId) ??
      await guild.channels.fetch(post.channelId).catch(logDiscordCatch("fetch scheduled wipe react channel", null, {
        guildId: guild.id,
        channelId: post.channelId,
        postId: post.id,
      }));
    if (!channel?.isTextBased() || !("send" in channel)) {
      await getBackend().releaseWipeReactSchedulePost(workerID, guild.id, post.id).catch(() => undefined);
      return;
    }

    const state: WipeReactMessageState = {
      name: post.name,
      description: post.description,
      timeZone: post.timeZone,
      scheduledUnix: post.wipeUnix,
      creatorId: post.createdBy,
      askLateReason: post.askLateReason,
      askDeclinedReason: post.askDeclinedReason,
      acceptedUserIds: [],
      declinedUserIds: [],
      lateUserIds: [],
      embedTimestamp: new Date().toISOString(),
    };

    const message = await channel.send({
      embeds: [buildWipeReactEmbed(state)],
      components: createWipeReactButtons(state, false),
    });

    try {
      await getBackend().createWipeReact(
        workerID,
        guild.id,
        buildPersistedWipeReact(guild.id, message.channelId, message.id, state),
      );
    } catch (error) {
      console.error("[bot] failed to persist scheduled wipe react", {
        guildId: guild.id,
        scheduleId: post.scheduleId,
        postId: post.id,
        error,
      });
      await message.delete().catch(logDiscordCatch("delete failed scheduled wipe react message", undefined, {
        guildId: guild.id,
        channelId: message.channelId,
        messageId: message.id,
        postId: post.id,
      }));
      await getBackend().releaseWipeReactSchedulePost(workerID, guild.id, post.id).catch(() => undefined);
      return;
    }

    scheduleWipeReactExpiry(message, post.wipeUnix);

    await getBackend().completeWipeReactSchedulePost(
      workerID,
      guild.id,
      post.id,
      message.id,
      message.channelId,
    ).catch((error) => {
      console.error("[bot] failed to complete scheduled wipe react post", {
        guildId: guild.id,
        scheduleId: post.scheduleId,
        postId: post.id,
        error,
      });
    });

    console.log("[bot] auto-posted scheduled wipe react", {
      guildId: guild.id,
      scheduleId: post.scheduleId,
      postId: post.id,
      wipeUnix: post.wipeUnix,
      messageId: message.id,
    });
  }

  const timer = setInterval(() => {
    tick().catch((error) => {
      console.error("[bot] scheduler tick error", error);
    });
  }, SCHEDULER_INTERVAL_MS);

  setTimeout(() => {
    tick().catch((error) => {
      console.error("[bot] initial scheduler tick error", error);
    });
  }, 5000);

  console.log("[bot] wipe react scheduler started");
  return timer;
}

export const __testables = {
  applyUserReaction,
  buildScheduleConfirmEmbed,
  buildSchedulePayload,
  buildScheduleStepTwoModal,
  buildScheduleWipeReactModal,
  buildWipeReactEmbed,
  buildMissingReactantsEmbed,
  buildWipeReactModal,
  createScheduleConfirmButtons,
  createWipeReactButtons,
  formatScheduleChoiceName,
  formatScheduleSummary,
  formatTimestampForInput,
  getUserReactionType,
  parseActionButton,
  parseFlexibleDateTime,
  parseScheduleTime,
  parseWipeReactMessage,
  scheduleDrafts,
  wipeReactCommand,
};
