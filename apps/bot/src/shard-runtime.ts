import { ChildProcess, fork } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

type MessageListener = (shardID: number, message: unknown) => void;

export class ShardRuntime {
  private readonly children = new Map<number, ChildProcess>();
  private messageListener: MessageListener | undefined;

  constructor(private readonly token: string, private readonly workerId: string) {}

  activeShards() {
    return [...this.children.keys()].sort((a, b) => a - b);
  }

  onChildMessage(listener: MessageListener) {
    this.messageListener = listener;
  }

  start(shardID: number, totalShards: number) {
    if (this.children.has(shardID)) {
      return;
    }

    const distEntry = resolve(process.cwd(), "dist/shard-worker.js");
    const sourceEntry = resolve(process.cwd(), "src/shard-worker.ts");
    const isDev = process.env.NODE_ENV !== "production";
    const useDist = !isDev && existsSync(distEntry);

    console.log(`[bot] starting shard ${shardID}/${totalShards} from ${useDist ? distEntry : sourceEntry}`);

    const child = fork(useDist ? distEntry : sourceEntry, [], {
      stdio: "inherit",
      execArgv: useDist ? [] : ["--import", "tsx"],
      env: {
        ...process.env,
        SHARD_ID: String(shardID),
        SHARD_COUNT: String(totalShards),
        DISCORD_TOKEN: this.token,
        BOT_WORKER_ID: this.workerId,
      },
    });

    child.on("message", (message) => {
      this.messageListener?.(shardID, message);
    });

    child.on("exit", () => {
      this.children.delete(shardID);
    });

    this.children.set(shardID, child);
  }

  stop(shardID: number) {
    const child = this.children.get(shardID);
    if (!child) {
      return;
    }

    child.kill("SIGTERM");
    this.children.delete(shardID);
  }

  stopAll() {
    for (const shardID of this.activeShards()) {
      this.stop(shardID);
    }
  }
}
