#!/usr/bin/env bash
set -euo pipefail

SUDO=""
if [[ "${EUID}" -ne 0 ]]; then
  SUDO="sudo"
fi

echo "[1/6] Installing base dependencies"
${SUDO} apt-get update
${SUDO} apt-get install -y ca-certificates curl gnupg lsb-release git docker.io docker-compose-plugin

echo "[2/6] Installing Node.js LTS"
curl -fsSL https://deb.nodesource.com/setup_20.x | ${SUDO} -E bash -
${SUDO} apt-get install -y nodejs

echo "[3/6] Enabling Docker"
${SUDO} systemctl enable docker
${SUDO} systemctl start docker
${SUDO} usermod -aG docker "${USER}"

echo "[4/6] Enabling corepack + pnpm"
if command -v corepack >/dev/null 2>&1; then
  corepack enable
  corepack prepare pnpm@10.6.0 --activate
else
  npm install -g pnpm@10.6.0
fi

echo "[5/6] Installing Rust (rustup)"
if ! command -v cargo >/dev/null 2>&1; then
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  # shellcheck disable=SC1090
  source "${HOME}/.cargo/env"
fi

echo "[6/6] Version check"
docker --version
node --version
pnpm --version
cargo --version
git --version

echo "Done"
echo "Open a new shell (or run: newgrp docker) to apply docker group changes, then run: pnpm run setup && pnpm dev"
