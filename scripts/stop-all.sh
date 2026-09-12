#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

"$SCRIPTS/sample-all.sh" stop
log "all services stopped"
