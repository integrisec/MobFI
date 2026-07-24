<#
.SYNOPSIS
  MobFI installer for Windows.

.DESCRIPTION
  Resolves everything a newcomer needs - the Go toolchain, a C compiler (Wails
  needs cgo on Windows), the Wails CLI, the WebView2 runtime, and the runtime
  tools MobFI shells out to (Android platform-tools / adb; libimobiledevice for
  iOS) - then builds the CLI and GUI and creates Start Menu / Desktop shortcuts
  for the GUI. Uses winget, and bootstraps scoop for the tools winget lacks
  (gcc, libimobiledevice). Safe to re-run: anything already present is skipped.

  The GUI Console tab uses the Windows ConPTY backend, so it works on
  Windows 10 1809+.

.PARAMETER CliOnly        Build the CLI only (skip Wails/GUI).
.PARAMETER GuiOnly        Build the GUI only (skip the standalone CLI).
.PARAMETER NoRuntimeTools Skip adb/libimobiledevice (toolchain + build only).
.PARAMETER NoShortcuts    Do not create Start Menu / Desktop shortcuts.
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
  [switch]$NoShortcuts,
  [ValidateSet('cli', 'gui')][string]$Launch
)

$ErrorActionPreference = 'Stop'
$GoMin = [version]'1.23'
$Root = Split-Path -Parent $PSScriptRoot   # repo root (scripts\..)
$script:GuiExe = $null                      # set by Build-Gui when it succeeds

function Step($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Ok($m)   { Write-Host "  [ok] $m"   -ForegroundColor Green }
function Warn($m) { Write-Host "  [!] $m"    -ForegroundColor Yellow }
function Have($c) { [bool](Get-Command $c -ErrorAction SilentlyContinue) }

# Run external tools (scoop, wails, the scoop bootstrap) without our global
# 'Stop' preference turning their benign stderr / cleanup errors into a
# script-aborting terminating error. Scoop runs in-process and inherits our
# preference, so a locked temp file during a dependency install would otherwise
# kill the whole installer. Callers verify success via Have / exit code.
function Invoke-Native([scriptblock]$Block) {
  $prev = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { & $Block } finally { $ErrorActionPreference = $prev }
}

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

# Install scoop (a per-user package manager) for the tools winget lacks. Scoop
# refuses to install from an elevated shell, so bail out with guidance there.
function Ensure-Scoop {
  Update-Path
  if (Have scoop) { return $true }
  $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
  if ($admin) {
    Warn "scoop will not install from an Administrator shell."
    Warn "Re-run in a normal (non-admin) PowerShell, or install scoop by hand: https://scoop.sh"
    return $false
  }
  Step "Bootstrapping scoop (per-user package manager)"
  try {
    $ep = Get-ExecutionPolicy -Scope CurrentUser
    if ($ep -eq 'Restricted' -or $ep -eq 'AllSigned' -or $ep -eq 'Undefined') {
      Set-ExecutionPolicy -Scope CurrentUser RemoteSigned -Force
    }
    Invoke-Native { Invoke-Expression (Invoke-RestMethod -Uri 'https://get.scoop.sh') }
    Update-Path
  } catch {
    Warn "scoop bootstrap failed: $($_.Exception.Message)"
    return $false
  }
  if (Have scoop) { Ok "scoop installed"; return $true }
  Warn "scoop still not on PATH; open a new terminal or see https://scoop.sh"
  return $false
}

# Wails links the WebView2 loader via cgo on Windows, so a C compiler is
# required to build the GUI (the CLI is cgo-free and needs none).
function Ensure-CCompiler {
  Update-Path
  if ((Have gcc) -or (Have clang) -or (Have cc)) { Ok "C compiler present"; return $true }
  Step "Installing a C compiler (Wails needs cgo/gcc on Windows)"
  if (Ensure-Scoop) { Invoke-Native { & scoop install gcc 2>&1 | Out-Null }; Update-Path }
  if (Have gcc) { Ok "gcc"; return $true }
  Warn "No C compiler found; the GUI build needs one (e.g. 'scoop install gcc')."
  Warn "Run 'wails doctor' to check your GUI toolchain."
  return $false
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
    $wv = (& wails version 2>$null | Where-Object { $_ -match '\S' } | Select-Object -First 1)
    Ok "Wails $wv"
  } else {
    Step "Installing the Wails CLI"
    & go install github.com/wailsapp/wails/v2/cmd/wails@latest
    Update-Path
    if (Have wails) { Ok "Wails installed" } else { Warn "wails not on PATH; add %USERPROFILE%\go\bin to PATH" }
  }
  Ensure-CCompiler | Out-Null
}

function Ensure-RuntimeTools {
  if ($NoRuntimeTools) { Warn "skipping runtime tools (-NoRuntimeTools)"; return }
  Step "Device tools (Android adb + iOS libimobiledevice)"

  if (Have adb) { Ok "adb" } else { if (Winget-Install 'Google.PlatformTools') { Update-Path; Ok "adb (platform-tools)" } }

  # iOS: libimobiledevice is not on winget, so bootstrap scoop and install it.
  if (Have idevice_id) {
    Ok "libimobiledevice"
  } else {
    if (Ensure-Scoop) {
      Step "Installing libimobiledevice via scoop"
      Invoke-Native { & scoop install libimobiledevice 2>&1 | Out-Null }
      Update-Path
    }
    if (Have idevice_id) { Ok "libimobiledevice" } else { Warn "libimobiledevice still unavailable (see README for a prebuilt bundle)" }
  }

  # MobFI also shells out to these; warn if the scoop port omitted either.
  foreach ($t in @('ideviceinstaller', 'afcclient')) {
    if (Have $t) { Ok $t } else { Warn "$t not found - some iOS features need it (see README for a prebuilt bundle)" }
  }

  # Even with the tools installed, iOS on Windows needs Apple's USB stack.
  Warn "iOS on Windows also needs Apple's USB driver + Apple Mobile Device Service:"
  Warn "  install iTunes from apple.com, plug in the device, then tap 'Trust'."
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
  $binDir = Join-Path $Root 'cmd\mfi-gui\build\bin'

  # Wails links WebView2 via cgo. On Windows/ARM64 a native build needs an
  # aarch64 compiler; scoop's gcc is x86-64, so unless a native aarch64
  # toolchain is present we build a windows/amd64 binary, which runs under
  # Windows-on-ARM's x64 emulation.
  $arch = (& go env GOARCH 2>$null)
  $platformArgs = @()
  if ($arch -eq 'arm64' -and -not (Have 'aarch64-w64-mingw32-gcc')) {
    Warn "ARM64 host without an aarch64 compiler: building the GUI as windows/amd64 (runs under emulation)."
    $platformArgs = @('-platform', 'windows/amd64')
  }

  Push-Location (Join-Path $Root 'cmd\mfi-gui')
  try { Invoke-Native { & wails build @platformArgs } } finally { Pop-Location }
  $exe = Get-ChildItem -Path $binDir -Filter *.exe -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($exe) {
    $script:GuiExe = $exe.FullName
    Ok "GUI: $($exe.FullName)"
  } else {
    Warn "GUI build produced no .exe in $binDir."
    Warn "This usually means a missing C compiler - run 'wails doctor', then 'scoop install gcc'."
  }
}

# Create per-user Start Menu and Desktop shortcuts to the built GUI (no admin
# rights needed; both live under the user's profile).
function New-Shortcuts {
  if (-not $script:GuiExe) { return }
  if ($NoShortcuts) { Warn "skipping shortcuts (-NoShortcuts)"; return }
  Step "Creating shortcuts"
  $targets = @(
    (Join-Path ([Environment]::GetFolderPath('Programs')) 'MobFI.lnk'),
    (Join-Path ([Environment]::GetFolderPath('Desktop'))  'MobFI.lnk')
  )
  $ws = New-Object -ComObject WScript.Shell
  foreach ($lnkPath in $targets) {
    try {
      $lnk = $ws.CreateShortcut($lnkPath)
      $lnk.TargetPath = $script:GuiExe
      $lnk.WorkingDirectory = (Split-Path $script:GuiExe)
      $lnk.IconLocation = "$($script:GuiExe),0"
      $lnk.Description = 'MobFI - Mobile Filesystem Inspector'
      $lnk.Save()
      Ok $lnkPath
    } catch {
      Warn "could not create $lnkPath : $($_.Exception.Message)"
    }
  }
}

# --- main --------------------------------------------------------------------
Write-Host "MobFI installer - Windows`n" -ForegroundColor White
$buildCli = -not $GuiOnly
$buildGui = -not $CliOnly

Ensure-Go
if ($buildGui) { Ensure-GuiToolchain }
Ensure-RuntimeTools
if ($buildCli) { Build-Cli }
if ($buildGui) { Build-Gui; New-Shortcuts }

Write-Host ""
Step "Done"
if ($buildCli) { Write-Host "  CLI:  $Root\bin\mfi.exe   (try: .\bin\mfi.exe detect)" }
if ($buildGui) {
  if ($script:GuiExe) { Write-Host "  GUI:  $script:GuiExe   (also on the Start Menu / Desktop)" }
  else { Write-Host "  GUI:  build produced no .exe - run 'wails doctor', then re-run this script" }
}
Write-Host "  If go / wails are not found in new terminals, add these to PATH:"
Write-Host "    C:\Program Files\Go\bin  and  %USERPROFILE%\go\bin"

switch ($Launch) {
  'cli' { Step "Launching the CLI"; & "$Root\bin\mfi.exe" }
  'gui' {
    Step "Launching the GUI"
    $exe = $script:GuiExe
    if (-not $exe) {
      $found = Get-ChildItem -Path (Join-Path $Root 'cmd\mfi-gui\build\bin') -Filter *.exe -ErrorAction SilentlyContinue | Select-Object -First 1
      if ($found) { $exe = $found.FullName }
    }
    if ($exe) { & $exe } else { Push-Location (Join-Path $Root 'cmd\mfi-gui'); try { & wails dev } finally { Pop-Location } }
  }
}
