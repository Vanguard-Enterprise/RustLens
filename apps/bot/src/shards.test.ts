import { describe, expect, it } from "vitest";
import { computeShardDelta } from "./shards.js";

describe("computeShardDelta", () => {
  it("returns shard start and stop lists", () => {
    const delta = computeShardDelta([0, 1, 3], [1, 2, 3]);
    expect(delta).toEqual({ toStart: [2], toStop: [0] });
  });
});