import { ChildProcess, fork } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

export class ShardRuntime {
  private readonly children = new Map<number, ChildProcess>();

  constructor(private readonly token: string) {}

  activeShards() {
    return [...this.children.keys()].sort((a, b) => a - b);
  }

  start(shardID: number, totalShards: number) {
    if (this.children.has(shardID)) {
      return;
    }

    const distEntry = resolve(process.cwd(), "dist/shard-worker.js");
    const sourceEntry = resolve(process.cwd(), "src/shard-worker.ts");
    const useDist = existsSync(distEntry);

    const child = fork(useDist ? distEntry : sourceEntry, [], {
      stdio: "inherit",
      execArgv: useDist ? [] : ["--loader", "tsx"],
      env: {
        ...process.env,
        SHARD_ID: String(shardID),
        SHARD_COUNT: String(totalShards),
        DISCORD_TOKEN: this.token,
      },
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
