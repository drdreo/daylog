# Daylog — Omarchy widget

A Quickshell bar widget for Omarchy 4. It reads **v2** `daylog today --json`: curated narrative, open todos (with explicit agent proposals marked for adoption), and a separate current Open PRs snapshot. Queued/shadow reports are not entries.

Narrative rows use `display_at` in its captured offset; completed todos also show `filed_at`. PR/CI status never decorates narrative rows. Host-qualified PR refs provide links without joining live status into accomplishments.

## Install

Build/install the matching CLI, initialize a fresh store, then explicitly copy the widget:

```sh
./install.sh
cp -r omarchy-plugin ~/.config/omarchy/plugins/drdreo.daylog
omarchy-shell shell rescanPlugins
omarchy plugin enable drdreo.daylog
omarchy plugin validate ./omarchy-plugin
```

Avoid overwriting an unrelated plugin directory. For development, symlink the directory instead. Runtime validation of this widget requires an Omarchy machine; Go cross-compilation does not test QML.

## Settings

Configure the widget in `~/.config/omarchy/shell.json` or the widget settings UI:

| Key | Default | Meaning |
|---|---|---|
| `daylogPath` | `daylog` | Absolute binary path recommended |
| `dataDir` | empty | Absolute fresh v2 data directory; empty uses CLI default |
| `refreshIntervalSec` | 60 | Refresh interval |
| `icon` | `󰃭` | Bar glyph |

The data directory is passed on **every read and action**, not inferred from the desktop shell's environment. Human actions explicitly use `human:widget`.

## Controls

Left-click toggles, right-click polls GitHub, middle-click refreshes. In the panel:

- `↑/↓` or `j/k`: navigate rows; `Enter`, `Space`, or `o`: open referenced PR.
- `←/→` or `h/l`: previous/next day; `t`: today. Only narrative changes day; obligations/PRs remain current.
- `a` / `x`: adopt/decline a proposal; `d`: finish an adopted todo.
- `r`: refresh; `p`: poll GitHub; `Esc`: close; `Tab`: neighboring panel.

IPC: `omarchy-shell shell call drdreo.daylog toggle|refresh|poll`.

Configure capture/editor scheduling separately with [`daylog setup`](../README.md). The widget is not a publisher or queue consumer.
