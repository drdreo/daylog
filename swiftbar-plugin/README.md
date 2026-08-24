# Daylog — macOS menu bar widget

A menu bar widget for macOS, as a [SwiftBar](https://swiftbar.app) plugin
(the output is xbar-compatible). One icon, one menu: open todos with agent
proposals to triage, today's entries, and every open PR with live
checks/review state — the macOS sibling of the Omarchy bar widget.

Like its sibling, this is deliberately the thinnest component in the system
(ARCHITECTURE.md §9): it shells out to `daylog today --json` and renders the
result. All state, folding, and PR joining happen in the CLI — the widget is
a dumb consumer and is replaceable in an afternoon. It is a single JXA
(JavaScript for Automation) script, so it needs nothing beyond what macOS
ships — no python, no jq.

## What it shows

- **Menu bar icon** — the day's note (`note.text`), turning red with a count
  when something needs you: an agent proposal awaiting triage, or an open
  PR with failing checks. A warning triangle means the widget itself could
  not load the day.
- **Open todos** — every open todo in one list, yours and the agents'. An
  agent proposal still awaiting your verdict is accented and marked `●`.
- **The day's log** — its entries with time, source, and live PR status on
  entries that reference a PR. Closed todos are checked and dimmed. The
  heading names the day (`TODAY · MON, AUG 24`, `WED, AUG 19 · 5 DAYS AGO`)
  and the `◀`/`▶` rows walk to the days on either side.
- **Open PRs** — the poller snapshot, marked STALE when it is hours old.

## Actions

| Where | Action |
|---|---|
| Untriaged proposal → submenu | **Accept** (`daylog accept <id>` — adopt it as yours; it stays a todo and stops nagging), **Decline** (`daylog decline <id>` — it drops out of every view) |
| Todo row → submenu | **Mark done** (`daylog done <id>`), **Open PR** when the entry references one |
| Day heading | `◀`/`▶` step a day back or forward, `↩ Back to today` returns in one click |
| Today entry / PR row | Click opens the PR in the browser |
| Footer | **Refresh** re-runs `daylog today --json`; **Poll GitHub** runs `daylog poll gh` and re-renders |

Hover any entry for the full detail (source, refs, close note).

## Walking back through days

The `◀`/`▶` rows re-run the same read against another day
(`daylog today 2026-08-19 --json`) and re-render. Only the log section moves:
open todos are obligations that don't expire at midnight and PRs are live
state, so the menu bar icon keeps counting what needs you *now* whichever day
you are reading. Forward stops at today. An empty day says which day it was
empty about, so an untouched Tuesday can't read as a quiet morning.

They are rows and not arrow keys because an open macOS menu owns the arrow
keys for its own row and submenu navigation — a plugin never sees them. (The
Omarchy panel is a real focused window, so there `←`/`→` do this directly.)

The day you picked is remembered in `$TMPDIR/daylog-view-day` — SwiftBar
re-executes the plugin from scratch on every refresh, so it has to live
somewhere — and expires after 10 minutes. That is deliberate: this is a
*today* widget, and a menu bar that still reads Tuesday three hours after you
went looking is worse than one that forgets.

A `◀`/`▶` row re-runs *this plugin* with two plain arguments
(`daylog.1m.js view-day 2026-08-19`), which records the choice and lets
SwiftBar's `refresh=true` redraw. Keep it that way: menu lines carry
arguments, never shell commands. A first cut wrote the file with
`bash=/bin/sh param1=-c param2="printf '%s %s' …"`, and the `%s` inside that
param was enough to stop SwiftBar building the menu at all — no error, no
plugin, no menu bar icon.

## Install

Install [SwiftBar](https://swiftbar.app) (`brew install swiftbar`) and pick a
plugin folder on first launch. Then, from the daylog checkout:

```sh
./install.sh --swiftbar      # builds daylog + copies the plugin into SwiftBar
```

Or by hand:

```sh
cp swiftbar-plugin/daylog.1m.js "$(defaults read com.ameba.SwiftBar PluginDirectory)/"
chmod +x "$(defaults read com.ameba.SwiftBar PluginDirectory)/daylog.1m.js"
open -g swiftbar://refreshallplugins
```

For hacking, symlink instead of copying — SwiftBar picks up saved changes on
the next refresh cycle (or `open -g swiftbar://refreshallplugins`):

```sh
ln -s "$(pwd)/swiftbar-plugin/daylog.1m.js" "$(defaults read com.ameba.SwiftBar PluginDirectory)/"
```

The `1m` in the filename is the refresh interval (SwiftBar convention) —
rename to `daylog.30s.js` or `daylog.5m.js` to taste. The plugin also works
under [xbar](https://xbarapp.com): copy it into `~/Library/Application
Support/xbar/plugins/` instead (SF Symbols and per-appearance colors degrade
gracefully there).

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `DAYLOG_PATH` | *(search PATH)* | Absolute path to the daylog CLI |
| `DAYLOG_ICON` | `note.text` | SF Symbol name for the menu bar icon (used for both the calm and the red needs-you state) |

Set it in SwiftBar's per-plugin settings (⌥-click the icon → Settings), or
rely on the built-in search: the plugin looks on `PATH` plus the usual
install locations (`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`,
`~/go/bin`).

## Scheduling the poller

The widget's **Poll GitHub** action polls on demand; for the background
cadence use cron (`crontab -e`):

```
*/10 * * * * $HOME/.local/bin/daylog poll gh
```

or a launchd user agent if you prefer — see the main README.
