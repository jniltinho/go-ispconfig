#!/usr/bin/env bash
# agent-browser E2E for the Client module UI (add-client-module, task 5.6).
#
# Flows (spec client-ui): admin creates a limit template, a reseller and
# a client under that reseller; assigns an additional template; creates a
# welcome message template; attempts send-message (delivery-disabled
# feedback without SMTP); reseller login sees only its own clients; the
# delete confirmation shows owned-resource counts and delete-everything
# removes the client. Screenshots go to docs/prints/ (never committed).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8092 ADMIN_PASSWORD=... e2e/panel-clients.sh
#
# Requires: agent-browser, a running panel with a freshly migrated DB
# (admin + server row seeded; no SMTP configured).
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"

AB=agent-browser
PASS=0
PRINTS=docs/prints
mkdir -p "$PRINTS"
trap '$AB close --all >/dev/null 2>&1 || true' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

ok() {
  PASS=$((PASS + 1))
  echo "ok $PASS: $*"
}

# evaljs <js> — evaluate in the page; surrounding quotes of string
# results are stripped so comparisons and URL interpolation stay plain.
evaljs() {
  $AB eval "$1" | sed 's/^"//; s/"$//'
}

expect() {
  [ "$2" = "$3" ] || fail "$1: got $2 — want $3"
  ok "$1"
}

# wait_eval <desc> <js> <want> — poll until match (30 x 500ms).
wait_eval() {
  local i actual
  for i in $(seq 1 30); do
    actual=$(evaljs "$2")
    if [ "$actual" = "$3" ]; then
      ok "$1"
      return 0
    fi
    sleep 0.5
  done
  fail "$1: got $actual — want $3"
}

# set_field <selector> <value> — set an input/select through v-model.
set_field() {
  $AB eval --stdin >/dev/null <<EOF
(() => {
  const el = document.querySelector('$1')
  el.value = $(python3 -c "import json,sys;print(json.dumps('$2'))")
  el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
})(); 'set'
EOF
}

# login <username> <password-env-name>
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
  sleep 1
}

logout() {
  evaljs "document.querySelector('header .btn-danger')?.click(); 'out'" >/dev/null || true
  sleep 1
  $AB open "$PANEL_URL/login" >/dev/null
  $AB wait --load networkidle >/dev/null
}

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null

# ------------------------------------------------------------ admin login
login admin ADMIN_PASSWORD
expect "admin login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
CLIENT_TAB=$(evaljs "document.querySelector('header nav a[href=\\'/clients\\']') !== null")
expect "Client module tab visible for admin" "$CLIENT_TAB" 'true'

# ---------------------------------------------------- create limit template
$AB open "$PANEL_URL/clients/limit-templates/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "limit template form renders" "document.querySelector('#field-template_name') !== null" 'true'
set_field '#field-template_name' 'E2E Addon'
set_field '#field-template_type' 'a'
set_field '#field-limit_web_domain' '3'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "limit template saved (back on list)" "location.pathname" '/clients/limit-templates'
wait_eval "limit template listed" "document.body.innerText.includes('E2E Addon')" 'true'
$AB screenshot "$PRINTS/clients-e2e-limit-template.png" >/dev/null

# ------------------------------------------------------- create a reseller
$AB open "$PANEL_URL/clients/resellers/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "reseller form renders" "document.querySelector('#field-contact_name') !== null" 'true'
LIMIT_CLIENT_FIELD=$(evaljs "(() => { const b=[...document.querySelectorAll('[role=tab], button')].find(x=>x.textContent.trim()==='Limits'); b?.click(); return document.querySelector('#field-limit_client') !== null })()")
expect "reseller form exposes limit_client on the Limits tab" "$LIMIT_CLIENT_FIELD" 'true'
evaljs "[...document.querySelectorAll('[role=tab], button')].find(x=>x.textContent.trim()==='Address')?.click(); 'tab'" >/dev/null
set_field '#field-contact_name' 'E2E Reseller'
set_field '#field-username' 'e2ereseller'
set_field '#field-password' 'e2e-res-pw-123'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "reseller saved (back on resellers list)" "location.pathname" '/clients/resellers'
wait_eval "reseller listed" "document.body.innerText.includes('e2ereseller')" 'true'
$AB screenshot "$PRINTS/clients-e2e-resellers.png" >/dev/null

RESELLER_ID=$(evaljs "fetch('/api/clients/by-username/e2ereseller').then(r=>r.json()).then(c=>String(c.client_id))")

# ------------------------------------- create a client under the reseller
$AB open "$PANEL_URL/clients/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "client form renders" "document.querySelector('#field-contact_name') !== null" 'true'
COUNTRY_OPTIONS=$(evaljs "document.querySelectorAll('#field-country option').length")
[ "$COUNTRY_OPTIONS" -gt 100 ] || fail "country select not populated from the API ($COUNTRY_OPTIONS options)"
ok "country select offers $COUNTRY_OPTIONS options"
PASSWORD_TYPE=$(evaljs "document.querySelector('#field-password').type")
expect "password renders as a write-only password input" "$PASSWORD_TYPE" 'password'
set_field '#field-contact_name' 'E2E Client'
set_field '#field-username' 'e2eclient'
set_field '#field-password' 'e2e-cli-pw-123'
set_field '#field-country' 'DE'
set_field '#field-parent_client_id' "$RESELLER_ID"
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "client saved (back on clients list)" "location.pathname" '/clients'
wait_eval "client listed" "document.body.innerText.includes('e2eclient')" 'true'
$AB screenshot "$PRINTS/clients-e2e-clients.png" >/dev/null

CLIENT_ID=$(evaljs "fetch('/api/clients/by-username/e2eclient').then(r=>r.json()).then(c=>String(c.client_id))")

# ------------------------------------------- assign an additional template
$AB open "$PANEL_URL/clients/$CLIENT_ID" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "edit form loads with the template manager" "document.querySelector('[data-test=additional-templates]') !== null" 'true'
evaljs "
(() => {
  const sel = document.querySelector('[data-test=add-template-select]')
  const opt = [...sel.options].find(o => o.textContent.includes('E2E Addon'))
  sel.value = opt.value
  sel.dispatchEvent(new Event('change', { bubbles: true }))
  document.querySelector('[data-test=add-template]').click()
})(); 'assign'
" >/dev/null
wait_eval "template assigned and listed" \
  "document.querySelectorAll('[data-test=additional-templates] li').length" '1'
CAPPED=$(evaljs "fetch('/api/clients/$CLIENT_ID').then(r=>r.json()).then(c=>String(c.limit_web_domain))")
ok "materialized limit_web_domain after assignment: $CAPPED"
$AB screenshot "$PRINTS/clients-e2e-template-assign.png" >/dev/null

# ------------------------------------------ create welcome message template
$AB open "$PANEL_URL/clients/message-templates/new" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "message template form renders" "document.querySelector('#field-template_name') !== null" 'true'
set_field '#field-template_type' 'welcome'
set_field '#field-template_name' 'E2E Welcome'
set_field '#field-subject' 'Welcome {username}'
set_field '#field-message' 'Hello {contact_name}'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
wait_eval "message template saved" "location.pathname" '/clients/message-templates'
wait_eval "message template listed" "document.body.innerText.includes('E2E Welcome')" 'true'

# ------------------------------------------------- send-message (no SMTP)
$AB open "$PANEL_URL/clients/send-message" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "send message form renders" "document.querySelector('#msg-subject') !== null" 'true'
set_field '#msg-subject' 'E2E hello'
set_field '#msg-body' 'Hi {contact_name}'
evaljs "document.querySelector('[data-test=send-message]').click(); 'send'" >/dev/null
wait_eval "delivery-disabled feedback shown" "document.querySelector('[data-test=send-error]') !== null" 'true'
$AB screenshot "$PRINTS/clients-e2e-send-message.png" >/dev/null

# --------------------------------------------------- reseller isolation
logout
export RESELLER_PASSWORD='e2e-res-pw-123'
login e2ereseller RESELLER_PASSWORD
expect "reseller login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
$AB open "$PANEL_URL/clients" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "reseller sees exactly its own client" \
  "document.querySelectorAll('tbody tr:not([data-test=skeleton-row]):not([data-test=empty-state])').length" '1'
OWN=$(evaljs "document.body.innerText.includes('e2eclient')")
expect "the visible client is e2eclient" "$OWN" 'true'
RESELLERS_LINK=$(evaljs "[...document.querySelectorAll('aside a, nav a')].some(a => a.getAttribute('href') === '/clients/resellers')")
expect "resellers section hidden from the reseller" "$RESELLERS_LINK" 'false'
$AB screenshot "$PRINTS/clients-e2e-reseller-scope.png" >/dev/null

# --------------------------------------------- delete flow (confirmation)
logout
login admin ADMIN_PASSWORD
$AB open "$PANEL_URL/clients" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "clients list renders" "document.querySelectorAll('[data-test=delete]').length >= 1" 'true'
evaljs "
(() => {
  const row = [...document.querySelectorAll('tbody tr')].find(tr => tr.textContent.includes('e2eclient'))
  row.querySelector('[data-test=delete]').click()
})(); 'open'
" >/dev/null
wait_eval "delete dialog opens" "document.querySelector('[data-test=delete-dialog]') !== null" 'true'
wait_eval "dialog shows owned dns zone count field" "document.querySelector('[data-test=count-dns_zones]') !== null" 'true'
$AB screenshot "$PRINTS/clients-e2e-delete-dialog.png" >/dev/null
evaljs "document.querySelector('[data-test=delete-everything]').click(); 'boom'" >/dev/null
wait_eval "client removed from the list" "document.body.innerText.includes('e2eclient')" 'false'

echo "PASS: $PASS checks — client module E2E complete"
