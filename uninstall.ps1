#!/usr/bin/env pwsh
# Uninstall recoil on Windows.
# Usage: irm https://raw.githubusercontent.com/1broseidon/recoil/main/uninstall.ps1 | iex
#
# By default removes the binary and PATH entry but keeps your memories.
# Pass -Purge to also delete the memory database and state directory.

param(
    [switch]$Purge
)

$ErrorActionPreference = "Stop"

$installDir = "$env:LOCALAPPDATA\recoil"

# recoil resolves its state directory from os.UserConfigDir(), which on Windows
# is %AppData% (roaming) — not the %LOCALAPPDATA% the binary installs into.
# RECOIL_DB overrides the database path, so honour it when it is set.
$stateDir = Join-Path $env:AppData "recoil"

# Remove from user PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -like "*$installDir*") {
    $newPath = ($userPath -split ";" | Where-Object { $_ -ne $installDir }) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "Removed $installDir from user PATH." -ForegroundColor Yellow
}

# Remove binary
$bin = Join-Path $installDir "recoil.exe"
if (Test-Path $bin) {
    Remove-Item -Force $bin
    Write-Host "Removed recoil.exe." -ForegroundColor Yellow
} else {
    Write-Host "recoil.exe not found — may already be uninstalled." -ForegroundColor Gray
}

# Remove the install directory if it is now empty.
if (Test-Path $installDir) {
    $meta = Join-Path $installDir "install.json"
    if (Test-Path $meta) { Remove-Item -Force $meta }
    $remaining = Get-ChildItem $installDir -ErrorAction SilentlyContinue
    if (-not $remaining) { Remove-Item -Force $installDir }
}

# Remove memories only when explicitly asked.
if ($Purge) {
    if ($env:RECOIL_DB) {
        Write-Host "RECOIL_DB is set to $env:RECOIL_DB — remove that file yourself if you want it gone." -ForegroundColor Yellow
    }
    if (Test-Path $stateDir) {
        Remove-Item -Recurse -Force $stateDir
        Write-Host "Removed memory database and state at $stateDir." -ForegroundColor Yellow
    } else {
        Write-Host "No state directory at $stateDir." -ForegroundColor Gray
    }
} else {
    Write-Host "Memories kept at $stateDir (run with -Purge to remove)." -ForegroundColor Gray
}

Write-Host "recoil uninstalled." -ForegroundColor Green
