#!/usr/bin/env bash
# Self-contained bootstrap + run for the DNS module E2E smoke with the
# server row set to the PowerDNS backend (add-dns-powerdns-module, task
# 6.2). Builds the binary, starts an ephemeral MariaDB container with both
# the panel (dbispconfig) and powerdns databases, migrates + seeds the
# panel, applies the embedded powerdns.sql schema, flips the local server
# row to [dns] dns_backend=powerdns, starts `go-ispconfig serve` and runs
# e2e/panel-dns-powerdns.sh against it.
#
# The daemon is intentionally not started (see the note in
# e2e/panel-dns-powerdns.sh): this script proves the panel/API work
# unchanged on a PowerDNS-provisioned server row; the daemon-side SQL sync
# is covered by `go test -tags=integration ./internal/powerdns/...`
# (TestDatalogToPowerDNSPipeline).
#
# Requirements: docker, agent-browser, make, curl, python3, mariadb client
# (or docker exec into the container, used here to avoid a host dependency).
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
PORT=18299
DB_PORT=13399
DB_CONTAINER=mariadb-e2e-dns-powerdns
ADMIN_PASSWORD='E2eTest123!'
SERVE_PID=""

cleanup() {
  agent-browser close --all >/dev/null 2>&1 || true
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null || true
  docker rm -f "$DB_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

make -C "$REPO" all

docker rm -f "$DB_CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$DB_CONTAINER" \
  -e MARIADB_ROOT_PASSWORD=e2eroot -e MARIADB_DATABASE=dbispconfig \
  -p "127.0.0.1:$DB_PORT:3306" mariadb:11 >/dev/null
for _ in $(seq 1 60); do
  docker exec "$DB_CONTAINER" mariadb -uroot -pe2eroot -e "SELECT 1" >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$DB_CONTAINER" mariadb -uroot -pe2eroot \
  -e "CREATE DATABASE IF NOT EXISTS powerdns CHARACTER SET utf8mb4"
docker exec -i "$DB_CONTAINER" mariadb -uroot -pe2eroot powerdns < "$REPO/internal/powerdns/powerdns.sql"

cat > "$WORK/config.toml" <<EOF
[server]
host = "127.0.0.1"
port = $PORT
https = false

[database]
dsn = "root:e2eroot@tcp(127.0.0.1:$DB_PORT)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local"
EOF

GOISP_ADMIN_PASSWORD="$ADMIN_PASSWORD" "$REPO/bin/go-ispconfig" migrate --config "$WORK/config.toml"

# Flip the seeded local server row (server_id=1) to the PowerDNS backend;
# the seeded server.config already carries dns_backend=bind
# (internal/database/server_config.ini).
docker exec "$DB_CONTAINER" mariadb -uroot -pe2eroot dbispconfig -e "
  UPDATE server SET config = REPLACE(config, 'dns_backend=bind', 'dns_backend=powerdns')
  WHERE server_id = 1;"
BACKEND=$(docker exec "$DB_CONTAINER" mariadb -uroot -pe2eroot dbispconfig -N -B \
  -e "SELECT config LIKE '%dns_backend=powerdns%' FROM server WHERE server_id=1")
[ "$BACKEND" = "1" ] || { echo "FAIL: server row was not switched to dns_backend=powerdns"; exit 1; }
echo "server_id=1 provisioned with [dns] dns_backend=powerdns"

"$REPO/bin/go-ispconfig" serve --config "$WORK/config.toml" > "$WORK/serve.log" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 30); do
  curl -fs "http://127.0.0.1:$PORT/" >/dev/null 2>&1 && break
  sleep 1
done

mkdir -p "$REPO/docs/prints"
PANEL_URL="http://127.0.0.1:$PORT" ADMIN_PASSWORD="$ADMIN_PASSWORD" "$REPO/e2e/panel-dns-powerdns.sh"
