// Package powerdns applies panel DNS events to a PowerDNS gmysql backend
// (port of powerdns_plugin.inc.php). The panel UI, REST API and dns_* tables
// stay shared with the Bind path; only this applying plugin and its service
// differ when server.config [dns] dns_backend=powerdns.
package powerdns

import _ "embed"

// SchemaSQL is the PowerDNS gmysql schema from install/sql/powerdns.sql
// (domains, records, supermasters, domainmetadata with ISPConfig bridge
// columns ispconfig_id on domains and records). Used by the installer
// configure_powerdns step and by tests.
//
//go:embed powerdns.sql
var SchemaSQL string

// LocalMasterTemplate is install/tpl/pdns.local.master rendered to
// /etc/powerdns/pdns.d/pdns.local by the installer (gmysql backend config).
//
// Placeholders: {mysql_server_host}, {mysql_server_ispconfig_user},
// {mysql_server_ispconfig_password}, {powerdns_database}, {mysql_server_port}.
//
//go:embed pdns.local.master
var LocalMasterTemplate string

// DatabaseName is the fixed PowerDNS database name (PHP plugin comment:
// "The powerdns database name has to be 'powerdns'").
const DatabaseName = "powerdns"
