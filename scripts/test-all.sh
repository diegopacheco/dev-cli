#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

log "bash syntax"
for script in "$SCRIPTS"/*.sh "$ROOT"/infra/loki/seed.sh; do
  bash -n "$script" || fail "syntax error in $script"
done

log "go vet"
go vet ./... || fail "go vet failed"

log "unit tests"
go test -race -count=1 ./... || fail "unit tests failed"

for name in $(service_names); do
  port_up "$(service_port "$name")" || fail "$name is down, run scripts/start-all.sh before the integration tests"
done

log "integration tests"
go test -count=1 -tags integration ./internal/backend ./internal/jvm ./internal/cli || fail "integration tests failed"

log "all tests passed"
