// Package fail2ban owns the panel-managed fail2ban jails (rendered as
// drop-ins under /etc/fail2ban/jail.d) and reads live ban state through
// fail2ban-client. ISPConfig3 has no fail2ban plugin: its
// configure_fail2ban() is a stub and its only jail template
// (install/tpl/dovecot_fail2ban_jail.local.master) was never copied, so the
// option values below follow that orphaned template.
package fail2ban

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
)

// JailDir is the drop-in directory fail2ban reads after jail.conf. Only
// files matching ispconfig-*.local are owned by the panel; operator
// drop-ins with other names are never touched.
const JailDir = "/etc/fail2ban/jail.d"

// Jail option defaults, matching ISPConfig3's only fail2ban template.
const (
	defaultFindtime = 1200
	defaultBantime  = 1200
	defaultMaxretry = 5
)

// Jail is one rendered jail stanza.
type Jail struct {
	// Name is both the stanza name and the ispconfig-<name>.local suffix.
	Name string
	// Filter is a packaged fail2ban filter; the panel ships none of its own.
	Filter string
	// LogPath is the log the filter is applied to.
	LogPath string
	// Ports is the action's multiport list (service names or numbers).
	Ports string
	// Maxretry is the per-jail failure count before a ban.
	Maxretry int
}

// Jails returns the panel-owned jail set. webServer picks the HTTP jail:
// an "apache"-prefixed server_type (getconf [web] server_type) yields the
// apache-auth jail, anything else the nginx one; an empty value yields no
// HTTP jail at all (node runs no web server).
func Jails(webServer string) []Jail {
	jails := []Jail{
		{Name: "sshd", Filter: "sshd", LogPath: "/var/log/auth.log", Ports: "ssh", Maxretry: 3},
		{Name: "pure-ftpd", Filter: "pure-ftpd", LogPath: "/var/log/syslog", Ports: "ftp,ftp-data,ftps", Maxretry: defaultMaxretry},
		{Name: "dovecot", Filter: "dovecot", LogPath: "/var/log/mail.log", Ports: "pop3,pop3s,imap,imaps", Maxretry: 20},
		{Name: "postfix", Filter: "postfix", LogPath: "/var/log/mail.log", Ports: "smtp,submission,submissions", Maxretry: defaultMaxretry},
	}
	switch {
	case strings.HasPrefix(webServer, "apache"):
		jails = append(jails, Jail{Name: "apache-auth", Filter: "apache-auth",
			LogPath: "/var/log/apache2/error.log", Ports: "http,https", Maxretry: defaultMaxretry})
	case webServer != "":
		jails = append(jails, Jail{Name: "nginx-http-auth", Filter: "nginx-http-auth",
			LogPath: "/var/log/nginx/error.log", Ports: "http,https", Maxretry: defaultMaxretry})
	}
	return jails
}

// Render returns the drop-in content for one jail. ignoreip always carries
// loopback so an apply can never ban the panel out of its own host.
func (j Jail) Render() string {
	return fmt.Sprintf(`# Managed by go-ispconfig - do not edit.
# Add your own jails as other files in %s.
[%s]
enabled  = true
filter   = %s
logpath  = %s
action   = iptables-multiport[name=%s, port="%s", protocol=tcp]
ignoreip = 127.0.0.1/8 ::1
maxretry = %d
findtime = %d
bantime  = %d
`, JailDir, j.Name, j.Filter, j.LogPath, j.Name, j.Ports, j.Maxretry, defaultFindtime, defaultBantime)
}

// Write renders every jail into dir as ispconfig-<name>.local and removes
// the panel-owned drop-ins that are no longer part of the set — switching
// the web server must not leave the previous HTTP jail behind watching a
// log file that no longer exists. It reports whether anything changed
// (idempotent re-run writes and removes nothing).
func Write(dir string, jails []Jail) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", dir, err)
	}
	changed := false
	wanted := make(map[string]bool, len(jails))
	for _, j := range jails {
		name := "ispconfig-" + j.Name + ".local"
		wanted[name] = true
		path := filepath.Join(dir, name)
		content := []byte(j.Render())
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
			continue
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return changed, fmt.Errorf("writing %s: %w", path, err)
		}
		changed = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return changed, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if wanted[name] || !strings.HasPrefix(name, "ispconfig-") || !strings.HasSuffix(name, ".local") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return changed, fmt.Errorf("removing %s: %w", filepath.Join(dir, name), err)
		}
		changed = true
	}
	return changed, nil
}

// Apply writes the jail set for webServer into dir and reloads fail2ban
// only when a file actually changed.
func Apply(ctx context.Context, runner engine.CommandRunner, dir, webServer string) error {
	changed, err := Write(dir, Jails(webServer))
	if err != nil || !changed {
		return err
	}
	if out, err := runner.Run(ctx, "fail2ban-client", "reload"); err != nil {
		return fmt.Errorf("fail2ban-client reload: %w (%s)", err, bytes.TrimSpace(out))
	}
	return nil
}
