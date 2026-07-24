<#
.SYNOPSIS
  MobFI installer for Windows.

.DESCRIPTION
  Resolves everything a newcomer needs — the Go toolchain, the Wails CLI, the
  WebView2 runtime, and the runtime tools MobFI shells out to (Android
  platform-tools / adb; libimobiledevice for iOS where available) — then builds
  the CLI and GUI. Uses winget; falls back to scoop for iOS tools. Safe to
  re-run: anything already present is left alone.

  The GUI Console tab uses the Windows ConPTY backend, so it works on
  Windows 10 1809+.

.PARAMETER CliOnly        Build the CLI only (skip Wails/GUI).
.PARAMETER GuiOnly        Build the GUI only (skip the standalone CLI).
.PARAMETER NoRuntimeTools Skip adb/libimobiledevice (toolchain + build only).
.PARAMETER Launch         After building, run 'cli' or 'gui'.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\install.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Launch gui
#>
[CmdletBinding()]
param(
  [switch]$CliOnly,
  [switch]$GuiOnly,
  [switch]$NoRuntimeTools,
  [ValidateSet('cli', 'gui')][string]$Launch
)

$ErrorActionPreference = 'Stop'
$GoMin = [version]'1.23'
$Root = Split-Path -Parent $PSScriptRoot   # repo root (scripts\..)

function Step($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Ok($m)   { Write-Host "  [ok] $m"   -ForegroundColor Green }
function Warn($m) { Write-Host "  [!] $m"    -ForegroundColor Yellow }
function Have($c) { [bool](Get-Command $c -ErrorAction SilentlyContinue) }

# Pull the current PATH from the registry so tools installed this run are
# visible without opening a new shell.
function Update-Path {
  $machine = [Environment]::GetEnvironmentVariable('Path', 'Machine')
  $user    = [Environment]::GetEnvironmentVariable('Path', 'User')
  $env:Path = ($machine, $user | Where-Object { $_ }) -join ';'
  $gobin = Join-Path $env:USERPROFILE 'go\bin'
  if (Test-Path $gobin) { $env:Path = "$gobin;$env:Path" }
  $goroot = 'C:\Program Files\Go\bin'
  if (Test-Path $goroot) { $env:Path = "$goroot;$env:Path" }
}

function Winget-Install($id) {
  if (-not (Have winget)) { Warn "winget not found; install '$id' manually"; return $false }
  Step "winget install $id"
  & winget install --id $id -e --accept-source-agreements --accept-package-agreements --silent 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0 -or $LASTEXITCODE -eq -1978335189) { return $true }  # already installed
  Warn "winget could not install '$id' (exit $LASTEXITCODE)"; return $false
}

function Ensure-Go {
  Update-Path
  if (Have go) {
    $v = [version]((& go env GOVERSION) -replace '^go', '')
    if ($v -ge $GoMin) { Ok "Go $v (>= $GoMin)"; return }
    Warn "Go $v is older than $GoMin; upgrading"
  }
  Winget-Install 'GoLang.Go' | Out-Null
  Update-Path
  if (-not (Have go)) { throw "Go install did not take effect; open a new terminal and re-run." }
  Ok "Go $((& go env GOVERSION) -replace '^go','')"
}

function Ensure-GuiToolchain {
  Step "GUI build toolchain (Wails + WebView2)"
  # WebView2 runtime ships with Windows 11 and updated Windows 10; install if absent.
  Winget-Install 'Microsoft.EdgeWebView2Runtime' | Out-Null
  Update-Path
  if (Have wails) {
    Ok "Wails $((& wails version) 2>$null)"
  } else {
    Step "Installing the Wails CLI"
    & go install github.com/wailsapp/wails/v2/cmd/wails@latest
    Update-Path
    if (Have wails) { Ok "Wails installed" } else { Warn "wails not on PATH; add %USERPROFILE%\go\bin to PATH" }
  }
}

function Ensure-RuntimeTools {
  if ($NoRuntimeTools) { Warn "skipping runtime tools (-NoRuntimeTools)"; return }
  Step "Device tools (Android adb + iOS libimobiledevice)"

  if (Have adb) { Ok "adb" } else { if (Winget-Install 'Google.PlatformTools') { Update-Path; Ok "adb (platform-tools)" } }

  # libimobiledevice on Windows is best-effort: try the scoop port if present.
  if (Have idevice_id) {
    Ok "libimobiledevice"
  } elseif (Have scoop) {
    Step "Installing libimobiledevice via scoop"
    & scoop install libimobiledevice 2>&1 | Out-Null
    if (Have idevice_id) { Ok "libimobiledevice" } else { Warn "scoop could not provide libimobiledevice" }
  } else {
    Warn "iOS tools (libimobiledevice) are not auto-installable via winget."
    Warn "For iOS on Windows: install Apple Devices/iTunes (USB driver), then libimobiledevice"
    Warn "  e.g. install scoop (https://scoop.sh) and run: scoop install libimobiledevice"
    Warn "Android (adb) works without this."
  }
}

function Build-Cli {
  Step "Building the CLI -> bin\mfi.exe"
  Push-Location $Root
  try { & go build -o bin\mfi.exe .\cmd\mfi; Ok "bin\mfi.exe" }
  finally { Pop-Location }
}

function Build-Gui {
  Step "Building the GUI (Wails)"
  Update-Path
  if (-not (Have wails)) { Warn "wails unavailable; skipping GUI build"; return }
  Push-Location (Join-Path $Root 'cmd\mfi-gui')
  try { & wails build; Ok "cmd\mfi-gui\build\bin" }
  finally { Pop-Location }
}

# --- main --------------------------------------------------------------------
Write-Host "MobFI installer — Windows`n" -ForegroundColor White
$buildCli = -not $GuiOnly
$buildGui = -not $CliOnly

Ensure-Go
if ($buildGui) { Ensure-GuiToolchain }
Ensure-RuntimeTools
if ($buildCli) { Build-Cli }
if ($buildGui) { Build-Gui }

Write-Host ""
Step "Done"
if ($buildCli) { Write-Host "  CLI:  $Root\bin\mfi.exe   (try: .\bin\mfi.exe detect)" }
if ($buildGui) { Write-Host "  GUI:  $Root\cmd\mfi-gui\build\bin" }
Write-Host "  If go / wails are not found in new terminals, add these to PATH:"
Write-Host "    C:\Program Files\Go\bin  and  %USERPROFILE%\go\bin"

switch ($Launch) {
  'cli' { Step "Launching the CLI"; & "$Root\bin\mfi.exe" }
  'gui' {
    Step "Launching the GUI"
    $exe = Get-ChildItem -Path (Join-Path $Root 'cmd\mfi-gui\build\bin') -Filter *.exe -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($exe) { & $exe.FullName } else { Push-Location (Join-Path $Root 'cmd\mfi-gui'); try { & wails dev } finally { Pop-Location } }
  }
}
