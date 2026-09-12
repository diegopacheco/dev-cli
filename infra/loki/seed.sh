#!/usr/bin/env bash
set -euo pipefail
URL="${1:?loki url}"
NOW=$(date +%s)
push() {
  local offset="$1" app="$2" level="$3" line="$4"
  local ts=$(( (NOW - offset) * 1000000000 ))
  local escaped=${line//\\/\\\\}
  escaped=${escaped//\"/\\\"}
  curl -fsS -H 'Content-Type: application/json' -X POST "$URL/loki/api/v1/push" \
    -d "{\"streams\":[{\"stream\":{\"app\":\"$app\",\"level\":\"$level\",\"env\":\"dev\"},\"values\":[[\"$ts\",\"$escaped\"]]}]}" >/dev/null
}
push 300 api info '{"msg":"request served","method":"GET","path":"/orders","status":200,"ms":12}'
push 280 api info '{"msg":"request served","method":"POST","path":"/orders","status":201,"ms":48}'
push 240 api warn '{"msg":"slow query","db":"postgres","table":"orders","ms":812}'
push 200 api error '{"msg":"payment declined","order_id":3,"provider":"stripe","retry":true}'
push 180 worker info '{"msg":"job finished","job":"reindex","items":1284,"ms":5230}'
push 150 worker error '{"msg":"job failed","job":"backup","attempt":3,"error":"timeout"}'
push 120 web info 'GET /dashboard 200 31ms user=ana'
push 90 api info '{"msg":"cache hit","key":"user:1","store":"redis"}'
push 60 api error '{"msg":"upstream unavailable","service":"cassandra","ms":10000}'
push 30 api info '{"msg":"request served","method":"GET","path":"/users/1","status":200,"ms":7}'
