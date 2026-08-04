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

func TestJailkitConfigDefaultsAndDecode(t *testing.T) {
	// server.ini.master defaults survive a missing [jailkit] section.
	cfg := DefaultJailkitConfig()
	assert.Equal(t, "/home/[username]", cfg.ChrootHome)
	assert.Equal(t, "coreutils basicshell editors extendedshell netutils "+
		"ssh sftp scp jk_lsh mysql-client git", cfg.ChrootAppSections)
	assert.Equal(t, "lesspipe pico unzip zip patch which", cfg.ChrootAppPrograms)
	assert.Contains(t, cfg.ChrootCronPrograms, "/usr/bin/php")
	assert.Equal(t, "/root/.ssh/authorized_keys", cfg.ChrootAuthorizedKeysTmpl)
	assert.Equal(t, "allow", cfg.Hardlinks)

	// Present keys override; absent keys keep the default.
	raw := ParseINI("[jailkit]\njailkit_hardlinks=yes\n" +
		"jailkit_chroot_app_sections=coreutils basicshell git\n")
	decodeSection(raw["jailkit"], &cfg)
	assert.Equal(t, "yes", cfg.Hardlinks)
	assert.Equal(t, "coreutils basicshell git", cfg.ChrootAppSections)
	assert.Equal(t, "/home/[username]", cfg.ChrootHome, "default kept for absent key")
	assert.Equal(t, "lesspipe pico unzip zip patch which", cfg.ChrootAppPrograms)
}

func TestDNSConfigDefaultsAndBackend(t *testing.T) {
	cfg := DefaultDNSConfig()
	assert.Equal(t, DNSBackendBind, cfg.DNSBackend)
	assert.Equal(t, DefaultPowerDNSAXFRConf, cfg.PowerDNSAXFRConf)
	assert.Equal(t, "/etc/bind", cfg.BindZonefilesDir)
	assert.Equal(t, "pri.", cfg.BindZonefilesMasterPfx)

	// Decode powerdns backend + custom AXFR path; absent keys keep defaults.
	raw := ParseINI("[dns]\ndns_backend=powerdns\n" +
		"powerdns_axfr_conf=/tmp/pdns.axfr\n" +
		"bind_user=named\n")
	decodeSection(raw["dns"], &cfg)
	assert.Equal(t, DNSBackendPowerDNS, cfg.DNSBackend)
	assert.Equal(t, "/tmp/pdns.axfr", cfg.PowerDNSAXFRConf)
	assert.Equal(t, "named", cfg.BindUser)
	assert.Equal(t, "bind", cfg.BindGroup, "default kept for absent key")

	// NormalizeDNSBackend: empty/unknown → bind; case-insensitive powerdns.
	assert.Equal(t, DNSBackendBind, NormalizeDNSBackend(""))
	assert.Equal(t, DNSBackendBind, NormalizeDNSBackend("  "))
	assert.Equal(t, DNSBackendBind, NormalizeDNSBackend("BIND"))
	assert.Equal(t, DNSBackendBind, NormalizeDNSBackend("mydns"))
	assert.Equal(t, DNSBackendPowerDNS, NormalizeDNSBackend("powerdns"))
	assert.Equal(t, DNSBackendPowerDNS, NormalizeDNSBackend(" PowerDNS "))
	assert.Equal(t, DNSBackendPowerDNS, NormalizeDNSBackend("POWERDNS"))
}

func TestMailConfigDefaultsAndDecode(t *testing.T) {
	// Defaults survive a missing [mail] section entirely.
	cfg := DefaultMailConfig()
	assert.Equal(t, "/var/vmail", cfg.HomedirPath)
	assert.Equal(t, "dovecot", cfg.POP3IMAPDaemon)
	assert.Equal(t, "rspamd", cfg.ContentFilter)
	assert.Equal(t, "2048", cfg.DKIMStrength)
	assert.Equal(t, "vmail", cfg.MailuserName)
	assert.Equal(t, "0", cfg.MailboxSoftDelete)

	// Present keys override; absent keys keep the default.
	raw := ParseINI("[mail]\nhomedir_path=/srv/vmail\nmailbox_soft_delete=30\ndkim_path=/etc/dkim\n")
	decodeSection(raw["mail"], &cfg)
	assert.Equal(t, "/srv/vmail", cfg.HomedirPath)
	assert.Equal(t, "30", cfg.MailboxSoftDelete)
	assert.Equal(t, "/etc/dkim", cfg.DKIMPath)
	assert.Equal(t, "dovecot", cfg.POP3IMAPDaemon, "default kept for absent key")
	assert.Equal(t, "5000", cfg.MailuserUID)
}

// TestServerSectionDecode covers the two [server] keys this port acts on. The
// section carries many more that only ISPConfig3's PHP daemon reads — those
// stay in Raw and are surfaced by the panel as compatibility fields, so
// decoding them here would blur the line the Server tab draws.
func TestServerSectionDecode(t *testing.T) {
	cfg := DefaultServerSection()
	assert.Empty(t, cfg.IPAddress, "no address means no database host suggestion")
	assert.Empty(t, cfg.SSHPort, "no port means the firewall keeps its own default")

	raw := ParseINI("[server]\nip_address=10.0.0.5\nssh_port=2222\n" +
		"backup_dir=/var/backup\nmonit_url=https://monit.example.com\n")
	decodeSection(raw["server"], &cfg)
	assert.Equal(t, "10.0.0.5", cfg.IPAddress)
	assert.Equal(t, "2222", cfg.SSHPort)

	// The compatibility keys are readable from Raw but never land on the
	// decoded struct.
	assert.Equal(t, "/var/backup", raw["server"]["backup_dir"])
	assert.Equal(t, "https://monit.example.com", raw["server"]["monit_url"])
}

// TestServerSectionAbsent is the fresh-install case: a config with no [server]
// section at all must decode to the zero value rather than fail.
func TestServerSectionAbsent(t *testing.T) {
	cfg := DefaultServerSection()
	raw := ParseINI("[web]\nserver_type=nginx\n")
	decodeSection(raw["server"], &cfg)
	assert.Empty(t, cfg.IPAddress)
	assert.Empty(t, cfg.SSHPort)
}
