#!/usr/bin/env sh
# Build daylog from this checkout and install the binary.
#
#   ./install.sh              install to ~/.local/bin (override: DAYLOG_INSTALL_DIR)
#   ./install.sh --timer      also enable the systemd user timer for `daylog poll gh`
#   ./install.sh --omarchy    also install + enable the Omarchy bar widget (Linux)
#   ./install.sh --swiftbar   also install the SwiftBar menu bar widget (macOS)
#   ./install.sh --skills     also install the daylog skill for Codex and Claude Code
#
# Works on Linux and macOS; on Windows use `go install .` instead, plus
# windows-plugin\install.ps1 for the tray widget.
set -eu

INSTALL_DIR="${DAYLOG_INSTALL_DIR:-$HOME/.local/bin}"
REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WITH_TIMER=false
WITH_OMARCHY=false
WITH_SWIFTBAR=false
WITH_SKILLS=false

for arg in "$@"; do
    case "$arg" in
        --timer) WITH_TIMER=true ;;
        --omarchy) WITH_OMARCHY=true ;;
        --swiftbar) WITH_SWIFTBAR=true ;;
        --skills) WITH_SKILLS=true ;;
        *) echo "usage: ./install.sh [--timer] [--omarchy] [--swiftbar] [--skills]" >&2; exit 1 ;;
    esac
done

command -v go >/dev/null 2>&1 || {
    echo "error: go is not installed (https://go.dev/dl/)" >&2
    exit 1
}

echo "Building daylog..."
mkdir -p "$INSTALL_DIR"
go -C "$REPO_DIR" build -o "$INSTALL_DIR/daylog" .
"$INSTALL_DIR/daylog" --help >/dev/null # sanity: the binary runs
echo "Installed $INSTALL_DIR/daylog"

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "note: $INSTALL_DIR is not on your PATH — add: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

if [ "$WITH_TIMER" = true ]; then
    if ! command -v systemctl >/dev/null 2>&1; then
        echo "error: --timer needs systemd (Linux); see README for cron/launchd/schtasks equivalents" >&2
        exit 1
    fi
    UNIT_DIR="$HOME/.config/systemd/user"
    mkdir -p "$UNIT_DIR"
    # The unit template assumes %h/.local/bin; point it at the real install dir.
    sed "s|ExecStart=.*|ExecStart=$INSTALL_DIR/daylog poll gh|" \
        "$REPO_DIR/docs/systemd/daylog-poll-gh.service" > "$UNIT_DIR/daylog-poll-gh.service"
    cp "$REPO_DIR/docs/systemd/daylog-poll-gh.timer" "$UNIT_DIR/"
    systemctl --user daemon-reload
    systemctl --user enable --now daylog-poll-gh.timer
    echo "Timer enabled: systemctl --user list-timers daylog-poll-gh.timer"
fi

if [ "$WITH_OMARCHY" = true ]; then
    if ! command -v omarchy >/dev/null 2>&1; then
        echo "error: --omarchy needs the omarchy CLI (an Omarchy 4 install)" >&2
        exit 1
    fi
    PLUGIN_DIR="$HOME/.config/omarchy/plugins/drdreo.daylog"
    if [ -e "$PLUGIN_DIR" ] && [ ! -e "$PLUGIN_DIR/manifest.json" ]; then
        echo "error: $PLUGIN_DIR exists but is not the daylog plugin — not touching it" >&2
        exit 1
    fi
    mkdir -p "$PLUGIN_DIR"
    cp "$REPO_DIR"/omarchy-plugin/manifest.json "$REPO_DIR"/omarchy-plugin/*.qml "$PLUGIN_DIR/"
    omarchy-shell shell rescanPlugins || true # shell may not be running (e.g. TTY)
    omarchy plugin enable drdreo.daylog || echo "note: enable it later with: omarchy plugin enable drdreo.daylog"
    echo "Omarchy widget installed: $PLUGIN_DIR"
fi

if [ "$WITH_SWIFTBAR" = true ]; then
    if [ "$(uname)" != "Darwin" ]; then
        echo "error: --swiftbar is macOS-only" >&2
        exit 1
    fi
    # SwiftBar stores the user-chosen plugin folder in its defaults; without
    # it, SwiftBar has never been launched (or never got a folder picked).
    SWIFTBAR_DIR=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
    if [ -z "$SWIFTBAR_DIR" ]; then
        echo "error: SwiftBar plugin folder not set — install SwiftBar (brew install swiftbar)," >&2
        echo "       launch it once to pick a plugin folder, then re-run" >&2
        exit 1
    fi
    cp "$REPO_DIR/swiftbar-plugin/daylog.1m.js" "$SWIFTBAR_DIR/"
    chmod +x "$SWIFTBAR_DIR/daylog.1m.js"
    open -g swiftbar://refreshallplugins || true # SwiftBar may not be running
    echo "SwiftBar widget installed: $SWIFTBAR_DIR/daylog.1m.js"
fi

if [ "$WITH_SKILLS" = true ]; then
    AGENTS_SKILLS_DIR="${DAYLOG_AGENTS_SKILLS_DIR:-$HOME/.agents/skills}"
    CLAUDE_SKILLS_DIR="${DAYLOG_CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"

    # Preflight both destinations before copying so a name collision cannot
    # leave only one harness updated.
    for skills_root in "$AGENTS_SKILLS_DIR" "$CLAUDE_SKILLS_DIR"; do
        skill_dir="$skills_root/daylog"
        if [ -e "$skill_dir" ] && {
            [ ! -f "$skill_dir/SKILL.md" ] ||
            ! grep -q '^  source: github.com/drdreo/daylog$' "$skill_dir/SKILL.md"
        }; then
            echo "error: $skill_dir exists but is not the daylog skill — not touching it" >&2
            exit 1
        fi
    done

    for skills_root in "$AGENTS_SKILLS_DIR" "$CLAUDE_SKILLS_DIR"; do
        skill_dir="$skills_root/daylog"
        mkdir -p "$skill_dir"
        cp "$REPO_DIR/skills/daylog/SKILL.md" "$skill_dir/SKILL.md"
        echo "Agent skill installed: $skill_dir"
    done
fi

echo
echo "Next steps:"
echo "  daylog add \"first entry\"     # start logging"
echo "  daylog poll gh                # needs the gh CLI, authed"
echo "  daylog today                  # the day view"
echo "  Set DAYLOG_SOURCE=agent:<name> in each agent's launch wrapper."
