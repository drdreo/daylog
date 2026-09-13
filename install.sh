#!/bin/sh
# Build/install only. Store creation, capture scopes and scheduler activation
# are separate explicit setup actions; never migrate history.
set -eu
REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INSTALL_DIR=${DAYLOG_INSTALL_DIR:-"$HOME/.local/bin"}
if [ "$#" -ne 0 ]; then
    echo 'usage: ./install.sh (DAYLOG_INSTALL_DIR overrides ~/.local/bin)' >&2
    echo 'Then: daylog --data-dir /fresh/private/path init' >&2
    echo '      daylog --data-dir /fresh/private/path setup --help' >&2
    exit 1
fi
command -v go >/dev/null 2>&1 || { echo 'Go 1.24+ required' >&2; exit 1; }
mkdir -p "$INSTALL_DIR"
TMP=$(mktemp "$INSTALL_DIR/.daylog-build.XXXXXX")
trap 'rm -f "$TMP"' EXIT HUP INT TERM
VERSION=$(git -C "$REPO_DIR" describe --always --dirty 2>/dev/null || printf gatekeeper-dev)
go -C "$REPO_DIR" build -ldflags "-X github.com/drdreo/daylog/internal/store.BuildVersion=$VERSION" -o "$TMP" .
"$TMP" version
chmod 755 "$TMP"
mv "$TMP" "$INSTALL_DIR/daylog"
printf 'Installed %s/daylog\nNo stores, hooks, providers, or schedulers were changed.\n' "$INSTALL_DIR"
printf 'Next: daylog --data-dir /fresh/private/path init\n'
