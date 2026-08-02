#!/bin/bash
# Unattended WordPress installs into the lab fixture sites (task 3.1 of
# openspec add-legacy-test-lab). Run ON THE GUEST as root (vagrant ssh -c).
#
#   wordpress.sh <suffix>     # n (legacy/nginx) | a (legacy-apache)
#
# Installs wp-cli once, then for each fixture WordPress site (wp1$S, wp2$S)
# downloads core into the ISPConfig docroot, wires the fixture client
# database (vagrant/lab/dataset.md) and runs `wp core install`. Idempotent:
# a site whose wp-config.php exists and responds to `wp core is-installed`
# is skipped.
set -euo pipefail

SFX="${1:?usage: wordpress.sh <n|a>}"
WP_ADMIN_USER=labadmin
WP_ADMIN_PW='LabWpAdmin2026!'
DB_PW='LabDbPw2026!'

if [ ! -x /usr/local/bin/wp ]; then
  curl -fsSL -o /usr/local/bin/wp \
    https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar
  chmod +x /usr/local/bin/wp
fi

for site in "wp1$SFX" "wp2$SFX"; do
  domain="$site.goisp.test"
  read -r docroot sysuser sysgrp dbname < <(mysql dbispconfig -N -B -e "
    SELECT d.document_root, d.system_user, d.system_group, db.database_name
      FROM web_domain d
      JOIN web_database db ON db.parent_domain_id = d.domain_id
     WHERE d.domain = '$domain'")
  [ -n "${docroot:-}" ] || { echo "no fixture site/db for $domain" >&2; exit 1; }
  web="$docroot/web"

  wpcli() { sudo -u "$sysuser" -- /usr/local/bin/wp --path="$web" "$@"; }

  if wpcli core is-installed 2>/dev/null; then
    echo "$domain: WordPress already installed, skipping"
    continue
  fi
  echo "$domain: installing WordPress into $web (db $dbname)"
  rm -f "$web/index.html"   # ISPConfig default page would shadow index.php
  wpcli core download --skip-content=0 2>&1 | tail -1
  wpcli config create --dbname="$dbname" --dbuser="$dbname" \
    --dbpass="$DB_PW" --dbhost=localhost --skip-check --force >/dev/null
  wpcli core install --url="http://$domain" --title="Lab $site" \
    --admin_user="$WP_ADMIN_USER" --admin_password="$WP_ADMIN_PW" \
    --admin_email="labadmin@$domain" --skip-email >/dev/null
  echo "$domain: installed"
done

# Behavior check: front page must render WordPress content over HTTP.
for site in "wp1$SFX" "wp2$SFX"; do
  domain="$site.goisp.test"
  body=$(curl -s -H "Host: $domain" http://127.0.0.1/)
  echo "$body" | grep -qi "Lab $site" \
    && echo "OK: http://$domain front page renders" \
    || { echo "FAIL: http://$domain front page"; exit 1; }
done
