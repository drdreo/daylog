# Daylog — macOS menu bar widget

A menu bar widget for macOS, as a [SwiftBar](https://swiftbar.app) plugin
(the output is xbar-compatible). One icon, one menu: today's entries, open
todos, the agent inbox awaiting triage, and every open PR with live
checks/review state — the macOS sibling of the Omarchy bar widget.

Like its sibling, this is deliberately the thinnest component in the system
(ARCHITECTURE.md §9): it shells out to `daylog today --json` and renders the
result. All state, folding, and PR joining happen in the CLI — the widget is
a dumb consumer and is replaceable in an afternoon. It is a single JXA
(JavaScript for Automation) script, so it needs nothing beyond what macOS
ships — no python, no jq.

## What it shows

- **Menu bar icon** — the day's note (`note.text`), turning red with a count
  when something needs you: untriaged agent todos in the inbox, or an open
  PR with failing checks. A warning triangle means the widget itself could
  not load the day.
- **Agent inbox** — todos filed by agents, awaiting your triage.
- **Open todos** — your own open obligations, any day.
- **Today** — the day's entries with time, source, and live PR status on
  entries that reference a PR. Closed todos are checked and dimmed.
- **Open PRs** — the poller snapshot, marked STALE when it is hours old.

## Actions

| Where | Action |
|---|---|
| Inbox / todo row → submenu | **Mark done** (`daylog done <id>` — dismisses an inbox proposal or finishes your own), **Open PR** when the entry references one |
| Today entry / PR row | Click opens the PR in the browser |
| Footer | **Refresh** re-runs `daylog today --json`; **Poll GitHub** runs `daylog poll gh` and re-renders |

Hover any entry for the full detail (source, refs, close note).

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
