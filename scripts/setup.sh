#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for tool in go podman podman-compose java jcmd clojure sqlite3 curl lsof; do
  require "$tool"
done

podman info >/dev/null 2>&1 || fail "podman is not reachable, start it with: podman machine start"

log "downloading go modules"
go mod download

log "building $BIN"
go build -o "$BIN" .

log "pulling container images"
podman-compose -f "$ROOT/podman-compose.yml" pull >"$LOGS/pull.log" 2>&1 || fail "image pull failed, see $LOGS/pull.log"

log "setup done"
