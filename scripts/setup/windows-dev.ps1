[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Require-Admin {
  $currentUser = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
  if (-not $currentUser.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this script as Administrator."
  }
}

function Install-WingetPackage([string]$Id) {
  Write-Host "Installing $Id..."
  winget install --id $Id -e --accept-package-agreements --accept-source-agreements
}

Require-Admin

Install-WingetPackage "Docker.DockerDesktop"
Install-WingetPackage "OpenJS.NodeJS.LTS"
Install-WingetPackage "Rustlang.Rustup"
Install-WingetPackage "Git.Git"

Write-Host "Enabling corepack and pnpm"
if (Get-Command corepack -ErrorAction SilentlyContinue) {
  corepack enable
  corepack prepare pnpm@10.6.0 --activate
}
else {
  npm install -g pnpm@10.6.0
}

Write-Host "Version check"
docker --version
node --version
pnpm --version
cargo --version
git --version

Write-Host "Done. Restart your terminal, then run: pnpm run setup && pnpm dev"
