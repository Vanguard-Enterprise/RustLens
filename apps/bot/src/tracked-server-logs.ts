import type { Client, Guild } from "discord.js";
import { EmbedBuilder } from "discord.js";
import { BackendClient, type TrackedServerLogEvent } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { fetchGuildSettings, resolveConfiguredGuildChannel } from "./guild-settings.js";
import { RUSTLENS_EMBED_ICON_URL } from "./shared.js";

const POLL_INTERVAL_MS = 20_000;
const JOIN_COLOR = 1233708;
const LEAVE_COLOR = 13832761;

const EMOJIS = {
  steam: "<:steam:1444016467648581642>",
  battlemetrics: "<:battlemetrics:1444011821529956472>",
  discord: "<:discord:1496989877831729284>",
} as const;

let _backend: BackendClient | undefined;
let _workerId: string | undefined;

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerId = `${config.workerId}-tracked-logs`;
  }
  return { backend: _backend, workerId: _workerId! };
}

export function startTrackedServerLogPoller(client: Client) {
  let running = false;

  const tick = async () => {
    if (running) {
      return;
    }
    running = true;
    try {
      await pollGuilds(client);
    } catch (error) {
      console.error("[bot] tracked-server-log poller failed", error);
    } finally {
      running = false;
    }
  };

  setInterval(tick, POLL_INTERVAL_MS);
  void tick();
}

async function pollGuilds(client: Client) {
  const guilds = [...client.guilds.cache.values()];
  for (const guild of guilds) {
    try {
      await processGuild(guild);
    } catch (error) {
      console.error(`[bot] tracked-server-log processing failed for guild ${guild.id}`, error);
    }
  }
}

async function processGuild(guild: Guild) {
  const settings = await fetchGuildSettings(guild.id).catch(() => null);
  if (!settings?.trackedServerLogChannelId) {
    return;
  }

  const channel = await resolveConfiguredGuildChannel(guild, settings.trackedServerLogChannelId);
  if (!channel || !("send" in channel)) {
    return;
  }

  const { backend, workerId } = getBackend();
  const response = await backend.listGuildTrackedServerLogEvents(workerId, guild.id);

  let lastPostedID = 0;
  for (const event of response.items) {
    const embed = buildLogEmbed(event);
    try {
      await (channel as { send: (payload: { embeds: EmbedBuilder[] }) => Promise<unknown> }).send({ embeds: [embed] });
      lastPostedID = event.id;
    } catch (error) {
      console.error(`[bot] failed to post tracked-server-log embed for guild ${guild.id}`, error);
      break;
    }
  }

  const ackTarget = lastPostedID > 0 ? lastPostedID : response.lastEventId;
  if (ackTarget > 0) {
    await backend.ackGuildTrackedServerLogEvents(workerId, guild.id, ackTarget).catch((error) => {
      console.error(`[bot] failed to ack tracked-server-log cursor for guild ${guild.id}`, error);
    });
  }
}

export function buildLogEmbed(event: TrackedServerLogEvent) {
  const action = event.eventType === "online" ? "joined" : "left";
  const color = event.eventType === "online" ? JOIN_COLOR : LEAVE_COLOR;
  const title = `${event.bmName || event.playerBmId} has ${action} ${event.serverName}`;

  const lines = [
    `${EMOJIS.steam} [${event.steamId}](https://steamcommunity.com/profiles/${event.steamId})`,
    `${EMOJIS.battlemetrics} [${event.playerBmId}](https://www.battlemetrics.com/players/${event.playerBmId})`,
    `${EMOJIS.discord} <@${event.discordId}>`,
  ];

  const embed = new EmbedBuilder()
    .setTitle(title)
    .setColor(color)
    .setTimestamp(new Date(event.occurredUnix * 1000))
    .setFooter({ text: "RustLens.com", iconURL: RUSTLENS_EMBED_ICON_URL })
    .addFields({ name: "Information", value: lines.join("\n") });

  if (event.avatarImage) {
    embed.setThumbnail(event.avatarImage);
  }

  return embed;
}

export const __testables = {
  buildLogEmbed,
};
