#!/bin/bash
# Smoke test for a provisioned go-ispconfig guest (spec install-test-rig).
# Runs as root inside the VM; exits non-zero naming the failed check.
set -u

PANEL_IP="${PANEL_IP:-192.168.56.10}"
PANEL_PORT="${PANEL_PORT:-8080}"
BASE="https://${PANEL_IP}:${PANEL_PORT}"
CRED_FILE=/root/.go-ispconfig-credentials
RUN_ID=$(date +%s)
SITE_DOMAIN="smoke${RUN_ID}.goisp.test"
ZONE_DOMAIN="zone${RUN_ID}.goisp.test"

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }
pass() { echo "SMOKE OK: $1"; }

json_field() { # stdin: json, $1: key — prints string value or empty
  python3 -c "import sys,json; print(json.load(sys.stdin).get('$1',''))" 2>/dev/null
}

wait_for_file() { # $1: path, $2: seconds
  for _ in $(seq 1 "$2"); do
    [ -e "$1" ] && return 0
    sleep 1
  done
  return 1
}

# --- 1. systemd units active -------------------------------------------------
for u in go-ispconfig-serve go-ispconfig-daemon; do
  systemctl is-active --quiet "$u" || fail "unit-active:$u"
done
pass units-active

# --- 2. panel over HTTPS (self-signed) ---------------------------------------
curl -ksSf -o /dev/null "$BASE/" || fail panel-https
pass panel-https

# --- 3. REST API login as admin ----------------------------------------------
[ -f "$CRED_FILE" ] || fail "admin-credentials-file:$CRED_FILE missing (install --write-credentials)"
ADMIN_PW=$(sed -n 's|.*Admin login: admin / ||p' "$CRED_FILE" | head -1)
[ -n "$ADMIN_PW" ] || fail "admin-credentials-file:no password line in $CRED_FILE"
TOKEN=$(curl -ksS -X POST "$BASE/api/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PW}\"}" | json_field session_id)
[ -n "$TOKEN" ] || fail api-login
AUTH="Authorization: Bearer $TOKEN"
pass api-login

# --- 4. create website via API -> vhost exists and nginx -t passes -----------
SITE_ID=$(curl -ksS -X POST "$BASE/api/sites/web-domains" -H "$AUTH" \
  -H 'Content-Type: application/json' \
  -d "{\"server_id\":1,\"domain\":\"${SITE_DOMAIN}\",\"type\":\"vhost\",\"active\":\"y\"}" \
  | json_field domain_id)
[ -n "$SITE_ID" ] && [ "$SITE_ID" != "0" ] || fail api-create-site
VHOST="/etc/nginx/sites-available/${SITE_DOMAIN}.vhost"
wait_for_file "$VHOST" 120 || fail "site-vhost-rendered:$VHOST not written by the daemon"
nginx -t 2>/dev/null || fail nginx-t
pass api-create-site

# --- 5. create DNS zone via API -> zone file passes named-checkzone ----------
ZONE_ORIGIN=$(curl -ksS -X POST "$BASE/api/dns/zones/wizard" -H "$AUTH" \
  -H 'Content-Type: application/json' \
  -d "{\"template_id\":1,\"server_id\":1,\"domain\":\"${ZONE_DOMAIN}\",\"ip\":\"${PANEL_IP}\",\"ns1\":\"ns1.goisp.test\",\"ns2\":\"ns2.goisp.test\",\"email\":\"hostmaster@goisp.test\"}" \
  | json_field origin)
[ "$ZONE_ORIGIN" = "${ZONE_DOMAIN}." ] || fail api-create-zone
ZONEFILE="/etc/bind/pri.${ZONE_DOMAIN}"
wait_for_file "$ZONEFILE" 120 || fail "zone-file-rendered:$ZONEFILE not written by the daemon"
named-checkzone "${ZONE_DOMAIN}." "$ZONEFILE" >/dev/null || fail named-checkzone
pass api-create-zone

# --- 6. second install --yes is idempotent -----------------------------------
/usr/local/bin/go-ispconfig install --yes || fail install-idempotent-rerun
for u in go-ispconfig-serve go-ispconfig-daemon; do
  systemctl is-active --quiet "$u" || fail "install-idempotent-units:$u"
done
pass install-idempotent

echo "SMOKE TEST PASSED (${BASE})"
