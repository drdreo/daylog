#!/bin/sh
# Local-only, ad-hoc signed app bundle. No Xcode project or paid account needed.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "${1:-}" in
    '') ;;
    --install) ;;
    *) echo 'usage: macos-app/build.sh [--install]' >&2; exit 1 ;;
esac
swift build --package-path "$ROOT/macos-app" -c release
BIN=$(swift build --package-path "$ROOT/macos-app" -c release --show-bin-path)
APP="$ROOT/dist/Daylog.app"
mkdir -p "$APP/Contents/MacOS"
cp "$BIN/Daylog" "$APP/Contents/MacOS/Daylog"
/usr/libexec/PlistBuddy -c 'Print' "$ROOT/macos-app/Info.plist" >/dev/null
cp "$ROOT/macos-app/Info.plist" "$APP/Contents/Info.plist"
codesign --force --sign - "$APP"
if [ "${1:-}" = --install ]; then
    DEST=${DAYLOG_APP_INSTALL_DIR:-"$HOME/Applications"}
    mkdir -p "$DEST"
    # Stop only our running app before replacing its executable.
    pkill -x Daylog 2>/dev/null || true
    ditto "$APP" "$DEST/Daylog.app"
    printf 'Installed %s/Daylog.app\n' "$DEST"
    printf 'Launch with: open "%s/Daylog.app"\n' "$DEST"
else
    printf 'Built %s\n' "$APP"
fi
