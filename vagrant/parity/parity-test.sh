#!/bin/bash
# Parity assertions between the legacy PHP ISPConfig VM (192.168.56.20) and
# the go-ispconfig VM (192.168.56.10), run from the HOST after the
# agent-browser flows created the dataset of vagrant/parity/dataset.md on
# both panels. Scope: clients + sites + DNS (no email, spec install-test-rig).
#
# Compares the shared-schema columns row by row, checks the generated
# configs on both guests and writes docs/prints/parity-report.md. Unintended
# divergence (not listed in intended-differences.txt) => exit 1.
set -u
cd "$(dirname "$0")/.."   # vagrant/

GOISP_IP=192.168.56.10
LEGACY_IP=192.168.56.20
REPORT=../docs/prints/parity-report.md
INTENDED=parity/intended-differences.txt
FAILED=0

mkdir -p ../docs/prints
: > /tmp/parity-divergences.txt

sql() { # $1: vm, $2: query  — one normalized row per line, tab-separated
  vagrant ssh "$1" -c "sudo mysql dbispconfig -N -B -e \"$2\"" 2>/dev/null | tr -d '\r'
}

compare() { # $1: label, $2: query
  local legacy goisp
  legacy=$(sql legacy "$2")
  goisp=$(sql ubuntu "$2")
  if [ "$legacy" = "$goisp" ]; then
    echo "PARITY OK: $1"
  else
    echo "PARITY DIFF: $1" >&2
    diff <(echo "$legacy") <(echo "$goisp") | sed "s/^/  /" >&2
    printf '%s\n' "$1" >> /tmp/parity-divergences.txt
    {
      echo "### $1"
      echo '```diff'
      diff <(echo "$legacy") <(echo "$goisp")
      echo '```'
    } >> /tmp/parity-diff-details.txt
  fi
}

: > /tmp/parity-diff-details.txt

# --- shared-schema column parity --------------------------------------------
# Scoped to the canonical parity dataset (vagrant/parity/dataset.md): the
# legacy VM intentionally carries extra standing-lab fixtures (vagrant/lab/)
# that have no go-ispconfig counterpart yet and are not compared.
compare "client (contact, username, email)" \
  "SELECT contact_name, username, email FROM client WHERE username LIKE 'pclient%' ORDER BY username"

compare "web_domain (domain, type, active, subdomain, vhost_type)" \
  "SELECT domain, type, active, subdomain, vhost_type FROM web_domain WHERE domain LIKE 'parity%' ORDER BY domain"

compare "dns_soa (origin, ns, mbox, refresh, retry, expire, minimum, ttl, active)" \
  "SELECT origin, ns, mbox, refresh, retry, expire, minimum, ttl, active FROM dns_soa WHERE origin LIKE 'parity%' ORDER BY origin"

# A-record data holds each panel's own server IP (.20 legacy, .10 go-ispconfig)
# by design — normalize both to SERVER_IP so only real divergences surface.
compare "dns_rr (zone origin, name, type, data, aux, ttl, active)" \
  "SELECT s.origin, r.name, r.type,
          REPLACE(REPLACE(r.data,'$LEGACY_IP','SERVER_IP'),'$GOISP_IP','SERVER_IP'),
          r.aux, r.ttl, r.active
     FROM dns_rr r JOIN dns_soa s ON s.id = r.zone
    WHERE s.origin LIKE 'parity%' ORDER BY s.origin, r.type, r.name, r.data"

# --- generated configs -------------------------------------------------------
check() { # $1: label, $2: vm, $3: command
  if vagrant ssh "$2" -c "sudo bash -c '$3'" >/dev/null 2>&1; then
    echo "PARITY OK: $1"
  else
    echo "PARITY FAIL: $1" >&2
    printf '%s\n' "$1" >> /tmp/parity-divergences.txt
  fi
}

for vm in legacy ubuntu; do
  check "nginx -t ($vm)" "$vm" "nginx -t"
  check "vhost parity1.goisp.test exists ($vm)" "$vm" \
    "test -f /etc/nginx/sites-available/parity1.goisp.test.vhost"
  check "zone file parity1.goisp.test valid ($vm)" "$vm" \
    "named-checkzone parity1.goisp.test. /etc/bind/pri.parity1.goisp.test"
done

# --- both hosts serve the site (HTTP 200 on the test vhost) ------------------
for ip in $LEGACY_IP $GOISP_IP; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Host: parity1.goisp.test" "http://$ip/")
  if [ "$code" = "200" ]; then
    echo "PARITY OK: http-200 parity1.goisp.test @ $ip"
  else
    echo "PARITY FAIL: http-200 parity1.goisp.test @ $ip (got $code)" >&2
    printf 'http-200 @ %s\n' "$ip" >> /tmp/parity-divergences.txt
  fi
done

# --- report ------------------------------------------------------------------
{
  echo "# Parity report — legacy PHP ISPConfig vs go-ispconfig"
  echo
  echo "Generated: $(date -u +%FT%TZ)"
  echo "Scope: clients + sites + DNS, parity dataset only (vagrant/parity/dataset.md)."
  echo "The legacy VM's standing-lab fixtures (vagrant/lab/) and its mail stack are"
  echo "excluded from parity until the corresponding go-ispconfig modules ship."
  echo
  echo "## Intended differences (allowlisted)"
  echo
  sed 's/^/- /' "$INTENDED"
  echo
  echo "## Divergences found"
  echo
  if [ -s /tmp/parity-divergences.txt ]; then
    sed 's/^/- /' /tmp/parity-divergences.txt
    echo
    cat /tmp/parity-diff-details.txt
  else
    echo "None."
  fi
} > "$REPORT"
echo "Report: $REPORT"

# --- verdict: any divergence not allowlisted fails the suite -----------------
while IFS= read -r d; do
  grep -qF "$d" "$INTENDED" || { echo "UNINTENDED DIVERGENCE: $d" >&2; FAILED=1; }
done < /tmp/parity-divergences.txt

exit $FAILED
