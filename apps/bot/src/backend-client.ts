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

export type GuildSetupState = {
  guildId: string;
  status: "missing" | "pending" | "configured";
  configuredBy: string;
  selectedRoles: string[];
  selectedChannels: string[];
};

export type GuildSettings = {
  guildId: string;
  logChannelId: string;
};

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

type ClaimGuildSetupResponse = GuildSetupState & {
  claimed: boolean;
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
    });

    if (!response.ok) {
      const message = await response.text();
      throw new Error(`backend call failed (${response.status}): ${message}`);
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

  guildSetup(workerId: string, guildId: string) {
    return this.call<GuildSetupState>(workerId, `/api/internal/bot/setup/guilds/${guildId}`, "GET");
  }

  claimGuildSetup(workerId: string, guildId: string) {
    return this.call<ClaimGuildSetupResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/claim`,
      "POST",
    );
  }

  completeGuildSetup(
    workerId: string,
    guildId: string,
    configuredBy: string,
    selectedRoles: string[],
    selectedChannels: string[],
  ) {
    return this.call<GuildSetupState>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/complete`,
      "POST",
      { configuredBy, selectedRoles, selectedChannels },
    );
  }

  resetGuildSetup(workerId: string, guildId: string) {
    return this.call<{ status: string }>(workerId, `/api/internal/bot/setup/guilds/${guildId}/reset`, "POST");
  }

  guildSettings(workerId: string, guildId: string) {
    return this.call<GuildSettings>(workerId, `/api/internal/bot/setup/guilds/${guildId}/settings`, "GET");
  }

  setGuildLogChannel(workerId: string, guildId: string, logChannelId: string) {
    return this.call<GuildSettings>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/settings/log-channel`,
      "POST",
      { logChannelId },
    );
  }

  listActiveWipeReacts(workerId: string, guildId: string) {
    return this.call<WipeReactListResponse>(
      workerId,
      `/api/internal/bot/setup/guilds/${guildId}/wipe-reacts/active`,
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
}
