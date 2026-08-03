package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"go-ispconfig/internal/fail2ban"
	"go-ispconfig/internal/nginx"
)

// Step is one idempotent installer action (design D1). Run must check
// current state first and either converge or return Skip(reason); re-running
// a completed pipeline is always safe.
type Step interface {
	Name() string
	Run(ctx context.Context, st *State) error
}

// skipError marks a step as intentionally not run (already done / disabled
// by an answer). The runner logs it and continues.
type skipError struct{ reason string }

func (e skipError) Error() string { return e.reason }

// Skip returns the sentinel a Step uses to report "nothing to do".
func Skip(reason string) error { return skipError{reason} }

// State carries the resolved inputs and the values generated while the
// pipeline runs (DB handle, generated secrets, summary lines).
type State struct {
	Profile *Profile
	Answers *Answers
	Exec    Executor
	Out     io.Writer
	// Update marks the reconfigure-only run (design D9): config renders and
	// unit restarts, never DB/credentials/certs.
	Update bool
	// WriteCredentials persists the summary to CredentialsFile (default off).
	WriteCredentials bool

	// Euid is the effective uid (preflight root gate); tests set 0.
	Euid int

	// Paths, overridable in tests (defaults set by NewState).
	ConfigDir       string // /etc/go-ispconfig
	SystemdDir      string // /etc/systemd/system
	SystemdMarker   string // /run/systemd/system (systemd presence check)
	BinPath         string // /usr/local/bin/go-ispconfig
	CredentialsFile string // /root/.go-ispconfig-credentials
	LegacyMarker    string // PHP ISPConfig3 install marker
	MySQLSocket     string // /run/mysqld/mysqld.sock (root unix_socket auth)
	// DBAddr is the TCP host:port used in the application DSN and the root
	// TCP fallback (integration tests point it at a container).
	DBAddr string
	// DBUserHosts are the MariaDB host parts the ispconfig user is created
	// for.
	DBUserHosts []string
	// HostIPs enumerates the host's global addresses (tests inject fixed
	// values).
	HostIPs func() []net.IP
	// AcmeWebroot is the Let's Encrypt challenge dir served by site vhosts
	// (must match internal/nginx.AcmeWebroot).
	AcmeWebroot string
	// SelfExe is the running executable, copied to BinPath by the systemd
	// step ("" disables the copy).
	SelfExe string
	// AcmeShHome is acme.sh's default install dir (client detection).
	AcmeShHome string
	// PowerDNSConfPath is the rendered gmysql backend config
	// (/etc/powerdns/pdns.d/pdns.local, mode 0600).
	PowerDNSConfPath string
	// PureFTPdConfigDir holds db/mysql.conf and the conf/ flag files.
	PureFTPdConfigDir string
	// PureFTPdDefaults is the Debian pure-ftpd-common defaults file.
	PureFTPdDefaults string
	// Fail2banJailDir holds the panel-owned ispconfig-*.local jail drop-ins.
	Fail2banJailDir string

	// Set by the mariadb step, consumed by later steps.
	DB         *gorm.DB
	DBPassword string
	// AdminPassword is set only when the seed ran (fresh install); empty on
	// re-runs so credentials are never reprinted (spec: printed once).
	AdminPassword string

	// Summary lines printed after the pipeline finishes.
	Summary []string
}

// NewState builds a State with production defaults.
func NewState(profile *Profile, answers *Answers) *State {
	return &State{
		Profile:         profile,
		Answers:         answers,
		Exec:            execRunner{},
		Out:             os.Stdout,
		Euid:            os.Geteuid(),
		ConfigDir:       "/etc/go-ispconfig",
		SystemdDir:      "/etc/systemd/system",
		SystemdMarker:   "/run/systemd/system",
		BinPath:         "/usr/local/bin/go-ispconfig",
		CredentialsFile: "/root/.go-ispconfig-credentials",
		LegacyMarker:    "/usr/local/ispconfig/server/lib/config.inc.php",
		MySQLSocket:     "/run/mysqld/mysqld.sock",
		DBAddr:          "127.0.0.1:3306",
		DBUserHosts:     []string{"localhost", "127.0.0.1"},
		HostIPs:         detectHostIPs,
		AcmeWebroot:     nginx.AcmeWebroot,
		SelfExe:         selfExe(),
		AcmeShHome:      "/root/.acme.sh",

		PowerDNSConfPath:  "/etc/powerdns/pdns.d/pdns.local",
		PureFTPdConfigDir: "/etc/pure-ftpd",
		PureFTPdDefaults:  "/etc/default/pure-ftpd-common",
		Fail2banJailDir:   fail2ban.JailDir,
	}
}

// selfExe resolves the running binary path ("" when undeterminable).
func selfExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

func (st *State) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(st.Out, format+"\n", args...)
}

// Run executes steps in order, logging done/skipped/failed per step. The
// first real failure stops the pipeline (re-running converges, design D1).
func Run(ctx context.Context, st *State, steps []Step) error {
	for _, s := range steps {
		st.logf("[%s] ...", s.Name())
		err := s.Run(ctx, st)
		var skip skipError
		switch {
		case err == nil:
			st.logf("[%s] done", s.Name())
		case errors.As(err, &skip):
			st.logf("[%s] skipped: %s", s.Name(), skip.reason)
		default:
			st.logf("[%s] FAILED: %v", s.Name(), err)
			return fmt.Errorf("step %s: %w", s.Name(), err)
		}
	}
	return nil
}
