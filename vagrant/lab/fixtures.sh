#!/bin/bash
# Idempotent fixture dataset for the legacy ISPConfig3 lab VMs
# (openspec add-legacy-test-lab, vagrant/lab/dataset.md is the catalog).
#
# Usage: fixtures.sh <legacy|legacy-apache>
#
# Driven entirely over the legacy remote JSON API (remote/json.php) with the
# goisp-lab remote user provisioned by provision-legacy.sh. Every entity is
# keyed by its natural key (username/domain/email/...): present -> skipped,
# absent -> created. Safe to re-run at any time.
#
# Requires: curl, jq on the host.
set -euo pipefail

VM="${1:?usage: fixtures.sh <legacy|legacy-apache>}"
case "$VM" in
  legacy)        IP=192.168.56.20; SFX=n ;;
  legacy-apache) IP=192.168.56.21; SFX=a ;;
  *) echo "unknown VM: $VM" >&2; exit 1 ;;
esac

API="https://$IP:8080/remote/json.php"
REMOTE_USER=goisp-lab
REMOTE_PW=GoIspRemote2026        # test rig only (vagrant/Vagrantfile)
CLIENT_PW='LabPw2026!'           # all lab client panel logins
PARITY_PW='ParityPw2026!'        # parity clients (vagrant/parity/dataset.md)
MAIL_PW='LabMailPw2026!'
FTP_PW='LabFtpPw2026!'
SHELL_PW='LabShellPw2026!'
DB_PW='LabDbPw2026!'
SERVER_ID=1

# ---------------------------------------------------------------- api helpers
call() { # $1: method, $2: JSON body -> .response on stdout; dies on API error
  local out code
  out=$(curl -sk --fail-with-body "$API?$1" --data-binary "$2") || {
    echo "HTTP error on $1: $out" >&2; return 1; }
  code=$(jq -r .code <<<"$out")
  if [ "$code" != ok ]; then
    echo "API error on $1: $(jq -r .message <<<"$out")" >&2
    return 1
  fi
  jq -c .response <<<"$out"
}

SID=$(call login "{\"username\":\"$REMOTE_USER\",\"password\":\"$REMOTE_PW\"}" | jq -r .)
[ -n "$SID" ] && [ "$SID" != null ] || { echo "login failed" >&2; exit 1; }
trap 'call logout "{\"session_id\":\"$SID\"}" >/dev/null 2>&1 || true' EXIT

lookup() { # $1: get-method, $2: filter object, $3: id field -> id or empty
  call "$1" "{\"session_id\":\"$SID\",\"primary_id\":$2}" \
    | jq -r "if type==\"array\" and length>0 then .[0].$3 else empty end"
}

site_field() { # $1: domain_id, $2: field -> value
  call sites_web_domain_get "{\"session_id\":\"$SID\",\"primary_id\":$1}" \
    | jq -r ".$2"
}

# ---------------------------------------------------------------- ensure_*
ensure_client() { # $1 username, $2 contact, $3 email, $4 reseller_id, $5 password, extra params via $6 (json obj frag "k":v,...)
  local id
  id=$(call client_get_by_username "{\"session_id\":\"$SID\",\"username\":\"$1\"}" 2>/dev/null \
    | jq -r '.client_id // empty' || true)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call client_add "{\"session_id\":\"$SID\",\"reseller_id\":$4,\"params\":{
    \"company_name\":\"\",\"contact_name\":\"$2\",\"username\":\"$1\",
    \"password\":\"$5\",\"language\":\"en\",\"usertheme\":\"default\",
    \"street\":\"\",\"zip\":\"\",\"city\":\"\",\"state\":\"\",\"country\":\"BR\",
    \"telephone\":\"\",\"mobile\":\"\",\"fax\":\"\",\"email\":\"$3\",
    \"internet\":\"\",\"icq\":\"\",\"notes\":\"lab fixture\",
    \"default_mailserver\":$SERVER_ID,\"default_webserver\":$SERVER_ID,
    \"default_dnsserver\":$SERVER_ID,\"default_dbserver\":$SERVER_ID,
    \"limit_client\":0,\"web_php_options\":\"no,fast-cgi,cgi,mod,suphp,php-fpm\",
    \"ssh_chroot\":\"no,jailkit\"${6:+,$6}}}" | jq -r .
  echo "  + client $1" >&2
}

ensure_site() { # $1 client_id, $2 domain, $3 php (php-fpm|no), $4 extra frag
  local id
  id=$(lookup sites_web_domain_get "{\"domain\":\"$2\"}" domain_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call sites_web_domain_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"ip_address\":\"*\",\"domain\":\"$2\",
    \"type\":\"vhost\",\"parent_domain_id\":0,\"vhost_type\":\"name\",
    \"hd_quota\":-1,\"traffic_quota\":-1,\"cgi\":\"n\",\"ssi\":\"n\",
    \"suexec\":\"y\",\"errordocs\":1,\"subdomain\":\"www\",\"ssl\":\"n\",
    \"php\":\"$3\",\"active\":\"y\",\"redirect_type\":\"\",\"redirect_path\":\"\",
    \"stats_password\":\"\",\"stats_type\":\"awstats\",
    \"allow_override\":\"All\",\"apache_directives\":\"\",\"nginx_directives\":\"\",
    \"php_open_basedir\":\"/\",\"pm\":\"ondemand\",\"pm_max_requests\":0,
    \"pm_process_idle_timeout\":10,\"http_port\":80,\"https_port\":443
    ${4:+,$4}}}" | jq -r .
  echo "  + site $2" >&2
}

ensure_zone() { # $1 client_id, $2 domain (no trailing dot)
  local id
  id=$(lookup dns_zone_get "{\"origin\":\"$2.\"}" id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call dns_templatezone_add "{\"session_id\":\"$SID\",\"client_id\":$1,
    \"template_id\":1,\"domain\":\"$2\",\"ip\":\"$IP\",
    \"ns1\":\"ns1.goisp.test\",\"ns2\":\"ns2.goisp.test\",
    \"email\":\"hostmaster@goisp.test\"}" | jq -r .
  echo "  + zone $2" >&2
}

ensure_rr() { # $1 client_id, $2 add-method, $3 zone_id, $4 name, $5 data, $6 extra frag
  local id
  id=$(lookup "${2%_add}_get" "{\"zone\":$3,\"name\":\"$4\",\"data\":\"$5\"}" id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  local type; type=$(echo "${2#dns_}" | sed 's/_add//' | tr a-z A-Z)
  call "$2" "{\"session_id\":\"$SID\",\"client_id\":$1,\"update_serial\":true,\"params\":{
    \"server_id\":$SERVER_ID,\"zone\":$3,\"name\":\"$4\",\"type\":\"$type\",
    \"data\":\"$5\",\"aux\":0,\"ttl\":3600,\"active\":\"y\",\"stamp\":\"\",
    \"serial\":0${6:+,$6}}}" | jq -r .
  echo "  + rr $4 ($2)" >&2
}

ensure_maildomain() { # $1 client_id, $2 domain
  local id
  id=$(lookup mail_domain_get "{\"domain\":\"$2\"}" domain_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call mail_domain_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"domain\":\"$2\",\"dkim\":\"n\",\"active\":\"y\"}}" | jq -r .
  echo "  + mail domain $2" >&2
}

ensure_mailbox() { # $1 client_id, $2 email, $3 name, $4 extra frag
  local id
  id=$(lookup mail_user_get "{\"email\":\"$2\"}" mailuser_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  local local_part=${2%@*} domain=${2#*@}
  call mail_user_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"email\":\"$2\",\"login\":\"$2\",
    \"password\":\"$MAIL_PW\",\"name\":\"$3\",\"uid\":5000,\"gid\":5000,
    \"maildir\":\"/var/vmail/$domain/$local_part\",\"maildir_format\":\"maildir\",
    \"quota\":0,\"cc\":\"\",\"homedir\":\"/var/vmail\",
    \"autoresponder\":\"n\",\"autoresponder_start_date\":\"0000-00-00 00:00:00\",
    \"autoresponder_end_date\":\"0000-00-00 00:00:00\",\"autoresponder_text\":\"\",
    \"move_junk\":\"y\",\"postfix\":\"y\",\"access\":\"n\",
    \"disableimap\":\"n\",\"disablepop3\":\"n\",\"disabledeliver\":\"n\",
    \"disablesmtp\":\"n\"${4:+,$4}}}" | jq -r .
  echo "  + mailbox $2" >&2
}

ensure_mailalias() { # $1 client_id, $2 source, $3 destination
  local id
  id=$(lookup mail_alias_get "{\"source\":\"$2\"}" forwarding_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call mail_alias_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"source\":\"$2\",\"destination\":\"$3\",
    \"type\":\"alias\",\"active\":\"y\"}}" | jq -r .
  echo "  + alias $2 -> $3" >&2
}

ensure_ftpuser() { # $1 client_id, $2 site_id, $3 username
  local id
  id=$(lookup sites_ftp_user_get "{\"username\":\"$3\"}" ftp_user_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  local uid gid dir
  uid=$(site_field "$2" system_user); gid=$(site_field "$2" system_group)
  dir=$(site_field "$2" document_root)
  call sites_ftp_user_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"parent_domain_id\":$2,\"username\":\"$3\",
    \"password\":\"$FTP_PW\",\"quota_size\":-1,\"active\":\"y\",
    \"uid\":\"$uid\",\"gid\":\"$gid\",\"dir\":\"$dir\",
    \"uploadratio\":-1,\"downloadratio\":-1,\"uploadbandwidth\":-1,
    \"downloadbandwidth\":-1}}" | jq -r .
  echo "  + ftp user $3" >&2
}

ensure_shelluser() { # $1 client_id, $2 site_id, $3 username, $4 chroot (''|jailkit)
  local id
  id=$(lookup sites_shell_user_get "{\"username\":\"$3\"}" shell_user_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  local uid gid dir
  uid=$(site_field "$2" system_user); gid=$(site_field "$2" system_group)
  dir=$(site_field "$2" document_root)
  call sites_shell_user_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"parent_domain_id\":$2,\"username\":\"$3\",
    \"password\":\"$SHELL_PW\",\"quota_size\":-1,\"active\":\"y\",
    \"puser\":\"$uid\",\"pgroup\":\"$gid\",\"shell\":\"/bin/bash\",
    \"dir\":\"$dir\",\"chroot\":\"$4\"}}" | jq -r .
  echo "  + shell user $3 (chroot='$4')" >&2
}

ensure_dbuser() { # $1 client_id, $2 username
  local id
  id=$(lookup sites_database_user_get "{\"database_user\":\"$2\"}" database_user_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call sites_database_user_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"database_user\":\"$2\",
    \"database_password\":\"$DB_PW\"}}" | jq -r .
  echo "  + db user $2" >&2
}

ensure_db() { # $1 client_id, $2 site_id, $3 db name, $4 db user_id
  local id
  id=$(lookup sites_database_get "{\"database_name\":\"$3\"}" database_id)
  if [ -n "$id" ]; then echo "$id"; return; fi
  call sites_database_add "{\"session_id\":\"$SID\",\"client_id\":$1,\"params\":{
    \"server_id\":$SERVER_ID,\"parent_domain_id\":$2,\"type\":\"mysql\",
    \"database_name\":\"$3\",\"database_user_id\":$4,\"database_ro_user_id\":0,
    \"database_charset\":\"utf8\",\"remote_access\":\"n\",\"remote_ips\":\"\",
    \"backup_interval\":\"none\",\"backup_copies\":1,\"active\":\"y\"}}" | jq -r .
  echo "  + database $3" >&2
}

wait_jobqueue() { # wait until the legacy server cron drained the datalog
  echo "waiting for datalog jobqueue to drain..." >&2
  for _ in $(seq 1 36); do
    local n
    n=$(call monitor_jobqueue_count "{\"session_id\":\"$SID\",\"server_id\":$SERVER_ID}" | jq -r .)
    [ "$n" = 0 ] && { echo "jobqueue empty" >&2; return 0; }
    sleep 5
  done
  echo "WARNING: jobqueue not empty after 3 min" >&2; return 1
}

# ================================================================ dataset
echo "== fixtures on $VM ($IP, suffix $SFX) =="

# --- parity dataset (legacy/nginx VM only; vagrant/parity/dataset.md) ------
if [ "$VM" = legacy ]; then
  PC1=$(ensure_client pclient1 "Parity Client One" pclient1@goisp.test 0 "$PARITY_PW")
  PC2=$(ensure_client pclient2 "Parity Client Two" pclient2@goisp.test 0 "$PARITY_PW")
  ensure_site "$PC1" parity1.goisp.test php-fpm >/dev/null
  ensure_site "$PC2" parity2.goisp.test php-fpm >/dev/null
  ensure_zone "$PC1" parity1.goisp.test >/dev/null
fi

# --- clients (2.2): 3 direct + 1 reseller + 1 child, distinct limits -------
C1=$(ensure_client "labc1$SFX" "Lab Client One ($SFX)" "labc1$SFX@goisp.test" 0 "$CLIENT_PW" \
  '"limit_web_domain":5,"limit_maildomain":2,"limit_mailbox":10,"limit_dns_zone":5,"limit_database":5,"limit_ftp_user":5,"limit_shell_user":2')
C2=$(ensure_client "labc2$SFX" "Lab Client Two ($SFX)" "labc2$SFX@goisp.test" 0 "$CLIENT_PW" \
  '"limit_web_domain":2,"limit_maildomain":1,"limit_mailbox":3,"limit_dns_zone":2,"limit_database":2,"limit_ftp_user":2,"limit_shell_user":1')
C3=$(ensure_client "labc3$SFX" "Lab Client Three ($SFX)" "labc3$SFX@goisp.test" 0 "$CLIENT_PW" \
  '"limit_web_domain":1,"limit_maildomain":0,"limit_mailbox":0,"limit_dns_zone":1,"limit_database":1,"limit_ftp_user":1,"limit_shell_user":0')
R1=$(ensure_client "labres1$SFX" "Lab Reseller One ($SFX)" "labres1$SFX@goisp.test" 0 "$CLIENT_PW" \
  '"limit_client":5,"limit_web_domain":10,"limit_maildomain":5,"limit_mailbox":20,"limit_dns_zone":10,"limit_database":10,"limit_ftp_user":10,"limit_shell_user":5')
CH1=$(ensure_client "labchild1$SFX" "Lab Child One ($SFX)" "labchild1$SFX@goisp.test" "$R1" "$CLIENT_PW" \
  '"limit_web_domain":1,"limit_maildomain":1,"limit_mailbox":2,"limit_dns_zone":1,"limit_database":1,"limit_ftp_user":1,"limit_shell_user":0')

# --- sites (2.3): 2 WordPress + 1 plain PHP + 1 static w/ custom directives
WP1=$(ensure_site "$C1" "wp1$SFX.goisp.test" php-fpm)
WP2=$(ensure_site "$C2" "wp2$SFX.goisp.test" php-fpm)
PHP1=$(ensure_site "$CH1" "php1$SFX.goisp.test" php-fpm)
STATIC_DIRECTIVES='"nginx_directives":"add_header X-Goisp-Lab static1;","apache_directives":"Header set X-Goisp-Lab static1"'
ST1=$(ensure_site "$C3" "static1$SFX.goisp.test" no "$STATIC_DIRECTIVES")

# --- DNS (2.4): wizard-template zones + extra record types ------------------
for d in "wp1$SFX" "wp2$SFX" "php1$SFX" "static1$SFX"; do
  ensure_zone "$C1" "$d.goisp.test" >/dev/null
done
Z1=$(lookup dns_zone_get "{\"origin\":\"wp1$SFX.goisp.test.\"}" id)
ensure_rr "$C1" dns_aaaa_add "$Z1" "ipv6.wp1$SFX.goisp.test." "fd00::20" >/dev/null
ensure_rr "$C1" dns_cname_add "$Z1" "blog.wp1$SFX.goisp.test." "wp1$SFX.goisp.test." >/dev/null
ensure_rr "$C1" dns_txt_add "$Z1" "wp1$SFX.goisp.test." "v=spf1 mx a -all" >/dev/null
ensure_rr "$C1" dns_srv_add "$Z1" "_sip._tcp.wp1$SFX.goisp.test." "5 5060 sip.wp1$SFX.goisp.test." '"aux":10' >/dev/null

# --- email (2.5): domain, 2 mailboxes, alias, autoresponder ----------------
MD=$(ensure_maildomain "$C1" "mail$SFX.goisp.test")
MB1=$(ensure_mailbox "$C1" "user1@mail$SFX.goisp.test" "Lab User One")
MB2=$(ensure_mailbox "$C1" "user2@mail$SFX.goisp.test" "Lab User Two" \
  '"autoresponder":"y","autoresponder_text":"Out of office (lab fixture)","autoresponder_start_date":"2026-01-01 00:00:00","autoresponder_end_date":"2036-01-01 00:00:00"')
ensure_mailalias "$C1" "contact@mail$SFX.goisp.test" "user1@mail$SFX.goisp.test" >/dev/null

# --- FTP + shell users (2.6) -----------------------------------------------
ensure_ftpuser "$C1" "$WP1" "wp1${SFX}ftp" >/dev/null
ensure_ftpuser "$C2" "$WP2" "wp2${SFX}ftp" >/dev/null
ensure_shelluser "$C1" "$WP1" "shell1$SFX" "" >/dev/null
ensure_shelluser "$C2" "$WP2" "shell2$SFX" "jailkit" >/dev/null

# --- client databases + users for the WordPress installs (2.7) -------------
DBU1=$(ensure_dbuser "$C1" "c${C1}wp1$SFX")
DBU2=$(ensure_dbuser "$C2" "c${C2}wp2$SFX")
ensure_db "$C1" "$WP1" "c${C1}wp1$SFX" "$DBU1" >/dev/null
ensure_db "$C2" "$WP2" "c${C2}wp2$SFX" "$DBU2" >/dev/null

wait_jobqueue || true
echo "== fixtures on $VM done =="
