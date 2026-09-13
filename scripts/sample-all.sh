#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

start() {
  podman-compose -f "$ROOT/podman-compose.yml" up -d >"$LOGS/compose.log" 2>&1 || fail "podman-compose up failed, see $LOGS/compose.log"

  wait_for MYSQL podman exec devcli-mysql mysql -uroot -pdevcli -h127.0.0.1 -e "USE devcli"
  wait_for POSTGRES podman exec devcli-postgres psql -U postgres -h 127.0.0.1 -d devcli -c "SELECT 1"
  wait_for REDIS podman exec devcli-redis redis-cli ping
  wait_for LOKI curl -fsS "$DEVCLI_LOKI/loki/api/v1/labels"
  wait_for PROMETHEUS curl -fsS "$DEVCLI_PROMETHEUS/-/ready"
  wait_for GRAFANA curl -fsS "http://127.0.0.1:$GRAFANA/api/datasources/uid/loki/health" -u admin:devcli
  wait_for CASSANDRA podman exec devcli-cassandra cqlsh -e "DESCRIBE KEYSPACES"

  podman exec -i devcli-postgres psql -q -v ON_ERROR_STOP=1 -U postgres -d devcli < "$ROOT/infra/postgres/init.sql" >"$LOGS/postgres-seed.log" 2>&1 || fail "postgres seed failed, see $LOGS/postgres-seed.log"
  log "postgres loaded: users, orders, 200 events"
  podman exec -i devcli-mysql mysql -uroot -pdevcli devcli < "$ROOT/infra/mysql/init.sql" >"$LOGS/mysql-seed.log" 2>&1 || fail "mysql seed failed, see $LOGS/mysql-seed.log"
  log "mysql loaded: users, orders, 200 events"
  podman exec devcli-cassandra cqlsh -f /init.cql >"$LOGS/cassandra-seed.log" 2>&1 || fail "cassandra seed failed, see $LOGS/cassandra-seed.log"
  log "cassandra loaded: devcli.users, devcli.events"
  podman exec -i devcli-redis redis-cli < "$ROOT/infra/redis/seed.redis" >"$LOGS/redis-seed.log" 2>&1 || fail "redis seed failed, see $LOGS/redis-seed.log"
  log "redis loaded: json strings, hash, list, sorted set, set, stream"
  sqlite3 "$SQLITE_DB" < "$ROOT/infra/sqlite/init.sql" || fail "sqlite seed failed"
  log "sqlite loaded: $SQLITE_DB hosts, metrics"
  "$ROOT/infra/loki/seed.sh" "$DEVCLI_LOKI" || fail "loki seed failed"
  log "loki loaded: api, worker, web log lines"
  wait_for PROMETHEUS sh -c "curl -fsS '$DEVCLI_PROMETHEUS/api/v1/query?query=up' | grep -q loki"
  log "prometheus scraping: prometheus, loki, grafana"

  "$SCRIPTS/sample-java.sh" start
  start_bg jvm-clojure clojure -M -e "(ns devcli.sample) (defn worker-loop [] (Thread/sleep 1000) (recur)) (worker-loop)"
  wait_jvm jvm-clojure
  log "sample data loaded"
}

stop() {
  "$SCRIPTS/sample-java.sh" stop
  stop_bg jvm-clojure
  podman-compose -f "$ROOT/podman-compose.yml" stop >"$LOGS/compose-stop.log" 2>&1 || fail "podman-compose stop failed, see $LOGS/compose-stop.log"
  for name in $(service_names); do
    port="$(service_port "$name")"
    wait_port_down "$port" 30 || fail "port $port for $name is still in use"
  done
  log "sample services stopped"
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  *) fail "usage: scripts/sample-all.sh start|stop" ;;
esac
