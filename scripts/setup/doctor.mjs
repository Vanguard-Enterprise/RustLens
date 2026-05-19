#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

const isWin = process.platform === 'win32';
const result = isWin
  ? spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', 'scripts/setup/doctor.ps1'], { stdio: 'inherit' })
  : spawnSync('bash', ['scripts/setup/doctor.sh'], { stdio: 'inherit' });

process.exit(result.status ?? 1);
