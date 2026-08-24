# Daylog — Omarchy bar widget

A bar widget for [Omarchy 4 (Quattro)](https://omarchy.org)'s Quickshell-based
desktop shell. One icon, one panel: today's entries, open todos, the agent
inbox awaiting triage, and every open PR with live checks/review state.

This is deliberately the thinnest component in the system (ARCHITECTURE.md §9):
it shells out to `daylog today --json` and renders the result. All state,
folding, and PR joining happen in the CLI — the widget is a dumb consumer and
is replaceable in an afternoon.

## What it shows

- **Bar icon** — lights up (active color) when something needs you: an agent
  proposal awaiting triage, or an open PR with failing checks.
  Left-click toggles the panel, right-click runs `daylog poll gh`,
  middle-click refreshes.
- **Open todos** — every open todo in one list, yours and the agents'. An
  agent proposal still awaiting your verdict is accented and marked `●`.
- **Today** — the day's entries with time, source, and live PR status on
  entries that reference a PR. Closed todos are struck through.
- **Open PRs** — the poller snapshot, marked STALE when it is hours old.

## Keyboard

The panel is fully keyboard-driven, one flat cursor over every row
(todos → today → PRs):

| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j` | Move the cursor |
| `Enter` / `Space` / `o` | Open the selected PR in the browser (PR rows, or entries referencing one) |
| `d` | Close the selected todo (`daylog done <id>`) |
| `a` | Accept the selected agent proposal (`daylog accept <id>`) — adopt it as yours |
| `x` | Decline the selected agent proposal (`daylog decline <id>`) — it drops out of every view |
| `r` | Refresh |
| `p` | Run `daylog poll gh` |
| `Tab` | Switch to the neighboring bar panel |
| `Esc` | Close |

Rows are also clickable: clicking a PR (or an entry that references one)
opens it in the browser.

## Install

From the daylog checkout, on an Omarchy machine:

```sh
./install.sh --omarchy       # copies the plugin + rescans + enables it
```

Or by hand:

```sh
cp -r omarchy-plugin ~/.config/omarchy/plugins/drdreo.daylog
omarchy-shell shell rescanPlugins
omarchy plugin enable drdreo.daylog
```

For hacking, symlink instead of copying — the shell hot-reloads plugin code
on every save under `~/.config/omarchy/plugins/`:

```sh
ln -s "$(pwd)/omarchy-plugin" ~/.config/omarchy/plugins/drdreo.daylog
```

Check it the way the shell does:

```sh
omarchy plugin validate ./omarchy-plugin
```

> `omarchy plugin add <git-url>` expects `manifest.json` at the repo root, so
> distributing through the registry at omarchyplugins.com means splitting this
> directory into its own repo (e.g. `drdreo/omarchy-daylog`). Until then, the
> copy/symlink install above is the path.

## Settings

Inline on the widget's entry in `~/.config/omarchy/shell.json`
(editable via the bar's widget settings UI):

| Key | Default | Meaning |
|---|---|---|
| `daylogPath` | `daylog` | Command or absolute path to the daylog CLI |
| `refreshIntervalSec` | `60` | How often to re-run `daylog today --json` |
| `icon` | `󰃭` | The bar glyph |

## IPC

```sh
omarchy-shell shell call drdreo.daylog toggle
omarchy-shell shell call drdreo.daylog refresh
omarchy-shell shell call drdreo.daylog poll
```
