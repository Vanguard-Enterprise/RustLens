$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repoRoot

function Stop-ListenerOnPort {
  param(
    [int]$Port
  )

  $listeners = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique

  foreach ($listenerPid in $listeners) {
    if ($listenerPid -and $listenerPid -ne $PID) {
      try {
        Stop-Process -Id $listenerPid -Force -ErrorAction Stop
        Write-Host "[dev] stopped stale listener on port $Port (pid=$listenerPid)" -ForegroundColor Yellow
      } catch {
        Write-Warning "failed to stop process ${listenerPid} on port ${Port}: $($_.Exception.Message)"
      }
    }
  }
}

function Stop-RustLensDevProcess {
  param(
    [string]$Pattern
  )

  $matches = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
    Where-Object { $_.CommandLine -like $Pattern } |
    Select-Object -ExpandProperty ProcessId -Unique

  foreach ($matchPid in $matches) {
    if ($matchPid -and $matchPid -ne $PID) {
      try {
        Stop-Process -Id $matchPid -Force -ErrorAction Stop
      } catch {
        Write-Warning "failed to stop process $matchPid for pattern $Pattern"
      }
    }
  }
}

Write-Host "[dev] clearing stale RustLens dev processes..." -ForegroundColor Cyan
Stop-ListenerOnPort -Port 3000
Stop-ListenerOnPort -Port 8080
Stop-RustLensDevProcess -Pattern "*air@latest -c .air.toml*"

Write-Host "[dev] starting docker infrastructure..." -ForegroundColor Cyan
docker compose -f infra/docker-compose.yml up -d

Write-Host "[dev] starting backend and frontend..." -ForegroundColor Cyan

$backendCommand = "Set-Location '$repoRoot\apps\backend'; go run github.com/air-verse/air@latest -c .air.toml"
$frontendCommand = "pnpm --dir `"$repoRoot\apps\frontend`" dev"

pnpm exec concurrently `
  -k `
  --names "backend,frontend" `
  --prefix "[{name}]" `
  --prefix-colors "blue.bold,green.bold" `
  "powershell -NoProfile -ExecutionPolicy Bypass -Command $backendCommand" `
  $frontendCommand
