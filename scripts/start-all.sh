#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

[ -x "$BIN" ] || fail "binary missing, run scripts/setup.sh first"

podman-compose -f "$ROOT/podman-compose.yml" up -d >"$LOGS/compose.log" 2>&1 || fail "podman-compose up failed, see $LOGS/compose.log"

wait_for MYSQL podman exec devcli-mysql mysql -uroot -pdevcli -h127.0.0.1 -e "SELECT 1 FROM devcli.orders LIMIT 1"
wait_for POSTGRES podman exec devcli-postgres psql -U postgres -h 127.0.0.1 -d devcli -c "SELECT 1 FROM orders LIMIT 1"
wait_for REDIS podman exec devcli-redis redis-cli ping
wait_for LOKI curl -fsS "$DEVCLI_LOKI/loki/api/v1/labels"
wait_for GRAFANA curl -fsS "http://127.0.0.1:$GRAFANA/api/datasources/uid/loki/health" -u admin:devcli
wait_for CASSANDRA podman exec devcli-cassandra cqlsh -e "DESCRIBE KEYSPACES"

podman exec -i devcli-redis redis-cli < "$ROOT/infra/redis/seed.redis" >"$LOGS/redis-seed.log" 2>&1 || fail "redis seed failed, see $LOGS/redis-seed.log"
podman exec devcli-cassandra cqlsh -f /init.cql >"$LOGS/cassandra-seed.log" 2>&1 || fail "cassandra seed failed, see $LOGS/cassandra-seed.log"
log "redis and cassandra seeded"

if curl -fsS -G "$DEVCLI_LOKI/loki/api/v1/query_range" --data-urlencode 'query={app="api"}' --data-urlencode 'limit=1' | grep -q '"values"'; then
  log "loki already has api logs"
else
  "$ROOT/infra/loki/seed.sh" "$DEVCLI_LOKI" || fail "loki seed failed"
  log "loki seeded"
fi

start_bg jvm-java java "$ROOT/infra/jvm/DevcliJvm.java"
start_bg jvm-clojure clojure -M -e "(ns devcli.sample) (defn worker-loop [] (Thread/sleep 1000) (recur)) (worker-loop)"

for name in $JVM_SAMPLES; do
  started=$SECONDS
  until jcmd "$(cat "$RUN/$name.pid")" VM.version >/dev/null 2>&1; do
    pid_alive "$name" || fail "$name exited, see $LOGS/$name.log"
    [ $((SECONDS - started)) -lt 60 ] || fail "$name did not answer jcmd after 60 seconds"
    sleep 1
  done
  log "$name ready"
done

log "all services started"
