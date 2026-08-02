#!/usr/bin/env bash
# agent-browser E2E for the System → Firewall module UI
# (add-firewall-module, task 4.4).
#
# Flows (spec firewall-panel-ui): the superadmin opens System → Firewall,
# creates a record (server + TCP/UDP ports), edits the ports, toggles
# active off then on, and deletes it; a non-admin client user can neither
# see the Firewall nav section nor open /system/firewall (the router guard
# redirects to the dashboard, the API would 403 anyway). Screenshots go to
# docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8094 ADMIN_PASSWORD=... e2e/panel-firewall.sh
#
# Requires: agent-browser, a running panel with a freshly migrated DB
# (admin + server row seeded).
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
  el.value = $(python3 -c "import json;print(json.dumps('$2'))")
  el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
})(); 'set'
EOF
}

# set_checkbox <selector> <y|n> — the active field is a boolean checkbox.
set_checkbox() {
  local want=false
  [ "$2" = "y" ] && want=true
  $AB eval --stdin >/dev/null <<EOF
(() => {
  const el = document.querySelector('$1')
  el.checked = $want
  el.dispatchEvent(new Event('change', { bubbles: true }))
})(); 'set'
EOF
}

login() {
  local user=$1 pw_json
  pw_json=$(python3 -c "import json, os; print(json.dumps(os.environ['$2']))")
  $AB open "$PANEL_URL/login" >/dev/null
  $AB wait --load networkidle >/dev/null
  $AB eval --stdin >/dev/null <<EOF
(() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    el.value = v
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }
  set('#login-username', '$user')
  set('#login-password', $pw_json)
  document.querySelector('button[type=submit]').click()
})(); 'submitted'
EOF
  $AB wait --load networkidle >/dev/null
  # Deterministic: wait until the server session reports this user, so a
  # later request can never race an un-switched session.
  local i who
  for i in $(seq 1 30); do
    who=$(evaljs "fetch('/api/session').then(r=>r.ok?r.json():{}).then(s=>s.username||'').catch(()=>'')")
    [ "$who" = "$user" ] && return 0
    sleep 0.5
  done
  fail "login $user: session still reports '$who' after submit"
}

logout() {
  # Deterministic: end the server session via the API (with its CSRF),
  # then land on /login. Clicking the header button races the SPA state.
  evaljs "fetch('/api/session').then(r=>r.json()).then(s=>fetch('/api/logout',{method:'POST',headers:{'X-CSRF-Token':s.csrf_token||''}})).then(()=>'out').catch(()=>'out')" >/dev/null || true
  sleep 1
  $AB open "$PANEL_URL/login" >/dev/null
  $AB wait --load networkidle >/dev/null
  sleep 1
}

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" --ignore-https-errors >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null

# --------------------------------------------------------- superadmin login
login admin ADMIN_PASSWORD
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"

# csrf() — the app keeps the token module-local; read it from the session.
csrf_js='fetch("/api/session").then(r=>r.json()).then(s=>s.csrf_token)'

# Ensure a clean slate: delete any pre-existing firewall row for server 1.
evaljs "$csrf_js.then(tok=>fetch('/api/firewall?server_id=1').then(r=>r.json()).then(d=>Promise.all((d.items||[]).map(x=>fetch('/api/firewall/'+x.firewall_id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true

# ------------------------------------------------------------- create record
$AB open "$PANEL_URL/system/firewall/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "firewall form renders" "document.querySelector('#field-server_id') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-tcp_port' '22,80,443'
set_field '#field-udp_port' '53'
set_checkbox '#field-active' 'y'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "record saved (back on list)" "location.pathname" '/system/firewall'
wait_eval "record listed with its TCP ports" "document.body.innerText.includes('22,80,443')" 'true'
$AB screenshot "$PRINTS/firewall-e2e-list.png" >/dev/null

FW_ID=$(evaljs "fetch('/api/firewall?server_id=1').then(r=>r.json()).then(d=>String(d.items[0].firewall_id))")
[ -n "$FW_ID" ] || fail "could not resolve the created firewall_id"
ok "created firewall_id=$FW_ID"

# --------------------------------------------------------------- edit ports
$AB open "$PANEL_URL/system/firewall/$FW_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "edit form loads existing ports" "document.querySelector('#field-tcp_port').value" '22,80,443'
SERVER_DISABLED=$(evaljs "(() => { const f = document.querySelector('#field-server_id'); return String(!!(f && (f.disabled || f.readOnly))) })()")
expect "server_id field disabled on edit" "$SERVER_DISABLED" 'true'
set_field '#field-tcp_port' '22,80,443,8080'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "edited ports persisted" "location.pathname" '/system/firewall'
NEWPORTS=$(evaljs "fetch('/api/firewall/$FW_ID').then(r=>r.json()).then(f=>f.tcp_port)")
expect "tcp_port updated via the form" "$NEWPORTS" '22,80,443,8080'
$AB screenshot "$PRINTS/firewall-e2e-edit.png" >/dev/null

# ------------------------------------------------------------ toggle active
$AB open "$PANEL_URL/system/firewall/$FW_ID" >/dev/null
$AB wait --load networkidle >/dev/null
set_checkbox '#field-active' 'n'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "active toggled off (back on list)" "location.pathname" '/system/firewall'
ACT=$(evaljs "fetch('/api/firewall/$FW_ID').then(r=>r.json()).then(f=>f.active)")
expect "active is now n" "$ACT" 'n'
$AB open "$PANEL_URL/system/firewall/$FW_ID" >/dev/null
$AB wait --load networkidle >/dev/null
set_checkbox '#field-active' 'y'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "active toggled back on" "location.pathname" '/system/firewall'
ACT=$(evaljs "fetch('/api/firewall/$FW_ID').then(r=>r.json()).then(f=>f.active)")
expect "active is now y" "$ACT" 'y'

# ------------------------------------------- non-admin cannot see the module
# Create a client (type=user) through the client API, then log in as it.
evaljs "$csrf_js.then(tok=>fetch('/api/clients',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':tok},body:JSON.stringify({contact_name:'FW E2E Client',username:'fwe2eclient',password:'fw-e2e-pw-123',email:'fwe2e@example.com'})}).then(r=>String(r.status)))" >/dev/null || true
sleep 1
logout
FW_CLIENT_PW=fw-e2e-pw-123 login fwe2eclient FW_CLIENT_PW
NAV=$(evaljs "(() => { const s = [...document.querySelectorAll('a')].some(a => (a.getAttribute('href')||'').includes('/system/firewall')); return String(s) })()")
expect "Firewall nav section hidden for a non-admin" "$NAV" 'false'
$AB open "$PANEL_URL/system/firewall" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
GUARD=$(evaljs "location.pathname")
[ "$GUARD" != "/system/firewall" ] || fail "non-admin reached /system/firewall (guard failed)"
ok "non-admin redirected away from /system/firewall (landed on $GUARD)"
$AB screenshot "$PRINTS/firewall-e2e-nonadmin.png" >/dev/null

# --------------------------------------------------------------- delete row
logout
login admin ADMIN_PASSWORD
$AB open "$PANEL_URL/system/firewall" >/dev/null
$AB wait --load networkidle >/dev/null
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/firewall/$FW_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "delete returns 204" "$DEL" '204'
# The record is gone from the list (a GET on a missing id returns 403 by
# design — missing and denied are indistinguishable — so assert the list).
wait_eval "record no longer in the firewall list" \
  "fetch('/api/firewall?server_id=1').then(r=>r.json()).then(d=>String((d.items||[]).some(x=>String(x.firewall_id)==='$FW_ID')))" 'false'
ok "firewall record deleted"

echo "PASS ($PASS checks)"
