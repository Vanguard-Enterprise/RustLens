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

type CountResponse = {
  count: number;
};

export type ShardStatSnapshot = {
  shardId: number;
  status: "ready" | "disconnected" | "idle";
  pingMs: number;
  guilds: number;
  members: number;
};

export type WorkerStatSnapshot = {
  uptimeSec: number;
  rssBytes: number;
  shards: ShardStatSnapshot[];
};

export type OwnerStatsWorker = {
  workerId: string;
  uptimeSec: number;
  rssBytes: number;
  shards: ShardStatSnapshot[];
};

export type OwnerStatsResponse = {
  guildCount: number;
  memberCount: number;
  configuredGuilds: number;
  steamAccountCount: number;
  hardLinkCount: number;
  uniqueLinkedUsers: number;
  trackedServerCount: number;
  onlineTrackedPlayers: number;
  activeWipeReacts: number;
  targetShards: number;
  workers: OwnerStatsWorker[];
};

export type OwnerProxy = {
  id: number;
  endpoint: string;
  enabled: boolean;
};

export type OwnerProxyListResponse = {
  items: OwnerProxy[];
};

export type GuildSettings = {
  guildId: string;
  wipeReactLogChannelId: string;
  linkingLogChannelId: string;
  trackedServerLogChannelId: string;
  trackedUsersCategoryId: string;
  adminRoleId: string;
  alertRoleId: string;
  limited: boolean;
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

export type WipeReactSchedulePost = {
	id: number;
	scheduleId: number;
	guildId: string;
	channelId: string;
	wipeUnix: number;
	postUnix: number;
	timeZone: string;
	name: string;
	description: string;
	askLateReason: boolean;
	askDeclinedReason: boolean;
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
  personaName: string;
  avatarImage: string;
  firstSeenUnix: number | null;
  hours: SteamProfileHours;
  serversPlayed: number;
  nameGaps: SteamAnalyticsGap[];
  status?: SteamProfileStatus;
};

export type SteamProfileStatus = {
  isOnline: boolean;
  serverBmId: string;
  serverName: string;
  onlineUnix: number | null;
  offlineUnix: number | null;
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

export type SteamFriendListResponse = {
  items: string[];
  private: boolean;
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

export type SetSteamCandidate = {
  steamId: string;
  type: "soft" | "hard";
  displayName: string;
  createdAt: string;
};

export type SetSteamCandidateListResponse = {
  items: SetSteamCandidate[];
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
  source: "direct_guild_link" | "linked_account";
};

export type GuildRecentGameBanListResponse = {
  items: GuildRecentGameBan[];
};

export type TrackedServerLogEvent = {
  serverBmId: string;
  serverName: string;
  playerBmId: string;
  steamId: string;
  discordId: string;
  bmName: string;
  avatarImage: string;
  isOnline: boolean;
  occurredUnix: number;
};

export type TrackedServerLogEventsResponse = {
  items: TrackedServerLogEvent[];
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
  highFrequency: boolean;
  lastPolledAt: string | null;
  lastPollError: string;
};

export type TrackedServerListResponse = {
  items: TrackedServer[];
};

export type ServerAnalyticsTimeframe = "7d" | "14d" | "30d" | "60d" | "90d";

export type ServerAnalyticsPoint = {
  timestampUnix: number;
  timestamp: string;
  min: number;
  value: number;
  max: number;
};

export type ServerAnalyticsWeekday = {
  weekday: string;
  min: number;
  value: number;
  max: number;
};

export type ServerAnalyticsResponse = {
  serverBmId: string;
  serverName: string;
  timeframe: ServerAnalyticsTimeframe;
  start: string;
  stop: string;
  points: ServerAnalyticsPoint[];
  weekdays: ServerAnalyticsWeekday[];
  cachedAt: string;
  cacheWindow: string;
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

export type PlayerSearchResult = {
  serverBmId: string;
  serverName: string;
  playerBmId: string;
  steamId: string;
  bmName: string;
  isOnline: boolean;
};

export type PlayerSearchListResponse = {
  items: PlayerSearchResult[];
};

export type BulkHardLinkEntry = {
  steamId: string;
  bmId?: string | null;
  discordId: string;
  source: string;
};

export type PlatformSyncResult = {
  synced: boolean;
  reason?: string;
  lastSyncedAt: string;
  platform: "steam" | "discord";
  platformId: string;
  linked: boolean;
  steamId?: string;
  discordId?: string;
  previousSteamIds?: string[];
  previousDiscordIds?: string[];
  hardLinkCount: number;
};

export type UserBan = {
  discordId: string;
  categories: string[];
  /**
   * The linked app_user (matched by discord_id on the website) is currently
   * suspended. The bot treats this as a blanket ban across every category.
   */
  suspended?: boolean;
};

export type TrackGroup = {
  id: number;
  guildId: string;
  name: string;
  serverBmId: string;
  channelId: string;
  messageId: string;
  sendUpdates: boolean;
  pingAlertRoleOnEmpty: boolean;
  onlineCount: number;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
};

export type TrackGroupListResponse = {
  items: TrackGroup[];
};

export type TrackGroupMember = {
  trackGroupId: number;
  steamId: string;
  bmId: string;
  createdAt: string;
};

export type TrackGroupMemberListResponse = {
  items: TrackGroupMember[];
};

export type TrackGroupOnlineRow = {
  steamId: string;
  bmId: string;
  bmName: string;
  discordId: string;
  avatarImage: string;
  isOnline: boolean;
  onlineUnix: number;
};

export type TrackGroupOnlineStateResponse = {
  items: TrackGroupOnlineRow[];
};

export type TrackGroupAnalyticsHour = {
  hourUnix: number;
  hour: number;
  avg: number;
  minutes: number;
};

export type TrackGroupAnalyticsDay = {
  weekday: string;
  bestStart: number;
  bestEnd: number;
  hours: TrackGroupAnalyticsHour[];
};

export type TrackGroupAnalyticsResponse = {
  groupName: string;
  serverName: string;
  timeZone: string;
  members: number;
  analyzed: number;
  days: TrackGroupAnalyticsDay[];
};

export type CommandUsageTarget = {
  kind:         "user" | "role" | "channel" | "guild" | "steam";
  snowflake:    string;
  displayName?: string | null;
  avatarHash?:  string | null;
  relation:     "recipient" | "scope" | "subject";
};

export type CommandUsagePayload = {
  discordId:           string;
  guildId:             string | null;
  channelId:           string | null;
  interactionType:     "command" | "button" | "modal" | "string_select";
  command:             string;
  subcommand:          string | null;
  target:              string | null;
  options:             Record<string, unknown>;
  outcome:             "success" | "error" | "banned" | "denied";
  errorCode:           string | null;
  latencyMs:           number;
  // Per-invocation id (a-z, A-Z, 0-9, -, _; <=64 chars). Embedded by the bot
  // in any modal/button/select customId it builds so follow-up submits can
  // round-trip it back as parentCorrelationId.
  correlationId?:      string | null;
  // When set, the backend resolves this to the most recent command_usage row
  // for the same user with a matching correlationId and stores its id as
  // parent_usage_id so the admin UI can fold follow-ups into the parent row.
  parentCorrelationId?: string | null;
  // Where the interaction was dispatched. The backend uses this so the
  // admin UI can render the right kind of label/avatar in the Where column.
  channelKind?:        "guild" | "dm" | "group_dm" | "unknown" | null;
  // Display name for non-guild channels (e.g. group DM name). Capped at 100 chars by the backend.
  channelName?:        string | null;
  targets?:            CommandUsageTarget[];
};

type UpsertBotGuildPayload = {
  guildId: string;
  name: string;
  iconHash: string | null;
  bannerHash: string | null;
  ownerDiscordId: string | null;
  ownerUsername: string | null;
  ownerGlobalName: string | null;
  ownerAvatarHash: string | null;
  approximateMemberCount: number | null;
  members?: string[];
  membersSyncComplete?: boolean;
};

type UpsertBotGuildResponse = {
  status: "ok";
  membersSyncedAt: string | null;
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

  private async call<T>(
    workerId: string,
    path: string,
    method: string,
    body?: unknown,
    timeoutMs = 10_000,
  ): Promise<T> {
    const response = await fetch(`${this.backendUrl}${path}`, {
      method,
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${this.token(workerId)}`,
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(timeoutMs),
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
    return this.call<RegisterResponse>(workerId, "/api/internal/bot/coordinator/shards/register", "POST", {
      workerId,
    });
  }

  heartbeat(workerId: string, shardIds: number[], stats?: WorkerStatSnapshot) {
    const body: Record<string, unknown> = { workerId, shardIds };
    if (stats) {
      body.stats = stats;
    }
    return this.call<{ status: string }>(
      workerId,
      "/api/internal/bot/coordinator/shards/heartbeat",
      "POST",
      body,
    );
  }

  ownerStats(workerId: string) {
    return this.call<OwnerStatsResponse>(workerId, "/api/internal/bot/coordinator/stats", "GET");
  }

  listOwnerProxies(workerId: string) {
    return this.call<OwnerProxyListResponse>(workerId, "/api/internal/bot/servers/owner/proxies", "GET");
  }

  createOwnerProxy(workerId: string, endpoint: string) {
    return this.call<OwnerProxy>(workerId, "/api/internal/bot/servers/owner/proxies", "POST", { endpoint });
  }

  updateOwnerProxy(workerId: string, proxyId: number, enabled: boolean) {
    return this.call<OwnerProxy>(workerId, `/api/internal/bot/servers/owner/proxies/${proxyId}`, "PATCH", { enabled });
  }

  deleteOwnerProxy(workerId: string, proxyId: number) {
    return this.call<{ status: string }>(workerId, `/api/internal/bot/servers/owner/proxies/${proxyId}`, "DELETE");
  }

  release(workerId: string) {
    return this.call<{ status: string }>(workerId, "/api/internal/bot/coordinator/shards/release", "POST", {
      workerId,
    });
  }

  assignments(workerId: string) {
    const query = new URLSearchParams({ workerId }).toString();
    return this.call<AssignmentsResponse>(
      workerId,
      `/api/internal/bot/coordinator/shards/assignments?${query}`,
      "GET",
    );
  }

  steamAccountCount(workerId: string) {
    return this.call<CountResponse>(workerId, "/api/internal/bot/players/steam-accounts/count", "GET");
  }

  guildSettings(workerId: string, guildId: string) {
    return this.call<GuildSettings>(workerId, `/api/internal/bot/guilds/${guildId}/settings`, "GET");
  }

  updateGuildSettings(workerId: string, guildId: string, patch: GuildSettingsPatch) {
    return this.call<GuildSettings>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/settings`,
      "PUT",
      patch,
    );
  }

  listActiveWipeReacts(workerId: string, guildId: string) {
    return this.call<WipeReactListResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/active`,
      "GET",
    );
  }

  listWipeReacts(workerId: string, guildId: string) {
    return this.call<WipeReactListResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts`,
      "GET",
    );
  }

  getWipeReact(workerId: string, guildId: string, messageId: string) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/${messageId}`,
      "GET",
    );
  }

  createWipeReact(workerId: string, guildId: string, wipeReact: WipeReact) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts`,
      "POST",
      wipeReact,
    );
  }

  updateWipeReact(workerId: string, guildId: string, messageId: string, wipeReact: WipeReact) {
    return this.call<WipeReact>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/${messageId}`,
      "PUT",
      wipeReact,
    );
  }

  deleteWipeReact(workerId: string, guildId: string, messageId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/${messageId}`,
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
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/${messageId}/reactions`,
      "POST",
      { userId, type },
    );
  }

  removeWipeReactReaction(workerId: string, guildId: string, messageId: string, userId: string) {
    return this.call<WipeReactReactionRemoval>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-reacts/${messageId}/reactions/${userId}`,
      "DELETE",
    );
  }

  listWipeReactSchedules(workerId: string, guildId: string) {
    return this.call<WipeReactScheduleListResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-react-schedules`,
      "GET",
    );
  }

  getWipeReactSchedule(workerId: string, guildId: string, scheduleId: number) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "GET",
    );
  }

  createWipeReactSchedule(workerId: string, guildId: string, schedule: Omit<WipeReactSchedule, "id" | "guildId" | "lastPostedForUnix">) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-react-schedules`,
      "POST",
      schedule,
    );
  }

  updateWipeReactSchedule(workerId: string, guildId: string, scheduleId: number, schedule: Omit<WipeReactSchedule, "id" | "guildId" | "lastPostedForUnix">) {
    return this.call<WipeReactSchedule>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "PUT",
      schedule,
    );
  }

  deleteWipeReactSchedule(workerId: string, guildId: string, scheduleId: number) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/wipe-react-schedules/${scheduleId}`,
      "DELETE",
    );
  }

  claimNextWipeReactSchedulePost(workerId: string, guildId: string) {
	return this.call<{ item: WipeReactSchedulePost | null }>(
	  workerId,
	  `/api/internal/bot/guilds/${guildId}/wipe-react-schedule-posts/claim`,
	  "POST",
	  { workerId },
	);
  }

  completeWipeReactSchedulePost(
	workerId: string,
	guildId: string,
	postId: number,
	messageId: string,
	channelId: string,
  ) {
	return this.call<{ status: string }>(
	  workerId,
	  `/api/internal/bot/guilds/${guildId}/wipe-react-schedule-posts/${postId}/complete`,
	  "POST",
	  { workerId, messageId, channelId },
	);
  }

  releaseWipeReactSchedulePost(workerId: string, guildId: string, postId: number) {
	return this.call<{ status: string }>(
	  workerId,
	  `/api/internal/bot/guilds/${guildId}/wipe-react-schedule-posts/${postId}/release`,
	  "POST",
	  { workerId },
	);
  }

  listTrackedServers(workerId: string) {
    return this.call<TrackedServerListResponse>(
      workerId,
      `/api/internal/bot/servers/tracked-servers`,
      "GET",
    );
  }

  getServerAnalytics(workerId: string, bmId: string, timeframe: ServerAnalyticsTimeframe) {
    const query = new URLSearchParams({ timeframe }).toString();
    return this.call<ServerAnalyticsResponse>(
      workerId,
      `/api/internal/bot/servers/tracked-servers/${bmId}/player-count-history?${query}`,
      "GET",
      undefined,
      30_000,
    );
  }

  createTrackedServer(
    workerId: string,
    payload: Omit<TrackedServer, "lastPolledAt" | "lastPollError">,
  ) {
    return this.call<TrackedServer>(
      workerId,
      `/api/internal/bot/servers/tracked-servers`,
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
      `/api/internal/bot/servers/tracked-servers/${currentBmId}`,
      "PUT",
      payload,
    );
  }

  deleteTrackedServer(workerId: string, bmId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/servers/tracked-servers/${bmId}`,
      "DELETE",
    );
  }

  listGuildTrackedServers(workerId: string, guildId: string) {
    return this.call<GuildTrackedServerListResponse>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/tracked-servers`,
      "GET",
    );
  }

  replaceGuildTrackedServers(workerId: string, guildId: string, bmIds: string[]) {
    return this.call<GuildTrackedServerListResponse>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/tracked-servers`,
      "PUT",
      { bmIds },
    );
  }

  addGuildTrackedServer(workerId: string, guildId: string, bmId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/tracked-servers/${bmId}`,
      "POST",
    );
  }

  removeGuildTrackedServer(workerId: string, guildId: string, bmId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/tracked-servers/${bmId}`,
      "DELETE",
    );
  }

  listOnlinePlayerStatesByServer(workerId: string, serverId: string) {
    return this.call<PlayerStateListResponse>(
      workerId,
      `/api/internal/bot/servers/player-states/servers/${serverId}`,
      "GET",
    );
  }

  searchPlayerStatesByName(
    workerId: string,
    serverId: string,
    query: string,
    limit: number,
    includeOffline = false,
  ) {
    const params = new URLSearchParams({ q: query, limit: String(limit) });
    if (includeOffline) {
      params.set("includeOffline", "true");
    }
    return this.call<PlayerSearchListResponse>(
      workerId,
      `/api/internal/bot/servers/player-states/servers/${serverId}/search?${params.toString()}`,
      "GET",
    );
  }

  getPlayerState(workerId: string, serverId: string, playerId: string) {
    return this.call<PlayerState>(
      workerId,
      `/api/internal/bot/servers/player-states/servers/${serverId}/players/${playerId}`,
      "GET",
    );
  }

  listPlayerStatesByPlayer(workerId: string, playerId: string) {
    return this.call<PlayerStateListResponse>(
      workerId,
      `/api/internal/bot/servers/player-states/players/${playerId}`,
      "GET",
    );
  }

  getSteamAccount(workerId: string, steamId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}`,
      "GET",
    );
  }

  getSteamAccountByBmID(workerId: string, bmId: string) {
    return this.call<SteamAccount>(
      workerId,
      `/api/internal/bot/players/steam/by-bm/${bmId}`,
      "GET",
    );
  }

  getBmPlayerProfile(workerId: string, bmId: string) {
    return this.call<SteamProfile>(
      workerId,
      `/api/internal/bot/players/bm/${bmId}/bm-profile`,
      "GET",
    );
  }

  getBmPlayerAnalytics(workerId: string, bmId: string) {
    return this.call<SteamAnalytics>(
      workerId,
      `/api/internal/bot/players/bm/${bmId}/bm-analytics`,
      "GET",
    );
  }

  getSteamProfile(workerId: string, steamId: string) {
    return this.call<SteamProfile>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}/profile`,
      "GET",
    );
  }

  getSteamAnalytics(workerId: string, steamId: string) {
    return this.call<SteamAnalytics>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}/analytics`,
      "GET",
    );
  }

  getSteamFriends(workerId: string, steamId: string) {
    return this.call<SteamFriendListResponse>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}/friends`,
      "GET",
      undefined,
      30_000,
    );
  }

  getLinksBySteamID(workerId: string, steamId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}/links`,
      "GET",
    );
  }

  getLinksByDiscordID(workerId: string, discordId: string) {
    return this.call<SteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/players/discord/${discordId}/links`,
      "GET",
    );
  }

  isDiscordWebsiteAdmin(workerId: string, discordId: string) {
    return this.call<{ isAdmin: boolean }>(
      workerId,
      `/api/internal/bot/identity/discord/${discordId}/is-website-admin`,
      "GET",
    );
  }

  hasDiscordProRole(workerId: string, discordId: string) {
    return this.call<{ hasRole: boolean }>(
      workerId,
      `/api/internal/bot/identity/discord/${discordId}/has-pro-role`,
      "GET",
    );
  }

  resolveSteamID(workerId: string, input: string) {
    return this.call<ResolveSteamIDResponse>(
      workerId,
      `/api/internal/bot/players/steam/resolve`,
      "POST",
      { input },
    );
  }

  createSoftLink(workerId: string, steamId: string, discordId: string, source: string) {
    return this.call<SteamDiscordLink>(
      workerId,
      `/api/internal/bot/players/steam/links`,
      "POST",
      { steamId, discordId, source },
    );
  }

  createUserGuildLink(workerId: string, discordId: string, guildId: string) {
    return this.call<UserGuildLink>(
      workerId,
      `/api/internal/bot/guilds/user-guilds`,
      "POST",
      { discordId, guildId },
    );
  }

  markUserGuildLeft(workerId: string, discordId: string, guildId: string) {
    return this.call<UserGuildLink>(
      workerId,
      `/api/internal/bot/guilds/user-guilds/leave`,
      "POST",
      { discordId, guildId },
    );
  }

  upsertBotGuild(workerId: string, payload: UpsertBotGuildPayload) {
    return this.call<UpsertBotGuildResponse>(
      workerId,
      `/api/internal/bot/guilds/bot-guilds/upsert`,
      "POST",
      payload,
    );
  }

  markBotGuildLeft(workerId: string, guildId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/bot-guilds/leave`,
      "POST",
      { guildId },
    );
  }

  reconcileBotGuilds(
    workerId: string,
    payload: { shardId: number; shardCount: number; guildIds: string[] },
  ) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/bot-guilds/reconcile`,
      "POST",
      payload,
    );
  }

  touchBotUserInstallSeen(
    workerId: string,
    discordId: string,
    metadata?: {
      username?: string;
      globalName?: string | null;
      avatarHash?: string | null;
    },
  ) {
    const payload: Record<string, unknown> = { discordId };
    if (metadata?.username) payload.username = metadata.username;
    if (metadata?.globalName !== undefined) payload.globalName = metadata.globalName;
    if (metadata?.avatarHash !== undefined) payload.avatarHash = metadata.avatarHash;
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/bot-user-installs/seen`,
      "POST",
      payload,
    );
  }

  ensureAppUser(
    workerId: string,
    payload: {
      discordId:  string;
      username:   string;
      globalName: string | null;
      avatarHash: string | null;
    },
  ): Promise<{ userId: string; created: boolean }> {
    return this.call<{ userId: string; created: boolean }>(
      workerId,
      `/api/internal/bot/identity/users/ensure`,
      "POST",
      {
        discordId:  payload.discordId,
        username:   payload.username,
        globalName: payload.globalName,
        avatarHash: payload.avatarHash,
      },
    );
  }

  async recordCommandUsage(workerId: string, payload: CommandUsagePayload): Promise<void> {
    const response = await fetch(`${this.backendUrl}/api/internal/bot/identity/command-usage`, {
      method:  "POST",
      headers: {
        "content-type": "application/json",
        authorization:  `Bearer ${this.token(workerId)}`,
      },
      body:   JSON.stringify(payload),
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) {
      const message = await response.text();
      const error = new Error(`backend call failed (${response.status}): ${message}`);
      (error as any).status = response.status;
      throw error;
    }
  }

  createGuildLink(workerId: string, guildId: string, discordId: string, steamId: string) {
    return this.call<GuildSteamDiscordLink>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/links`,
      "POST",
      { discordId, steamId },
    );
  }

  deleteGuildLink(workerId: string, guildId: string, discordId: string, steamId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/discord/${discordId}/steam/${steamId}`,
      "DELETE",
    );
  }

  bulkUpsertHardLinks(workerId: string, links: BulkHardLinkEntry[]) {
    return this.call<{ status: string; count: number }>(
      workerId,
      `/api/internal/bot/players/steam/bulk-hard-links`,
      "POST",
      { links },
    );
  }

  syncPlatformIdentity(workerId: string, platform: "steam" | "discord", id: string) {
    return this.call<PlatformSyncResult>(
      workerId,
      `/api/internal/bot/players/platform-sync/${platform}/${id}`,
      "POST",
      undefined,
      30_000,
    );
  }

  deleteSoftLink(workerId: string, steamId: string, discordId: string) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/players/steam/${steamId}/links/${discordId}/soft`,
      "DELETE",
    );
  }

  listGuildLinks(workerId: string, guildId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/links`,
      "GET",
    );
  }

  listGuildNicknameTargets(workerId: string, guildId: string) {
    return this.call<GuildNicknameTargetListResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/nickname-targets`,
      "GET",
    );
  }

  listUserGuildLinksByDiscord(workerId: string, discordId: string) {
    return this.call<UserGuildLinkListResponse>(
      workerId,
      `/api/internal/bot/guilds/discord/${discordId}/guilds`,
      "GET",
    );
  }

  listGuildOnlineSummary(workerId: string, guildId: string) {
    return this.call<GuildOnlineSummaryListResponse>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/online-summary`,
      "GET",
    );
  }

  listGuildRecentGameBans(
    workerId: string,
    guildId: string,
    method: "light" | "deep" = "light",
    timeframe: "30d" | "90d" | "180d" | "all" = "90d",
    scopeDiscordIds: readonly string[] = [],
  ) {
    const query = new URLSearchParams({ method, timeframe });
    for (const discordId of scopeDiscordIds) {
      query.append("scopeDiscordId", discordId);
    }
    return this.call<GuildRecentGameBanListResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/game-bans?${query}`,
      "GET",
      undefined,
      method === "deep" ? 120_000 : 30_000,
    );
  }

  listGuildTrackedServerLogEvents(workerId: string, guildId: string) {
    return this.call<TrackedServerLogEventsResponse>(
      workerId,
      `/api/internal/bot/servers/guilds/${guildId}/tracked-server-log-events`,
      "GET",
    );
  }

  listGuildLinksByDiscord(workerId: string, guildId: string, discordId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/discord/${discordId}/links`,
      "GET",
    );
  }

  listSetSteamCandidates(workerId: string, discordId: string) {
    return this.call<SetSteamCandidateListResponse>(
      workerId,
      `/api/internal/bot/players/discord/${discordId}/setsteam-candidates`,
      "GET",
    );
  }

  listGuildLinksBySteam(workerId: string, guildId: string, steamId: string) {
    return this.call<GuildSteamDiscordLinkListResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/steam/${steamId}/links`,
      "GET",
    );
  }

  getUserBan(workerId: string, discordId: string) {
    return this.call<UserBan>(
      workerId,
      `/api/internal/bot/identity/bans/${discordId}`,
      "GET",
    );
  }

  setUserBan(workerId: string, discordId: string, categories: string[], updatedBy: string) {
    return this.call<UserBan>(
      workerId,
      `/api/internal/bot/identity/bans/${discordId}`,
      "PUT",
      { categories, updatedBy },
    );
  }

  listTrackGroups(workerId: string, guildId: string) {
    return this.call<TrackGroupListResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups`,
      "GET",
    );
  }

  createTrackGroup(
    workerId: string,
    guildId: string,
    payload: {
      name: string;
      serverBmId: string;
      channelId: string;
      messageId: string;
      sendUpdates: boolean;
      pingAlertRoleOnEmpty: boolean;
      onlineCount: number;
      createdBy: string;
    },
  ) {
    return this.call<TrackGroup>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups`,
      "POST",
      payload,
    );
  }

  getTrackGroup(workerId: string, guildId: string, groupId: number) {
    return this.call<TrackGroup>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}`,
      "GET",
    );
  }

  updateTrackGroup(
    workerId: string,
    guildId: string,
    groupId: number,
    patch: {
      channelId?: string;
      messageId?: string;
      sendUpdates?: boolean;
      pingAlertRoleOnEmpty?: boolean;
      onlineCount?: number;
    },
  ) {
    return this.call<TrackGroup>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}`,
      "PUT",
      patch,
    );
  }

  deleteTrackGroup(workerId: string, guildId: string, groupId: number) {
    return this.call<{ status: string }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}`,
      "DELETE",
    );
  }

  listTrackGroupMembers(workerId: string, guildId: string, groupId: number) {
    return this.call<TrackGroupMemberListResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}/members`,
      "GET",
    );
  }

  addTrackGroupMembers(
    workerId: string,
    guildId: string,
    groupId: number,
    members: { steamId: string; bmId: string }[],
  ) {
    return this.call<{ added: string[]; count: number }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}/members`,
      "POST",
      { members },
    );
  }

  removeTrackGroupMembers(workerId: string, guildId: string, groupId: number, identifiers: string[]) {
    return this.call<{ count: number }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}/members/remove`,
      "POST",
      { identifiers },
    );
  }

  listTrackGroupOnlineState(workerId: string, guildId: string, groupId: number) {
    return this.call<TrackGroupOnlineStateResponse>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}/online-state`,
      "GET",
    );
  }

  claimTrackGroupNotification(
    workerId: string,
    guildId: string,
    groupId: number,
    payload: { steamId: string; isOnline: boolean; onlineUnix: number },
  ) {
    return this.call<{ claimed: boolean }>(
      workerId,
      `/api/internal/bot/guilds/${guildId}/track-groups/${groupId}/notifications/claim`,
      "POST",
      { ...payload, workerId },
    );
  }

  getTrackGroupAnalytics(workerId: string, guildId: string, groupId: number) {
    return this.call<TrackGroupAnalyticsResponse>(
      workerId,
      `/api/internal/bot/players/guilds/${guildId}/track-groups/${groupId}/analytics`,
      "GET",
      undefined,
      60_000,
    );
  }

  getPlayerOfflineAnalytics(workerId: string, bmId: string) {
    return this.call<TrackGroupAnalyticsResponse>(
      workerId,
      `/api/internal/bot/players/bm/${bmId}/offline-analytics`,
      "GET",
      undefined,
      60_000,
    );
  }

  consumeRateLimit(
    workerId: string,
    payload: {
      discordId: string;
      command: string;
      guildId?: string;
      memberCount?: number;
    },
  ) {
    return this.call<RateLimitConsumeResponse>(
      workerId,
      "/api/internal/bot/coordinator/rate-limit/consume",
      "POST",
      payload,
      2_000,
    );
  }
}

export type RateLimitFraudAlert = {
  guildId: string;
  command: string;
  count: number;
  memberCount: number;
  windowSecs: number;
};

export type RateLimitConsumeResponse = {
  allowed: boolean;
  retryAfterSeconds: number;
  violationLevel?: number;
  fraudAlert?: RateLimitFraudAlert;
};

export type { UpsertBotGuildPayload, UpsertBotGuildResponse };
