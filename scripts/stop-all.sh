#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for name in $JVM_SAMPLES; do
  stop_bg "$name"
done

podman-compose -f "$ROOT/podman-compose.yml" stop >"$LOGS/compose-stop.log" 2>&1 || fail "podman-compose stop failed, see $LOGS/compose-stop.log"

for name in $(service_names); do
  port="$(service_port "$name")"
  wait_port_down "$port" 30 || fail "port $port for $name is still in use"
done

log "all services stopped"
