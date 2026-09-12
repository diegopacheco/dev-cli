#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

target="${1:-}"
case "$target" in
  mysql) exec podman exec -it devcli-mysql mysql -uroot -pdevcli devcli ;;
  postgres) exec podman exec -it devcli-postgres psql -U postgres -d devcli ;;
  cassandra) exec podman exec -it devcli-cassandra cqlsh -k devcli ;;
  redis) exec podman exec -it devcli-redis redis-cli ;;
  sqlite) exec sqlite3 "$SQLITE_DB" ;;
  *) fail "usage: scripts/sql-console.sh mysql|postgres|cassandra|redis|sqlite" ;;
esac
