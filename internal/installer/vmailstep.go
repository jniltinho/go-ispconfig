package installer

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// vmailStep provisions the virtual mail identity Dovecot, Postfix and
// Rspamd deliver as. Nothing else creates it — the mail plugin only ever
// runs `chown vmail:vmail`, which fails (and is merely logged) when the
// user is missing, leaving every maildir root-owned and undeliverable.
type vmailStep struct{}

// Name identifies the step in the pipeline log.
func (vmailStep) Name() string { return "vmail" }

// Run provisions the mail user on mail servers only; it is skipped before
// the database step ran or when this host has no mail role.
func (vmailStep) Run(ctx context.Context, st *State) error {
	if st.DB == nil {
		return Skip("no database connection")
	}
	serverID, err := localServerID(st.DB, st.Answers.Hostname)
	if err != nil {
		return err
	}
	var srv model.Server
	if err := st.DB.Take(&srv, serverID).Error; err != nil {
		return fmt.Errorf("loading server %d: %w", serverID, err)
	}
	if srv.MailServer != 1 {
		return Skip("mail role disabled")
	}

	cfg := getconf.DefaultMailConfig()
	if sc, err := getconf.GetServerConfig(st.DB, serverID); err == nil {
		cfg = sc.Mail
	}
	return provisionVmail(ctx, st, cfg)
}

// provisionVmail creates the mailuser group and user with the uid/gid the
// mail plugin chowns to, then converges the homedir. Idempotent: an
// existing group/user is left untouched.
func provisionVmail(ctx context.Context, st *State, cfg getconf.MailConfig) error {
	def := getconf.DefaultMailConfig()
	home := cmp.Or(strings.TrimSuffix(cfg.HomedirPath, "/"), def.HomedirPath)
	name := cmp.Or(cfg.MailuserName, def.MailuserName)
	group := cmp.Or(cfg.MailuserGroup, def.MailuserGroup)

	if _, err := st.Exec.Run(ctx, nil, "getent", "group", group); err != nil {
		args := []string{"--system"}
		if cfg.MailuserGID != "" {
			args = append(args, "--gid", cfg.MailuserGID)
		}
		if _, err := st.Exec.Run(ctx, nil, "groupadd", append(args, group)...); err != nil {
			return fmt.Errorf("creating group %s: %w", group, err)
		}
		st.logf("  created group: %s", group)
	}
	if _, err := st.Exec.Run(ctx, nil, "id", "-u", name); err != nil {
		args := []string{"--system", "--gid", group, "--home-dir", home,
			"--no-create-home", "--shell", "/usr/sbin/nologin",
			"--comment", "Virtual mail handler"}
		if cfg.MailuserUID != "" {
			args = append(args, "--uid", cfg.MailuserUID)
		}
		if _, err := st.Exec.Run(ctx, nil, "useradd", append(args, name)...); err != nil {
			return fmt.Errorf("creating user %s: %w", name, err)
		}
		st.logf("  created user: %s (home %s)", name, home)
	}

	// 0750 like legacy: the maildirs below are reachable through the vmail
	// identity only.
	if err := os.MkdirAll(home, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", home, err)
	}
	if err := os.Chmod(home, 0o750); err != nil {
		return fmt.Errorf("chmod %s: %w", home, err)
	}
	args := []string{name + ":" + group, home}
	if cfg.MailboxVirtualUidgidMaps != "y" {
		// Repairs trees created before this step existed. Never recursive
		// under virtual uid/gid maps: there the maildirs are deliberately
		// owned by the individual web system users.
		args = append([]string{"-R"}, args...)
	}
	if _, err := st.Exec.Run(ctx, nil, "chown", args...); err != nil {
		return fmt.Errorf("chown %s: %w", home, err)
	}
	return nil
}
