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

  async function runWorker() {
    while (true) {
      const currentIndex = nextIndex;
      nextIndex += 1;
      if (currentIndex >= items.length) {
        return;
      }

      results[currentIndex] = await mapper(items[currentIndex], currentIndex);
    }
  }

  await Promise.all(
    Array.from({ length: Math.min(concurrency, items.length) }, () => runWorker()),
  );

  return results;
}
