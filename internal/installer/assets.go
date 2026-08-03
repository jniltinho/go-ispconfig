package installer

import _ "embed"

// namedConfOptions is the bind base config written by the bind-base step
// (derived from install/tpl/named.conf.options.master; zonefiles stay in
// /etc/bind per the distro profile, named.conf.local is included by the
// distro named.conf).
//
//go:embed assets/named.conf.options
var namedConfOptions string

// pureFTPdMySQLConf is the PureFTPd MySQL auth backend (verbatim copy of
// install/tpl/pureftpd_mysql.conf.master); the ftp step fills the
// {mysql_*}/{server_id} placeholders.
//
//go:embed assets/pureftpd-mysql.conf
var pureFTPdMySQLConf string

// nginxSitesInclude re-enables the sites-enabled include on hosts whose
// nginx.conf lost it; on stock Debian/Ubuntu nginx it is never written.
//
//go:embed assets/nginx-sites.conf
var nginxSitesInclude string
