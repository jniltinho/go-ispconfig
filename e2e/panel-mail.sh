#!/usr/bin/env bash
# agent-browser E2E for the Mail module UI (add-mail-module, task 8.5).
#
# Flows (spec mail-panel-ui): admin creates a mail domain and generates a
# DKIM key, creates a mailbox, an alias and a transport, then a spamfilter
# policy and a whitelist entry. Screenshots go to docs/prints/.
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8093 ADMIN_PASSWORD=... e2e/panel-mail.sh
#
# Requires: agent-browser, a running panel with a freshly migrated DB
# whose server row has mail_server = 1.
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
    if [ "$actual" = "$3" ]; then ok "$1"; return 0; fi
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

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null

wait_eval "login form mounted" "document.querySelector('#login-username') !== null" 'true'
PASS_JSON=$(python3 -c 'import json, os; print(json.dumps(os.environ["ADMIN_PASSWORD"]))')
$AB eval --stdin >/dev/null <<LOGIN_EOF
(() => {
  const set = (sel, v) => { const el = document.querySelector(sel); el.value = v; el.dispatchEvent(new Event('input', { bubbles: true })) }
  set('#login-username', 'admin')
  set('#login-password', ${PASS_JSON})
  document.querySelector('button[type=submit]').click()
})(); 'submitted'
LOGIN_EOF
$AB wait --load networkidle >/dev/null
sleep 1
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
MAIL_TAB=$(evaljs "document.querySelector('header nav a[href=\\'/mail\\']') !== null")
expect "Email module tab visible" "$MAIL_TAB" 'true'

# ---------------------------------------------------- create mail domain + DKIM
$AB open "$PANEL_URL/mail/domains/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "domain form renders" "document.querySelector('#field-domain') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-domain' 'e2e-mail.example'
# Enable + generate DKIM.
evaljs "document.querySelector('[data-test=dkim-generate]').click(); 'gen'" >/dev/null
wait_eval "DKIM private key populated" "document.querySelector('#field-dkim_private').value.includes('PRIVATE KEY')" 'true'
DKIM_ON=$(evaljs "document.querySelector('#field-dkim').checked")
expect "DKIM enabled by generate" "$DKIM_ON" 'true'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "domain saved (back on list)" "location.pathname" '/mail'
wait_eval "domain listed" "document.body.innerText.includes('e2e-mail.example')" 'true'
$AB screenshot "$PRINTS/mail-e2e-domain.png" >/dev/null

# ---------------------------------------------------------------- create mailbox
$AB open "$PANEL_URL/mail/mailboxes/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "mailbox form renders" "document.querySelector('#field-email') !== null" 'true'
PW_TYPE=$(evaljs "document.querySelector('#field-password').type")
expect "password is a write-only input" "$PW_TYPE" 'password'
set_field '#field-email' 'user1@e2e-mail.example'
set_field '#field-password' 'e2e-mbox-pw-1'
set_field '#field-quota' '1048576'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "mailbox saved" "location.pathname" '/mail/mailboxes'
wait_eval "mailbox listed" "document.body.innerText.includes('user1@e2e-mail.example')" 'true'
$AB screenshot "$PRINTS/mail-e2e-mailbox.png" >/dev/null

# ----------------------------------------------------------------- create alias
$AB open "$PANEL_URL/mail/aliases/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "alias form renders" "document.querySelector('#field-source') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-source' 'alias@e2e-mail.example'
set_field '#field-destination' 'user1@e2e-mail.example'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "alias saved" "location.pathname" '/mail/aliases'
wait_eval "alias listed" "document.body.innerText.includes('alias@e2e-mail.example')" 'true'

# --------------------------------------------------------------- create transport
$AB open "$PANEL_URL/mail/transports/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "transport form renders" "document.querySelector('#field-domain') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-domain' 'relay.e2e.example'
set_field '#field-transport' 'smtp:[10.0.0.9]:25'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "transport saved" "location.pathname" '/mail/transports'
wait_eval "transport listed" "document.body.innerText.includes('relay.e2e.example')" 'true'
$AB screenshot "$PRINTS/mail-e2e-transport.png" >/dev/null

# ---------------------------------------------------- spamfilter policy + whitelist
$AB open "$PANEL_URL/mail/spamfilter/policies/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "policy form renders" "document.querySelector('#field-policy_name') !== null" 'true'
set_field '#field-policy_name' 'E2E Normal'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "policy saved" "location.pathname" '/mail/spamfilter/policies'
wait_eval "policy listed" "document.body.innerText.includes('E2E Normal')" 'true'

# A spamfilter user to attach the whitelist to.
POLICY_ID=$(evaljs "fetch('/api/mail/spamfilter/policies?limit=100').then(r=>r.json()).then(j=>String(j.items[0].id))")
$AB open "$PANEL_URL/mail/spamfilter/users/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "spamfilter user form renders" "document.querySelector('#field-email') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-email' 'user1@e2e-mail.example'
set_field '#field-policy_id' "$POLICY_ID"
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "spamfilter user saved" "location.pathname" '/mail/spamfilter/users'

RID=$(evaljs "fetch('/api/mail/spamfilter/users?limit=100').then(r=>r.json()).then(j=>String(j.items[0].id))")
$AB open "$PANEL_URL/mail/spamfilter/wblists/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "wblist form renders" "document.querySelector('#field-email') !== null" 'true'
set_field '#field-server_id' '1'
set_field '#field-rid' "$RID"
set_field '#field-email' 'friend@good.example'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "wblist saved" "location.pathname" '/mail/spamfilter/wblists'
wait_eval "wblist listed" "document.body.innerText.includes('friend@good.example')" 'true'
$AB screenshot "$PRINTS/mail-e2e-spamfilter.png" >/dev/null

echo "PASS: $PASS checks — mail module E2E complete"
