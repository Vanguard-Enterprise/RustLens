import {
  ChannelType,
  EmbedBuilder,
  MessageFlags,
  type ChatInputCommandInteraction,
  type Client,
} from "discord.js";
import { BackendClient, type RateLimitFraudAlert } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { logDiscordError, RUSTLENS_EMBED_COLOR } from "./shared.js";

// Static fraud-alert destination. Fill in to enable; empty string disables
// posting (alerts are still detected and logged to stdout).
const FRAUD_ALERT_CHANNEL_ID = "1492270634594603170";

const RATE_LIMIT_BUDGET_MS = 1_500;
const FRAUD_EMBED_COLOR = 0xd31239;

const rateLimitTimedOut = Symbol("rateLimitTimedOut");

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
    _workerID = `${config.workerId}-rl`;
  }
  return _backend;
}

function getWorkerID() {
  getBackend();
  return _workerID!;
}

function formatRetry(seconds: number) {
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  if (minutes < 60) {
    return remainder ? `${minutes}m ${remainder}s` : `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return remMinutes ? `${hours}h ${remMinutes}m` : `${hours}h`;
}

function buildCooldownEmbed(retryAfterSeconds: number) {
  return new EmbedBuilder()
    .setTitle("Slow down")
    .setDescription(
      `You're using this command too often. Try again in ${formatRetry(retryAfterSeconds)}.`,
    )
    .setColor(RUSTLENS_EMBED_COLOR);
}

async function postFraudAlert(client: Client, alert: RateLimitFraudAlert) {
  if (!FRAUD_ALERT_CHANNEL_ID) {
    console.warn("[bot] fraud alert (no channel configured)", alert);
    return;
  }
  try {
    const channel = await client.channels.fetch(FRAUD_ALERT_CHANNEL_ID);
    if (!channel || channel.type !== ChannelType.GuildText) {
      console.warn("[bot] fraud alert channel is not a text channel", {
        channelId: FRAUD_ALERT_CHANNEL_ID,
      });
      return;
    }
    const windowMinutes = Math.max(1, Math.round(alert.windowSecs / 60));
    const embed = new EmbedBuilder()
      .setTitle("Possible scraping detected")
      .setDescription(
        [
          `**Command:** /${alert.command}`,
          `**Guild:** \`${alert.guildId}\``,
          `**Approx members:** ${alert.memberCount}`,
          `**Invocations:** ${alert.count} in last ${windowMinutes}m`,
        ].join("\n"),
      )
      .setColor(FRAUD_EMBED_COLOR)
      .setTimestamp(new Date());
    await channel.send({ embeds: [embed] });
  } catch (error) {
    logDiscordError("post fraud alert", error, {
      channelId: FRAUD_ALERT_CHANNEL_ID,
      ...alert,
    });
  }
}

// enforceRateLimit consumes a slot for (interaction.user, command) on the
// backend. Returns true when the interaction has been handled (replied to)
// because the user was rate-limited. Returns false when the call should
// proceed.
//
// Fails open: if the backend is unreachable or times out, we allow the
// call (matching ban-check's separate fail-closed budget; rate limiting
// is best-effort, not security).
export async function enforceRateLimit(
  interaction: ChatInputCommandInteraction,
  command: string,
): Promise<boolean> {
  const guildId = interaction.guildId ?? undefined;
  const memberCount =
    interaction.inCachedGuild() && typeof interaction.guild.memberCount === "number"
      ? interaction.guild.memberCount
      : undefined;

  let result: Awaited<ReturnType<BackendClient["consumeRateLimit"]>> | typeof rateLimitTimedOut;
  try {
    result = await Promise.race([
      getBackend().consumeRateLimit(getWorkerID(), {
        discordId: interaction.user.id,
        command,
        guildId,
        memberCount,
      }),
      new Promise<typeof rateLimitTimedOut>((resolve) => {
        setTimeout(() => resolve(rateLimitTimedOut), RATE_LIMIT_BUDGET_MS);
      }),
    ]);
  } catch (error) {
    console.error("[bot] rate-limit consume failed (failing open)", {
      discordId: interaction.user.id,
      command,
      error,
    });
    return false;
  }

  if (result === rateLimitTimedOut) {
    console.warn("[bot] rate-limit consume timed out (failing open)", {
      discordId: interaction.user.id,
      command,
      budgetMs: RATE_LIMIT_BUDGET_MS,
    });
    return false;
  }

  if (result.fraudAlert) {
    void postFraudAlert(interaction.client, result.fraudAlert);
  }

  if (result.allowed) {
    return false;
  }

  const embed = buildCooldownEmbed(result.retryAfterSeconds);
  try {
    if (interaction.deferred || interaction.replied) {
      await interaction.editReply({ embeds: [embed], components: [] });
    } else {
      await interaction.reply({ embeds: [embed], flags: MessageFlags.Ephemeral });
    }
  } catch (error) {
    logDiscordError("reply with rate-limit cooldown", error, {
      discordId: interaction.user.id,
      command,
    });
  }
  return true;
}
