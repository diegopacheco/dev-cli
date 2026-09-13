#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

INSTALL_DIR="${DEVCLI_INSTALL_DIR:-$HOME/.local/bin}"
TARGET="$INSTALL_DIR/devcli"

[ "$(uname -s)" = "Darwin" ] || fail "install-macos.sh only runs on macOS"
require go

"$SCRIPTS/uninstall-macos.sh"

log "building $BIN"
go build -trimpath -o "$BIN" . || fail "go build failed"

mkdir -p "$INSTALL_DIR"
cp "$BIN" "$TARGET.tmp"
chmod 0755 "$TARGET.tmp"
mv "$TARGET.tmp" "$TARGET"
log "installed $TARGET"

"$TARGET" -version >/dev/null 2>&1 || fail "$TARGET does not run"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    RC="$HOME/.zshrc"
    case "${SHELL:-}" in
      */bash) RC="$HOME/.bash_profile" ;;
    esac
    LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
    if ! grep -qsF "$LINE" "$RC"; then
      printf "\n%s\n" "$LINE" >>"$RC"
      log "added $INSTALL_DIR to PATH in $RC, open a new terminal or run: source $RC"
    fi
    ;;
esac

OTHER="$(PATH="$(printf "%s" "$PATH" | tr ':' '\n' | grep -vxF "$INSTALL_DIR" | paste -sd: -)" command -v devcli || true)"
if [ -n "$OTHER" ]; then
  printf "WARNING: another devcli is on PATH at %s and is not managed by this script\n" "$OTHER" >&2
fi

log "$("$TARGET" -version) ready, run: devcli --help"
