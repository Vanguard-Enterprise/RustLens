$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repoRoot

$healthUrl = "http://localhost/api/healthz"
$deadline = (Get-Date).AddSeconds(90)

Write-Host "[dev] waiting for backend health before starting bot..." -ForegroundColor Cyan

while ((Get-Date) -lt $deadline) {
  try {
    $response = Invoke-WebRequest -Uri $healthUrl -Method Get -UseBasicParsing -TimeoutSec 5
    if ($response.StatusCode -eq 200) {
      Write-Host "[dev] backend is ready at $healthUrl" -ForegroundColor Green
      break
    }
  } catch {
    Start-Sleep -Seconds 2
  }
}

if ((Get-Date) -ge $deadline) {
  throw "backend did not become ready within 90s: $healthUrl"
}

pnpm --dir "$repoRoot\apps\bot" dev
