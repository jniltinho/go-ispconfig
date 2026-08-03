#!/usr/bin/env bash
# agent-browser E2E for the Sites → FTP Users and Shell Users module UI
# (add-ftp-shell-module, task 6.4).
#
# Flows: admin creates a parent website, creates an FTP user under it, edits
# dir/quota, deletes it; creates a shell user (non-jailkit), toggles active
# off, deletes it. Screenshots go to docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8097 ADMIN_PASSWORD=... e2e/panel-ftp-shell.sh
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

# Clean slate leftovers, then ensure a parent website exists.
evaljs "$csrf_js.then(tok=>fetch('/api/sites/ftp-users?username=ftpe2e').then(r=>r.json()).then(d=>Promise.all((d.items||[]).map(x=>fetch('/api/sites/ftp-users/'+x.ftp_user_id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true
evaljs "$csrf_js.then(tok=>fetch('/api/sites/shell-users?username=shelle2e').then(r=>r.json()).then(d=>Promise.all((d.items||[]).map(x=>fetch('/api/sites/shell-users/'+x.shell_user_id,{method:'DELETE',headers:{'X-CSRF-Token':tok}}))))).then(()=>'clean').catch(()=>'clean')" >/dev/null || true

DOMAIN_ID=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/web-domains?domain=ftpshell-e2e.example.com').then(r=>r.json()).then(d=>d.items&&d.items[0]?String(d.items[0].domain_id):fetch('/api/sites/web-domains',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':tok},body:JSON.stringify({server_id:1,domain:'ftpshell-e2e.example.com',type:'vhost'})}).then(r=>r.json()).then(x=>String(x.domain_id))))")
[ -n "$DOMAIN_ID" ] && [ "$DOMAIN_ID" != "undefined" ] || fail "could not create the parent website"
ok "parent website domain_id=$DOMAIN_ID"

# ---------------------------------------------------------------- FTP users
# Sidebar / list
$AB open "$PANEL_URL/sites/ftp-users" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "FTP Users page title" "document.body.innerText.includes('FTP Users')" 'true'
FTP_NAV=$(evaljs "document.querySelector('a[href=\"/sites/ftp-users\"]') !== null")
expect "FTP Users sidebar link present" "$FTP_NAV" 'true'
$AB screenshot "$PRINTS/ftp-e2e-list-empty.png" >/dev/null

# Create
$AB open "$PANEL_URL/sites/ftp-users/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "FTP form renders" "document.querySelector('#field-username') !== null" 'true'
set_field '#field-parent_domain_id' "$DOMAIN_ID"
set_field '#field-username' 'ftpe2e'
set_field '#field-password' 'Sup3r-Secret-1!'
set_field '#field-quota_size' '-1'
$AB screenshot "$PRINTS/ftp-e2e-form-create.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "FTP user saved (back on list)" "location.pathname" '/sites/ftp-users'
wait_eval "FTP user listed" "document.body.innerText.includes('ftpe2e')" 'true'
$AB screenshot "$PRINTS/ftp-e2e-list-created.png" >/dev/null

FTP_ID=$(evaljs "fetch('/api/sites/ftp-users?username=ftpe2e').then(r=>r.json()).then(d=>String(d.items[0].ftp_user_id))")
[ -n "$FTP_ID" ] && [ "$FTP_ID" != "undefined" ] || fail "could not resolve the created ftp_user_id"
ok "created ftp_user_id=$FTP_ID"

# Edit dir + quota (Options tab fields are in the DOM even when inactive)
$AB open "$PANEL_URL/sites/ftp-users/$FTP_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "FTP edit form loads username" "document.querySelector('#field-username').value" 'ftpe2e'
DOCROOT=$(evaljs "fetch('/api/sites/web-domains/$DOMAIN_ID').then(r=>r.json()).then(d=>d.document_root)")
[ -n "$DOCROOT" ] && [ "$DOCROOT" != "undefined" ] || fail "could not read document_root"
SUBDIR="${DOCROOT}/uploads"
# Click the Options/limits tab when present so screenshots show the fields.
evaljs "Array.from(document.querySelectorAll('button,[role=tab],a')).find(el=>/limit|option|advanced|directory/i.test(el.textContent||''))?.click(); 'tab'" >/dev/null || true
sleep 0.3
$AB eval --stdin >/dev/null <<EOF
(() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    if (!el) return
    el.value = v
    el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
  }
  set('#field-dir', $(python3 -c "import json; print(json.dumps('$SUBDIR'))"))
  set('#field-quota_size', '512')
})(); 'set'
EOF
$AB screenshot "$PRINTS/ftp-e2e-form-edit.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "FTP edit saved" "location.pathname" '/sites/ftp-users'
FTP_STATE=$(evaljs "fetch('/api/sites/ftp-users/$FTP_ID').then(r=>r.json()).then(d=>String(d.quota_size)+':'+d.dir)")
expect "FTP quota and dir persisted" "$FTP_STATE" "512:$SUBDIR"
$AB screenshot "$PRINTS/ftp-e2e-list-edited.png" >/dev/null

# Delete via API (UI confirm() is awkward for agent-browser)
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/ftp-users/$FTP_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "FTP delete returns 204" "$DEL" '204'
wait_eval "FTP user gone from API" \
  "fetch('/api/sites/ftp-users?username=ftpe2e').then(r=>r.json()).then(d=>String((d.items||[]).length))" '0'
ok "FTP user deleted"

# --------------------------------------------------------------- Shell users
$AB open "$PANEL_URL/sites/shell-users" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "Shell Users page title" "document.body.innerText.includes('Shell Users')" 'true'
SHELL_NAV=$(evaljs "document.querySelector('a[href=\"/sites/shell-users\"]') !== null")
expect "Shell Users sidebar link present" "$SHELL_NAV" 'true'
$AB screenshot "$PRINTS/shell-e2e-list-empty.png" >/dev/null

# Create non-jailkit shell user
$AB open "$PANEL_URL/sites/shell-users/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "Shell form renders" "document.querySelector('#field-username') !== null" 'true'
set_field '#field-parent_domain_id' "$DOMAIN_ID"
set_field '#field-username' 'shelle2e'
set_field '#field-password' 'Sup3r-Secret-1!'
# chroot empty/no = non-jailkit
set_field '#field-chroot' ''
$AB screenshot "$PRINTS/shell-e2e-form-create.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "shell user saved (back on list)" "location.pathname" '/sites/shell-users'
wait_eval "shell user listed" "document.body.innerText.includes('shelle2e')" 'true'
$AB screenshot "$PRINTS/shell-e2e-list-created.png" >/dev/null

SHELL_ID=$(evaljs "fetch('/api/sites/shell-users?username=shelle2e').then(r=>r.json()).then(d=>String(d.items[0].shell_user_id))")
[ -n "$SHELL_ID" ] && [ "$SHELL_ID" != "undefined" ] || fail "could not resolve the created shell_user_id"
ok "created shell_user_id=$SHELL_ID"

# Toggle active off
$AB open "$PANEL_URL/sites/shell-users/$SHELL_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "shell edit form loads username" "document.querySelector('#field-username').value" 'shelle2e'
PARENT_DISABLED=$(evaljs "(() => { const f = document.querySelector('#field-parent_domain_id'); return String(!!(f && (f.disabled || f.readOnly))) })()")
expect "parent_domain_id locked on edit" "$PARENT_DISABLED" 'true'
set_checkbox '#field-active' 'n'
$AB screenshot "$PRINTS/shell-e2e-form-inactive.png" >/dev/null
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "shell active toggle saved" "location.pathname" '/sites/shell-users'
ACTIVE=$(evaljs "fetch('/api/sites/shell-users/$SHELL_ID').then(r=>r.json()).then(d=>d.active)")
expect "shell user inactive" "$ACTIVE" 'n'
$AB screenshot "$PRINTS/shell-e2e-list-inactive.png" >/dev/null

# Delete
DEL=$(evaljs "$csrf_js.then(tok=>fetch('/api/sites/shell-users/$SHELL_ID',{method:'DELETE',headers:{'X-CSRF-Token':tok}}).then(r=>String(r.status)))")
expect "shell delete returns 204" "$DEL" '204'
wait_eval "shell user gone from API" \
  "fetch('/api/sites/shell-users?username=shelle2e').then(r=>r.json()).then(d=>String((d.items||[]).length))" '0'
ok "shell user deleted"

echo "PASS ($PASS checks)"
