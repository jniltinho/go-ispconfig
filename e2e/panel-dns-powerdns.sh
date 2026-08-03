#!/usr/bin/env bash
# agent-browser E2E for the DNS module UI with the server row set to the
# PowerDNS backend (add-dns-powerdns-module, task 6.2).
#
# Flows: admin logs in, creates a zone through the wizard against the
# "Default" template (DOMAIN/IP/NS1/NS2/EMAIL, DNSSEC checkbox ticked),
# lands on the zone form with the template's A/NS/MX/TXT records already
# present, then adds one more record by hand through the record dialog.
# The panel/API never look at dns_backend — this proves the DNS screens
# work unchanged when the server is provisioned for PowerDNS instead of
# Bind. Screenshots go to docs/prints/ (never committed).
#
# What this script does NOT cover: the daemon is not started here (no
# panel-*.sh script runs the daemon — see scripts/e2e-panel.sh), so the
# PowerDNS SQL sync (dns_soa/dns_rr -> powerdns.domains/records via
# pdns_control/pdnsutil) is not exercised by this script. That path is
# covered end-to-end against a real MariaDB container by
# TestDatalogToPowerDNSPipeline (internal/powerdns/e2e_integration_test.go,
# `go test -tags=integration ./internal/powerdns/...`).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8098 ADMIN_PASSWORD=... e2e/panel-dns-powerdns.sh
#
# Requires: agent-browser, a running panel with a freshly migrated DB whose
# local server row has [dns] dns_backend=powerdns (see
# scripts/e2e-dns-powerdns.sh for a self-contained bootstrap + run).
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"

AB=agent-browser
PASS=0
PRINTS=docs/prints
mkdir -p "$PRINTS"
trap '$AB close --all >/dev/null 2>&1 || true' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { PASS=$((PASS + 1)); echo "ok $PASS: $*"; }

evaljs() { $AB eval "$1" | sed 's/^"//; s/"$//'; }
expect() { [ "$2" = "$3" ] || fail "$1: got $2 — want $3"; ok "$1"; }

wait_eval() {
  local i actual
  for i in $(seq 1 30); do
    actual=$(evaljs "$2")
    [ "$actual" = "$3" ] && { ok "$1"; return 0; }
    sleep 0.5
  done
  fail "$1: got $actual — want $3"
}

set_field() {
  $AB eval --stdin >/dev/null <<EOF
(() => {
  const el = document.querySelector('$1')
  el.value = $(python3 -c "import json,sys;print(json.dumps('$2'))")
  el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
})(); 'set'
EOF
}

login() {
  local pw_json
  pw_json=$(python3 -c "import json, os; print(json.dumps(os.environ['ADMIN_PASSWORD']))")
  $AB open "$PANEL_URL/login" --args "--no-sandbox" >/dev/null
  $AB set viewport 1440 900 >/dev/null
  $AB wait --load networkidle >/dev/null
  $AB eval --stdin >/dev/null <<EOF
(() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    el.value = v
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }
  set('#login-username', 'admin')
  set('#login-password', $pw_json)
  document.querySelector('button[type=submit]').click()
})(); 'submitted'
EOF
  $AB wait --load networkidle >/dev/null
  sleep 1
}

$AB close --all >/dev/null 2>&1 || true
login
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"

# ---------------------------------------------------------------- DNS tab
$AB open "$PANEL_URL/dns" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "DNS zones list renders" "document.querySelector('[data-test=add-zone-wizard]') !== null" 'true'
$AB screenshot "$PRINTS/dns-powerdns-zones.png" >/dev/null

# ------------------------------------------------- create a zone (wizard)
evaljs "document.querySelector('[data-test=add-zone-wizard]').click(); 'open'" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "wizard form renders" "document.querySelector('#wizard-template') !== null" 'true'

DOMAIN="e2e-powerdns-$(date +%s).test"
set_field '#wizard-domain' "$DOMAIN"
set_field '#wizard-ip' '203.0.113.10'
set_field '#wizard-ns1' 'ns1.e2e.test'
set_field '#wizard-ns2' 'ns2.e2e.test'
set_field '#wizard-email' "hostmaster@$DOMAIN"
DNSSEC_BEFORE=$(evaljs "document.querySelector('#wizard-dnssec')?.checked")
[ "$DNSSEC_BEFORE" = "false" ] || fail "DNSSEC checkbox should start unticked"
evaljs "document.querySelector('#wizard-dnssec').click(); 'toggled'" >/dev/null
DNSSEC_AFTER=$(evaljs "document.querySelector('#wizard-dnssec')?.checked")
expect "DNSSEC wanted checkbox toggled on" "$DNSSEC_AFTER" 'true'
$AB screenshot "$PRINTS/dns-powerdns-wizard.png" >/dev/null

evaljs "document.querySelector('[data-test=wizard-create]').click(); 'create'" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "zone created, records tab renders" "document.querySelector('[data-test=record-grid]') !== null" 'true'
ok "zone form loaded on the Records tab (backend is server-config only, panel unaware of powerdns)"

# The Default template seeds A (apex/www/mail), 2x NS, MX and TXT already
# (dns_template "Default" row from the embedded ispconfig3.sql dump).
wait_eval "template records present (A/NS/MX/TXT)" \
  "[...document.querySelectorAll('[data-test=record-grid] tbody tr')].map(tr=>tr.children[1]?.textContent.trim()).filter(Boolean).sort().join(',')" \
  'A,A,A,MX,NS,NS,TXT'
$AB screenshot "$PRINTS/dns-powerdns-zone-records.png" >/dev/null

# ------------------------------------------------- add one record by hand
evaljs "document.querySelector('[data-test=add-record]').click(); 'open'" >/dev/null
wait_eval "record dialog opens" "document.querySelector('[data-test=record-dialog]') !== null" 'true'
set_field '#rr-type' 'A'
set_field '#rr-name' 'ftp'
set_field '#rr-data' '203.0.113.11'
evaljs "document.querySelector('[data-test=record-save]').click(); 'save'" >/dev/null
wait_eval "manual A record listed" \
  "[...document.querySelectorAll('[data-test=record-grid] td')].some(td=>td.textContent.trim()==='ftp')" 'true'
$AB screenshot "$PRINTS/dns-powerdns-zone-manual-record.png" >/dev/null

echo "DNS (PowerDNS backend) panel E2E smoke: ALL PASS ($PASS checks)"
