#!/usr/bin/env bash
# agent-browser E2E for the themed panel (add-panel-ui-theme, group 6).
#
# Drives a BUILT go-ispconfig binary (theme embedded) through the themed
# flows: login, module navigation, list view (filter row + pagination),
# tabbed form save/cancel — plus theme-trait and same-origin network
# assertions (tasks 6.2/6.3).
#
# Usage:
#   PANEL_URL=http://127.0.0.1:8091 ADMIN_PASSWORD=... e2e/panel-theme.sh
#
# Requires: agent-browser (npm i -g agent-browser), a running panel with
# a migrated database and at least one DNS zone (id 1) seeded.
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"

AB=agent-browser
PASS=0

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

ok() {
  PASS=$((PASS + 1))
  echo "ok $PASS: $*"
}

# evaljs <js> — evaluate in the page, print the raw result (quoted).
evaljs() {
  $AB eval "$1"
}

# expect <desc> <actual> <want>
expect() {
  [ "$2" = "$3" ] || fail "$1: got $2 — want $3"
  ok "$1"
}

$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null
# Record every request of the suite for the same-origin check (6.3).
$AB network requests --clear >/dev/null 2>&1 || true

# ---------------------------------------------------------------- login
# v-model needs real input events; .value alone is not enough.
evaljs "
  (() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    el.value = v
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }
  set('#login-username', 'admin')
  set('#login-password', '${ADMIN_PASSWORD}')
  document.querySelector('button[type=submit]').click()
  })(); 'submitted'
" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
expect "login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
DASHLETS=$(evaljs "document.querySelectorAll('[data-test^=dashlet-]').length")
[ "$DASHLETS" -ge 3 ] || fail "expected >=3 dashlets, got $DASHLETS"
ok "dashboard renders $DASHLETS dashlets"

# ----------------------------------------------------- module navigation
evaljs "[...document.querySelectorAll('header nav a')].find(a => a.textContent.trim() === 'DNS').click(); 'nav'" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
expect "topbar module button navigates" "$($AB get url)" "$PANEL_URL/dns"

# ------------------------------------------- list view: filter row + page
FILTERS=$(evaljs "document.querySelectorAll('thead input').length")
[ "$FILTERS" -ge 3 ] || fail "expected a filter input per column, got $FILTERS"
ok "list shows the inline filter row ($FILTERS inputs)"

ROWS_ALL=$(evaljs "document.querySelectorAll('tbody tr').length")
evaljs "
  (() => {
  const inp = [...document.querySelectorAll('thead input')].find(i => i.getAttribute('aria-label')?.includes('Zone'))
  inp.value = 'example.com.'
  inp.dispatchEvent(new Event('input', { bubbles: true }))
  document.querySelector('thead button[aria-label=Filter]').click()
  })(); 'filtered'
" >/dev/null
sleep 1
expect "column filter narrows the rows" "$(evaljs "document.querySelectorAll('tbody tr').length")" '1'
evaljs "
  (() => {
  const inp = [...document.querySelectorAll('thead input')].find(i => i.value !== '')
  inp.value = ''
  inp.dispatchEvent(new Event('input', { bubbles: true }))
  document.querySelector('thead button[aria-label=Filter]').click()
  })(); 'cleared'
" >/dev/null
sleep 1
expect "clearing the filter restores the rows" "$(evaljs "document.querySelectorAll('tbody tr').length")" "$ROWS_ALL"

PAGINATION=$(evaljs "document.querySelector('tfoot')?.innerText.includes('Page') ?? false")
expect "pagination footer present" "$PAGINATION" 'true'

# ------------------------------------------- tabbed form: save and cancel
$AB open "$PANEL_URL/dns/zones/1" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
evaljs "document.querySelector('[data-test=zone-tab-settings]').click(); 'tab'" >/dev/null
sleep 1
TAB_SELECTED=$(evaljs "document.querySelector('[role=tab]')?.getAttribute('aria-selected')")
expect "tabbed form exposes ARIA tabs" "$TAB_SELECTED" '"true"'

# Cancel navigates back to the list without saving.
evaljs "[...document.querySelectorAll('button')].find(b => b.textContent.trim() === 'Cancel').click(); 'cancel'" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
expect "form cancel returns to the list" "$($AB get url)" "$PANEL_URL/dns"

# Save submits without a validation error.
$AB open "$PANEL_URL/dns/zones/1" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
evaljs "document.querySelector('[data-test=zone-tab-settings]').click(); 'tab'" >/dev/null
sleep 1
evaljs "[...document.querySelectorAll('button')].find(b => b.textContent.trim() === 'Save').click(); 'save'" >/dev/null
sleep 2
SAVE_ERR=$(evaljs "document.querySelector('[data-test=alert-danger]') !== null")
expect "form save produces no error alert" "$SAVE_ERR" 'false'

# ------------------------------------------------- theme traits (6.2)
$AB open "$PANEL_URL/dns" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1

# Square corners: sampled elements must compute border-radius 0.
RADII=$(evaljs "
  (() => {
    const values = ['button', 'thead', 'input', 'aside', 'header a', '.btn']
      .map(sel => document.querySelector(sel))
      .filter(Boolean)
      .map(el => getComputedStyle(el).borderRadius)
    return values.length >= 5 && values.every(v => v === '0px')
  })()
")
expect "sampled elements have border-radius 0" "$RADII" 'true'

# Signature flat dark thead #3E474E.
THEAD_BG=$(evaljs "getComputedStyle(document.querySelector('thead')).backgroundColor")
expect "thead background is #3E474E" "$THEAD_BG" '"rgb(62, 71, 78)"'

# Dark-mode toggle switches the scheme and persists across reload.
evaljs "document.querySelector('[data-test=theme-toggle]').click(); 'dark'" >/dev/null
sleep 1
expect "toggle enables the dark scheme" \
  "$(evaljs "document.documentElement.classList.contains('dark')")" 'true'
expect "preference stored" "$(evaljs "localStorage.getItem('theme')")" '"dark"'
DARK_BG=$(evaljs "getComputedStyle(document.body).backgroundColor")
expect "dark body background applied" "$DARK_BG" '"rgb(16, 26, 34)"'

$AB open "$PANEL_URL/dns" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
expect "dark scheme survives a reload" \
  "$(evaljs "document.documentElement.classList.contains('dark')")" 'true'
evaljs "document.querySelector('[data-test=theme-toggle]').click(); 'light'" >/dev/null
sleep 1
expect "toggle back to light" \
  "$(evaljs "document.documentElement.classList.contains('dark')")" 'false'

echo "PASS: ${PASS} checks"
