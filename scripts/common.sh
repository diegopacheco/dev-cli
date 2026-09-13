#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPTS="$ROOT/scripts"
RUN="$ROOT/.run"
LOGS="$RUN/logs"
BIN="$ROOT/bin/devcli"
SQLITE_DB="$RUN/devcli.db"
cd "$ROOT"

mkdir -p "$RUN" "$LOGS"

SERVICES="$(grep -v '^[[:space:]]*#' "$SCRIPTS/ports.env" | grep '=' || true)"

set -a
. "$SCRIPTS/ports.env"
set +a

export DEVCLI_MYSQL="mysql://root:devcli@127.0.0.1:$MYSQL/devcli"
export DEVCLI_POSTGRES="postgres://postgres:devcli@127.0.0.1:$POSTGRES/devcli?sslmode=disable"
export DEVCLI_SQLITE="$SQLITE_DB"
export DEVCLI_CASSANDRA="127.0.0.1:$CASSANDRA/devcli"
export DEVCLI_REDIS="redis://127.0.0.1:$REDIS/0"
export DEVCLI_LOKI="http://127.0.0.1:$LOKI"
export DEVCLI_GRAFANA="http://admin:devcli@127.0.0.1:$GRAFANA"
export DEVCLI_PROMETHEUS="http://127.0.0.1:$PROMETHEUS"

JVM_SAMPLES="jvm-java jvm-clojure"

service_names() {
  printf "%s\n" "$SERVICES" | sed '/^$/d' | cut -d= -f1
}

service_port() {
  printf "%s\n" "$SERVICES" | sed '/^$/d' | awk -F= -v n="$1" '$1==n{print $2; exit}'
}

container_name() {
  printf "devcli-%s" "$(printf "%s" "$1" | tr '[:upper:]' '[:lower:]')"
}

port_pid() {
  lsof -ti "tcp:$1" -sTCP:LISTEN 2>/dev/null | head -1 || true
}

port_up() {
  [ -n "$(port_pid "$1")" ]
}

wait_port_down() {
  local tries
  tries="${2:-30}"
  while [ "$tries" -gt 0 ]; do
    if ! port_up "$1"; then return 0; fi
    sleep 1
    tries=$((tries - 1))
  done
  return 1
}

wait_for() {
  local name started
  name="$1"
  shift
  started=$SECONDS
  while [ $((SECONDS - started)) -lt 60 ]; do
    if "$@" >/dev/null 2>&1; then
      log "$name ready"
      return 0
    fi
    sleep 1
  done
  fail "$name was not ready after 60 seconds, see: podman logs $(container_name "$name")"
}

pid_alive() {
  [ -f "$RUN/$1.pid" ] && kill -0 "$(cat "$RUN/$1.pid")" 2>/dev/null
}

start_bg() {
  local name
  name="$1"
  shift
  if pid_alive "$name"; then
    log "$name already running"
    return 0
  fi
  ( cd "$ROOT" && exec "$@" >"$LOGS/$name.log" 2>&1 ) &
  echo $! >"$RUN/$name.pid"
  log "$name started pid $!"
}

stop_bg() {
  local name pid
  name="$1"
  if [ -f "$RUN/$name.pid" ]; then
    pid="$(cat "$RUN/$name.pid")"
    if kill -0 "$pid" 2>/dev/null; then
      pkill -TERM -P "$pid" 2>/dev/null || true
      kill -TERM "$pid" 2>/dev/null || true
    fi
    rm -f "$RUN/$name.pid"
  fi
  log "$name stopped"
}

wait_jvm() {
  local name started
  name="$1"
  started=$SECONDS
  until jcmd "$(cat "$RUN/$name.pid")" VM.version >/dev/null 2>&1; do
    pid_alive "$name" || fail "$name exited, see $LOGS/$name.log"
    [ $((SECONDS - started)) -lt 60 ] || fail "$name did not answer jcmd after 60 seconds"
    sleep 1
  done
  log "$name ready"
}

require() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed"
}

log() {
  printf "%s\n" "$*"
}

fail() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}
