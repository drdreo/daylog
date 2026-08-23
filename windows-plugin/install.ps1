# Install the Daylog tray widget on Windows.
#
#   .\install.ps1              copy the widget, add a Startup shortcut, launch it
#   .\install.ps1 -PollTask    ...and schedule `daylog poll gh` every 10 minutes
#   .\install.ps1 -NoStartup   copy + launch only (no Startup shortcut)
#
# The widget needs the daylog CLI itself: `go install github.com/drdreo/daylog@latest`
# (or build daylog.exe and put it on PATH / point DAYLOG_PATH at it).

param(
    [switch]$PollTask,
    [switch]$NoStartup
)
$ErrorActionPreference = 'Stop'

$dest = Join-Path $env:LOCALAPPDATA 'daylog'
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item (Join-Path $PSScriptRoot 'daylog-tray.ps1') $dest -Force
Write-Host "Installed $dest\daylog-tray.ps1"

$powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$argsLine = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$dest\daylog-tray.ps1`""

if (-not $NoStartup) {
    $shell = New-Object -ComObject WScript.Shell
    $lnkPath = Join-Path ([Environment]::GetFolderPath('Startup')) 'Daylog.lnk'
    $lnk = $shell.CreateShortcut($lnkPath)
    $lnk.TargetPath = $powershell
    $lnk.Arguments = $argsLine
    $lnk.WorkingDirectory = $dest
    $lnk.Description = 'Daylog tray widget'
    $lnk.Save()
    Write-Host "Startup shortcut: $lnkPath"
}

if ($PollTask) {
    $daylog = $env:DAYLOG_PATH
    if (-not $daylog) {
        $cmd = Get-Command daylog -ErrorAction SilentlyContinue
        if ($cmd) { $daylog = $cmd.Source }
    }
    if (-not $daylog) {
        $goBin = Join-Path $env:USERPROFILE 'go\bin\daylog.exe'
        if (Test-Path $goBin) { $daylog = $goBin }
    }
    if (-not $daylog) {
        Write-Warning 'daylog.exe not found - skipping the poll task. Install it, then re-run with -PollTask.'
    } else {
        schtasks /Create /F /TN 'daylog poll gh' /SC MINUTE /MO 10 /TR "`"$daylog`" poll gh" | Out-Null
        Write-Host "Scheduled task 'daylog poll gh' (every 10 minutes)"
    }
}

# If the widget is already running, the new instance exits on its own
# (single-instance mutex) - use the tray icon's Exit first to restart it.
Start-Process -FilePath $powershell -ArgumentList $argsLine -WorkingDirectory $dest -WindowStyle Hidden
Write-Host 'Tray widget launched - look for the note icon in the system tray.'
