#!/usr/bin/env pwsh
# Install recoil on Windows.
# Usage: irm https://raw.githubusercontent.com/1broseidon/recoil/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "1broseidon/recoil"
$installDir = "$env:LOCALAPPDATA\recoil"

# Get latest release tag
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
$tag = $release.tag_name
$version = $tag -replace '^v', ''

Write-Host "Installing recoil $version ..." -ForegroundColor Cyan

# Download and verify
$asset = "recoil_${tag}_windows_x86_64.zip"
$url = "https://github.com/$repo/releases/download/$tag/$asset"
$tmp = Join-Path $env:TEMP $asset

Invoke-WebRequest -Uri $url -OutFile $tmp

# Verify against the release's checksums.txt before unpacking anything.
$sumsUrl = "https://github.com/$repo/releases/download/$tag/checksums.txt"
$sums = (Invoke-WebRequest -Uri $sumsUrl).Content
$expected = ($sums -split "`n" | Where-Object { $_ -match [regex]::Escape($asset) }) -split '\s+' | Select-Object -First 1
if (-not $expected) {
    Remove-Item $tmp -ErrorAction SilentlyContinue
    Write-Error "No checksum published for $asset"
    exit 1
}
$actual = (Get-FileHash -Path $tmp -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected.ToLower()) {
    Remove-Item $tmp -ErrorAction SilentlyContinue
    Write-Error "Checksum mismatch for $asset`n  expected $expected`n  actual   $actual"
    exit 1
}

if (Test-Path $installDir) { Remove-Item -Recurse -Force $installDir }
New-Item -ItemType Directory -Path $installDir -Force | Out-Null
Expand-Archive -Path $tmp -DestinationPath $installDir -Force
Remove-Item $tmp

# Verify binary exists
$bin = Join-Path $installDir "recoil.exe"
if (-not (Test-Path $bin)) {
    Write-Error "Failed to install: recoil.exe not found in archive"
    exit 1
}

# Record install metadata for update guidance.
$installMeta = Join-Path $installDir "install.json"
$meta = @{
    install_type = "powershell"
} | ConvertTo-Json
Set-Content -Path $installMeta -Value $meta -Encoding UTF8

# Add to user PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$installDir;$userPath", "User")
    Write-Host "Added $installDir to user PATH." -ForegroundColor Yellow
    Write-Host "Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
}

Write-Host "recoil $version installed to $installDir" -ForegroundColor Green
Write-Host "Next: run 'recoil setup' at a project root." -ForegroundColor Gray
