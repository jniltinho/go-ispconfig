package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Web server choices for Answers.WebServer. nginx and Apache are mutually
// exclusive: one server owns port 80.
const (
	WebServerNginx  = "nginx"
	WebServerApache = "apache2"
)

// apachePackages is the Apache package set: the server, mod_fcgid for the
// fast-cgi PHP mode and suexec for the per-site identity the vhosts assume.
var apachePackages = []string{"apache2", "libapache2-mod-fcgid", "apache2-suexec-pristine"}

// apacheModules are the modules every rendered vhost depends on. Enabling a
// module that is already on is a no-op for a2enmod, so the step stays
// idempotent without probing first.
var apacheModules = []string{
	"rewrite", "ssl", "fcgid", "proxy", "proxy_fcgi", "headers", "setenvif", "suexec",
}

// apache2Step installs and wires Apache for the sites the daemon renders:
// packages, modules, the vhost directories and the ACME challenge alias. It
// only runs when the operator chose apache2 as the web server; the nginx
// step skips itself in that case, so exactly one web server is configured.
type apache2Step struct{}

// Name identifies the step in the pipeline log.
func (apache2Step) Name() string { return "apache2" }

// Run converges the Apache install, validating before every reload.
func (apache2Step) Run(ctx context.Context, st *State) error {
	if !st.Answers.EnableWeb {
		return Skip("web server disabled by answer")
	}
	if st.Answers.WebServer != WebServerApache {
		return Skip("web server is nginx")
	}
	p := st.Profile

	if !st.Update {
		missing := make([]string, 0, len(apachePackages))
		for _, pkg := range apachePackages {
			ok, err := dpkgInstalled(ctx, st, pkg)
			if err != nil {
				return err
			}
			if !ok {
				missing = append(missing, pkg)
			}
		}
		if len(missing) > 0 {
			args := append(append([]string{}, aptOptions...), "install", "-y", "-q")
			args = append(args, missing...)
			if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
				return fmt.Errorf("installing %s: %w", strings.Join(missing, " "), err)
			}
		}
	}

	for _, dir := range []string{p.ApacheVhostConfDir, p.ApacheVhostEnabledDir, p.WebsiteBasedir, st.AcmeWebroot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	for _, mod := range apacheModules {
		if _, err := st.Exec.Run(ctx, nil, "a2enmod", "-q", mod); err != nil {
			return fmt.Errorf("enabling apache module %s: %w", mod, err)
		}
	}

	// The ACME challenge alias must be served by every site, so it lives in
	// conf-available rather than in each vhost — the same split the PHP
	// installer uses (apache_acme.conf.master).
	acmeConf := filepath.Join(p.ApacheConfigDir, "conf-available", "go-ispconfig-acme.conf")
	if err := os.MkdirAll(filepath.Dir(acmeConf), 0o755); err != nil {
		return err
	}
	changed, restore, err := writeFileBackup(acmeConf, []byte(apacheAcmeConf(st.AcmeWebroot)), 0o644)
	if err != nil {
		return err
	}
	if _, err := st.Exec.Run(ctx, nil, "a2enconf", "-q", "go-ispconfig-acme"); err != nil {
		return fmt.Errorf("enabling the acme conf: %w", err)
	}

	if _, err := st.Exec.Run(ctx, nil, "apache2ctl", "-t"); err != nil {
		if changed {
			if rerr := restore(); rerr != nil {
				return fmt.Errorf("apache2ctl -t failed (%v) and restoring %s failed too: %w", err, acmeConf, rerr)
			}
		}
		return fmt.Errorf("apache2ctl -t rejected the configuration (previous config restored): %w", err)
	}
	// The packages step skips the web unit for apache2 (its packages are
	// installed here, after that step ran), so this is where the unit is
	// enabled; a reused host may have it stopped or masked.
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", p.ApacheService); err != nil {
		return fmt.Errorf("enabling apache2: %w", err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "reload", p.ApacheService); err != nil {
		return fmt.Errorf("reloading apache2: %w", err)
	}
	// The seeded server config always carries the nginx defaults; without
	// this the daemon on an Apache host registers the nginx plugin and every
	// site fails writing its vhost into /etc/nginx.
	return setServerWebServer(st, WebServerApache)
}

// setServerWebServer persists the web server choice in the local server row:
// [web] server_type (which plugin the daemon registers) and the vhost
// directories that plugin writes into.
func setServerWebServer(st *State, webServer string) error {
	if st.DB == nil {
		return nil
	}
	p := st.Profile
	keys := map[string]string{
		"server_type":                  "nginx",
		"vhost_conf_dir":               p.NginxVhostConfDir,
		"vhost_conf_enabled_dir":       p.NginxVhostEnabledDir,
		"nginx_vhost_conf_dir":         p.NginxVhostConfDir,
		"nginx_vhost_conf_enabled_dir": p.NginxVhostEnabledDir,
	}
	if webServer == WebServerApache {
		// "apache" is the spelling getconf and the fail2ban jail selection
		// compare against, matching ISPConfig's server.ini.
		keys["server_type"] = "apache"
		keys["vhost_conf_dir"] = p.ApacheVhostConfDir
		keys["vhost_conf_enabled_dir"] = p.ApacheVhostEnabledDir
	}
	return updateServerConfig(st.DB, st.Answers.Hostname, "[web]", keys)
}

// apacheAcmeConf is the server-wide Let's Encrypt challenge alias. It is
// intentionally not a .master template: it has one variable and no PHP
// counterpart worth staying byte-compatible with.
func apacheAcmeConf(webroot string) string {
	return fmt.Sprintf(`# Managed by go-ispconfig — do not edit.
Alias /.well-known/acme-challenge %[1]s/.well-known/acme-challenge
<Directory %[1]s/.well-known/acme-challenge>
	Options -Indexes
	AllowOverride None
	Require all granted
</Directory>
`, strings.TrimSuffix(webroot, "/"))
}
