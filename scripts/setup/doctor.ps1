$ErrorActionPreference = "Stop"

$missing = $false

function Check-Cmd([string]$Name, [string]$Hint) {
  $cmd = Get-Command $Name -ErrorAction SilentlyContinue
  if (-not $cmd) {
    Write-Host "[missing] $Name - $Hint"
    $script:missing = $true
    return
  }

  $version = ""
  try {
    if ($Name -eq "go") {
      $version = & go version 2>$null | Select-Object -First 1
    }
    else {
      $version = & $Name --version 2>$null | Select-Object -First 1
    }
  }
  catch {}

  Write-Host "[ok] $Name $version"
}

Check-Cmd "docker" "Install Docker Desktop"
Check-Cmd "node" "Install Node.js LTS"
Check-Cmd "pnpm" "Enable corepack and pnpm"
Check-Cmd "go" "Install Go"
Check-Cmd "git" "Install Git"
Check-Cmd "make" "Install GNU make"

if ($missing) {
  throw "One or more required developer tools are missing."
}

Write-Host "All required developer tools are installed."
