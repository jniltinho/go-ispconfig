package installer

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// PureFTPd package/unit names, identical on all five supported distros.
const (
	// PureFTPdPackage is the MySQL-authenticating flavour: FTP accounts are
	// virtual (internal/ftp design D2), so no system account is ever made.
	PureFTPdPackage = "pure-ftpd-mysql"
	// PureFTPdService is the unit shipped by that package.
	PureFTPdService = "pure-ftpd-mysql"
)

// ftpStep is the Go port of installer_base.lib.php::configure_pureftpd:
// db/mysql.conf pointing at the panel's ftp_user table, the chroot/compat
// flags under conf/, and standalone+virtualchroot in the Debian defaults.
type ftpStep struct{}

// Name identifies the step in the pipeline log.
func (ftpStep) Name() string { return "pure-ftpd" }

// Run converges the FTP server; it is skipped when the web module is off
// (no websites, no ftp_user rows) or before the database step ran.
func (ftpStep) Run(ctx context.Context, st *State) error {
	if !st.Answers.EnableWeb {
		return Skip("web module disabled")
	}
	if st.DB == nil {
		return Skip("no database connection")
	}
	serverID, err := localServerID(st.DB, st.Answers.Hostname)
	if err != nil {
		return err
	}

	changed, _, err := writeFileBackup(filepath.Join(st.PureFTPdConfigDir, "db", "mysql.conf"),
		[]byte(st.renderPureFTPdMySQL(serverID)), 0o600)
	if err != nil {
		return err
	}
	// Chroot every login into its dir; the two compat flags match the PHP
	// installer (clients that break on ASCII listings, dotfiles visible).
	for _, flag := range []string{"ChrootEveryone", "BrokenClientsCompatibility", "DisplayDotFiles", "DontResolve"} {
		flagChanged, _, err := writeFileBackup(
			filepath.Join(st.PureFTPdConfigDir, "conf", flag), []byte("yes\n"), 0o644)
		if err != nil {
			return err
		}
		changed = changed || flagChanged
	}
	defaultsChanged, err := enablePureFTPdVirtualChroot(st.PureFTPdDefaults)
	if err != nil {
		return err
	}
	changed = changed || defaultsChanged

	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", PureFTPdService); err != nil {
		return fmt.Errorf("enabling %s: %w", PureFTPdService, err)
	}
	if !changed {
		return Skip("pure-ftpd config already in place")
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", PureFTPdService); err != nil {
		return fmt.Errorf("restarting %s: %w", PureFTPdService, err)
	}
	return nil
}

// renderPureFTPdMySQL fills the embedded pureftpd_mysql.conf.master
// placeholders with the panel database credentials and this server's id
// (port of the str_replace block of configure_pureftpd).
func (st *State) renderPureFTPdMySQL(serverID uint32) string {
	host, _, err := net.SplitHostPort(st.DBAddr)
	if err != nil {
		host = st.DBAddr
	}
	return strings.NewReplacer(
		"{mysql_server_ip}", host,
		"{mysql_server_ispconfig_user}", st.Answers.DBUser,
		"{mysql_server_ispconfig_password}", st.DBPassword,
		"{mysql_server_database}", st.Answers.DBName,
		"{server_id}", fmt.Sprint(serverID),
	).Replace(pureFTPdMySQLConf)
}

// pureFTPdDefaultsRe matches the two Debian pure-ftpd-common keys the panel
// owns (STANDALONE_OR_INETD, VIRTUALCHROOT).
var pureFTPdDefaultsRe = regexp.MustCompile(`(?m)^[ \t]*(STANDALONE_OR_INETD|VIRTUALCHROOT)[ \t]*=.*$`)

// enablePureFTPdVirtualChroot rewrites /etc/default/pure-ftpd-common to
// standalone mode with virtual chroots (symlinks inside the site resolve
// for the user, not for the chroot). A missing file is not an error: only
// Debian/Ubuntu ships it, and it is not needed elsewhere.
func enablePureFTPdVirtualChroot(path string) (bool, error) {
	old, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	updated := pureFTPdDefaultsRe.ReplaceAllStringFunc(string(old), func(line string) string {
		if strings.Contains(line, "VIRTUALCHROOT") {
			return "VIRTUALCHROOT=true"
		}
		return "STANDALONE_OR_INETD=standalone"
	})
	changed, _, err := writeFileBackup(path, []byte(updated), 0o644)
	return changed, err
}

// localServerID resolves this host's server_id (the ftp_user rows and the
// pure-ftpd queries are scoped to it).
func localServerID(db *gorm.DB, hostname string) (uint32, error) {
	srv, err := localServer(db, hostname)
	if err != nil {
		return 0, err
	}
	return srv.ServerID, nil
}
