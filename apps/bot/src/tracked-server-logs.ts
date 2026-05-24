import type { Client, Guild } from "discord.js";
import { EmbedBuilder } from "discord.js";
import { BackendClient, type TrackedServerLogEvent } from "./backend-client.js";
import { loadConfig } from "./config.js";
import { fetchGuildSettings, resolveConfiguredGuildChannel } from "./guild-settings.js";
import { logger } from "./logger.js";
import { buildFooter } from "./shared.js";

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
const previousSnapshots = new Map<string, Map<string, TrackedServerLogEvent>>();

function getBackend() {
  if (!_backend) {
    const config = loadConfig();
    _backend = new BackendClient(config.backendUrl, config.jwtSecret, config.jwtIssuer, config.jwtAudience);
    _workerId = `${config.workerId}-tracked-logs`;
  }
  return { backend: _backend, workerId: _workerId! };
}

export function startTrackedServerLogPoller(client: Client): NodeJS.Timeout {
  let running = false;

  const tick = async () => {
    if (running) {
      return;
    }
    running = true;
    try {
      await pollGuilds(client);
    } catch (error) {
      logger.error({ error }, "tracked-server-log poller failed");
    } finally {
      running = false;
    }
  };

  const timer = setInterval(tick, POLL_INTERVAL_MS);
  void tick();
  return timer;
}

async function pollGuilds(client: Client) {
  const guilds = [...client.guilds.cache.values()];
  for (const guild of guilds) {
    try {
      await processGuild(guild);
    } catch (error) {
      logger.error({ error, guildId: guild.id }, "tracked-server-log processing failed for guild");
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
  const currentSnapshot = new Map<string, TrackedServerLogEvent>();
  for (const item of response.items) {
    currentSnapshot.set(snapshotKey(item), item);
  }

  const previousSnapshot = previousSnapshots.get(guild.id);
  if (!previousSnapshot) {
    previousSnapshots.set(guild.id, currentSnapshot);
    return;
  }

  const changes = diffSnapshot(previousSnapshot, currentSnapshot);
  // Start the next snapshot from the current state, then for every change we
  // FAIL to emit, copy the previous state for that key back in so the
  // transition is re-detected on the next tick. Previously this loop did
  // `break` on first error AND unconditionally stored currentSnapshot, which
  // permanently dropped every subsequent join/leave event in the tick.
  const nextSnapshot = new Map(currentSnapshot);
  for (const event of changes) {
    const embed = buildLogEmbed(event);
    try {
      await (channel as { send: (payload: { embeds: EmbedBuilder[] }) => Promise<unknown> }).send({ embeds: [embed] });
    } catch (error) {
      logger.error({ error, guildId: guild.id }, "failed to post tracked-server-log embed for guild");
      const key = snapshotKey(event);
      const previousState = previousSnapshot.get(key);
      if (previousState) {
        nextSnapshot.set(key, previousState);
      } else {
        nextSnapshot.delete(key);
      }
      continue;
    }
  }

  previousSnapshots.set(guild.id, nextSnapshot);
}

export function buildLogEmbed(event: TrackedServerLogEvent) {
  const action = event.isOnline ? "joined" : "left";
  const color = event.isOnline ? JOIN_COLOR : LEAVE_COLOR;
  const title = `${event.bmName || event.playerBmId} has ${action} ${event.serverName}`;

  const lines = [
    `${EMOJIS.steam} [${event.steamId}](https://steamcommunity.com/profiles/${event.steamId})`,
    `${EMOJIS.battlemetrics} [${event.playerBmId}](https://www.battlemetrics.com/rcon/players/${event.playerBmId})`,
    `${EMOJIS.discord} <@${event.discordId}>`,
  ];

  const embed = new EmbedBuilder()
    .setTitle(title)
    .setColor(color)
    .setTimestamp()
    .setFooter(buildFooter())
    .addFields({ name: "Information", value: lines.join("\n") });

  if (event.avatarImage) {
    embed.setThumbnail(event.avatarImage);
  }

  return embed;
}

function snapshotKey(event: TrackedServerLogEvent) {
  return `${event.serverBmId}:${event.playerBmId}:${event.steamId}:${event.discordId}`;
}

function diffSnapshot(
  previousSnapshot: Map<string, TrackedServerLogEvent>,
  currentSnapshot: Map<string, TrackedServerLogEvent>,
) {
  const changes: TrackedServerLogEvent[] = [];

  for (const [key, current] of currentSnapshot) {
    const previous = previousSnapshot.get(key);
    if (!previous || previous.isOnline === current.isOnline) {
      continue;
    }
    changes.push(current);
  }

  changes.sort((a, b) => a.occurredUnix - b.occurredUnix || a.serverName.localeCompare(b.serverName) || a.discordId.localeCompare(b.discordId));
  return changes;
}

export const __testables = {
  buildLogEmbed,
  diffSnapshot,
};
