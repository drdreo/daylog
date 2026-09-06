# Daylog — SwiftBar widget

A dependency-free JXA menu bar plugin for macOS/SwiftBar (xbar-compatible output). It reads **v2** `daylog today --json`, showing curated outcomes, open todos with explicit agent proposals, and a separate Open PRs snapshot. Queued/shadow reports do not appear as entries.

Rows use required `display_at` in its captured offset and `filed_at` for completed todos. Narrative links come from host-qualified PR refs; live checks/review state stays in Open PRs.

## Install

Install SwiftBar (`brew install swiftbar`), launch it and choose its plugin folder. Build the matching CLI and explicitly copy the plugin:

```sh
./install.sh
cp swiftbar-plugin/daylog.1m.js "$(defaults read com.ameba.SwiftBar PluginDirectory)/"
chmod +x "$(defaults read com.ameba.SwiftBar PluginDirectory)/daylog.1m.js"
open -g swiftbar://refreshallplugins
```

Initialize a fresh store and set the plugin's directory below. Update binary and widget together; unsupported view versions are errors. Symlink instead of copying for development. The `1m` filename controls refresh frequency.

## Plugin settings

Set through SwiftBar's per-plugin settings (option-click → Settings):

| Variable | Meaning |
|---|---|
| `DAYLOG_PATH` | Absolute daylog binary; empty searches PATH and common install locations |
| `DAYLOG_DIR` | Absolute fresh v2 store; empty uses CLI platform default |
| `DAYLOG_ICON` | SF Symbol, default `note.text` |

Reads and menu actions explicitly carry the selected directory. Human menu actions use `human:widget`, regardless of inherited agent identity.

## Actions

- Untriaged todos offer **Accept** and **Decline**; adopted todos offer **Mark done**.
- Click a narrative/PR row to open its referenced PR. Hover shows details.
- Click the `◀` day heading to walk backward; hold option for the route back toward today. Only the narrative is day-scoped; todos and PRs stay current.
- **Refresh** reloads; **Poll GitHub** refreshes the separate snapshot.

Day navigation is remembered temporarily in `$TMPDIR/daylog-view-day` and expires after ten minutes. If the icon disappears despite valid script output, check menu-bar space/notch overflow and restart SwiftBar.

Worker/capture scheduling is [separate](../docs/launchd/README.md). Configure a separate explicit `daylog --data-dir /fresh/path poll gh` schedule if desired. This plugin does not read queues, run Luna, or publish reports.
