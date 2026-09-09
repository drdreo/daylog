# Daylog — native macOS app (POC)

A SwiftUI menu-bar **panel**, not a SwiftBar plugin or webview. macOS 13+ and Apple's Command Line Tools (`xcode-select --install`) are sufficient. No Xcode project, App Store, paid developer account, web server, or third-party dependencies.

```sh
./install.sh                       # matching Daylog CLI
./macos-app/build.sh --install      # builds + ad-hoc signs ~/Applications/Daylog.app
open "$HOME/Applications/Daylog.app"
```

The note icon appears in the menu bar; the app has no Dock icon. Its number counts non-draft PRs with approval and passing checks in the current snapshot (hidden when zero). This is a readiness hint, not a guarantee that GitHub's conflict checks or other branch rules permit merging. Click it for compact journal headlines (click a headline to expand the full text, details and reporting source), previous/next day navigation (click the Today/date heading for a Monday-first month grid with circular selection and dots on days containing entries), current todos with accept/decline/done actions, a separate PR snapshot, and quick human notes/todos. The journal and completed todos are day-scoped; open todos stay current and quick additions always belong to today. Refresh runs on opening and every minute while the panel is active; this is not a background capture/curation worker. Use **Quit Daylog** in the gear panel to stop the app.

**Connection** (gear) lets you set the CLI path and data directory. Defaults: `~/.local/bin/daylog` and the CLI's platform-default store. These settings persist in `com.drdreo.daylog` preferences; Finder-launched apps do not inherit shell `DAYLOG_DIR`. Set the same explicit directory you used in SwiftBar if applicable. The app never initializes/migrates your real store, runs Athena, or changes scheduling. Click the PR snapshot's **Fetched … ago** line to refresh from GitHub (or **Fetch GitHub PRs** when no snapshot exists). This is an explicit network action using your existing `gh` authentication; the line shows progress and is disabled while a command is running.

Athena supplies a short headline, optional plain-text details, optional tags, and selected typed references; Swift owns the layout. Reference chips remain clickable while an entry is collapsed: GitHub PRs and Linear issues have links; Jira IDs are labels because no Jira host is configured. Tags are optional, not mandatory decoration. Older entries use their existing text as a single-line preview and reveal the complete wording on expansion; no history rewrite is needed. Human wording corrections clear previous details/tags so stale explanations cannot survive an edit.

The calendar loads entry counts with one `daylog days YYYY-MM --json` call per displayed month. Counts match the folded journal, including completed todos on their completion day; open todos, dismissed/merged/declined entries, queue items, and PRs do not create dots. Loading and failures are shown explicitly, and older CLI versions need `./install.sh` before markers are available. Month arrows browse without changing the selected journal; click a day to jump.

The app shells out via `Process` argument arrays, not a shell. Every command carries the configured store and a human widget identity, with inherited agent attribution removed. No queue or raw ledger reads happen in Swift. Each call times out after 60 seconds; after a timeout, refresh before retrying a write because it may already have committed.

## Replace SwiftBar locally

Once the native app works, move just `daylog.1m.js` out of SwiftBar's plugin folder (find it with `defaults read com.ameba.SwiftBar PluginDirectory`). Keep a backup for rollback. Quit SwiftBar if you have no other plugins. The build script deliberately does not change SwiftBar or login items.

For automatic startup, add **Daylog.app** in System Settings → General → Login Items. Remove SwiftBar there if no longer needed. This POC does not register itself automatically.

## Development / checks

```sh
swift run --package-path macos-app DaylogCoreChecks
# Also exercise writes only in a disposable store, then decode the current store read-only:
swift run --package-path macos-app DaylogCoreChecks "$HOME/.local/bin/daylog"
./macos-app/build.sh                # dist/Daylog.app, without installation
```

Checks use an executable target instead of XCTest so they run with Command Line Tools alone. They cover v2 decoding, required display timestamps, captured-offset clocks, ref URLs, argument boundaries, human attribution, subprocess failures/timeouts, merge-readiness combinations, calendar index validation, Monday-first/leap-year/DST month grids, and optional real-CLI note/todo completion and calendar counts. Visual interaction still needs a manual click-through.

Rebuild with `--install` to update (stops the existing Daylog process first), then reopen. Override `DAYLOG_APP_INSTALL_DIR` to choose a different install location. Local ad-hoc signing is for your own Mac, not a notarized distribution. POC limitations: fixed-size panel, no automatic updates, no native launch-at-login toggle, and no app icon artwork yet. SwiftBar's source remains in the repo as an optional legacy consumer/rollback route; the native app has no dependency on it.
