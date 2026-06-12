#!/usr/bin/env bash
# zinc.sql dogfood: zc run against a real Postgres on a shared docker network
# (zc vendors epgsql into _checkouts first). The Application serves forever
# (static children), so timeout reaping it is the expected exit; the assertion
# is on main()'s client output.
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd ../.. && pwd)"
PG_IMG="public.ecr.aws/docker/library/postgres:16-alpine"
NET=zincsql-net
PG=zincsql-pg

cleanup() {
  docker rm -f "$PG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
cleanup
trap cleanup EXIT

docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" \
  -e POSTGRES_USER=zinc -e POSTGRES_PASSWORD=zinc -e POSTGRES_DB=zinc \
  "$PG_IMG" >/dev/null
# alpine postgres restarts once during initdb: require two consecutive readies
ready=0
for _ in $(seq 1 60); do
  if docker exec "$PG" pg_isready -U zinc -d zinc >/dev/null 2>&1; then
    ready=$((ready + 1))
    [ "$ready" -ge 2 ] && break
  else
    ready=0
  fi
  sleep 1
done
[ "$ready" -ge 2 ] || { echo "FAIL  sqldemo (postgres never became ready)"; exit 1; }

got=$(docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp --network "$NET" \
  -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
  -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -w /work/dogfood/sqldemo erlang:slim \
  sh -c 'timeout 120 /work/bin/zc run; true' | tail -7)

want=$(printf '1\nvin\n7\n1\n2\nrolled back 2\nsql error caught')

if [ "$got" = "$want" ]; then
  echo "PASS  sqldemo"
else
  echo "FAIL  sqldemo"
  echo "--- want ---"; echo "$want"
  echo "--- got ----"; echo "$got"
  exit 1
fi
