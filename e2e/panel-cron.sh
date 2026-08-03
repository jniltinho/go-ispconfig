#!/usr/bin/env bash
# agent-browser E2E for the Sites → Cron module UI (add-cron-module, task 6.1).
#
# Flows (spec cron-panel-ui): the admin creates a URL cron job on a website
# through the form, edits the schedule and the log/active toggles, opens the
# run history (seeded into sys_log when a DB command is provided), deletes the
# job, and a client without websites sees neither the job nor its history.
# Screenshots go to docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=https://127.0.0.1:8096 ADMIN_PASSWORD=... e2e/panel-cron.sh
#
# Optional: CRON_SQL="docker exec -i <container> mariadb -uroot -proot <db>"
# seeds one cron_run sys_log row so the history table shows real data. Without
# it the history is asserted empty.
#
# Requires: agent-browser, a running panel with a freshly migrated DB
# (admin + server row seeded).
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"
CRON_SQL=${CRON_SQL:-}

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

logout() {
  evaljs "fetch('/api/session').then(r=>r.json()).then(s=>fetch('/api/logout',{method:'POST',headers:{'X-CSRF-Token':s.csrf_token}})).then(()=>'out')" >/dev/null
  $AB open "$PANEL_URL/login" >/dev/null
  $AB wait --load networkidle >/dev/null
}

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" --ignore-https-errors >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null

# --------------------------------------------------------------- admin login
login admin ADMIN_PASSWORD
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"

csrf_js='fetch("/api/session").then(r=>r.json()).then(s=>s.csrf_token)'

# Clean slate plus the parent website the cron job hangs off.
evaljs "$csrf_js.then(tok=>fetch('/api/sites/crons').then(r=>r.json()).then(d=>Promise.all((d.items||[]).filter(x=>String(x.command).includes('crone2e')).map(x=>fetch('/api/sites/crons/'+x.id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true

DOMAIN_ID=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/web-domains?domain=crone2e.example.com').then(r=>r.json()).then(d=>d.items&&d.items[0]?String(d.items[0].domain_id):fetch('/api/sites/web-domains',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':tok},body:JSON.stringify({server_id:1,domain:'crone2e.example.com',type:'vhost'})}).then(r=>r.json()).then(x=>String(x.domain_id))))")
[ -n "$DOMAIN_ID" ] && [ "$DOMAIN_ID" != "undefined" ] || fail "could not create the parent website"
ok "parent website domain_id=$DOMAIN_ID"

# ---------------------------------------------------------- create a URL cron
$AB open "$PANEL_URL/sites/crons" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "cron list renders" "document.body.innerText.includes('Cron Jobs')" 'true'
$AB screenshot "$PRINTS/cron-e2e-list-empty.png" >/dev/null

$AB open "$PANEL_URL/sites/crons/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "cron form renders" "document.querySelector('#field-command') !== null" 'true'
TYPE_DISABLED=$(evaljs "(() => { const f = document.querySelector('#field-type'); return String(!!(f && (f.disabled || f.readOnly))) })()")
expect "type field is display-only (auto-derived)" "$TYPE_DISABLED" 'true'
set_field '#field-parent_domain_id' "$DOMAIN_ID"
set_field '#field-command' 'https://crone2e.example.com/cron.php'
set_field '#field-run_min' '*/15'
set_field '#field-run_hour' '*'
set_field '#field-run_mday' '*'
set_field '#field-run_month' '*'
set_field '#field-run_wday' '*'
$AB screenshot "$PRINTS/cron-e2e-form-new.png" >/dev/null

# Client-side validation blocks an obviously broken schedule before the POST.
set_field '#field-run_min' 'every-15'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "invalid minutes rejected on the client" \
  "String(document.body.innerText.includes('Invalid format for minutes'))" 'true'
expect "still on the create form" "$(evaljs 'location.pathname')" '/sites/crons/new'
$AB screenshot "$PRINTS/cron-e2e-form-invalid.png" >/dev/null

set_field '#field-run_min' '*/15'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "cron saved (back on list)" "location.pathname" '/sites/crons'
wait_eval "job listed with its command" \
  "String(document.body.innerText.includes('https://crone2e.example.com/cron.php'))" 'true'
wait_eval "list shows the parent website name" \
  "String(document.body.innerText.includes('crone2e.example.com'))" 'true'
$AB screenshot "$PRINTS/cron-e2e-list.png" >/dev/null

CRON_ID=$(evaljs "fetch('/api/sites/crons').then(r=>r.json()).then(d=>String(d.items.filter(x=>String(x.command).includes('crone2e'))[0].id))")
[ -n "$CRON_ID" ] || fail "could not resolve the created cron id"
ok "created cron id=$CRON_ID"

TYPE=$(evaljs "fetch('/api/sites/crons/$CRON_ID').then(r=>r.json()).then(c=>c.type)")
expect "URL command auto-derived to type=url" "$TYPE" 'url'

# ----------------------------------------------- edit schedule, log and active
$AB open "$PANEL_URL/sites/crons/$CRON_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "edit form loads the command" \
  "document.querySelector('#field-command').value" 'https://crone2e.example.com/cron.php'
PARENT_DISABLED=$(evaljs "(() => { const f = document.querySelector('#field-parent_domain_id'); return String(!!(f && (f.disabled || f.readOnly))) })()")
expect "parent website disabled on edit" "$PARENT_DISABLED" 'true'
set_field '#field-run_min' '30'
set_field '#field-run_hour' '2'
set_checkbox '#field-log' 'y'
set_checkbox '#field-active' 'n'
$AB screenshot "$PRINTS/cron-e2e-form-edit.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "edit saved (back on list)" "location.pathname" '/sites/crons'
SAVED=$(evaljs "fetch('/api/sites/crons/$CRON_ID').then(r=>r.json()).then(c=>[c.run_min,c.run_hour,c.log,c.active].join(':'))")
expect "schedule, log and active persisted" "$SAVED" '30:2:y:n'

# ------------------------------------------------------------------ run history
if [ -n "$CRON_SQL" ]; then
  NOW=$(date +%s)
  $CRON_SQL <<EOF >/dev/null
INSERT INTO sys_log (server_id, datalog_id, loglevel, tstamp, message)
VALUES (1, 0, 0, $NOW, 'cron_run id=$CRON_ID parent_domain_id=$DOMAIN_ID type=url status=ok exit=200 start=$NOW end=$((NOW + 2)) output=E2E seeded run');
EOF
  ok "seeded one cron_run sys_log row"
fi

$AB open "$PANEL_URL/sites/crons/$CRON_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "run history section renders" \
  "String(document.querySelector('[data-test=cron-runs]') !== null)" 'true'
if [ -n "$CRON_SQL" ]; then
  wait_eval "seeded run visible in the history" \
    "String(document.querySelector('[data-test=cron-runs]').innerText.includes('E2E seeded run'))" 'true'
else
  wait_eval "history empty state for a job that never ran" \
    "String(document.querySelector('[data-test=cron-runs] [data-test=empty-state]') !== null)" 'true'
fi
$AB screenshot "$PRINTS/cron-e2e-runs.png" >/dev/null

# ------------------------------------------------- client isolation (non-admin)
evaljs "$csrf_js.then(tok=>fetch('/api/clients',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':tok},body:JSON.stringify({contact_name:'Cron E2E Client',username:'crone2eclient',password:'cron-e2e-pw-123',email:'crone2e@example.com'})}).then(r=>String(r.status)))" >/dev/null || true
sleep 1
logout
CRON_CLIENT_PW=cron-e2e-pw-123 login crone2eclient CRON_CLIENT_PW
$AB open "$PANEL_URL/sites/crons" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "client sees no cron jobs of other tenants" \
  "fetch('/api/sites/crons').then(r=>r.json()).then(d=>String((d.items||[]).length))" '0'
DENIED=$(evaljs "fetch('/api/sites/crons/$CRON_ID').then(r=>String(r.status))")
expect "client is denied the admin cron row" "$DENIED" '403'
RUNS_DENIED=$(evaljs "fetch('/api/sites/crons/$CRON_ID/runs').then(r=>String(r.status))")
expect "client is denied its run history" "$RUNS_DENIED" '403'
$AB screenshot "$PRINTS/cron-e2e-client-isolation.png" >/dev/null

# ------------------------------------------------------------------- delete job
logout
login admin ADMIN_PASSWORD
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/crons/$CRON_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "delete returns 204" "$DEL" '204'
wait_eval "job gone from the list" \
  "fetch('/api/sites/crons').then(r=>r.json()).then(d=>String((d.items||[]).some(x=>String(x.id)==='$CRON_ID')))" 'false'

echo "PASS ($PASS checks)"
