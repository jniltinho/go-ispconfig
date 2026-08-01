package getconf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sampleINI is a realistic excerpt of ISPConfig's server.ini.master
// ([server], [web], [dns] sections) as stored in server.config.
const sampleINI = "junk line before any section\r\n" +
	`[server]
auto_network_configuration=n
hostname=server1.domain.tld

[WEB]
server_type=nginx
website_basedir=/var/www
website_path=/var/www/clients/client[client_id]/web[website_id]
php_open_basedir=[website_path]/web:[website_path]/tmp:/usr/share/php:/tmp
  security_level = 20
nginx_vhost_conf_dir=/etc/nginx/sites-available
nginx_vhost_conf_enabled_dir=/etc/nginx/sites-enabled
php_fpm_start_port=9010
apps_vhost_servername=
not a valid line
[dns]
bind_user=root
bind_group=bind
bind_zonefiles_dir=/etc/bind
bind_zonefiles_masterprefix=pri.
named_conf_local_path=/etc/bind/named.conf.local
disable_bind_log=n
`

func TestParseINI(t *testing.T) {
	cfg := ParseINI(sampleINI)

	// Section names are lowercased, like ini_parser::parse_ini_string.
	web, ok := cfg["web"]
	assert.True(t, ok, "[WEB] must be stored lowercased")
	assert.Equal(t, "nginx", web["server_type"])
	assert.Equal(t, "/var/www/clients/client[client_id]/web[website_id]", web["website_path"])
	// PHP parser requires ^[\w]+=... so an indented "key = value" line
	// does not match its item regex and is dropped after trimming applies
	// to the whole line first — trimmed "security_level = 20" has a space
	// before '=' and is ignored.
	_, hasIndented := web["security_level"]
	assert.False(t, hasIndented)
	assert.Equal(t, "", web["apps_vhost_servername"])
	_, hasJunk := web["not a valid line"]
	assert.False(t, hasJunk)

	dns := cfg["dns"]
	assert.Equal(t, "pri.", dns["bind_zonefiles_masterprefix"])
	assert.Equal(t, "/etc/bind/named.conf.local", dns["named_conf_local_path"])

	assert.Equal(t, "server1.domain.tld", cfg["server"]["hostname"])
	// Content before the first section header is ignored.
	assert.NotContains(t, cfg, "")
}

func TestDecodeSections(t *testing.T) {
	raw := ParseINI(sampleINI)
	var web WebConfig
	var dns DNSConfig
	decodeSection(raw["web"], &web)
	decodeSection(raw["dns"], &dns)

	assert.Equal(t, "nginx", web.ServerType)
	assert.Equal(t, "/var/www", web.WebsiteBasedir)
	assert.Equal(t, "/etc/nginx/sites-available", web.NginxVhostConfDir)
	assert.Equal(t, "9010", web.PHPFPMStartPort)
	assert.Equal(t, "", web.User, "missing key keeps zero value")

	assert.Equal(t, "root", dns.BindUser)
	assert.Equal(t, "bind", dns.BindGroup)
	assert.Equal(t, "/etc/bind", dns.BindZonefilesDir)
	assert.Equal(t, "n", dns.DisableBindLog)
}

func TestStripSlashes(t *testing.T) {
	assert.Equal(t, `it's a "test"`, StripSlashes(`it\'s a \"test\"`))
	assert.Equal(t, `C:\dir`, StripSlashes(`C:\\dir`))
	assert.Equal(t, "plain", StripSlashes("plain"))
	// PHP drops a trailing lone backslash (verified with PHP 8.2:
	// stripslashes("trailing\") == "trailing").
	assert.Equal(t, "trailing", StripSlashes(`trailing\`))
}
