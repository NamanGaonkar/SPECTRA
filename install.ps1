# ---------------------------------------------------------------------------
# SPECTRA installer (Windows)
#
# One-line remote install (any user, no Go needed) — run in PowerShell:
#   irm https://raw.githubusercontent.com/NamanGaonkar/SPECTRA/main/install.ps1 | iex
#
# Local mode (from a cloned repo): install.bat still works as before.
#
# Env overrides:
#   $env:SPECTRA_VERSION = "v0.1.0"   # pin a specific tag
#   $env:SPECTRA_INSTALL_DIR = "..."  # custom install directory
# ---------------------------------------------------------------------------
$ErrorActionPreference = "Stop"

$Repo = "NamanGaonkar/SPECTRA"
$Dest = if ($env:SPECTRA_INSTALL_DIR) { $env:SPECTRA_INSTALL_DIR }
        else { Join-Path $env:LOCALAPPDATA "Programs\spectra" }

function Say($m) { Write-Host ">> $m" }
function Ok($m)  { Write-Host "[OK] $m" -ForegroundColor Green }
function Die($m) { Write-Host "[!!] $m" -ForegroundColor Red; exit 1 }

# --- resolve tag ------------------------------------------------------------
if ($env:SPECTRA_VERSION) {
    $Tag = $env:SPECTRA_VERSION
} else {
    Say "resolving latest release..."
    try {
        $r = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
                               -Headers @{ "User-Agent" = "spectra-installer" }
        $Tag = $r.tag_name
    } catch { Die "could not reach GitHub API: $_" }
}
if (-not $Tag) { Die "could not determine the latest release tag" }
Ok "target: $Tag"

# --- pick artifact ----------------------------------------------------------
$Art  = "spectra_${Tag}_windows_amd64.exe"
$Base = "https://github.com/$Repo/releases/download/$Tag"
$Url  = "$Base/$Art"

Say "downloading $Art..."
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("spectra-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $pkg = Join-Path $tmp $Art
    try {
        Invoke-WebRequest -Uri $Url -OutFile $pkg -UserAgent "spectra-installer"
    } catch { Die "download failed - check https://github.com/$Repo/releases" }

    # --- verify checksum ----------------------------------------------------
    $sumUrl = "$Base/checksums.txt"
    try {
        Invoke-WebRequest -Uri $sumUrl -OutFile (Join-Path $tmp "checksums.txt") `
                          -UserAgent "spectra-installer"
        $line = Select-String -Path (Join-Path $tmp "checksums.txt") `
                              -Pattern ([regex]::Escape($Art)) | Select-Object -First 1
        if ($line) {
            $want = ($line.Line -split "\s+")[0]
            $got  = (Get-FileHash -Algorithm SHA256 $pkg).Hash.ToLower()
            if ($want.ToLower() -ne $got) { Die "checksum mismatch! got $got want $want" }
            Ok "checksum verified"
        }
    } catch { Say "checksums.txt unavailable; skipping verification" }

    # --- install ------------------------------------------------------------
    New-Item -ItemType Directory -Force -Path $Dest | Out-Null
    $exe = Join-Path $Dest "spectra.exe"
    Copy-Item $pkg $exe -Force
    Ok "installed $exe ($Tag)"
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# --- user PATH ----------------------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $Dest) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$Dest", "User")
    Ok "PATH updated - open a NEW terminal for 'spectra' to be global"
} else {
    Ok "PATH already contains $Dest"
}

Write-Host ""
Write-Host "Next:  spectra --check     # verify browser + Ollama + model"
Write-Host "       spectra             # launch the TUI"
