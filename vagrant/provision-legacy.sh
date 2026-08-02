#!/bin/bash
# Provision a legacy PHP ISPConfig3 lab VM (openspec add-legacy-test-lab)
# via the official auto-installer (https://get.ispconfig.org): full stack —
# web (nginx or apache2), bind, mail (postfix/dovecot/rspamd) and Roundcube.
# Then pin the admin password, make sure the remote API is enabled and
# provision a read-write remote-API user with the full grant set.
# Idempotent: safe to re-run with `vagrant provision <machine>`.
#
# Env (set by the Vagrantfile):
#   ADMIN_PW    fixed panel admin password
#   REMOTE_PW   fixed password for the `goisp-lab` remote-API user
#   WEB_SERVER  nginx | apache   (apache2 is the auto-installer default)
#   PANEL_IP    the VM's host-only IP (log message only)
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

if [ ! -f /usr/local/ispconfig/interface/lib/config.inc.php ]; then
  apt-get update
  apt-get -y install curl ca-certificates openssl php-cli php-mbstring dialog
  # The get.ispconfig.org bootstrap breaks without a TTY (it redirects stdin
  # from /dev/$TTY where ps reports "?"), so run the downloaded
  # auto-installer PHP entrypoint directly, exactly as the bootstrap would.
  WEB_FLAG=""
  [ "${WEB_SERVER:-apache}" = "nginx" ] && WEB_FLAG="--use-nginx"
  curl -fsSL -o /tmp/ispconfig-ai.tar.gz https://www.ispconfig.org/downloads/ispconfig-ai.tar.gz
  rm -rf /tmp/ispconfig-ai && mkdir /tmp/ispconfig-ai
  tar -C /tmp/ispconfig-ai -xzf /tmp/ispconfig-ai.tar.gz
  php -q /tmp/ispconfig-ai/ispconfig.ai.php \
    $WEB_FLAG --no-pma --no-firewall --no-quota \
    --no-ntp --i-know-what-i-am-doing 2>&1 | tee /var/log/ispconfig-install.log
  test -f /usr/local/ispconfig/interface/lib/config.inc.php
else
  echo "ISPConfig already installed; skipping auto-installer"
fi

# Fixed admin password (SHA-512 crypt, accepted by the ISPConfig login).
HASH=$(openssl passwd -6 "$ADMIN_PW")
mysql dbispconfig -e "UPDATE sys_user SET passwort='${HASH}' WHERE username='admin'"

# Remote API on (auto-installer default is yes; keep it that way on re-runs).
sed -i 's/^remote_api_allowed=.*/remote_api_allowed=yes/' \
  /usr/local/ispconfig/security/security_settings.ini

# Read-write remote-API user `goisp-lab` with the FULL grant set: every
# function group the installed interface exposes (web/*/lib/remote.conf.php),
# exactly what ticking all checkboxes on System > Remote Users would store.
FUNCS=$(php -r '
  $function_list = [];
  foreach (glob("/usr/local/ispconfig/interface/web/*/lib/remote.conf.php") as $f) include $f;
  $fns = [];
  foreach (array_keys($function_list) as $group)
    foreach (explode(",", $group) as $fn) $fns[trim($fn)] = true;
  echo implode(";", array_keys($fns));')
RHASH=$(openssl passwd -6 "$REMOTE_PW")
mysql dbispconfig <<SQL
DELETE FROM remote_user WHERE remote_username='goisp-lab';
INSERT INTO remote_user
  (sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other,
   remote_username, remote_password, remote_access, remote_ips, remote_functions)
VALUES
  (1, 1, 'riud', 'riud', '', 'goisp-lab', '${RHASH}', 'y', '', '${FUNCS}');
SQL

echo "legacy panel ready: https://${PANEL_IP:-<vm-ip>}:8080 (admin / \$ADMIN_PW; remote API: goisp-lab / \$REMOTE_PW)"
