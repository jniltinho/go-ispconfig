#!/usr/bin/env bash
# agent-browser E2E for the Sites → Databases module UI
# (add-database-module, task 6.5).
#
# Flows (spec database-panel-ui): the admin creates a database user and a
# database bound to a website through the forms, toggles remote access
# with an IP list, changes the user password (empty means unchanged) and
# deletes both records. Screenshots go to docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=https://127.0.0.1:8095 ADMIN_PASSWORD=... e2e/panel-database.sh
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

# set_checkbox <selector> <y|n> — checkbox fields are boolean v-models.
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
  local i who
  for i in $(seq 1 30); do
    who=$(evaljs "fetch('/api/session').then(r=>r.ok?r.json():{}).then(s=>s.username||'').catch(()=>'')")
    [ "$who" = "$user" ] && return 0
    sleep 0.5
  done
  fail "login $user: session still reports '$who' after submit"
}

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" --ignore-https-errors >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null

# --------------------------------------------------------------- admin login
login admin ADMIN_PASSWORD
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"

csrf_js='fetch("/api/session").then(r=>r.json()).then(s=>s.csrf_token)'

# Clean slate: remove any dbe2e leftovers, then create the parent website.
evaljs "$csrf_js.then(tok=>fetch('/api/sites/databases?database_name=dbe2e').then(r=>r.json()).then(d=>Promise.all((d.items||[]).map(x=>fetch('/api/sites/databases/'+x.database_id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true
evaljs "$csrf_js.then(tok=>fetch('/api/sites/database-users?database_user=dbe2e').then(r=>r.json()).then(d=>Promise.all((d.items||[]).map(x=>fetch('/api/sites/database-users/'+x.database_user_id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true

DOMAIN_ID=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/web-domains?domain=dbe2e.example.com').then(r=>r.json()).then(d=>d.items&&d.items[0]?String(d.items[0].domain_id):fetch('/api/sites/web-domains',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':tok},body:JSON.stringify({server_id:1,domain:'dbe2e.example.com',type:'vhost'})}).then(r=>r.json()).then(x=>String(x.domain_id))))")
[ -n "$DOMAIN_ID" ] && [ "$DOMAIN_ID" != "undefined" ] || fail "could not create the parent website"
ok "parent website domain_id=$DOMAIN_ID"

# ------------------------------------------------------ create database user
$AB open "$PANEL_URL/sites/database-users/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "database user form renders" "document.querySelector('#field-database_user') !== null" 'true'
set_field '#field-database_user' 'dbe2eu'
set_field '#field-database_password' 'Sup3r-Secret-1!'
$AB screenshot "$PRINTS/database-e2e-user-form.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "user saved (back on list)" "location.pathname" '/sites/database-users'
wait_eval "user listed" "document.body.innerText.includes('dbe2eu')" 'true'
$AB screenshot "$PRINTS/database-e2e-user-list.png" >/dev/null

USER_ID=$(evaljs "fetch('/api/sites/database-users?database_user=dbe2eu').then(r=>r.json()).then(d=>String(d.items[0].database_user_id))")
[ -n "$USER_ID" ] || fail "could not resolve the created database_user_id"
ok "created database_user_id=$USER_ID"

# ----------------------------------------------------------- create database
$AB open "$PANEL_URL/sites/databases/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "database form renders" "document.querySelector('#field-database_name') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-parent_domain_id' "$DOMAIN_ID"
set_field '#field-database_name' 'dbe2edb'
set_field '#field-database_user_id' "$USER_ID"
$AB screenshot "$PRINTS/database-e2e-db-form.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "database saved (back on list)" "location.pathname" '/sites/databases'
wait_eval "database listed" "document.body.innerText.includes('dbe2edb')" 'true'
$AB screenshot "$PRINTS/database-e2e-db-list.png" >/dev/null

DB_ID=$(evaljs "fetch('/api/sites/databases?database_name=dbe2edb').then(r=>r.json()).then(d=>String(d.items[0].database_id))")
[ -n "$DB_ID" ] || fail "could not resolve the created database_id"
ok "created database_id=$DB_ID"

# ------------------------------------------------------ remote access toggle
$AB open "$PANEL_URL/sites/databases/$DB_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "edit form loads the database name" "document.querySelector('#field-database_name').value" 'dbe2edb'
SERVER_DISABLED=$(evaljs "(() => { const f = document.querySelector('#field-server_id'); return String(!!(f && (f.disabled || f.readOnly))) })()")
expect "server_id field disabled on edit" "$SERVER_DISABLED" 'true'
set_checkbox '#field-remote_access' 'y'
set_field '#field-remote_ips' '10.0.0.9'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "remote access saved" "location.pathname" '/sites/databases'
REMOTE=$(evaljs "fetch('/api/sites/databases/$DB_ID').then(r=>r.json()).then(d=>d.remote_access+':'+d.remote_ips)")
expect "remote access + IPs persisted" "$REMOTE" 'y:10.0.0.9'
$AB screenshot "$PRINTS/database-e2e-db-remote.png" >/dev/null

# ------------------------------------------------------------ password change
$AB open "$PANEL_URL/sites/database-users/$USER_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "user edit form loads" "document.querySelector('#field-database_user').value" 'dbe2eu'
set_field '#field-database_password' 'An0ther-Secret-2!'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "password change saved" "location.pathname" '/sites/database-users'
ok "password changed through the form"

# -------------------------------------------------------------------- deletes
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/databases/$DB_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "database delete returns 204" "$DEL" '204'
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/database-users/$USER_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "database user delete returns 204" "$DEL" '204'
wait_eval "database gone from the list" \
  "fetch('/api/sites/databases?database_name=dbe2edb').then(r=>r.json()).then(d=>String((d.items||[]).length))" '0'

echo "PASS ($PASS checks)"
