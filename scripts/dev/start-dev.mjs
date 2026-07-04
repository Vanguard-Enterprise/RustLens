#!/usr/bin/env node
// Cross-platform dev orchestrator for RustLens.
// Replaces the former PowerShell scripts so `pnpm dev` runs on Linux, macOS,
// and Windows.
//
//   node scripts/dev/start-dev.mjs             # backend + frontend + bot (default)
//   node scripts/dev/start-dev.mjs --mode app  # backend + frontend only
//
// It frees any stale dev listeners, brings up the Docker infra
// (Postgres/Redis/Traefik + Rust services) from infra/docker-compose.yml, then
// runs the app processes together with prefixed, colored output. Ctrl+C stops
// everything (killOthers).

import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { concurrently } from 'concurrently';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..', '..');

const modeIndex = process.argv.indexOf('--mode');
const mode = modeIndex !== -1 ? process.argv[modeIndex + 1] : 'all';

const composeFile = path.join(repoRoot, 'infra', 'docker-compose.yml');
const devPorts = [3000, 8080];

function log(message) {
  console.log(`\x1b[36m[dev]\x1b[0m ${message}`);
}

// Best-effort: free a TCP port left occupied by a crashed previous dev run.
function freePort(port) {
  if (process.platform === 'win32') {
    const out = spawnSync('netstat', ['-ano'], { encoding: 'utf8' });
    if (out.status !== 0 || !out.stdout) return;
    const pids = new Set();
    for (const line of out.stdout.split(/\r?\n/)) {
      if (line.includes('LISTENING') && line.includes(`:${port} `)) {
        const pid = line.trim().split(/\s+/).pop();
        if (pid && pid !== '0') pids.add(pid);
      }
    }
    for (const pid of pids) {
      spawnSync('taskkill', ['/PID', pid, '/F'], { stdio: 'ignore' });
      log(`freed stale listener on port ${port} (pid ${pid})`);
    }
    return;
  }
  // Linux/macOS: lsof lists the PIDs listening on the port.
  const out = spawnSync('lsof', ['-ti', `tcp:${port}`, '-sTCP:LISTEN'], { encoding: 'utf8' });
  if (out.status !== 0 || !out.stdout) return;
  for (const pid of out.stdout.split(/\s+/).filter(Boolean)) {
    if (Number(pid) === process.pid) continue;
    spawnSync('kill', ['-9', pid], { stdio: 'ignore' });
    log(`freed stale listener on port ${port} (pid ${pid})`);
  }
}

log('clearing stale dev listeners...');
for (const port of devPorts) freePort(port);

log('starting docker infrastructure...');
const infra = spawnSync('docker', ['compose', '-f', composeFile, 'up', '-d'], { stdio: 'inherit' });
if (infra.status !== 0) {
  console.error('[dev] docker compose failed to start infra; is Docker running?');
  process.exit(infra.status ?? 1);
}

const commands = [
  {
    name: 'backend',
    command: 'go run github.com/air-verse/air@latest -c .air.toml',
    cwd: path.join(repoRoot, 'apps', 'backend'),
    prefixColor: 'blue',
  },
  {
    name: 'frontend',
    command: 'pnpm dev',
    cwd: path.join(repoRoot, 'apps', 'frontend'),
    prefixColor: 'cyan',
  },
];

if (mode !== 'app') {
  // The bot waits for the backend to answer /api/healthz before it registers.
  const waiter = JSON.stringify(path.join(repoRoot, 'scripts', 'dev', 'wait-for-backend.mjs'));
  commands.push({
    name: 'bot',
    command: `node ${waiter} && pnpm dev`,
    cwd: path.join(repoRoot, 'apps', 'bot'),
    prefixColor: 'magenta',
  });
}

log(`starting ${commands.map((command) => command.name).join(', ')}...`);

const { result } = concurrently(commands, {
  prefix: '[{name}]',
  killOthersOn: ['failure', 'success'],
  restartTries: 0,
});

result.then(
  () => process.exit(0),
  () => process.exit(1),
);
