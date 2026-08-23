#!/usr/bin/env sh
# Build daylog from this checkout and install the binary.
#
#   ./install.sh              install to ~/.local/bin (override: DAYLOG_INSTALL_DIR)
#   ./install.sh --timer      also enable the systemd user timer for `daylog poll gh`
#
# Works on Linux and macOS; on Windows use `go install .` instead.
set -eu

INSTALL_DIR="${DAYLOG_INSTALL_DIR:-$HOME/.local/bin}"
REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WITH_TIMER=false

for arg in "$@"; do
    case "$arg" in
        --timer) WITH_TIMER=true ;;
        *) echo "usage: ./install.sh [--timer]" >&2; exit 1 ;;
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

echo
echo "Next steps:"
echo "  daylog add \"first entry\"     # start logging"
echo "  daylog poll gh                # needs the gh CLI, authed"
echo "  daylog today                  # the day view"
echo "  Set DAYLOG_SOURCE=agent:<name> in each agent's launch wrapper."
