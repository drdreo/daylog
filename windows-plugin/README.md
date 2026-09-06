# Daylog — Windows tray widget

A Windows PowerShell 5.1/WinForms tray widget consuming **v2** `daylog today --json`. It shows curated narrative, open todos with explicit proposals, and a separate current Open PRs snapshot. Queued/shadow reports are not journal entries.

Narrative time is the required `display_at` in its captured offset. Completed todos retain `filed_at`. Host-qualified PR refs supply narrative links without attaching live PR/CI status to work entries.

## Install

Build the matching binary and [initialize a fresh v2 store](../docs/windows-setup.md). Set per-user environment variables (then restart the widget/login session):

```powershell
[Environment]::SetEnvironmentVariable('DAYLOG_PATH', 'C:\Tools\daylog.exe', 'User')
[Environment]::SetEnvironmentVariable('DAYLOG_DIR', "$env:LOCALAPPDATA\daylog-v2", 'User')
```

From this checkout, run `windows-plugin\install.ps1` to copy the widget, add a Startup shortcut, and launch it. `-NoStartup` skips the shortcut; `-PollTask` explicitly schedules a separate ten-minute GitHub poll. Inspect the script before enabling execution under your local PowerShell policy. Worker setup/scheduling is separate.

Or launch manually:

```powershell
powershell -NoProfile -WindowStyle Hidden -File .\windows-plugin\daylog-tray.ps1
```

Update binary/widget together; unsupported view versions are rejected. Exit the previous instance before restarting (single-instance mutex). If Windows hides the icon, move it out of the notification overflow.

## Settings and actions

- `DAYLOG_PATH`: absolute CLI executable (otherwise common install locations/PATH).
- `DAYLOG_DIR`: absolute fresh store, explicitly passed to all commands.
- `DAYLOG_REFRESH_SEC`: refresh interval, default 60.
- Proposals offer **Accept/Decline**, adopted todos **Mark done**. Actions use `human:widget`.
- Narrative/PR rows open referenced PRs; hover shows details.
- Click the day heading to go back; **Back to today** restores today. The day selection expires after ten minutes. Obligations/PRs remain current.
- Footer: refresh, poll GitHub, exit.

Windows Go builds have been cross-compiled. WinForms rendering, task registration, PowerShell argument handling, ACLs and filesystem crash durability still require **Windows runtime validation**; they are not established by the macOS test run.
