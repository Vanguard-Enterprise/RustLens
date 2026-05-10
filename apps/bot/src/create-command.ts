import type {
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  GuildMember,
} from "discord.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ButtonBuilder,
  ButtonStyle,
  EmbedBuilder,
  InteractionContextType,
  MessageFlags,
  SlashCommandBuilder,
} from "discord.js";
import { BackendClient } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { isGuildTooLargeForDms, replyDmLimitError } from "./dm-limit.js";
import { hasGuildAdminAccess } from "./guild-settings.js";
import { buildFooter, logDiscordCatch, logDiscordError, RUSTLENS_EMBED_COLOR, replyEphemeral } from "./shared.js";

export const CREATE_COMMAND_NAME = "create";

const CREATE_INVITE_SUBCOMMAND_NAME = "invite";
const CREATE_INVITE_ROLE_OPTION_NAME = "role";
const CREATE_INVITE_REQUIRED_ROLE_OPTION_NAME = "required_role";

export const CREATE_INVITE_REQUEST_PREFIX = "ci_req:";
export const CREATE_INVITE_ACCEPT_PREFIX = "ci_acc:";

const REQUEST_BUTTON_EMOJI = { name: "user_add", id: "1498006708549587155" };
const ACCEPT_BUTTON_EMOJI = { name: "user_tick", id: "1499109036954751138" };

const NO_REQUIRED_ROLE_SENTINEL = "0";

const PENDING_INVITE_TTL_MS = 30 * 60 * 1000;

type PendingInvite = {
  guildId: string;
  channelId: string;
  messageId: string;
  createdAt: number;
};

const pendingInvites = new Map<string, PendingInvite>();

function pendingInviteKey(guildId: string, requesterId: string) {
  return `${guildId}:${requesterId}`;
}

function prunePendingInvites(now = Date.now()) {
  for (const [key, pending] of pendingInvites) {
    if (now - pending.createdAt > PENDING_INVITE_TTL_MS) {
      pendingInvites.delete(key);
    }
  }
}

const createCommand = new SlashCommandBuilder()
  .setName(CREATE_COMMAND_NAME)
  .setDescription("RustLens create commands")
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall)
  .addSubcommand((subcommand) =>
    subcommand
      .setName(CREATE_INVITE_SUBCOMMAND_NAME)
      .setDescription("Post a clan invite request embed in this channel")
      .addRoleOption((option) =>
        option
          .setName(CREATE_INVITE_ROLE_OPTION_NAME)
          .setDescription("Role to ping when a member requests an invite")
          .setRequired(true),
      )
      .addRoleOption((option) =>
        option
          .setName(CREATE_INVITE_REQUIRED_ROLE_OPTION_NAME)
          .setDescription("Role required to request an invite")
          .setRequired(false),
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
    _workerID = `${config.workerId}-create`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildInviteEmbed(pingRoleId: string, requiredRoleId: string | null) {
  const prerequisites = requiredRoleId
    ? `> * Must have steam ID set via \`/setsteam\`\n> * Must have <@&${requiredRoleId}>`
    : "> * Must have steam ID set via `/setsteam`";

  return new EmbedBuilder()
    .setTitle("Clan Invite")
    .setDescription("Press the button below to request a clan invite!")
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter(buildFooter())
    .addFields(
      { name: "Prerequisites:", value: prerequisites },
      { name: "Information", value: `> * Pings: <@&${pingRoleId}>` },
    );
}

function buildRequestButton(pingRoleId: string, requiredRoleId: string | null) {
  return new ActionRowBuilder<ButtonBuilder>().addComponents(
    new ButtonBuilder()
      .setCustomId(
        `${CREATE_INVITE_REQUEST_PREFIX}${pingRoleId}:${requiredRoleId ?? NO_REQUIRED_ROLE_SENTINEL}`,
      )
      .setLabel("Request For Invite")
      .setStyle(ButtonStyle.Success)
      .setEmoji(REQUEST_BUTTON_EMOJI),
  );
}

function buildAcceptButton(requesterDiscordId: string) {
  return new ActionRowBuilder<ButtonBuilder>().addComponents(
    new ButtonBuilder()
      .setCustomId(`${CREATE_INVITE_ACCEPT_PREFIX}${requesterDiscordId}`)
      .setLabel("Accept")
      .setStyle(ButtonStyle.Success)
      .setEmoji(ACCEPT_BUTTON_EMOJI),
  );
}

function buildClanInviteEmbed(
  steamId: string,
  requesterDisplayName: string,
  requesterAvatarURL: string | null,
) {
  const embed = new EmbedBuilder()
    .setDescription(`\n\`\`\`\n/clan invite ${steamId}\`\`\``)
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter(buildFooter());

  embed.setAuthor({
    name: requesterDisplayName,
    iconURL: requesterAvatarURL ?? undefined,
  });

  return embed;
}

function buildAcceptedDmEmbed(accepterId: string, accepterDisplayName: string) {
  return new EmbedBuilder()
    .setTitle("Clan Invite Pending")
    .setDescription(
      `<@${accepterId}> (${accepterDisplayName}) has accepted your clan invite request.`,
    )
    .setColor(RUSTLENS_EMBED_COLOR)
    .setTimestamp()
    .setFooter(buildFooter());
}

export async function ensureCreateCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = createCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /create command (global)");
}

async function handleCreateInvite(interaction: ChatInputCommandInteraction) {
  if (!interaction.inGuild()) {
    await replyEphemeral(interaction, "This command can only be used in a server.");
    return;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeral(
      interaction,
      "Only the configured admin role or administrators can use this command.",
    );
    return;
  }

  const pingRole = interaction.options.getRole(CREATE_INVITE_ROLE_OPTION_NAME, true);
  const requiredRole = interaction.options.getRole(
    CREATE_INVITE_REQUIRED_ROLE_OPTION_NAME,
    false,
  );

  const embed = buildInviteEmbed(pingRole.id, requiredRole?.id ?? null);
  const row = buildRequestButton(pingRole.id, requiredRole?.id ?? null);

  await interaction.reply({
    embeds: [embed],
    components: [row],
  });
}

export async function handleCreateChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== CREATE_COMMAND_NAME) {
    return false;
  }

  const subcommand = interaction.options.getSubcommand(false);
  if (subcommand === CREATE_INVITE_SUBCOMMAND_NAME) {
    await handleCreateInvite(interaction);
    return true;
  }

  await replyEphemeral(interaction, `Unknown /${CREATE_COMMAND_NAME} subcommand: ${subcommand}`);
  return true;
}

function memberHasRole(member: GuildMember | null, roleId: string) {
  return member?.roles.cache.has(roleId) ?? false;
}

async function handleRequestInviteButton(interaction: ButtonInteraction) {
  if (!interaction.inGuild() || !interaction.guild) {
    await replyEphemeral(interaction, "This action can only be used in a server.");
    return;
  }

  const payload = interaction.customId.slice(CREATE_INVITE_REQUEST_PREFIX.length);
  const [pingRoleId, requiredRoleRaw] = payload.split(":");
  if (!pingRoleId) {
    await replyEphemeral(interaction, "This invite button is misconfigured.");
    return;
  }
  const requiredRoleId =
    requiredRoleRaw && requiredRoleRaw !== NO_REQUIRED_ROLE_SENTINEL ? requiredRoleRaw : null;

  const member =
    (interaction.member as GuildMember | null) ??
    (await interaction.guild.members.fetch(interaction.user.id).catch(logDiscordCatch("fetch create invite requester member", null, {
      guildId: interaction.guildId,
      discordId: interaction.user.id,
    })));

  if (requiredRoleId && !memberHasRole(member, requiredRoleId)) {
    await replyEphemeral(
      interaction,
      `You must have <@&${requiredRoleId}> to request a clan invite.`,
    );
    return;
  }

  const backend = getBackend();
  const workerId = getWorkerID();
  const links = await backend
    .listGuildLinksByDiscord(workerId, interaction.guildId, interaction.user.id)
    .catch(() => null);

  const steamId = links?.items[0]?.steamId;
  if (!steamId) {
    await replyEphemeral(
      interaction,
      "You must set your Steam ID with `/setsteam` before requesting a clan invite.",
    );
    return;
  }

  prunePendingInvites();

  const pendingKey = pendingInviteKey(interaction.guildId, interaction.user.id);
  if (pendingInvites.has(pendingKey)) {
    await replyEphemeral(
      interaction,
      "You already have a pending clan invite request. Wait for an admin to accept it before requesting another.",
    );
    return;
  }

  pendingInvites.set(pendingKey, {
    guildId: interaction.guildId,
    channelId: interaction.channelId ?? "",
    messageId: "",
    createdAt: Date.now(),
  });

  const requesterDisplayName =
    member?.displayName ?? interaction.user.globalName ?? interaction.user.username;
  const requesterAvatarURL =
    member?.displayAvatarURL({ size: 1024 }) ?? interaction.user.displayAvatarURL({ size: 1024 });

  try {
    await interaction.reply({
      content: `<@&${pingRoleId}> <@${interaction.user.id}>`,
      embeds: [buildClanInviteEmbed(steamId, requesterDisplayName, requesterAvatarURL)],
      components: [buildAcceptButton(interaction.user.id)],
      allowedMentions: { roles: [pingRoleId], users: [interaction.user.id] },
    });
  } catch (error) {
    pendingInvites.delete(pendingKey);
    throw error;
  }

  const replyMessage = await interaction.fetchReply().catch(logDiscordCatch("fetch create invite reply", null, {
    guildId: interaction.guildId,
    discordId: interaction.user.id,
  }));
  const existing = pendingInvites.get(pendingKey);
  if (replyMessage && existing) {
    pendingInvites.set(pendingKey, {
      ...existing,
      channelId: replyMessage.channelId,
      messageId: replyMessage.id,
    });
  }
}

async function handleAcceptInviteButton(interaction: ButtonInteraction) {
  if (!interaction.inGuild() || !interaction.guild) {
    await replyEphemeral(interaction, "This action can only be used in a server.");
    return;
  }

  if (!(await hasGuildAdminAccess(interaction))) {
    await replyEphemeral(
      interaction,
      "Only the configured admin role or administrators can accept clan invites.",
    );
    return;
  }

  const requesterId = interaction.customId.slice(CREATE_INVITE_ACCEPT_PREFIX.length);
  if (!requesterId) {
    await replyEphemeral(interaction, "This accept button is misconfigured.");
    return;
  }

  if (isGuildTooLargeForDms(interaction.guild)) {
    await replyDmLimitError(interaction, interaction.guild);
    return;
  }

  const accepterMember =
    (interaction.member as GuildMember | null) ??
    (await interaction.guild.members.fetch(interaction.user.id).catch(logDiscordCatch("fetch create invite accepter member", null, {
      guildId: interaction.guildId,
      discordId: interaction.user.id,
    })));
  const accepterDisplayName =
    accepterMember?.displayName ?? interaction.user.globalName ?? interaction.user.username;

  const dmEmbed = buildAcceptedDmEmbed(interaction.user.id, accepterDisplayName);

  let dmFailed = false;
  try {
    const requester = await interaction.client.users.fetch(requesterId);
    await requester.send({ embeds: [dmEmbed] });
  } catch (error) {
    logDiscordError("send create invite accepted DM", error, {
      guildId: interaction.guildId,
      discordId: requesterId,
    });
    dmFailed = true;
  }

  await interaction.message.delete().catch(logDiscordCatch("delete accepted create invite message", undefined, {
    guildId: interaction.guildId,
    channelId: interaction.message.channelId,
    messageId: interaction.message.id,
  }));

  pendingInvites.delete(pendingInviteKey(interaction.guildId, requesterId));

  await interaction.reply({
    content: dmFailed
      ? "Invite accepted, but I could not DM the requester."
      : "Invite accepted. The requester has been notified via DM.",
    flags: MessageFlags.Ephemeral,
  });
}

export async function handleCreateButtonInteraction(interaction: ButtonInteraction) {
  if (interaction.customId.startsWith(CREATE_INVITE_REQUEST_PREFIX)) {
    await handleRequestInviteButton(interaction);
    return true;
  }

  if (interaction.customId.startsWith(CREATE_INVITE_ACCEPT_PREFIX)) {
    await handleAcceptInviteButton(interaction);
    return true;
  }

  return false;
}
