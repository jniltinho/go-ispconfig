package installer

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/powerdns"
)

// powerDNSBackend is the dns_backend value selecting PowerDNS.
const powerDNSBackend = getconf.DNSBackendPowerDNS

// PowerDNSBackend reports whether this install runs the PowerDNS DNS path
// (DNS enabled and --dns-backend powerdns).
func (st *State) PowerDNSBackend() bool {
	return st.Answers.EnableDNS &&
		getconf.NormalizeDNSBackend(st.Answers.DNSBackend) == powerDNSBackend
}

// DNSService is the systemd unit of the selected DNS backend.
func (st *State) DNSService() string {
	if st.PowerDNSBackend() {
		return PowerDNSService
	}
	return st.Profile.BindService
}

// packageSet is the profile package list adjusted for the answers: the FTP
// server comes with the web module, and on the PowerDNS path bind9 is
// replaced by the pdns packages (both daemons would claim port 53).
func (st *State) packageSet() []string {
	pkgs := slices.Clone(st.Profile.Packages)
	if st.Answers.EnableWeb {
		pkgs = append(pkgs, PureFTPdPackage)
	}
	if !st.PowerDNSBackend() {
		return pkgs
	}
	pkgs = slices.DeleteFunc(pkgs, func(p string) bool { return p == BindPackage })
	return append(pkgs, powerDNSPackages...)
}

// powerDNSStep is the Go port of installer_base.lib.php::configure_powerdns:
// powerdns database + grant, embedded schema, pdns.local gmysql config and
// the pdns unit; it also flips server.config [dns] dns_backend to powerdns.
type powerDNSStep struct{}

// Name identifies the step in the pipeline log.
func (powerDNSStep) Name() string { return "powerdns" }

// Run converges the PowerDNS backend; it is skipped entirely on the bind path.
func (powerDNSStep) Run(ctx context.Context, st *State) error {
	if !st.PowerDNSBackend() {
		return Skip("dns backend is bind")
	}

	root, err := connectRoot(st)
	if err != nil {
		return err
	}
	defer closeDB(root)
	stmts := []string{fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci",
		powerdns.DatabaseName)}
	for _, host := range st.DBUserHosts {
		stmts = append(stmts, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'",
			powerdns.DatabaseName, st.Answers.DBUser, host))
	}
	for _, s := range stmts {
		if err := root.Exec(s).Error; err != nil {
			return fmt.Errorf("powerdns database setup: %w", err)
		}
	}

	// Schema as the panel user: it must be able to read/write the zones.
	dsn, err := powerdns.DeriveDSN(st.ispconfigDSN(), "")
	if err != nil {
		return err
	}
	pdnsDB, err := database.Open(dsn)
	if err != nil {
		return fmt.Errorf("connecting to the %s database: %w", powerdns.DatabaseName, err)
	}
	defer closeDB(pdnsDB)
	if err := powerdns.ApplySchema(pdnsDB); err != nil {
		return err
	}

	changed, _, err := writeFileBackup(st.PowerDNSConfPath, []byte(st.renderPdnsLocal()), 0o600)
	if err != nil {
		return err
	}
	if st.DB != nil {
		if err := setServerDNSBackend(st.DB, st.Answers.Hostname, powerDNSBackend); err != nil {
			return err
		}
	}

	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", PowerDNSService); err != nil {
		return fmt.Errorf("enabling %s: %w", PowerDNSService, err)
	}
	if !changed {
		return Skip("powerdns database and config already in place")
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", PowerDNSService); err != nil {
		return fmt.Errorf("restarting %s: %w", PowerDNSService, err)
	}
	return nil
}

// renderPdnsLocal fills the embedded pdns.local.master placeholders with the
// panel database credentials (port of the str_replace block).
func (st *State) renderPdnsLocal() string {
	host, port, err := net.SplitHostPort(st.DBAddr)
	if err != nil {
		host, port = st.DBAddr, "3306"
	}
	return strings.NewReplacer(
		"{mysql_server_host}", host,
		"{mysql_server_port}", port,
		"{mysql_server_ispconfig_user}", st.Answers.DBUser,
		"{mysql_server_ispconfig_password}", st.DBPassword,
		"{powerdns_database}", powerdns.DatabaseName,
	).Replace(powerdns.LocalMasterTemplate)
}

// dnsBackendRe matches the [dns] dns_backend line of a server.config INI.
var dnsBackendRe = regexp.MustCompile(`(?m)^[ \t]*dns_backend[ \t]*=.*$`)

// setDNSBackendKey returns cfg with dns_backend set to backend, adding the
// key to the [dns] section when it is missing (adopted ISPConfig schemas).
func setDNSBackendKey(cfg, backend string) string {
	line := "dns_backend=" + backend
	if dnsBackendRe.MatchString(cfg) {
		return dnsBackendRe.ReplaceAllString(cfg, line)
	}
	if i := strings.Index(cfg, "[dns]"); i >= 0 {
		j := i + len("[dns]")
		return cfg[:j] + "\n" + line + cfg[j:]
	}
	return strings.TrimRight(cfg, "\n") + "\n\n[dns]\n" + line + "\n"
}

// setServerDNSBackend persists the dns_backend choice in the local server row.
func setServerDNSBackend(db *gorm.DB, hostname, backend string) error {
	var srv model.Server
	if err := db.Where("server_name = ?", hostname).Order("server_id").Take(&srv).Error; err != nil {
		return fmt.Errorf("loading server row %q: %w", hostname, err)
	}
	updated := setDNSBackendKey(srv.Config, backend)
	if updated == srv.Config {
		return nil
	}
	return db.Model(&model.Server{}).
		Where("server_id = ?", srv.ServerID).
		Update("config", updated).Error
}
