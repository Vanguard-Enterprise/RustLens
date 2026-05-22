import { loadConfig } from "./config.js";

export function getProxiedRequestConcurrency() {
  return Math.max(1, loadConfig().proxiedRequestConcurrency);
}

export async function mapProxiedRequests<TInput, TOutput>(
  items: readonly TInput[],
  mapper: (item: TInput, index: number) => Promise<TOutput>,
) {
  const concurrency = getProxiedRequestConcurrency();
  if (items.length === 0) {
    return [] as TOutput[];
  }

  if (concurrency <= 1 || items.length === 1) {
    const results: TOutput[] = [];
    for (const [index, item] of items.entries()) {
      results.push(await mapper(item, index));
    }
    return results;
  }

  const results = new Array<TOutput>(items.length);
  let nextIndex = 0;
  // Promise.all rejects on the first worker error, but the other workers
  // keep pulling items until the queue drains — wasting expensive proxied
  // scrapes after the operation has already failed. Stop picking up new
  // work once any worker has thrown.
  let aborted = false;

  async function runWorker() {
    while (!aborted) {
      const currentIndex = nextIndex;
      nextIndex += 1;
      if (currentIndex >= items.length) {
        return;
      }

      try {
        results[currentIndex] = await mapper(items[currentIndex], currentIndex);
      } catch (err) {
        aborted = true;
        throw err;
      }
    }
  }

  await Promise.all(
    Array.from({ length: Math.min(concurrency, items.length) }, () => runWorker()),
  );

  return results;
}
