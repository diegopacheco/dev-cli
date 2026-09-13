#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

NAME=jvm-java
SOURCE="$ROOT/sample/java25/src/DevcliJvm.java"

java_major() {
  java -version 2>&1 | awk -F'"' '/version/ { split($2, v, "."); print v[1]; exit }'
}

start() {
  require java
  require jcmd
  major="$(java_major)"
  [ "${major:-0}" -ge 25 ] 2>/dev/null || fail "java 25 or newer is required, found ${major:-none}: $(java -version 2>&1 | head -1)"
  start_bg "$NAME" java "$SOURCE"
  wait_jvm "$NAME"
  started=$SECONDS
  until grep -q "sample running" "$LOGS/$NAME.log" 2>/dev/null; do
    pid_alive "$NAME" || fail "$NAME exited, see $LOGS/$NAME.log"
    [ $((SECONDS - started)) -lt 60 ] || fail "$NAME did not start its threads after 60 seconds, see $LOGS/$NAME.log"
    sleep 1
  done
  sleep 1
  log "$NAME running java $major pid $(cat "$RUN/$NAME.pid"), logs in $LOGS/$NAME.log"
}

stop() {
  stop_bg "$NAME"
}

status() {
  if pid_alive "$NAME"; then
    log "$NAME UP pid $(cat "$RUN/$NAME.pid")"
  else
    log "$NAME DOWN"
    return 1
  fi
}

dump() {
  pid_alive "$NAME" || fail "$NAME is not running, run scripts/sample-java.sh start"
  pid="$(cat "$RUN/$NAME.pid")"
  if [ -x "$BIN" ]; then
    exec "$BIN" -q -threads "$pid"
  fi
  exec jcmd "$pid" Thread.print -l
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  status) status ;;
  dump) dump ;;
  *) fail "usage: scripts/sample-java.sh start|stop|status|dump" ;;
esac
