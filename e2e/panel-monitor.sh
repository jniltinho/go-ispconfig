#!/usr/bin/env bash
# agent-browser E2E for the Monitor module UI (add-monitor-module, task 6.2).
#
# Flows (design D10): admin login → system state overview → one metric
# detail (cpu_info) → ISPConfig log (sys_log) → job queue → datalog history
# detail (old/new diff) → scheduler jobs (admin-only) → a client without the
# monitor module sees neither the nav tab nor real data. Screenshots go to
# docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:18080 ADMIN_PASSWORD=... e2e/panel-monitor.sh
#
# Optional: MONITOR_SQL="docker exec -i <container> mariadb -uroot -proot <db>"
# seeds monitor_data/sys_log/sys_datalog/sys_config rows (task 6.1) so every
# view shows real data instead of its empty state — the daemon never runs in
# this harness (only `serve`), so without a daemon or this seed the tables
# stay empty and the script asserts the empty states instead.
#
# Requires: agent-browser, a running panel with a freshly migrated DB
# (admin + server row seeded).
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"
MONITOR_SQL=${MONITOR_SQL:-}

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

# --------------------------------------------------------- seed monitor data
if [ -n "$MONITOR_SQL" ]; then
  NOW=$(date +%s)
  $MONITOR_SQL <<EOF >/dev/null
REPLACE INTO monitor_data (server_id, type, created, data, state) VALUES
 (1, 'cpu_info', $NOW, '{"model":"E2E Virtual CPU","cores":"4"}', 'no_state'),
 (1, 'disk_usage', $NOW, '{"1":{"fs":"/dev/sda1","type":"ext4","size":"20G","used":"5G","available":"15G","percent":"25%","mounted":"/"}}', 'ok'),
 (1, 'services', $NOW, '{"webserver":1,"ftpserver":-1,"smtpserver":-1,"pop3server":-1,"imapserver":-1,"bindserver":-1,"mysqlserver":1}', 'ok');
INSERT INTO sys_log (server_id, datalog_id, loglevel, tstamp, message)
VALUES (1, 0, 1, $NOW, 'E2E seeded monitor warning');
INSERT INTO sys_datalog (server_id, dbtable, dbidx, action, tstamp, user, data, status)
VALUES (1, 'web_domain', 'domain_id:998', 'u', $NOW - 60, 'admin', '{"old":{"active":"y"},"new":{"active":"n"}}', 'ok');
SET @consumed := LAST_INSERT_ID();
INSERT INTO sys_datalog (server_id, dbtable, dbidx, action, tstamp, user, data, status)
VALUES (1, 'web_domain', 'domain_id:999', 'u', $NOW, 'admin', '{"old":{"active":"n"},"new":{"active":"y"}}', 'ok');
-- server.updated marks how far the daemon consumed: row 998 is history only,
-- row 999 is still pending, so jobqueue and history are distinguishable.
UPDATE server SET updated = @consumed WHERE server_id = 1;
REPLACE INTO sys_config (\`group\`, \`name\`, \`value\`) VALUES
 ('scheduler', 'monitor_cpu_info_spec', '*/5 * * * *'),
 ('scheduler', 'monitor_cpu_info_status', 'ok');
EOF
  ok "seeded monitor_data/sys_log/sys_datalog/sys_config rows"
fi

# --------------------------------------------------------------- admin login
login admin ADMIN_PASSWORD
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
NAV=$(evaljs "(() => { const s = [...document.querySelectorAll('header nav a')].some(a => (a.getAttribute('href')||'') === '/monitor'); return String(s) })()")
expect "Monitor nav tab visible for admin" "$NAV" 'true'

# ------------------------------------------------------------ system state
$AB open "$PANEL_URL/monitor" >/dev/null
$AB wait --load networkidle >/dev/null
if [ -n "$MONITOR_SQL" ]; then
  wait_eval "server card shows the seeded severity" \
    "String(document.querySelector('[data-test=monitor-server-1]') !== null)" 'true'
else
  # The server row always appears (one card per readable server); without a
  # daemon it just has no samples yet, so the state folds to "unknown".
  wait_eval "server card renders with no samples yet" \
    "String(document.querySelector('[data-test=monitor-server-1]') !== null)" 'true'
  wait_eval "unfed server state is unknown" \
    "document.querySelector('[data-test=monitor-server-state]').textContent.trim()" 'Unknown'
fi
$AB screenshot "$PRINTS/monitor-e2e-state.png" >/dev/null

# --------------------------------------------------------- one metric detail
$AB open "$PANEL_URL/monitor/data/cpu_info?server_id=1" >/dev/null
$AB wait --load networkidle >/dev/null
if [ -n "$MONITOR_SQL" ]; then
  wait_eval "cpu_info detail shows the seeded model" \
    "String(document.body.innerText.includes('E2E Virtual CPU'))" 'true'
else
  wait_eval "check detail empty state without a sample" \
    "String(document.querySelector('[data-test=monitor-data-empty]') !== null)" 'true'
fi
$AB screenshot "$PRINTS/monitor-e2e-check-detail.png" >/dev/null

# ------------------------------------------------------------------ sys_log
$AB open "$PANEL_URL/monitor/sys-log" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "sys_log table renders" "String(document.querySelector('table') !== null)" 'true'
if [ -n "$MONITOR_SQL" ]; then
  wait_eval "seeded log message listed" \
    "String(document.body.innerText.includes('E2E seeded monitor warning'))" 'true'
  CLEAR_BTN=$(evaljs "String(document.querySelector('[data-test=monitor-clear-one]') !== null)")
  expect "admin sees the clear action" "$CLEAR_BTN" 'true'
fi
$AB screenshot "$PRINTS/monitor-e2e-sys-log.png" >/dev/null

# ------------------------------------------------------------------ jobqueue
$AB open "$PANEL_URL/monitor/jobqueue" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "jobqueue table renders" "String(document.querySelector('table') !== null)" 'true'
if [ -n "$MONITOR_SQL" ]; then
  wait_eval "seeded pending datalog row listed" \
    "String(document.body.innerText.includes('web_domain'))" 'true'
fi
$AB screenshot "$PRINTS/monitor-e2e-jobqueue.png" >/dev/null

# -------------------------------------------------- datalog history + detail
$AB open "$PANEL_URL/monitor/datalog" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "datalog history table renders" "String(document.querySelector('table') !== null)" 'true'
if [ -n "$MONITOR_SQL" ]; then
  DATALOG_ID=$(evaljs "fetch('/api/monitor/datalog?limit=1').then(r=>r.json()).then(d=>String((d.items||[])[0]?.datalog_id||''))")
  [ -n "$DATALOG_ID" ] || fail "could not resolve a seeded datalog_id"
  $AB open "$PANEL_URL/monitor/datalog/$DATALOG_ID" >/dev/null
  $AB wait --load networkidle >/dev/null
  wait_eval "detail shows the old/new diff" \
    "String(document.querySelector('[data-test=datalog-field-active]') !== null)" 'true'
  NOUNDO=$(evaljs "String([...document.querySelectorAll('button,a')].some(el => /undo/i.test(el.textContent||'')))")
  expect "no undo control anywhere on the detail view" "$NOUNDO" 'false'
  $AB screenshot "$PRINTS/monitor-e2e-datalog-detail.png" >/dev/null
fi

# -------------------------------------------------------------- scheduler
$AB open "$PANEL_URL/monitor/scheduler" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "scheduler table renders" "String(document.querySelector('table') !== null)" 'true'
if [ -n "$MONITOR_SQL" ]; then
  wait_eval "seeded scheduler job listed" \
    "String(document.body.innerText.includes('monitor_cpu_info'))" 'true'
fi
$AB screenshot "$PRINTS/monitor-e2e-scheduler.png" >/dev/null

# ------------------------------------- non-admin without the monitor module
evaljs "fetch('/api/session').then(r=>r.json()).then(s=>fetch('/api/clients',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':s.csrf_token},body:JSON.stringify({contact_name:'Monitor E2E Client',username:'monitore2eclient',password:'mon-e2e-pw-123',email:'mone2e@example.com'})}).then(r=>String(r.status)))" >/dev/null || true
sleep 1
logout
MONITOR_CLIENT_PW=mon-e2e-pw-123 login monitore2eclient MONITOR_CLIENT_PW
NAV=$(evaljs "(() => { const s = [...document.querySelectorAll('header nav a')].some(a => (a.getAttribute('href')||'') === '/monitor'); return String(s) })()")
expect "Monitor nav tab hidden without the monitor module" "$NAV" 'false'
$AB open "$PANEL_URL/monitor/scheduler" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
GUARD=$(evaljs "location.pathname")
[ "$GUARD" != "/monitor/scheduler" ] || fail "non-admin reached /monitor/scheduler (guard failed)"
ok "non-admin redirected away from the admin-only scheduler page (landed on $GUARD)"
DATA403=$(evaljs "fetch('/api/monitor/state').then(r=>String(r.status))")
expect "monitor API 403s for a session without the module" "$DATA403" '403'

echo "PASS ($PASS checks)"
