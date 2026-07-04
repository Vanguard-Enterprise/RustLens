#!/usr/bin/env node
// Polls the backend health endpoint (through Traefik) until it answers, so the
// bot does not try to register before the backend is up. Exits 0 once healthy,
// 1 after the timeout. Cross-platform (used by scripts/dev/start-dev.mjs).

const url = process.env.BACKEND_HEALTH_URL ?? 'http://localhost/api/healthz';
const timeoutMs = Number(process.env.BACKEND_HEALTH_TIMEOUT_MS ?? 90_000);
const deadline = Date.now() + timeoutMs;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

process.stdout.write(`[dev] waiting for backend at ${url} ...\n`);

while (Date.now() < deadline) {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
    if (res.ok) {
      process.stdout.write(`[dev] backend is ready at ${url}\n`);
      process.exit(0);
    }
  } catch {
    // Backend not reachable yet; keep polling until the deadline.
  }
  await sleep(2000);
}

process.stderr.write(`[dev] backend did not become ready within ${timeoutMs}ms: ${url}\n`);
process.exit(1);
