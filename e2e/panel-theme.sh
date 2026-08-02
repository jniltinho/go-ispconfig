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
trap '$AB close --all >/dev/null 2>&1 || true' EXIT

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

# wait_eval <desc> <js> <want> — poll the expression until it matches
# (20 x 500ms) instead of guessing SPA timing with fixed sleeps.
wait_eval() {
  local i actual
  for i in $(seq 1 20); do
    actual=$(evaljs "$2")
    if [ "$actual" = "$3" ]; then
      ok "$1"
      return 0
    fi
    sleep 0.5
  done
  fail "$1: got $actual — want $3"
}

# ponytail: fixed 1-2s sleeps after SPA transitions; switch to
# selector-based waits if the suite ever flakes on slow machines.
$AB close --all >/dev/null 2>&1 || true
$AB open "$PANEL_URL/login" --args "--no-sandbox" >/dev/null
$AB set viewport 1440 900 >/dev/null
$AB wait --load networkidle >/dev/null
# Deterministic start: a fresh profile would follow the OS color scheme;
# the suite begins in light and tests the toggle explicitly later.
evaljs "localStorage.setItem('theme', 'light'); 'init'" >/dev/null
$AB open "$PANEL_URL/login" >/dev/null
$AB wait --load networkidle >/dev/null
# The network log records from browser launch; nothing is cleared so the
# first-load assets are part of the same-origin check (6.3).

# ---------------------------------------------------------------- login
# v-model needs real input events; .value alone is not enough. The
# password is JSON-encoded and piped via stdin so it never appears in
# process arguments.
PASS_JSON=$(python3 -c 'import json, os; print(json.dumps(os.environ["ADMIN_PASSWORD"]))')
$AB eval --stdin >/dev/null <<LOGIN_EOF
(() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    el.value = v
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }
  set('#login-username', 'admin')
  set('#login-password', ${PASS_JSON})
  document.querySelector('button[type=submit]').click()
})(); 'submitted'
LOGIN_EOF
$AB wait --load networkidle >/dev/null
sleep 1
expect "login lands on the dashboard" "$($AB get url)" "$PANEL_URL/dashboard"
DASHLETS=$(evaljs "document.querySelectorAll('[data-test^=dashlet-]').length")
[ "$DASHLETS" -ge 3 ] || fail "expected >=3 dashlets, got $DASHLETS"
ok "dashboard renders $DASHLETS dashlets"

# ----------------------------------------------------- module navigation
evaljs "document.querySelector('header nav a[href=\\'/dns\\']').click(); 'nav'" >/dev/null
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
wait_eval "column filter narrows the rows" \
  "document.querySelectorAll('tbody tr:not([data-test=skeleton-row]):not([data-test=empty-state])').length" '1'
MATCHED=$(evaljs "document.querySelector('tbody tr:not([data-test=empty-state])').textContent.includes('example.com.')")
expect "filtered row is the matched zone" "$MATCHED" 'true'
evaljs "
  (() => {
  const inp = [...document.querySelectorAll('thead input')].find(i => i.value !== '')
  inp.value = ''
  inp.dispatchEvent(new Event('input', { bubbles: true }))
  document.querySelector('thead button[aria-label=Filter]').click()
  })(); 'cleared'
" >/dev/null
wait_eval "clearing the filter restores the rows" \
  "document.querySelectorAll('tbody tr:not([data-test=skeleton-row]):not([data-test=empty-state])').length" "$ROWS_ALL"

PAGINATION=$(evaljs "document.querySelector('tfoot')?.innerText.includes('Page') ?? false")
expect "pagination footer present" "$PAGINATION" 'true'
PREV_DISABLED=$(evaljs "document.querySelector('tfoot button').disabled")
expect "Previous is disabled on page 1" "$PREV_DISABLED" 'true'

# ------------------------------------------- tabbed form: save and cancel
$AB open "$PANEL_URL/dns/zones/1" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
evaljs "document.querySelector('[data-test=zone-tab-settings]').click(); 'tab'" >/dev/null
wait_eval "tabbed form exposes ARIA tabs" \
  "document.querySelector('form [role=tab]')?.getAttribute('aria-selected') ?? 'pending'" '"true"'

# Cancel navigates back to the list without saving.
evaljs "document.querySelector('[data-test=form-cancel]').click(); 'cancel'" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
expect "form cancel returns to the list" "$($AB get url)" "$PANEL_URL/dns"
NO_PUT=$($AB network requests | grep -cE 'PUT .*/api/dns/zones/' || true)
expect "cancel performed no write" "$NO_PUT" '0'

# Save submits without a validation error.
$AB open "$PANEL_URL/dns/zones/1" >/dev/null
$AB wait --load networkidle >/dev/null
sleep 1
evaljs "document.querySelector('[data-test=zone-tab-settings]').click(); 'tab'" >/dev/null
wait_eval "settings form ready for save" \
  "document.querySelector('[data-test=form-save]') !== null" 'true'
evaljs "document.querySelector('[data-test=form-save]').click(); 'save'" >/dev/null
sleep 2
SAVE_ERR=$(evaljs "document.querySelector('[data-test=alert-danger]') !== null")
expect "form save produces no error alert" "$SAVE_ERR" 'false'
SAVE_OK=$($AB network requests | grep -E 'PUT .*/api/dns/zones/1 ' | grep -c ' 200' || true)
[ "$SAVE_OK" -ge 1 ] || fail "no successful PUT /api/dns/zones/1 recorded"
ok "form save hit the API with 200"

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
$AB open "$PANEL_URL/dns" >/dev/null
$AB wait --load networkidle >/dev/null
wait_eval "dark thead uses the dark token" \
  "getComputedStyle(document.querySelector('thead')).backgroundColor" '"rgb(28, 57, 78)"'
evaljs "document.querySelector('[data-test=theme-toggle]').click(); 'light'" >/dev/null
sleep 1
expect "toggle back to light" \
  "$(evaljs "document.documentElement.classList.contains('dark')")" 'false'

# --------------------------------------- zero external requests (6.3)
# Every request recorded during the whole suite must target the panel
# origin (fonts, icons, CSS, JS bundled or same-origin).
ORIGIN_RE=$(printf '%s' "$PANEL_URL" | sed 's/[.[\*^$+?(){}|]/\\&/g')
EXTERNAL=$($AB network requests | grep -oE 'https?://[^ ]+' | grep -vE "^${ORIGIN_RE}(/|\$)" || true)
if [ -n "$EXTERNAL" ]; then
  fail "requests left the panel origin:
$EXTERNAL"
fi
TOTAL_REQS=$($AB network requests | grep -cE '^\[' || true)
[ "$TOTAL_REQS" -gt 0 ] || fail "network log is empty — the origin check asserted nothing"
ok "all $TOTAL_REQS recorded requests target $PANEL_URL"
FONT_OK=$($AB network requests | grep -E '/fonts/inter-.*woff2.*200' -c || true)
[ "$FONT_OK" -ge 1 ] || fail "vendored Inter was not loaded from /fonts/ with 200"
ok "vendored Inter served same-origin ($FONT_OK requests)"

echo "PASS: ${PASS} checks"
