#!/usr/bin/env bash
set -euo pipefail

SUDO=""
if [[ "${EUID}" -ne 0 ]]; then
  SUDO="sudo"
fi

echo "[1/7] Installing base dependencies"
${SUDO} apt-get update
${SUDO} apt-get install -y ca-certificates curl gnupg lsb-release make git golang-go docker.io docker-compose-plugin

echo "[2/7] Installing Node.js LTS"
curl -fsSL https://deb.nodesource.com/setup_20.x | ${SUDO} -E bash -
${SUDO} apt-get install -y nodejs

echo "[3/7] Enabling Docker"
${SUDO} systemctl enable docker
${SUDO} systemctl start docker
${SUDO} usermod -aG docker "${USER}"

echo "[4/7] Enabling corepack + pnpm"
if command -v corepack >/dev/null 2>&1; then
  corepack enable
  corepack prepare pnpm@10.6.0 --activate
else
  npm install -g pnpm@10.6.0
fi

echo "[5/7] Installing Air (Go hot reload)"
go install github.com/air-verse/air@latest

echo "[6/7] Version check"
docker --version
node --version
pnpm --version
go version
git --version
make --version

echo "[7/7] Done"
echo "Open a new shell (or run: newgrp docker) to apply docker group changes, then run: make setup && make dev"
