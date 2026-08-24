# Daylog — Windows 11 tray widget

A system tray (notification area) widget for Windows 11 — and Windows 10.
One icon, one menu: open todos with agent proposals to triage, today's
entries, and every open PR with live checks/review state — the Windows
sibling of the Omarchy bar widget and the macOS SwiftBar plugin.

Like its siblings, this is deliberately the thinnest component in the system
(ARCHITECTURE.md §9): it shells out to `daylog today --json` and renders the
result. All state, folding, and PR joining happen in the CLI — the widget is
a dumb consumer and is replaceable in an afternoon. It is a single Windows
PowerShell 5.1 script using WinForms, so it needs nothing beyond what
Windows ships — no runtime, no framework, no installer dependencies.

## What it shows

- **Tray icon** — the day's note (drawn to match your light/dark taskbar),
  turning into a red badge with a count when something needs you: an agent
  proposal awaiting triage, or an open PR with failing checks. An orange
  warning triangle means the widget itself could not load the day.
- **Open todos** — every open todo in one list, yours and the agents'. An
  agent proposal still awaiting your verdict is marked `*` and coloured.
- **The day's log** — its entries with time, source, and live PR status on
  entries that reference a PR. Closed todos are checked and dimmed. The
  heading names the day (`TODAY · MON, AUG 24`, `WED, AUG 19 · 5 DAYS AGO`)
  and the `◀`/`▶` rows walk to the days on either side.
- **Open PRs** — the poller snapshot, marked STALE when it is hours old.

## Actions

| Where | Action |
|---|---|
| Tray icon | Left or right click opens the menu |
| Untriaged proposal → submenu | **Accept** (`daylog accept <id>` — adopt it as yours; it stays a todo and stops nagging), **Decline** (`daylog decline <id>` — it drops out of every view) |
| Todo row → submenu | **Mark done** (`daylog done <id>`), **Open PR** when the entry references one |
| Day heading | `◀`/`▶` step a day back or forward, `↩ Back to today` returns in one click |
| Today entry / PR row | Click opens the PR in the browser |
| Footer | **Refresh** re-runs `daylog today --json`; **Poll GitHub** runs `daylog poll gh` and re-renders; **Exit** quits the widget |

Hover any entry for the full detail (source, refs, close note).

## Walking back through days

The `◀`/`▶` rows re-run the same read against another day
(`daylog today 2026-08-19 --json`) and rebuild the menu. Only the log section
moves: open todos are obligations that don't expire at midnight and PRs are
live state, so the tray badge keeps counting what needs you *now* whichever
day you are reading. Forward stops at today. An empty day says which day it
was empty about, so an untouched Tuesday can't read as a quiet morning.

They are rows and not `←`/`→` keys because an open Windows menu owns the
arrow keys for its own row and submenu navigation. (The Omarchy panel is a
real focused window, so there the arrow keys do this directly.) The day you
picked expires after 10 minutes: this is a *today* widget, and a tray icon
that still describes Tuesday three hours later is worse than one that
forgets.

## Install

Install the daylog CLI first (`go install github.com/drdreo/daylog@latest`,
or build `daylog.exe` and put it on `PATH`). Then, from the daylog checkout
in a PowerShell prompt:

```powershell
cd windows-plugin
.\install.ps1              # copies the widget, adds a Startup shortcut, launches it
.\install.ps1 -PollTask    # ...and schedules `daylog poll gh` every 10 minutes
```

That copies `daylog-tray.ps1` to `%LOCALAPPDATA%\daylog\` and creates a
Startup shortcut that runs it hidden. (A brief console flash at login is
normal — that's PowerShell starting windowless.)

Or run it by hand, no install:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File .\daylog-tray.ps1
```

For hacking, just re-run that line against your checkout — **Exit** the
running instance first (it is single-instance; a second launch exits
silently).

Windows hides new tray icons by default: drag the note icon out of the
overflow chevron, or enable it under Settings → Personalization → Taskbar →
Other system tray icons.

## Settings

Environment variables (set per-user via Settings → System → About →
Advanced system settings, or `setx`):

| Variable | Default | Meaning |
|---|---|---|
| `DAYLOG_PATH` | *(search PATH)* | Absolute path to the daylog CLI |
| `DAYLOG_REFRESH_SEC` | `60` | How often to re-run `daylog today --json` |

Without `DAYLOG_PATH`, the widget searches `PATH` plus the usual install
locations (`%LOCALAPPDATA%\Programs\daylog`, `%USERPROFILE%\go\bin`).

## Scheduling the poller

The widget's **Poll GitHub** action polls on demand; for the background
cadence use Task Scheduler — `install.ps1 -PollTask` sets it up, or by hand:

```powershell
schtasks /Create /TN "daylog poll gh" /SC MINUTE /MO 10 /TR "C:\path\to\daylog.exe poll gh"
```
