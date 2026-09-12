#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

[ -x "$BIN" ] || fail "binary missing, run scripts/setup.sh first"
exec "$BIN" "$@"
