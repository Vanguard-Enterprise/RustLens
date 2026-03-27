export function computeShardDelta(current: number[], target: number[]) {
  const currentSet = new Set(current);
  const targetSet = new Set(target);

  const toStart = [...targetSet].filter((shardId) => !currentSet.has(shardId)).sort((a, b) => a - b);
  const toStop = [...currentSet].filter((shardId) => !targetSet.has(shardId)).sort((a, b) => a - b);

  return { toStart, toStop };
}