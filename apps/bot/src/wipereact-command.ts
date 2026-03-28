import type {
  AutocompleteInteraction,
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  GuildBasedChannel,
  ModalSubmitInteraction,
  Message,
} from "discord.js";
import {
  ActionRowBuilder,
  ButtonBuilder,
  ButtonStyle,
  ChannelType,
  MessageFlags,
  PermissionFlagsBits,
  SlashCommandBuilder,
  TextChannel,
} from "discord.js";
import { BackendClient } from "./backend-client.js";
import {
  CheckboxGroupBuilder,
  ModalV2Builder,
  TextInputBuilder,
  TextInputStyle,
} from "./builders/index.js";
import { loadConfig } from "./config.js";

export const WIPEREACT_COMMAND_NAME = "wipereact";

const WIPEREACT_CREATE_SUBCOMMAND_NAME = "create";
const WIPEREACT_DELETE_SUBCOMMAND_NAME = "delete";
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
const WIPEREACT_DELETE_OPTION_NAME = "wipereact";
const WIPEREACT_OPTION_ASK_LATE_REASON = "ask_late_reason";
const WIPEREACT_OPTION_ASK_DECLINED_REASON = "ask_declined_reason";
const RUSTLENS_EMBED_COLOR = 14582045;
const RUSTLENS_EMBED_ICON_URL =
  "https://i.ibb.co/n8MWVLDN/1ee153f3c14d7decaeb6cb62c28fb5b4.webp";
const WIPE_REACT_ACCEPTED_FIELD_PREFIX = "✅ Accepted";
const WIPE_REACT_DECLINED_FIELD_PREFIX = "❌ Declined";
const WIPE_REACT_LATE_FIELD_PREFIX = "🕑 Late";
const WIPE_REACT_TIMEZONE_FIELD_NAME = "Timezone";
const WIPE_REACT_EMPTY_FIELD_VALUE = "-";
const LOG_ACCEPTED_COLOR = 0x67c587;
const LOG_DECLINED_COLOR = 0xd96b6b;
const LOG_LATE_COLOR = 0xe3a24f;
const DEFAULT_WIPEREACT_TIMEZONE = "Europe/London";

type ModalComponentNode = {
  customId?: string;
  value?: unknown;
  values?: unknown;
  component?: ModalComponentNode;
  components?: ModalComponentNode[];
};

type WipeReactAction = "accepted" | "declined" | "late";

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
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(WIPEREACT_CREATE_SUBCOMMAND_NAME)
      .setDescription("Create a wipe react message"),
  )
  .addSubcommand((subcommand) =>
    subcommand
      .setName(WIPEREACT_DELETE_SUBCOMMAND_NAME)
      .setDescription("Delete one of your wipe reacts")
      .addStringOption((option) =>
        option
          .setName(WIPEREACT_DELETE_OPTION_NAME)
          .setDescription("Select one of your wipe reacts")
          .setRequired(true)
          .setAutocomplete(true),
      ),
  );

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

function extractComponentValues(
  components: readonly ModalComponentNode[] | undefined,
  customId: string,
): string[] {
  if (!components) {
    return [];
  }

  for (const component of components) {
    if (component.customId === customId) {
      const values = extractFieldValues(component);
      if (values.length > 0) {
        return values;
      }
    }

    const nestedComponentValues = extractComponentValues(
      component.component ? [component.component] : undefined,
      customId,
    );
    if (nestedComponentValues.length > 0) {
      return nestedComponentValues;
    }

    const nestedValues = extractComponentValues(component.components, customId);
    if (nestedValues.length > 0) {
      return nestedValues;
    }
  }

  return [];
}

function getFieldValues(interaction: ModalSubmitInteraction, customId: string) {
  try {
    const accessorField = interaction.fields.getField(customId) as
      | { value?: unknown; values?: unknown }
      | null
      | undefined;
    const accessorValues = extractFieldValues(accessorField);
    if (accessorValues.length > 0) {
      return accessorValues;
    }
  } catch {
    // Fall through to the raw field map and component tree.
  }

  const mapField = interaction.fields.fields.get(customId) as
    | { value?: unknown; values?: unknown }
    | null
    | undefined;
  const mapValues = extractFieldValues(mapField);
  if (mapValues.length > 0) {
    return mapValues;
  }

  return extractComponentValues(
    interaction.components as unknown as readonly ModalComponentNode[],
    customId,
  );
}

function hasAdministratorPermission(
  interaction: ChatInputCommandInteraction | ButtonInteraction | ModalSubmitInteraction,
) {
  return interaction.memberPermissions?.has(PermissionFlagsBits.Administrator) ?? false;
}

async function replyEphemeral(
  interaction: ChatInputCommandInteraction | ButtonInteraction | ModalSubmitInteraction,
  content: string,
) {
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply({ content, components: [] });
    return;
  }

  await interaction.reply({
    content,
    flags: MessageFlags.Ephemeral,
  });
}

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

function formatUserMentions(userIds: readonly string[]) {
  if (userIds.length === 0) {
    return WIPE_REACT_EMPTY_FIELD_VALUE;
  }

  return userIds.map((userId) => `<@${userId}>`).join("\n");
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
    description: state.description,
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
        .setRequired(true)
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
  if (description.length === 0) {
    return {
      state: null,
      error: "Description is required.",
    } as const;
  }

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

function applyUserReaction(
  state: WipeReactMessageState,
  userId: string,
  action: WipeReactAction,
) {
  const acceptedUserIds = state.acceptedUserIds.filter((entry) => entry !== userId);
  const declinedUserIds = state.declinedUserIds.filter((entry) => entry !== userId);
  const lateUserIds = state.lateUserIds.filter((entry) => entry !== userId);

  const previousAction =
    state.acceptedUserIds.includes(userId)
      ? "accepted"
      : state.declinedUserIds.includes(userId)
        ? "declined"
        : state.lateUserIds.includes(userId)
          ? "late"
          : null;

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

async function resolveConfiguredLogsChannel(guildId: string, guild: {
  channels: {
    cache: Map<string, GuildBasedChannel>;
    fetch?: (id: string) => Promise<GuildBasedChannel | null>;
  };
}) {
  const settings = await getBackend().guildSettings(getWipeReactWorkerID(), guildId);
  if (!settings.logChannelId) {
    return null;
  }

  const configured =
    guild.channels.cache.get(settings.logChannelId) ??
    (guild.channels.fetch
      ? await guild.channels.fetch(settings.logChannelId).catch(() => null)
      : null);
  return configured?.type === ChannelType.GuildText ? (configured as TextChannel) : null;
}

async function resolveLogsChannel(interaction: {
  guildId: string | null;
  guild: { channels: { cache: Map<string, GuildBasedChannel> } } | null;
}) {
  if (!interaction.guildId || !interaction.guild) {
    return null;
  }

  const configured = await resolveConfiguredLogsChannel(
    interaction.guildId,
    interaction.guild,
  );
  if (configured) {
    return configured;
  }

  const namedLogsChannel = [...interaction.guild.channels.cache.values()].find(
    (channel) => channel.type === ChannelType.GuildText && channel.name === "logs",
  );
  return namedLogsChannel ? (namedLogsChannel as TextChannel) : null;
}

async function fetchWipeReactMessage(
  interaction: ModalSubmitInteraction,
  channelId: string,
  messageId: string,
) {
  const guild = interaction.guild;
  if (!guild) {
    return null;
  }

  const channel = guild.channels.cache.get(channelId) ?? (await guild.channels.fetch(channelId));
  if (!channel?.isTextBased() || !("messages" in channel)) {
    return null;
  }

  return channel.messages.fetch(messageId).catch(() => null);
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

  const latestMessage = await message.channel.messages.fetch(message.id).catch(() => null);
  if (!latestMessage) {
    return;
  }

  const state = parseWipeReactMessage(latestMessage);
  if (!state) {
    return;
  }

  const nowUnix = Math.floor(Date.now() / 1000);
  if (state.scheduledUnix > nowUnix) {
    scheduleWipeReactExpiry(latestMessage, state.scheduledUnix);
    return;
  }

  await disableExpiredWipeReact(latestMessage, state).catch(() => undefined);
}

function scheduleWipeReactExpiry(message: Message, scheduledUnix: number) {
  const existingTimer = wipeReactExpiryTimers.get(message.id);
  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  const delayMs = scheduledUnix * 1000 - Date.now();
  const timeoutMs = Math.min(Math.max(delayMs, 0), MAX_TIMEOUT_MS);
  const timer = setTimeout(() => {
    wipeReactExpiryTimers.delete(message.id);
    void disableScheduledWipeReact(message);
  }, timeoutMs);

  wipeReactExpiryTimers.set(message.id, timer);
}

async function processWipeReactAction(
  interaction: ButtonInteraction | ModalSubmitInteraction,
  message: Message,
  action: WipeReactAction,
  reason?: string,
) {
  const state = parseWipeReactMessage(message);
  if (!state) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  if (state.scheduledUnix <= Math.floor(Date.now() / 1000)) {
    await disableExpiredWipeReact(message, state).catch(() => undefined);
    await replyEphemeral(interaction, "This wipe react has already closed.");
    return true;
  }

  const logsChannel = await resolveLogsChannel(interaction);
  if (!logsChannel) {
    await replyEphemeral(
      interaction,
      "RustLens needs a logs channel before wipe reacts can be used. Run /setup logchannel and try again.",
    );
    return true;
  }

  const updated = applyUserReaction(state, interaction.user.id, action);
  if (!updated.changed) {
    await replyEphemeral(
      interaction,
      `${actionEmoji(action)} You are already marked as ${actionLabel(action).toLowerCase()}.`,
    );
    return true;
  }

  await logsChannel.send({
    embeds: [buildReactionLogEmbed(updated.state, action, interaction.user, reason?.trim())],
  });

  const updatedMessage = await message.edit({
    embeds: [buildWipeReactEmbed(updated.state)],
    components: createWipeReactButtons(updated.state, false),
  });
  scheduleWipeReactExpiry(updatedMessage, updated.state.scheduledUnix);

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

export async function handleWipeReactAutocomplete(interaction: AutocompleteInteraction) {
  if (interaction.commandName !== WIPEREACT_COMMAND_NAME || !interaction.inGuild()) {
    return false;
  }

  const subcommand = interaction.options.getSubcommand(false);
  const focused = interaction.options.getFocused(true);
  if (
    subcommand !== WIPEREACT_DELETE_SUBCOMMAND_NAME ||
    focused.name !== WIPEREACT_DELETE_OPTION_NAME
  ) {
    return false;
  }

  const channel = interaction.channel;
  if (!channel?.isTextBased() || !("messages" in channel)) {
    await interaction.respond([]);
    return true;
  }

  const query = focused.value.toLowerCase();
  const messages = await channel.messages.fetch({ limit: 100 }).catch(() => null);
  if (!messages) {
    await interaction.respond([]);
    return true;
  }

  const choices = [...messages.values()]
    .flatMap((message) => {
      const state = parseWipeReactMessage(message);
      if (!state || state.creatorId !== interaction.user.id) {
        return [];
      }

      return [{ message, state }];
    })
    .filter(({ state }) => state.name.toLowerCase().includes(query))
    .sort((left, right) => right.state.scheduledUnix - left.state.scheduledUnix)
    .slice(0, 25)
    .map(({ message, state }) => ({
      name: formatWipeReactChoiceName(state),
      value: message.id,
    }));

  await interaction.respond(choices);
  return true;
}

export async function handleWipeReactChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== WIPEREACT_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "Wipe react can only be used inside a server.");
    return true;
  }

  const subcommand = interaction.options.getSubcommand(false);
  if (subcommand === WIPEREACT_CREATE_SUBCOMMAND_NAME) {
    const logsChannel = await resolveLogsChannel(interaction);
    if (!logsChannel) {
      await replyEphemeral(
        interaction,
        "RustLens could not find a logs channel. Run /setup logchannel {channel} before creating a wipe react.",
      );
      return true;
    }

    await interaction.showModal(buildWipeReactModal(WIPEREACT_CREATE_MODAL_CUSTOM_ID).toJSON() as any);
    return true;
  }

  if (subcommand === WIPEREACT_DELETE_SUBCOMMAND_NAME) {
    const channel = interaction.channel;
    if (!channel?.isTextBased() || !("messages" in channel)) {
      await replyEphemeral(interaction, "Wipe react delete can only be used in a text channel.");
      return true;
    }

    const messageId = interaction.options.getString(WIPEREACT_DELETE_OPTION_NAME, true);
    const message = await channel.messages.fetch(messageId).catch(() => null);
    if (!message) {
      await replyEphemeral(interaction, "That wipe react could not be found in this channel.");
      return true;
    }

    const state = parseWipeReactMessage(message);
    if (!state) {
      await replyEphemeral(interaction, "That message is not a valid wipe react.");
      return true;
    }

    if (state.creatorId !== interaction.user.id) {
      await replyEphemeral(interaction, "You can only delete wipe reacts you created.");
      return true;
    }

    const timer = wipeReactExpiryTimers.get(message.id);
    if (timer) {
      clearTimeout(timer);
      wipeReactExpiryTimers.delete(message.id);
    }

    try {
      await message.delete();
    } catch {
      await replyEphemeral(interaction, "RustLens could not delete that wipe react.");
      return true;
    }

    await replyEphemeral(interaction, "Wipe react deleted.");
    return true;
  }

  await replyEphemeral(
    interaction,
    `Unknown /${WIPEREACT_COMMAND_NAME} subcommand: ${subcommand}`,
  );
  return true;
}

export async function handleWipeReactButtonInteraction(interaction: ButtonInteraction) {
  const action = parseActionButton(interaction.customId);
  if (action) {
    const state = parseWipeReactMessage(interaction.message);
    if (!state) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    scheduleWipeReactExpiry(interaction.message, state.scheduledUnix);

    if (state.scheduledUnix <= Math.floor(Date.now() / 1000)) {
      await deferEphemeral(interaction);
      await disableExpiredWipeReact(interaction.message, state).catch(() => undefined);
      await replyEphemeral(interaction, "This wipe react has already closed.");
      return true;
    }

    if (action.requiresReason) {
      await interaction.showModal(
        buildReasonModal(action.type, interaction.channelId, interaction.message.id).toJSON() as any,
      );
      return true;
    }

    await deferEphemeral(interaction);
    return processWipeReactAction(interaction, interaction.message, action.type);
  }

  if (
    interaction.customId !== WIPEREACT_EDIT_BUTTON_CUSTOM_ID &&
    !interaction.customId.startsWith(WIPEREACT_EDIT_BUTTON_PREFIX)
  ) {
    return false;
  }

  if (!hasAdministratorPermission(interaction)) {
    await replyEphemeral(interaction, "Only administrators can edit a wipe react.");
    return true;
  }

  const state = parseWipeReactMessage(interaction.message);
  if (!state) {
    await replyEphemeral(interaction, "This wipe react message is no longer valid.");
    return true;
  }

  if (state.scheduledUnix <= Math.floor(Date.now() / 1000)) {
    await deferEphemeral(interaction);
    await disableExpiredWipeReact(interaction.message, state).catch(() => undefined);
    await replyEphemeral(interaction, "This wipe react has already closed.");
    return true;
  }

  await interaction.showModal(
    buildWipeReactModal(
      `${WIPEREACT_EDIT_MODAL_PREFIX}${interaction.channelId}:${interaction.message.id}`,
      state,
    ).toJSON() as any,
  );
  return true;
}

export async function handleWipeReactModalSubmit(interaction: ModalSubmitInteraction) {
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
        "RustLens could not find a logs channel. Run /setup logchannel {channel} before creating a wipe react.",
      );
      return true;
    }

    await interaction.reply({
      embeds: [buildWipeReactEmbed(stateResult.state)],
      components: createWipeReactButtons(stateResult.state, false),
    });
    const createdMessage = await interaction.fetchReply();
    scheduleWipeReactExpiry(createdMessage, stateResult.state.scheduledUnix);
    return true;
  }

  const editTarget = extractTargetFromCustomId(
    interaction.customId,
    WIPEREACT_EDIT_MODAL_PREFIX,
  );
  if (editTarget) {
    const [channelId, messageId] = editTarget;
    const message = await fetchWipeReactMessage(interaction, channelId, messageId);
    if (!message) {
      await replyEphemeral(interaction, "The wipe react message could not be found.");
      return true;
    }

    const existing = parseWipeReactMessage(message);
    if (!existing) {
      await replyEphemeral(interaction, "This wipe react message is no longer valid.");
      return true;
    }

    if (existing.scheduledUnix <= Math.floor(Date.now() / 1000)) {
      await disableExpiredWipeReact(message, existing).catch(() => undefined);
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

    const preservedState: WipeReactMessageState = {
      ...updated.state,
      acceptedUserIds: existing.acceptedUserIds,
      declinedUserIds: existing.declinedUserIds,
      lateUserIds: existing.lateUserIds,
    };

    const updatedMessage = await message.edit({
      embeds: [buildWipeReactEmbed(preservedState)],
      components: createWipeReactButtons(preservedState, false),
    });
    scheduleWipeReactExpiry(updatedMessage, preservedState.scheduledUnix);
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

  const message = await fetchWipeReactMessage(interaction, channelId, messageId);
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

export const __testables = {
  applyUserReaction,
  buildWipeReactEmbed,
  buildWipeReactModal,
  createWipeReactButtons,
  formatTimestampForInput,
  parseActionButton,
  parseFlexibleDateTime,
  parseWipeReactMessage,
};
