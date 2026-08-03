#!/usr/bin/env bash
# agent-browser baseline smoke for ui-forms-tables-qa (task 1.3).
#
# Login as admin and open every topbar module + sidebar section that is
# implemented on the current binary. Screenshots land in
# docs/prints/ui-qa-baseline-* (never committed).
#
# Usage:
#   PANEL_URL=https://192.168.56.10:8080 ADMIN_PASSWORD=... e2e/panel-ui-qa-baseline.sh
#
# Optional:
#   SKIP_SCREENSHOTS=1  — assertions only (faster CI-style runs)
#   PRINTS_DIR=docs/prints — override screenshot directory
#
# Requires: agent-browser, a running panel (lab .10 or local binary).
set -euo pipefail

: "${PANEL_URL:?set PANEL_URL to the running panel origin}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD to the admin password}"

# Isolated session so parallel agent-browser runs (or a local panel on
# another port) cannot steal cookies / navigation.
SESSION="${AGENT_BROWSER_SESSION:-ui-qa-baseline}"
AB=(agent-browser --session "$SESSION")
PASS=0
PRINTS="${PRINTS_DIR:-docs/prints}"
SKIP_SHOTS="${SKIP_SCREENSHOTS:-0}"
mkdir -p "$PRINTS"
trap 'agent-browser --session "$SESSION" close >/dev/null 2>&1 || true' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

ok() {
  PASS=$((PASS + 1))
  echo "ok $PASS: $*"
}

# evaljs <js> — evaluate; strip surrounding quotes from string results.
evaljs() {
  "${AB[@]}" eval "$1" | sed 's/^"//; s/"$//'
}

expect() {
  [ "$2" = "$3" ] || fail "$1: got $2 — want $3"
  ok "$1"
}

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

shot() {
  local name=$1
  if [ "$SKIP_SHOTS" = "1" ]; then
    return 0
  fi
  "${AB[@]}" screenshot "$PRINTS/ui-qa-baseline-${name}.png" >/dev/null
  ok "screenshot ui-qa-baseline-${name}.png"
}

do_login() {
  local pass_json
  pass_json=$(python3 -c 'import json, os; print(json.dumps(os.environ["ADMIN_PASSWORD"]))')
  "${AB[@]}" open "$BASE/login" >/dev/null
  "${AB[@]}" wait --load networkidle >/dev/null
  "${AB[@]}" eval --stdin >/dev/null <<LOGIN_EOF
(() => {
  const set = (sel, v) => {
    const el = document.querySelector(sel)
    if (!el) throw new Error('missing ' + sel)
    el.value = v
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }
  set('#login-username', 'admin')
  set('#login-password', ${pass_json})
  document.querySelector('button[type=submit]').click()
})(); 'submitted'
LOGIN_EOF
  "${AB[@]}" wait --load networkidle >/dev/null
  sleep 1
}

# open_path <path> — navigate within PANEL_URL; re-login once if session expired.
open_path() {
  local path=$1
  "${AB[@]}" open "$BASE$path" >/dev/null
  "${AB[@]}" wait --load networkidle >/dev/null
  sleep 0.5
  local url
  url=$("${AB[@]}" get url | sed 's|/$||')
  if [[ "$url" == *"/login"* ]]; then
    do_login
    "${AB[@]}" open "$BASE$path" >/dev/null
    "${AB[@]}" wait --load networkidle >/dev/null
    sleep 0.5
  fi
}

# ---------------------------------------------------------------------------
# Sidebar sections that must open for an admin session on the current tree.
# Format: slug|path|expect_substring_in_main_or_url
# expect is checked against document title/h1/main text OR the final URL.
# Paths match frontend/src/modules.ts (N/A cron/ftp/shell omitted).
# ---------------------------------------------------------------------------
SECTIONS=(
  "dashboard|/dashboard|dashboard"
  "sites-websites|/sites|/sites"
  "sites-folders|/sites/folders|/sites/folders"
  "sites-databases|/sites/databases|/sites/databases"
  "sites-database-users|/sites/database-users|/sites/database-users"
  "dns-zones|/dns|/dns"
  "dns-slave-zones|/dns/slave-zones|/dns/slave-zones"
  "dns-templates|/dns/templates|/dns/templates"
  "mail-domains|/mail|/mail"
  "mail-mailboxes|/mail/mailboxes|/mail/mailboxes"
  "mail-aliases|/mail/aliases|/mail/aliases"
  "mail-forwards|/mail/forwards|/mail/forwards"
  "mail-catchalls|/mail/catchalls|/mail/catchalls"
  "mail-alias-domains|/mail/alias-domains|/mail/alias-domains"
  "mail-transports|/mail/transports|/mail/transports"
  "mail-spam-policies|/mail/spamfilter/policies|/mail/spamfilter/policies"
  "mail-spam-users|/mail/spamfilter/users|/mail/spamfilter/users"
  "mail-wblists|/mail/spamfilter/wblists|/mail/spamfilter/wblists"
  "mail-access|/mail/access|/mail/access"
  "clients|/clients|/clients"
  "resellers|/clients/resellers|/clients/resellers"
  "limit-templates|/clients/limit-templates|/clients/limit-templates"
  "message-templates|/clients/message-templates|/clients/message-templates"
  "send-message|/clients/send-message|/clients/send-message"
  "system-placeholder|/system|/system"
  "system-firewall|/system/firewall|/system/firewall"
  "system-migration|/system/migration|/system/migration"
)

# Topbar module tabs (path must become active after navigation).
TOPBAR_MODULES=(
  "dashboard|/dashboard"
  "sites|/sites"
  "dns|/dns"
  "mail|/mail"
  "client|/clients"
  "system|/system"
)

BASE="${PANEL_URL%/}"
agent-browser --session "$SESSION" close >/dev/null 2>&1 || true
"${AB[@]}" open "$BASE/login" --args "--no-sandbox" --ignore-https-errors >/dev/null
"${AB[@]}" set viewport 1440 900 >/dev/null
"${AB[@]}" wait --load networkidle >/dev/null
evaljs "localStorage.setItem('theme', 'light'); 'init'" >/dev/null

# ---------------------------------------------------------------- login
do_login
expect "login lands on the dashboard" "$("${AB[@]}" get url | sed 's|/$||')" "$BASE/dashboard"
shot "01-login-dashboard"

# ----------------------------------------------------- topbar modules
for entry in "${TOPBAR_MODULES[@]}"; do
  slug=${entry%%|*}
  path=${entry#*|}
  open_path "$path"
  url=$("${AB[@]}" get url | sed 's|/$||')
  case "$url" in
    *"$path"*) ok "topbar module $slug → $path" ;;
    *) fail "topbar module $slug: url $url does not contain $path" ;;
  esac
  # Sidebar must render for non-dashboard modules (and dashboard too).
  has_side=$(evaljs "document.querySelector('[data-test=sidebar]') !== null")
  expect "sidebar present on $slug" "$has_side" "true"
  shot "topbar-${slug}"
done

# -------------------------------------------------- every sidebar section
for entry in "${SECTIONS[@]}"; do
  IFS='|' read -r slug path expect_frag <<<"$entry"
  open_path "$path"
  url=$("${AB[@]}" get url | sed 's|/$||')
  case "$url" in
    *"$expect_frag"*) ok "section $slug opens ($path)" ;;
    *) fail "section $slug: url $url missing $expect_frag" ;;
  esac

  # Must not bounce to login and should not show a raw panic/blank-only main.
  still_auth=$(evaljs "document.querySelector('header nav') !== null")
  expect "section $slug keeps shell chrome" "$still_auth" "true"

  # List screens: either DataTable, form content, placeholder, or wizard root.
  has_ui=$(evaljs "
    (() => {
      const main = document.querySelector('main')
      if (!main) return 'no-main'
      if (main.querySelector('table')) return 'table'
      if (main.querySelector('form')) return 'form'
      if (main.querySelector('h1')) return 'h1'
      if ((main.textContent || '').trim().length > 0) return 'text'
      return 'empty'
    })()
  ")
  [ "$has_ui" != "empty" ] && [ "$has_ui" != "no-main" ] || fail "section $slug: main content empty ($has_ui)"
  ok "section $slug has content ($has_ui)"

  # List routes should expose the ISPConfig filter row when a table is present.
  if [ "$has_ui" = "table" ]; then
    filters=$(evaljs "document.querySelectorAll('thead input').length")
    [ "$filters" -ge 1 ] || fail "section $slug: table without filter inputs"
    ok "section $slug filter row ($filters inputs)"
  fi

  shot "section-${slug}"
done

echo
echo "PASS: $PASS checks — baseline UI QA smoke OK"
echo "Screenshots (if any): $PRINTS/ui-qa-baseline-*"
