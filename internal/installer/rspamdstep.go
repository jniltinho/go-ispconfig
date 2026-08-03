package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// Rspamd package/unit names, identical on all five supported distros.
const (
	// RspamdPackage is the scanner; redis-server is already in the base
	// profile set (asynq) and doubles as the Bayes/greylisting backend.
	RspamdPackage = "rspamd"
	// RspamdService is the unit shipped by that package.
	RspamdService = "rspamd"
	// RspamdConfigDir is the config root the mail daemon plugin writes to.
	RspamdConfigDir = "/etc/rspamd"
)

// rspamdUsersConf is the include that makes every per-identity settings
// file the rspamd plugin writes actually load. Without it the settings,
// white/blacklist and greylisting confs under local.d/users/ are read by
// nobody (installer_base.lib.php::configure_rspamd, rspamd_users.conf.master).
const rspamdUsersConf = `settings {
	.include(try=true; glob=true) "$LOCAL_CONFDIR/local.d/users/*.conf"
}
`

// rspamdStep installs Rspamd and deploys the baseline config the mail
// daemon plugin builds on: the users.conf settings include and the
// local.d/users directory. Everything score- or identity-specific is
// rendered by internal/mail/rspamd.go from the database, not here.
type rspamdStep struct{}

// Name identifies the step in the pipeline log.
func (rspamdStep) Name() string { return "rspamd" }

// Run converges Rspamd; it is skipped on hosts that are not mail servers.
func (rspamdStep) Run(ctx context.Context, st *State) error {
	if st.DB == nil {
		return Skip("no database connection")
	}
	mail, err := mailServerRow(st.DB, st.Answers.Hostname)
	if err != nil {
		return err
	}
	if !mail {
		return Skip("not a mail server")
	}

	installed, err := dpkgInstalled(ctx, st, RspamdPackage)
	if err != nil {
		return err
	}
	if !installed {
		st.logf("  installing: %s %s", RspamdPackage, st.Profile.RedisService)
		if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", append(aptOptions, "update", "-q")...); err != nil {
			return fmt.Errorf("apt-get update: %w", err)
		}
		args := append(append([]string{}, aptOptions...), "install", "-y", "-q",
			RspamdPackage, st.Profile.RedisService)
		if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
			return fmt.Errorf("apt-get install rspamd: %w", err)
		}
	}

	localD := filepath.Join(st.RspamdConfigDir, "local.d")
	if err := os.MkdirAll(filepath.Join(localD, "users"), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", localD, err)
	}
	// Daemon-owned: an operator edit here would silently switch the whole
	// per-identity config off, so it is rewritten unconditionally.
	changed, _, err := writeFileBackup(filepath.Join(localD, "users.conf"),
		[]byte(rspamdUsersConf), 0o644)
	if err != nil {
		return err
	}

	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now",
		st.Profile.RedisService, RspamdService); err != nil {
		return fmt.Errorf("enabling %s: %w", RspamdService, err)
	}
	if !changed && installed {
		return Skip("rspamd already configured; services ensured active")
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", RspamdService); err != nil {
		return fmt.Errorf("restarting %s: %w", RspamdService, err)
	}
	return nil
}

// mailServerRow reports whether this host's server row carries the mail
// role.
func mailServerRow(db *gorm.DB, hostname string) (bool, error) {
	var srv model.Server
	if err := db.Where("server_name = ?", hostname).Order("server_id").Take(&srv).Error; err != nil {
		return false, fmt.Errorf("loading server row %q: %w", hostname, err)
	}
	return srv.MailServer == 1, nil
}
