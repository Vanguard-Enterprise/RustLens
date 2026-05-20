import type {
  ButtonInteraction,
  ChatInputCommandInteraction,
  Client,
  ModalSubmitInteraction,
} from "discord.js";
import type { CommandUsageTarget } from "../backend-client.js";
import {
  ActionRowBuilder,
  ApplicationIntegrationType,
  ButtonBuilder,
  ButtonStyle,
  InteractionContextType,
  MessageFlags,
  ModalBuilder,
  PermissionFlagsBits,
  SlashCommandBuilder,
  TextInputBuilder,
  TextInputStyle,
} from "discord.js";
import { BackendClient } from "../backend-client.js";
import { loadConfig } from "../config.js";
import { createEmbed, logDiscordCatch } from "../shared.js";

export const TRANSFER_COMMAND_NAME = "transfer";

export type TransferModalSubmitResult = {
  handled: true;
  targets: CommandUsageTarget[];
};

export const TRANSFER_CONTINUE_BUTTON_CUSTOM_ID = "transfer:continue";
export const TRANSFER_MODAL_CUSTOM_ID = "transfer:modal";
const TRANSFER_MESSAGE_IDS_CUSTOM_ID = "transfer:message_ids";

const shardID = process.env.SHARD_ID ?? "0";

const transferCommand = new SlashCommandBuilder()
  .setName(TRANSFER_COMMAND_NAME)
  .setDescription("Transfer guild data from another bot into RustLens")
  .setDefaultMemberPermissions(PermissionFlagsBits.Administrator)
  .setDMPermission(false)
  .setContexts(InteractionContextType.Guild)
  .setIntegrationTypes(ApplicationIntegrationType.GuildInstall);

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
    _workerID = `${config.workerId}-transfer-${shardID}`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function buildTransferEmbed(title: string, description: string) {
  return createEmbed()
    .setTitle(title)
    .setDescription(description);
}

function buildInstructionsEmbed() {
  return createEmbed()
    .setTitle("Instructions")
    .addFields({
      name: "Hexaytron",
      value:
        "> * 1. run /summary\n" +
        "> * 2. copy the message ID(s) of the embeds\n" +
        "> * 3. Click the button below",
    });
}

function buildContinueButtonRow() {
  return new ActionRowBuilder<ButtonBuilder>().addComponents(
    new ButtonBuilder()
      .setCustomId(TRANSFER_CONTINUE_BUTTON_CUSTOM_ID)
      .setLabel("Continue")
      .setStyle(ButtonStyle.Success),
  );
}

function buildTransferModal() {
  return new ModalBuilder()
    .setCustomId(TRANSFER_MODAL_CUSTOM_ID)
    .setTitle("Transfer your data from Hexaytron")
    .addComponents(
      new ActionRowBuilder<TextInputBuilder>().addComponents(
        new TextInputBuilder()
          .setCustomId(TRANSFER_MESSAGE_IDS_CUSTOM_ID)
          .setLabel("Message ID(s)")
          .setStyle(TextInputStyle.Paragraph)
          .setRequired(true)
          .setPlaceholder("Message ID(s) separated by a comma (,)"),
      ),
    );
}

export async function ensureTransferCommand(client: Client, shardID: number) {
  if (shardID !== 0 || !client.application) {
    return;
  }

  const data = transferCommand.toJSON();
  const commands = await client.application.commands.fetch();
  const existing = commands.find((command) => command.name === data.name);

  if (existing) {
    await client.application.commands.edit(existing.id, data);
  } else {
    await client.application.commands.create(data);
  }

  console.log("[bot] ensured /transfer command (global)");
}

export async function handleTransferChatInput(interaction: ChatInputCommandInteraction) {
  if (interaction.commandName !== TRANSFER_COMMAND_NAME) {
    return false;
  }

  if (!interaction.inGuild()) {
    await interaction.reply({
      embeds: [buildTransferEmbed("Error", "This command can only be used inside a server.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!interaction.memberPermissions?.has(PermissionFlagsBits.Administrator)) {
    await interaction.reply({
      embeds: [buildTransferEmbed("Error", "Only guild administrators can use this command.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.reply({
    embeds: [buildInstructionsEmbed()],
    components: [buildContinueButtonRow()],
    flags: MessageFlags.Ephemeral,
  });
  return true;
}

export async function handleTransferButtonInteraction(interaction: ButtonInteraction) {
  if (interaction.customId !== TRANSFER_CONTINUE_BUTTON_CUSTOM_ID) {
    return false;
  }

  await interaction.showModal(buildTransferModal());
  return true;
}

type LinkPair = { discordId: string; steamId: string };

function parseMessageIds(input: string): string[] {
  return Array.from(
    new Set(
      input
        .split(/[,.\s]+/)
        .map((token) => token.trim())
        .filter((token) => /^\d{17,20}$/.test(token)),
    ),
  );
}

function extractPairsFromEmbedFields(
  fields: ReadonlyArray<{ value: string }>,
): LinkPair[] {
  const pairs: LinkPair[] = [];
  const seen = new Set<string>();

  for (const field of fields) {
    for (const line of field.value.split("\n")) {
      const discordMatch = line.match(/<@!?(\d{15,20})>/);
      const steamMatch = line.match(/\b(7656\d{13})\b/);
      if (!discordMatch || !steamMatch) {
        continue;
      }
      const key = `${discordMatch[1]}:${steamMatch[1]}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      pairs.push({ discordId: discordMatch[1], steamId: steamMatch[1] });
    }
  }

  return pairs;
}

export async function handleTransferModalSubmit(interaction: ModalSubmitInteraction): Promise<false | true | TransferModalSubmitResult> {
  if (interaction.customId !== TRANSFER_MODAL_CUSTOM_ID) {
    return false;
  }

  if (!interaction.inGuild() || !interaction.guildId) {
    await interaction.reply({
      embeds: [buildTransferEmbed("Error", "This command can only be used inside a server.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  if (!interaction.isFromMessage()) {
    await interaction.reply({
      embeds: [buildTransferEmbed("Error", "Invalid transfer session.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  const guild = interaction.guild;
  if (!guild) {
    await interaction.reply({
      embeds: [buildTransferEmbed("Error", "This command can only be used inside a server.")],
      flags: MessageFlags.Ephemeral,
    });
    return true;
  }

  await interaction.deferUpdate();

  const raw = interaction.fields.getTextInputValue(TRANSFER_MESSAGE_IDS_CUSTOM_ID);
  const messageIds = parseMessageIds(raw);

  if (messageIds.length === 0) {
    await interaction.editReply({
      embeds: [buildTransferEmbed("Error", "No valid message IDs were provided.")],
      components: [],
    });
    return true;
  }

  const channel = interaction.channel;
  if (!channel || !("messages" in channel)) {
    await interaction.editReply({
      embeds: [buildTransferEmbed("Error", "Cannot read messages from this channel.")],
      components: [],
    });
    return true;
  }

  const pairs: LinkPair[] = [];
  const seenPairs = new Set<string>();

  for (const messageId of messageIds) {
    const message = await channel.messages.fetch(messageId).catch(logDiscordCatch("fetch transfer source message", null, {
      guildId: interaction.guildId,
      channelId: channel.id,
      messageId,
    }));
    if (!message) {
      continue;
    }

    const fields = message.embeds.flatMap((embed) => embed.fields ?? []);
    for (const pair of extractPairsFromEmbedFields(fields)) {
      const key = `${pair.discordId}:${pair.steamId}`;
      if (seenPairs.has(key)) {
        continue;
      }
      seenPairs.add(key);
      pairs.push(pair);
    }
  }

  const backend = getBackend();
  const workerId = getWorkerID();
  let success = 0;
  let failed = 0;
  const linkedTargets: CommandUsageTarget[] = [];

  const linkOne = async (pair: LinkPair) => {
    try {
      const member = await guild.members.fetch(pair.discordId).catch(logDiscordCatch("fetch transfer target member", null, {
        guildId: interaction.guildId,
        discordId: pair.discordId,
      }));
      if (!member) {
        failed++;
        return;
      }

      try {
        await backend.createSoftLink(workerId, pair.steamId, pair.discordId, "transfer_hexaytron");
      } catch (error) {
        const status = (error as { status?: number }).status;
        if (status !== 409) {
          throw error;
        }
      }

      try {
        await backend.createGuildLink(workerId, interaction.guildId, pair.discordId, pair.steamId);
      } catch (error) {
        const status = (error as { status?: number }).status;
        if (status !== 409) {
          throw error;
        }
      }

      success++;
      linkedTargets.push({
        kind:        "user",
        snowflake:   member.id,
        displayName: member.displayName ?? null,
        avatarHash:  member.user.avatar ?? null,
        relation:    "recipient",
      });
    } catch {
      failed++;
    }
  };

  // Run with bounded concurrency so a long input doesn't race the 15-min
  // interaction-token lifetime, and so a failure on one pair doesn't abort the
  // batch.
  const TRANSFER_CONCURRENCY = 5;
  let cursor = 0;
  const runWorker = async () => {
    while (true) {
      const idx = cursor;
      cursor += 1;
      if (idx >= pairs.length) {
        return;
      }
      await linkOne(pairs[idx]);
    }
  };
  await Promise.all(
    Array.from({ length: Math.min(TRANSFER_CONCURRENCY, pairs.length) }, () => runWorker()),
  );

  const description = `**${success}** successfully linked\n**${failed}** unsuccessfully linked`;
  await interaction.editReply({
    embeds: [buildTransferEmbed("Transfer successful", description)],
    components: [],
  });
  return { handled: true, targets: linkedTargets };
}

export const __testables = {
  parseMessageIds,
  extractPairsFromEmbedFields,
};
