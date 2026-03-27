#!/usr/bin/env bash
set -euo pipefail

missing=0

check_cmd() {
  local cmd="$1"
  local hint="$2"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "[missing] $cmd - $hint"
    missing=1
  else
    local version_output=""
    case "$cmd" in
      go)
        version_output="$($cmd version 2>/dev/null | head -n1 || true)"
        ;;
      *)
        version_output="$($cmd --version 2>/dev/null | head -n1 || true)"
        ;;
    esac
    echo "[ok] $cmd: $version_output"
  fi
}

check_cmd docker "Install Docker Desktop/Engine"
check_cmd node "Install Node.js LTS"
check_cmd pnpm "Enable corepack and pnpm"
check_cmd go "Install Go"
check_cmd git "Install Git"
check_cmd make "Install GNU make"

if [[ $missing -ne 0 ]]; then
  echo "One or more required tools are missing."
  exit 1
fi

echo "All required developer tools are installed."
