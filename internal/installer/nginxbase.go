package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// sitesEnabledIncludeRe matches an active (uncommented) include of the
// sites-enabled dir in nginx.conf; a commented-out include must not count.
var sitesEnabledIncludeRe = regexp.MustCompile(`(?m)^[^#\n]*include[^#\n]*sites-enabled`)

// nginxBaseStep prepares the directories and include the web module
// (internal/nginx) expects: sites-available/sites-enabled, the website
// basedir and the acme challenge webroot. No panel vhost is rendered —
// serve terminates TLS itself (design D8). The sites-enabled include is
// written to conf.d only when the distro nginx.conf lost it; any write is
// gated by nginx -t with restore-on-failure and reloads only on change.
type nginxBaseStep struct{}

// Name identifies the step in the pipeline log.
func (nginxBaseStep) Name() string { return "nginx-base" }

// Run converges dirs and the optional include, validating before reload.
func (nginxBaseStep) Run(ctx context.Context, st *State) error {
	if !st.Answers.EnableWeb {
		return Skip("web server disabled by answer")
	}
	p := st.Profile
	for _, dir := range []string{p.NginxVhostConfDir, p.NginxVhostEnabledDir, p.WebsiteBasedir, st.AcmeWebroot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	nginxConf, err := os.ReadFile(filepath.Join(p.NginxConfigDir, "nginx.conf"))
	if err != nil {
		return fmt.Errorf("reading nginx.conf: %w", err)
	}
	includeFile := filepath.Join(p.NginxConfigDir, "conf.d", "go-ispconfig-sites.conf")
	if sitesEnabledIncludeRe.Match(nginxConf) {
		return Skip("nginx.conf already includes sites-enabled")
	}

	changed, restore, err := writeFileBackup(includeFile, []byte(nginxSitesInclude), 0o644)
	if err != nil {
		return err
	}
	if !changed {
		return Skip("sites include already in place")
	}
	if _, err := st.Exec.Run(ctx, nil, "nginx", "-t"); err != nil {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("nginx -t failed (%v) and restoring %s failed too: %w", err, includeFile, rerr)
		}
		return fmt.Errorf("nginx -t rejected %s (previous config restored): %w", includeFile, err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "reload", p.NginxService); err != nil {
		return fmt.Errorf("reloading nginx: %w", err)
	}
	return nil
}
