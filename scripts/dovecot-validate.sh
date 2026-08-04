#!/usr/bin/env bash
# Renders the dovecot assets of one dialect and runs `doveconf -n` against
# them on the matching Vagrant box. Used while developing the templates:
# the 2.3 and 2.4 syntaxes diverge enough that guessing does not work.
#
#   scripts/dovecot-validate.sh debian 2.4
#   scripts/dovecot-validate.sh ubuntu 2.3
set -euo pipefail
vm=${1:?vm name}
ver=${2:?dovecot dialect (2.3|2.4)}
root=$(cd "$(dirname "$0")/.." && pwd)

sed -e 's/{hostname}/mail.test/g' -e 's|{homedir}|/var/vmail|g' \
    -e 's/{mailuser_group}/vmail/g' -e 's/{mailuser_name}/vmail/g' \
    -e 's|{ssl_cert}|/etc/dovecot/private/dovecot.pem|g' \
    -e 's|{ssl_key}|/etc/dovecot/private/dovecot.key|g' \
    -e 's|{sql_conf}|/etc/dovecot/dovecot-sql.conf|g' \
    "$root/internal/installer/assets/dovecot-$ver.conf" >/tmp/dc.conf
sed -e 's/{mysql_server_ip}/127.0.0.1/g' -e 's/{mysql_server_port}/3306/g' \
    -e 's/{mysql_server_ispconfig_user}/ispconfig/g' \
    -e 's/{mysql_server_ispconfig_password}/pw/g' \
    -e 's/{mysql_server_database}/dbispconfig/g' -e 's/{server_id}/1/g' \
    "$root/internal/installer/assets/dovecot-sql-$ver.conf" >/tmp/dcsql.conf

cd "$root/vagrant"
vagrant upload /tmp/dc.conf /tmp/dc.conf "$vm" >/dev/null
vagrant upload /tmp/dcsql.conf /tmp/dcsql.conf "$vm" >/dev/null
vagrant ssh "$vm" -c '
  sudo cp /tmp/dc.conf /etc/dovecot/dovecot.conf
  sudo cp /tmp/dcsql.conf /etc/dovecot/dovecot-sql.conf
  sudo chmod 600 /etc/dovecot/dovecot-sql.conf
  sudo doveconf -n >/dev/null && echo "CONFIG OK"
' 2>&1 | grep -v '^$'
