package installer

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go-ispconfig/internal/getconf"
)

// Dovecot package/unit names, identical on all supported distros.
const (
	// DovecotService is the unit shipped by dovecot-core.
	DovecotService = "dovecot"
	// DovecotConfigDir holds dovecot.conf and the 0600 SQL auth file.
	DovecotConfigDir = "/etc/dovecot"
)

// dovecotPackages is IMAP + POP3 + LMTP delivery + MySQL auth + Sieve.
// dovecot-lmtpd is what Postfix's virtual_transport hands mail to, and
// dovecot-mysql is what turns the panel's mail_user rows into logins.
var dovecotPackages = []string{
	"dovecot-core", "dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd",
	"dovecot-mysql", "dovecot-sieve", "dovecot-managesieved",
}

// dovecotStep installs Dovecot and renders its config against the panel
// database. Dovecot 2.4 (Debian 13) renamed most of the settings 2.3
// (Ubuntu 24.04, Debian 12) uses, so two templates are kept and the
// installed version picks between them — a single file cannot parse on both.
type dovecotStep struct{}

// Name identifies the step in the pipeline log.
func (dovecotStep) Name() string { return "dovecot" }

// Run converges Dovecot; it is skipped on hosts that are not mail servers.
func (dovecotStep) Run(ctx context.Context, st *State) error {
	serverID, cfg, err := mailRole(st)
	if err != nil {
		return err
	}
	if daemon := cmp.Or(cfg.POP3IMAPDaemon, "dovecot"); daemon != "dovecot" {
		return Skip("pop3_imap_daemon is " + daemon)
	}

	installed, err := installDovecot(ctx, st)
	if err != nil {
		return err
	}
	major, err := st.dovecotVersion(ctx)
	if err != nil {
		return err
	}
	st.logf("  dovecot %s dialect", major)

	changed, err := st.writeDovecotConfig(major, serverID, cfg)
	if err != nil {
		return err
	}
	// Rejects a broken render before it can take the mail server down.
	if _, err := st.Exec.Run(ctx, nil, "doveconf", "-n"); err != nil {
		return fmt.Errorf("doveconf -n rejected the rendered config: %w", err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", DovecotService); err != nil {
		return fmt.Errorf("enabling %s: %w", DovecotService, err)
	}
	if !changed && installed {
		return Skip("dovecot already configured; unit ensured active")
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", DovecotService); err != nil {
		return fmt.Errorf("restarting %s: %w", DovecotService, err)
	}
	return st.verifyDovecotAuth(ctx)
}

// verifyDovecotAuth runs a real userdb lookup through the auth process.
//
// This is not redundant with `doveconf -n`: a config can parse and still be
// fatal at runtime (a 2.4 `passdb sql` without sql_driver parses fine, then
// the auth process dies on startup and every delivery is deferred). The
// wildcard runs the iterate query, so it exits 0 on a host that has no
// mailboxes yet and only fails when auth itself is broken.
func (st *State) verifyDovecotAuth(ctx context.Context) error {
	if _, err := st.Exec.Run(ctx, nil, "doveadm", "user", "*"); err != nil {
		return fmt.Errorf("dovecot auth is not answering userdb lookups "+
			"(the rendered SQL config parses but does not work): %w", err)
	}
	return nil
}

// installDovecot installs the packages when missing and reports whether
// they were already there.
func installDovecot(ctx context.Context, st *State) (bool, error) {
	var missing []string
	for _, pkg := range dovecotPackages {
		installed, err := dpkgInstalled(ctx, st, pkg)
		if err != nil {
			return false, err
		}
		if !installed {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return true, nil
	}
	st.logf("  installing: %s", strings.Join(missing, " "))
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", append(aptOptions, "update", "-q")...); err != nil {
		return false, fmt.Errorf("apt-get update: %w", err)
	}
	args := append(append([]string{}, aptOptions...), "install", "-y", "-q")
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", append(args, missing...)...); err != nil {
		return false, fmt.Errorf("apt-get install dovecot: %w", err)
	}
	return false, nil
}

// dovecotVersionRe pulls the major.minor out of `dovecot --version`
// ("2.4.1 (7d8c0e5759)", "2.3.21 (47349e2482)").
var dovecotVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// dovecotVersion reports the config dialect to render: "2.4" for 2.4 and
// newer, "2.3" otherwise.
func (st *State) dovecotVersion(ctx context.Context) (string, error) {
	out, err := st.Exec.Run(ctx, nil, "dovecot", "--version")
	if err != nil {
		return "", fmt.Errorf("dovecot --version: %w", err)
	}
	m := dovecotVersionRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", fmt.Errorf("dovecot --version: cannot parse %q", out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major > 2 || (major == 2 && minor >= 4) {
		return "2.4", nil
	}
	return "2.3", nil
}

// writeDovecotConfig renders dovecot.conf plus the SQL auth file for the
// given dialect and reports whether either changed.
func (st *State) writeDovecotConfig(major string, serverID uint32, cfg getconf.MailConfig) (bool, error) {
	def := getconf.DefaultMailConfig()
	sqlPath := filepath.Join(st.DovecotConfigDir, "dovecot-sql.conf")

	main := strings.NewReplacer(
		"{hostname}", st.Answers.Hostname,
		"{homedir}", cmp.Or(strings.TrimSuffix(cfg.HomedirPath, "/"), def.HomedirPath),
		"{mailuser_name}", cmp.Or(cfg.MailuserName, def.MailuserName),
		"{mailuser_group}", cmp.Or(cfg.MailuserGroup, def.MailuserGroup),
		"{ssl_cert}", st.dovecotTLSCert(),
		"{ssl_key}", st.dovecotTLSKey(),
		"{sql_conf}", sqlPath,
	).Replace(dovecotConf(major))

	host, port, err := net.SplitHostPort(st.DBAddr)
	if err != nil {
		host, port = st.DBAddr, "3306"
	}
	sql := strings.NewReplacer(
		"{mysql_server_ip}", host,
		"{mysql_server_port}", port,
		"{mysql_server_ispconfig_user}", st.Answers.DBUser,
		"{mysql_server_ispconfig_password}", st.DBPassword,
		"{mysql_server_database}", st.Answers.DBName,
		"{server_id}", fmt.Sprint(serverID),
	).Replace(dovecotSQLConf(major))

	mainChanged, restore, err := writeFileBackup(
		filepath.Join(st.DovecotConfigDir, "dovecot.conf"), []byte(main), 0o644)
	if err != nil {
		return false, err
	}
	// 0600: the SQL file carries the panel database password.
	sqlChanged, _, err := writeFileBackup(sqlPath, []byte(sql), 0o600)
	if err != nil {
		if restore != nil {
			_ = restore()
		}
		return false, err
	}
	return mainChanged || sqlChanged, nil
}

// dovecotConf/dovecotSQLConf select the template for the dialect.
func dovecotConf(major string) string {
	if major == "2.4" {
		return dovecot24Conf
	}
	return dovecot23Conf
}

func dovecotSQLConf(major string) string {
	if major == "2.4" {
		return dovecotSQL24Conf
	}
	return dovecotSQL23Conf
}

// dovecotTLSCert/dovecotTLSKey prefer the pair Postfix uses so both daemons
// present the same certificate, then dovecot-core's own self-signed one.
func (st *State) dovecotTLSCert() string {
	return firstExisting(filepath.Join(st.PostfixConfigDir, "smtpd.cert"),
		filepath.Join(st.DovecotConfigDir, "private/dovecot.pem"))
}

func (st *State) dovecotTLSKey() string {
	return firstExisting(filepath.Join(st.PostfixConfigDir, "smtpd.key"),
		filepath.Join(st.DovecotConfigDir, "private/dovecot.key"))
}

// lookupGroupID resolves a group name to its gid.
func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}
