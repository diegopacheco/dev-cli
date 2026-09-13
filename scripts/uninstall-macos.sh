#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

INSTALL_DIR="${DEVCLI_INSTALL_DIR:-$HOME/.local/bin}"
TARGET="$INSTALL_DIR/devcli"

pkill -x devcli 2>/dev/null || true

if [ -e "$TARGET" ]; then
  rm -f "$TARGET" || fail "could not remove $TARGET"
  log "removed $TARGET"
else
  log "devcli is not installed in $INSTALL_DIR"
fi
rm -f "$TARGET.tmp"
