#!/usr/bin/env bash
# E2E smoke test for the panel skeleton (task 7.6): login, module navigation,
# logout, and zero-external-requests check — run with agent-browser against
# the real built binary and an ephemeral MariaDB container.
#
# Requirements: docker, agent-browser (npm i -g agent-browser && agent-browser install),
# make, curl, python3. Run from the repo root: ./scripts/e2e-panel.sh
#
# Screenshots land in docs/prints/ (gitignored). Curated ones (login,
# dashboard) are copied to docs/screenshots/ which IS committed.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
PORT=18080
DB_PORT=13307
DB_CONTAINER=mariadb-e2e
SESSION=goisp-e2e
ADMIN_PASSWORD='E2eTest123!'
SERVE_PID=""

cleanup() {
  agent-browser --session "$SESSION" close >/dev/null 2>&1 || true
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null || true
  docker rm -f "$DB_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

ab() { agent-browser --session "$SESSION" "$@"; }

# --- Setup -----------------------------------------------------------------
make -C "$REPO" all

docker rm -f "$DB_CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$DB_CONTAINER" \
  -e MARIADB_ROOT_PASSWORD=e2eroot -e MARIADB_DATABASE=dbispconfig \
  -p "127.0.0.1:$DB_PORT:3306" mariadb:11 >/dev/null
for _ in $(seq 1 60); do
  docker exec "$DB_CONTAINER" mariadb -uroot -pe2eroot -e "SELECT 1" >/dev/null 2>&1 && break
  sleep 1
done

# Plain HTTP keeps agent-browser away from self-signed cert handling.
cat > "$WORK/config.toml" <<EOF
[server]
host = "127.0.0.1"
port = $PORT
https = false

[database]
dsn = "root:e2eroot@tcp(127.0.0.1:$DB_PORT)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local"
EOF

GOISP_ADMIN_PASSWORD="$ADMIN_PASSWORD" "$REPO/bin/go-ispconfig" migrate --config "$WORK/config.toml"
"$REPO/bin/go-ispconfig" serve --config "$WORK/config.toml" > "$WORK/serve.log" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 30); do
  curl -fs "http://127.0.0.1:$PORT/" >/dev/null 2>&1 && break
  sleep 1
done

mkdir -p "$REPO/docs/prints" "$REPO/docs/screenshots"

# --- E2E flows -------------------------------------------------------------
# --no-sandbox: needed on Ubuntu 23.10+ hosts where AppArmor blocks Chrome's
# unprivileged user namespaces (harmless elsewhere, only applies on launch).
ab --args "--no-sandbox" open "http://127.0.0.1:$PORT/"
ab network har start
ab open "http://127.0.0.1:$PORT/"   # reload so the HAR captures the full asset load
ab wait --load networkidle

# Login page renders
ab get url | grep -q "/login" || { echo "FAIL: not on /login"; exit 1; }
ab screenshot "$REPO/docs/prints/login.png"

# Login flow (semantic locators — no snapshot refs needed)
ab find label "Username" fill admin
ab find label "Password" fill "$ADMIN_PASSWORD"
ab find role button click --name "Login"
ab wait --url "**/dashboard"
ab snapshot -i | grep -q 'link "Sites"' || { echo "FAIL: topbar modules missing after login"; exit 1; }
ab screenshot "$REPO/docs/prints/dashboard.png"

# Module navigation: each module URL + a sidebar entry that identifies it.
check_module() { # <link-text> <url-suffix> <sidebar-text> <shot>
  ab find text "$1" click --exact
  ab wait --url "**/$2"
  ab wait --load networkidle
  ab snapshot -i | grep -q "$3" || { echo "FAIL: $1 sidebar missing '$3'"; exit 1; }
  ab screenshot "$REPO/docs/prints/$4.png"
  echo "PASS: $1"
}
check_module "Sites"  sites  'link "Websites"'      sites
check_module "DNS"    dns    'link "Zones"'         dns
check_module "System" system 'link "Server Config"' system

# Logout returns to the login page
ab find role button click --name "Logout admin"
ab wait --url "**/login"
ab snapshot -i | grep -q 'textbox "Username"' || { echo "FAIL: login form not shown after logout"; exit 1; }
echo "PASS: logout"

# Zero external requests: every HAR entry must target localhost.
ab network har stop "$WORK/trace.har"
python3 - "$WORK/trace.har" <<'PY'
import json, sys
from urllib.parse import urlparse
entries = json.load(open(sys.argv[1]))["log"]["entries"]
hosts = sorted({urlparse(e["request"]["url"]).hostname for e in entries})
external = [h for h in hosts if h not in ("127.0.0.1", "localhost")]
assert not external, f"FAIL: external requests to {external}"
print(f"PASS: zero external requests ({len(entries)} requests, hosts={hosts})")
PY

# --- Curated screenshots (committed) ---------------------------------------
cp "$REPO/docs/prints/login.png" "$REPO/docs/prints/dashboard.png" "$REPO/docs/screenshots/"

echo "E2E panel smoke: ALL PASS"
