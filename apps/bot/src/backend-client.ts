import jwt from "jsonwebtoken";

type RegisterResponse = {
  workerId: string;
  targetShards: number;
  heartbeatTtlSeconds: number;
};

type AssignmentsResponse = {
  workerId: string;
  targetShards: number;
  shardIds: number[];
};

export type GuildSettings = {
  guildId: string;
  wipeReactLogChannelId: string;
  linkingLogChannelId: string;
  trackedServerLogChannelId: string;
  alertsChannelId: string;
  adminRoleId: string;
};

export type GuildSettingsPatch = Partial<Omit<GuildSettings, "guildId">>;

export type WipeReactReactionType = "accepted" | "declined" | "late";

export type WipeReact = {
  guildId: string;
  channelId: string;
  messageId: string;
  creatorId: string;
  name: string;
  description: string;
  timeZone: string;
  scheduledUnix: number;
  askLateReason: boolean;
  askDeclinedReason: boolean;
  acceptedUserIds: string[];
  declinedUserIds: string[];
  lateUserIds: string[];
  embedTimestamp: string;
};

export type WipeReactListResponse = {
  items: WipeReact[];
};

export type WipeReactReactionUpdate = {
  changed: boolean;
  previousType?: WipeReactReactionType | null;
  wipeReact: WipeReact;
};

export type WipeReactReactionRemoval = {
  changed: boolean;
  removedType?: WipeReactReactionType | null;
  wipeReact: WipeReact;
};

export type WipeReactSchedule = {
  id: number;
  guildId: string;
  days: string[];
  includesForceWipe: boolean;
  time: string;
  timeZone: string;
  repeat: string;
  postBefore: string;
  name: string;
  description: string;
  askLateReason: boolean;
  askDeclinedReason: boolean;
  channelId: string;
  lastPostedForUnix: number;
  createdBy: string;
};

export type WipeReactScheduleListResponse = {
  items: WipeReactSchedule[];
};

export type SteamAccount = {
  steamId: string;
  bmId: string | null;
  bmName: string;
  avatarImage: string;
  firstSeenAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type SteamProfileHours = {
  vitality: number;
  atlas: number;
  vital: number;
  modded: number;
  official: number;
  build: number;
  bm: number;
  rust: number | null;
};

export type SteamProfile = {
  steamId: string;
  bmId: string;
  bmName: string;
  avatarImage: string;
  firstSeenUnix: number | null;
  hours: SteamProfileHours;
  serversPlayed: number;
  nameGaps: SteamAnalyticsGap[];
};

export type SteamAnalyticsGame = {
  appId: number;
  name: string;
  hours: number;
};

export type SteamAnalyticsServer = {
  bmId: string;
  name: string;
  hours: number;
};

export type SteamAnalyticsGap = {
  fromUnix: number;
  toUnix: number;
};

export type SteamAnalytics = {
  steamId: string;
  bmId: string;
  bmName: string;
  avatarImage: string;
  totalGames: number | null;
  topGames: SteamAnalyticsGame[];
  totalServers: number;
  topServers: SteamAnalyticsServer[];
  gaps: SteamAnalyticsGap[];
};

export type SteamDiscordLink = {
  steamId: string;
  discordId: string;
  type: "soft" | "hard";
  source: string;
  createdAt: string;
};

export type SteamDiscordLinkListResponse = {
  items: SteamDiscordLink[];
};

export type ResolveSteamIDResponse = {
  steamId: string;
};

export type GuildSteamDiscordLink = {
  guildId: string;
  discordId: string;
  steamId: string;
  createdAt: string;
};

export type GuildSteamDiscordLinkListResponse = {
  items: GuildSteamDiscordLink[];
};

export type GuildNicknameTarget = {
  guildId: string;
  discordId: string;
  steamId: string;
  bmId: string | null;
  bmName: string | null;
};

export type GuildNicknameTargetListResponse = {
  items: GuildNicknameTarget[];
};

export type UserGuildLink = {
  discordId: string;
  guildId: string;
  isActive: boolean;
  firstSeenAt: string;
  lastSeenAt: string;
  leftAt: string | null;
};

export type UserGuildLinkListResponse = {
  items: UserGuildLink[];
};

export type GuildOnlineSummaryItem = {
  guildId: string;
  discordId: string;
  steamId: string;
  playerBmId: string;
  bmName: string;
  serverBmId: string;
  serverName: string;
  onlineUnix: number;
};

export type GuildOnlineSummaryListResponse = {
  items: GuildOnlineSummaryItem[];
};

export type GuildRecentGameBan = {
  guildId: string;
  discordId: string;
  steamId: string;
  gameBannedAt: string;
  gameBannedUnix: number;
};

export type GuildRecentGameBanListResponse = {
  items: GuildRecentGameBan[];
};

export type TrackedServerLogEvent = {
  id: number;
  serverBmId: string;
  serverName: string;
  playerBmId: string;
  steamId: string;
  discordId: string;
  bmName: string;
  avatarImage: string;
  eventType: "online" | "offline";
  occurredUnix: number;
};

export type TrackedServerLogEventsResponse = {
  items: TrackedServerLogEvent[];
  lastEventId: number;
};

export type TrackedServer = {
  bmId: string;
  name: string;
  isVitality: boolean;
  isAtlas: boolean;
  isVital: boolean;
  isModded: boolean;
  isOfficial: boolean;
  isBuild: boolean;
  lastPolledAt: string | null;
  lastPollError: string;
};

export type TrackedServerListResponse = {
  items: TrackedServer[];
};

export type GuildTrackedServer = {
  guildId: string;
  bmId: string;
  name: string;
};

export type GuildTrackedServerListResponse = {
  items: GuildTrackedServer[];
};

export type PlayerState = {
  serverBmId: string;
  serverName: string;
  playerBmId: string;
  isOnline: boolean;
  onlineSince: string | null;
  offlineSince: string | null;
  updatedAt: string;
};

export type PlayerStateListResponse = {
  items: PlayerState[];
};

export type BulkHardLinkEntry = {
  steamId: string;
  bmId?: string | null;
  discordId: string;
  source: string;
};

export class BackendClient {
  constructor(
    private readonly backendUrl: string,
    private readonly jwtSecret: string,
    private readonly issuer: string,
    private readonly audience: string,
  ) {}

  private token(workerId: string) {
    return jwt.sign(
      {
        service: "bot",
        worker_id: workerId,
      },
      this.jwtSecret,
      {
        algorithm: "HS256",
        issuer: this.issuer,
        audience: this.audience,
        expiresIn: "2m",
      },
    );
  }

  private async call<T>(workerId: string, path: string, method: string, body?: unknown): Promise<T> {
    const response = await fetch(`${this.backendUrl}${path}`, {
      method,
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${this.token(workerId)}`,
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(10_000),
    });

    if (!response.ok) {
      const message = await response.text();
      const error = new Error(`backend call failed (${response.status}): ${message}`);
      (error as any).status = response.status;
      throw error;
    }

    return (await response.json()) as T;
  }

  register(workerId: string) {
    return this.call<RegisterResponse>(workerId, "/api/internal/bot/shards/register", "POST", { workerId });
  }

  heartbeat(workerId: string, shardIds: number[]) {
    return this.call<{ status: string }>(workerId, "/api/internal/bot/shards/heartbeat", "POST", { workerId, shardIds });
  }

  release(workerId: string) {
    return this.call<{ status: string }>(workerId, "/api/internal/bot/shards/release", "POST", { workerId });
  }

  assignments(workerId: string) {
    const query = new URLSearchParams({ workerId }).toString();
    return this.call<AssignmentsResponse>(workerId, `/api/internal/bot/shards/assignments?${query}`, "GET");
  }

  guildSettings(workerId: string, guildId: string) {
    return this.call<GuildSettings>(workerId, `/api/internal/bot/setup/guilds/${guildId}/settings`, "GET");
  }

  updateGuildSettings(workerId: string, guildId: string, patch: GuildSettingsPatch) {
    return this.call<GuildSettings>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/settings`,
      "PUT",
      patch,
    );
  }

  listActiveWipeReacts(workerId: string, guildId: string) {
    return this.call<WipeReactListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/active`,
      "GET",
    );
  }

  listWipeReacts(workerId: string, guildId: string) {
    return this.call<WipeReactListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts`,
      "GET",
    );
  }

  getWipeReact(workerId: string, guildId: string, messageId: string) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/${messageId}`,
      "GET",
    );
  }

  createWipeReact(workerId: string, guildId: string, wipeReact: WipeReact) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts`,
      "POST",
      wipeReact,
    );
  }

  updateWipeReact(workerId: string, guildId: string, messageId: string, wipeReact: WipeReact) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/${messageId}`,
      "PUT",
      wipeReact,
    );
  }

  deleteWipeReact(workerId: string, guildId: string, messageId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/${messageId}`,
      "DELETE",
    );
  }

  setWipeReactReaction(
    workerId: string,
    guildId: string,
    messageId: string,
    userId: string,
    type: WipeReactReactionType,
  ) {
    return this.call<WipeReactReactionUpdate>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/${messageId}/reactions`,
      "POST",
      { userId, type },
    );
  }

  removeWipeReactReaction(workerId: string, guildId: string, messageId: string, userId: string) {
    return this.call<WipeReactReactionRemoval>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/${messageId}/reactions/${userId}`,
      "DELETE",
    );
  }

  listWipeReactSchedules(workerId: string, guildId: string) {
    return this.call<WipeReactScheduleListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules`,
      "GET",
    );
  }

  getWipeReactSchedule(workerId: string, guildId: string, scheduleId: number) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "GET",
    );
  }

  createWipeReactSchedule(workerId: string, guildId: string, schedule: Omit<WipeReactSchedule, "id" | "guildId" | "lastPostedForUnix">) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules`,
      "POST",
      schedule,
    );
  }

  updateWipeReactSchedule(workerId: string, guildId: string, scheduleId: number, schedule: Omit<WipeReactSchedule, "id" | "guildId" | "lastPostedForUnix">) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "PUT",
      schedule,
    );
  }

  deleteWipeReactSchedule(workerId: string, guildId: string, scheduleId: number) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "DELETE",
    );
  }

  markWipeReactSchedulePosted(workerId: string, guildId: string, scheduleId: number, lastPostedForUnix: number) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-react-schedules/${scheduleId}/mark-posted`,
      "POST",
      { lastPostedForUnix },
    );
  }

  listTrackedServers(workerId: string) {
    return this.call<TrackedServerListResponse>(
      workerId,
      `/api/internal/bot/setup/tracked-servers`,
      "GET",
    );
  }

  createTrackedServer(
    workerId: string,
    payload: Omit<TrackedServer, "lastPolledAt" | "lastPollError">,
  ) {
    return this.call<TrackedServer>(
      workerId,
      `/api/internal/bot/setup/tracked-servers`,
      "POST",
      payload,
    );
  }

  updateTrackedServer(
    workerId: string,
    currentBmId: string,
    payload: Omit<TrackedServer, "lastPolledAt" | "lastPollError">,
  ) {
    return this.call<TrackedServer>(
      workerId,
      `/api/internal/bot/setup/tracked-servers/${currentBmId}`,
      "PUT",
      payload,
    );
  }

  deleteTrackedServer(workerId: string, bmId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/tracked-servers/${bmId}`,
      "DELETE",
    );
  }

  listGuildTrackedServers(workerId: string, guildId: string) {
    return this.call<GuildTrackedServerListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/tracked-servers`,
      "GET",
    );
  }

  replaceGuildTrackedServers(workerId: string, guildId: string, bmIds: string[]) {
    return this.call<GuildTrackedServerListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/tracked-servers`,
      "PUT",
      { bmIds },
    );
  }

  listOnlinePlayerStatesByServer(workerId: string, serverId: string) {
    return this.call<PlayerStateListResponse>(
      workerId,
      `/api/internal/bot/setup/player-states/servers/${serverId}`,
      "GET",
    );
  }

  getPlayerState(workerId: string, serverId: string, playerId: string) {
    return this.call<PlayerState>(
      workerId,
      `/api/internal/bot/setup/player-states/servers/${serverId}/players/${playerId}`,
      "GET",
    );
  }

  listPlayerStatesByPlayer(workerId: string, playerId: string) {
    return this.call<PlayerStateListResponse>(
      workerId,
      `/api/internal/bot/setup/player-states/players/${playerId}`,
      "GET",
    );
  }

  getSteamAccount(workerId: string, steamId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}`,
      "GET",
    );
  }

  getSteamAccountByBmID(workerId: string, bmId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/setup/steam/by-bm/${bmId}`,
      "GET",
    );
  }

  getSteamProfile(workerId: string, steamId: string) {
    return this.call<SteamProfile>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/profile`,
      "GET",
    );
  }

  getSteamAnalytics(workerId: string, steamId: string) {
    return this.call<SteamAnalytics>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/analytics`,
      "GET",
    );
  }

  getLinksBySteamID(workerId: string, steamId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/links`,
      "GET",
    );
  }

  getLinksByDiscordID(workerId: string, discordId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/discord/${discordId}/links`,
      "GET",
    );
  }

  resolveSteamID(workerId: string, input: string) {
    return this.call<ResolveSteamIDResponse>(
      workerId,
      `/api/internal/bot/setup/steam/resolve`,
      "POST",
      { input },
    );
  }

  createSoftLink(workerId: string, steamId: string, discordId: string, source: string) {
    return this.call<SteamDiscordLink>(
      workerId,
      `/api/internal/bot/setup/steam/links`,
      "POST",
      { steamId, discordId, source },
    );
  }

  createUserGuildLink(workerId: string, discordId: string, guildId: string) {
    return this.call<UserGuildLink>(
      workerId,
      `/api/internal/bot/setup/user-guilds`,
      "POST",
      { discordId, guildId },
    );
  }

  markUserGuildLeft(workerId: string, discordId: string, guildId: string) {
    return this.call<UserGuildLink>(
      workerId,
      `/api/internal/bot/setup/user-guilds/leave`,
      "POST",
      { discordId, guildId },
    );
  }

  createGuildLink(workerId: string, guildId: string, discordId: string, steamId: string) {
    return this.call<GuildSteamDiscordLink>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/links`,
      "POST",
      { discordId, steamId },
    );
  }

  deleteGuildLink(workerId: string, guildId: string, discordId: string, steamId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/discord/${discordId}/steam/${steamId}`,
      "DELETE",
    );
  }

  bulkUpsertHardLinks(workerId: string, links: BulkHardLinkEntry[]) {
    return this.call<{ status: string; count: number }>(
      workerId,
      `/api/internal/bot/setup/steam/bulk-hard-links`,
      "POST",
      { links },
    );
  }

  deleteSoftLink(workerId: string, steamId: string, discordId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/steam/${steamId}/links/${discordId}/soft`,
      "DELETE",
    );
  }

  listGuildLinks(workerId: string, guildId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/links`,
      "GET",
    );
  }

  listGuildNicknameTargets(workerId: string, guildId: string) {
    return this.call<GuildNicknameTargetListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/nickname-targets`,
      "GET",
    );
  }

  listUserGuildLinksByDiscord(workerId: string, discordId: string) {
    return this.call<UserGuildLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/discord/${discordId}/guilds`,
      "GET",
    );
  }

  listGuildOnlineSummary(workerId: string, guildId: string) {
    return this.call<GuildOnlineSummaryListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/online-summary`,
      "GET",
    );
  }

  listGuildRecentGameBans(workerId: string, guildId: string) {
    return this.call<GuildRecentGameBanListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/game-bans`,
      "GET",
    );
  }

  listGuildTrackedServerLogEvents(workerId: string, guildId: string) {
    return this.call<TrackedServerLogEventsResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/tracked-server-log-events`,
      "GET",
    );
  }

  ackGuildTrackedServerLogEvents(workerId: string, guildId: string, lastEventId: number) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/tracked-server-log-events/ack`,
      "POST",
      { lastEventId },
    );
  }

  listGuildLinksByDiscord(workerId: string, guildId: string, discordId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/discord/${discordId}/links`,
      "GET",
    );
  }

  listGuildLinksBySteam(workerId: string, guildId: string, steamId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/steam/${steamId}/links`,
      "GET",
    );
  }
}
