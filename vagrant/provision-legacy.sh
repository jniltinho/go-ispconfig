#!/bin/bash
# Provision the `legacy` VM with the original PHP ISPConfig3 (nginx + bind)
# via the official auto-installer (https://get.ispconfig.org), then pin the
# admin password to a fixed value for the parity tests.
# Env: ADMIN_PW (set by the Vagrantfile).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

if [ ! -f /usr/local/ispconfig/interface/lib/config.inc.php ]; then
  apt-get update
  apt-get -y install curl ca-certificates openssl php-cli php-mbstring dialog
  # nginx + bind, no mail stack — same scope as go-ispconfig. The
  # get.ispconfig.org bootstrap breaks without a TTY (it redirects stdin
  # from /dev/$TTY where ps reports "?"), so run the downloaded
  # auto-installer PHP entrypoint directly, exactly as the bootstrap would.
  curl -fsSL -o /tmp/ispconfig-ai.tar.gz https://www.ispconfig.org/downloads/ispconfig-ai.tar.gz
  rm -rf /tmp/ispconfig-ai && mkdir /tmp/ispconfig-ai
  tar -C /tmp/ispconfig-ai -xzf /tmp/ispconfig-ai.tar.gz
  php -q /tmp/ispconfig-ai/ispconfig.ai.php \
    --use-nginx --no-mail --no-roundcube --no-pma --no-firewall --no-quota \
    --no-ntp --i-know-what-i-am-doing 2>&1 | tee /var/log/ispconfig-install.log
  test -f /usr/local/ispconfig/interface/lib/config.inc.php
else
  echo "ISPConfig already installed; skipping auto-installer"
fi

# Fixed admin password (SHA-512 crypt, accepted by the ISPConfig login).
HASH=$(openssl passwd -6 "$ADMIN_PW")
mysql dbispconfig -e "UPDATE sys_user SET passwort='${HASH}' WHERE username='admin'"
echo "legacy panel ready: https://192.168.56.20:8080 (admin / \$ADMIN_PW)"
