package installer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
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

// rspamdUsersConf mirrors install/tpl/rspamd_users.conf.master. Besides the
// glob that makes every per-identity settings file the rspamd plugin writes
// actually load, it carries the two stanzas legacy relies on: authenticated
// senders skip the RBL and SPF groups, and the role addresses are never
// filtered.
const rspamdUsersConf = `settings {
	authenticated {
		priority = 10;
		authenticated = yes;
		apply "default" {
			symbols_disabled = [];
			groups_disabled = ["rbl", "spf"];
		}
	}
	whitelist {
		priority = 5;
		rcpt = "postmaster";
		rcpt = "hostmaster";
		rcpt = "abuse";
		want_spam = yes;
	}
	.include(try=true; glob=true) "$LOCAL_CONFDIR/local.d/users/*.conf"
	.include(try=true; priority=1,duplicate=merge) "$LOCAL_CONFDIR/local.d/users.local.conf"
}
`

// rspamdUsersInclude pulls users.conf into the main config. rspamd only
// auto-loads local.d/<module>.conf for names it knows as modules, and
// "users" is not one, so without this line every file above is read by
// nobody (installer_base.lib.php::configure_rspamd, lines 2218-2219).
const rspamdUsersInclude = `.include "$LOCAL_CONFDIR/local.d/users.conf"`

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
	included, err := ensureRspamdUsersInclude(filepath.Join(st.RspamdConfigDir, "rspamd.conf"))
	if err != nil {
		return err
	}
	changed = changed || included

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

// ensureRspamdUsersInclude appends the users.conf include to rspamd.conf
// unless it is already there, and reports whether it wrote. The rest of the
// file is the distribution's and is left untouched.
func ensureRspamdUsersInclude(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if bytes.Contains(content, []byte(rspamdUsersInclude)) {
		return false, nil
	}
	if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
		content = append(content, '\n')
	}
	content = append(content, rspamdUsersInclude...)
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// mailServerRow reports whether this host's server row carries the mail
// role.
func mailServerRow(db *gorm.DB, hostname string) (bool, error) {
	srv, err := localServer(db, hostname)
	if err != nil {
		return false, fmt.Errorf("loading server row %q: %w", hostname, err)
	}
	return srv.MailServer == 1, nil
}
