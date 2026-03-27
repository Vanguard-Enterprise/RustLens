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
}