package installer

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/getconf"
)

// Postfix package/unit names, identical on all supported distros.
const (
	// PostfixService is the unit shipped by the postfix package.
	PostfixService = "postfix"
	// PostfixConfigDir is where main.cf and the MySQL maps live.
	PostfixConfigDir = "/etc/postfix"
	// dovecotLMTPTransport is the socket Dovecot's LMTP service listens on
	// inside the Postfix spool; it is what makes a mailbox deliverable.
	dovecotLMTPTransport = "lmtp:unix:private/dovecot-lmtp"
)

// postfixPackages is the SMTP side: the MySQL map driver is a separate
// package and without it every proxy:mysql lookup below fails to open.
var postfixPackages = []string{"postfix", "postfix-mysql"}

// postfixStep installs Postfix and points it at the panel database: the
// virtual domains/mailboxes/aliases come from dbispconfig through
// proxy:mysql maps, delivery goes to Dovecot over LMTP and submission
// authenticates against Dovecot's SASL socket.
//
// main.cf is converged with `postconf -e` rather than rewritten from a
// template: it only touches the keys the panel owns, so operator settings
// and distro defaults elsewhere in the file survive a re-run.
type postfixStep struct{}

// Name identifies the step in the pipeline log.
func (postfixStep) Name() string { return "postfix" }

// Run converges Postfix; it is skipped on hosts that are not mail servers.
func (postfixStep) Run(ctx context.Context, st *State) error {
	serverID, cfg, err := mailRole(st)
	if err != nil {
		return err
	}

	installed, err := installPostfix(ctx, st)
	if err != nil {
		return err
	}
	if err := st.writePostfixMaps(serverID); err != nil {
		return err
	}
	if err := st.applyPostfixMainCf(ctx, cfg); err != nil {
		return err
	}
	if err := st.applyPostfixSubmission(ctx); err != nil {
		return err
	}

	if _, err := st.Exec.Run(ctx, nil, "postfix", "check"); err != nil {
		return fmt.Errorf("postfix check: %w", err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", PostfixService); err != nil {
		return fmt.Errorf("enabling %s: %w", PostfixService, err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", PostfixService); err != nil {
		return fmt.Errorf("restarting %s: %w", PostfixService, err)
	}
	if installed {
		return Skip("postfix already installed; config converged and unit restarted")
	}
	return nil
}

// installPostfix installs the packages when missing and reports whether
// they were already there. DEBIAN_FRONTEND=noninteractive keeps the
// debconf "mail configuration type" prompt from blocking the install; every
// value it guesses is overwritten by applyPostfixMainCf anyway.
func installPostfix(ctx context.Context, st *State) (bool, error) {
	var missing []string
	for _, pkg := range postfixPackages {
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
		return false, fmt.Errorf("apt-get install postfix: %w", err)
	}
	return false, nil
}

// writePostfixMaps renders every section of postfix-mysql-maps.conf into
// its own /etc/postfix/mysql-<name>.cf. They carry the panel database
// password, so they are group-readable by postfix only (the proxymap
// service reads them, not smtpd).
func (st *State) writePostfixMaps(serverID uint32) error {
	for name, query := range parsePostfixMaps(postfixMySQLMaps) {
		path := filepath.Join(st.PostfixConfigDir, "mysql-"+name+".cf")
		content := st.renderPostfixMap(query, serverID)
		if _, _, err := writeFileBackup(path, []byte(content), 0o640); err != nil {
			return err
		}
		// A missing postfix group means the package install failed; that is
		// already an error above, so ignore the lookup and let chown report.
		if err := chownToPostfix(path); err != nil {
			return err
		}
	}
	return nil
}

// renderPostfixMap wraps a query in the connection header every map file
// needs.
func (st *State) renderPostfixMap(query string, serverID uint32) string {
	host, _, err := net.SplitHostPort(st.DBAddr)
	if err != nil {
		host = st.DBAddr
	}
	return fmt.Sprintf(`# Managed by go-ispconfig — holds the panel database password.
user = %s
password = %s
dbname = %s
hosts = %s
query = %s
`, st.Answers.DBUser, st.DBPassword, st.Answers.DBName, host,
		strings.ReplaceAll(strings.TrimRight(query, "\n"), "{server_id}", fmt.Sprint(serverID)))
}

// chownToPostfix hands a map file to the postfix group so proxymap can read
// it while it stays unreadable to everyone else.
func chownToPostfix(path string) error {
	grp, err := lookupGroupID("postfix")
	if err != nil {
		return nil //nolint:nilerr // no postfix group yet: mode 0640 already denies others
	}
	if err := os.Chown(path, 0, grp); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

// parsePostfixMaps splits the embedded asset into name -> query. Sections
// start at a line "=== <name>"; anything before the first one is the file
// header comment.
func parsePostfixMaps(asset string) map[string]string {
	maps := map[string]string{}
	for _, section := range strings.Split(asset, "\n=== ") {
		name, query, ok := strings.Cut(section, "\n")
		if !ok || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "=== ") {
			continue
		}
		maps[strings.TrimSpace(name)] = query
	}
	return maps
}

// applyPostfixMainCf converges the main.cf keys the panel owns in a single
// `postconf -e`.
func (st *State) applyPostfixMainCf(ctx context.Context, cfg getconf.MailConfig) error {
	m := func(name string) string {
		return "proxy:mysql:" + filepath.Join(st.PostfixConfigDir, "mysql-"+name+".cf")
	}
	hostname := st.Answers.Hostname
	settings := []string{
		"myhostname=" + hostname,
		"mydestination=" + hostname + ", localhost, localhost.localdomain",
		"myorigin=/etc/mailname",
		"mynetworks=127.0.0.0/8 [::1]/128",
		"inet_interfaces=all",
		"inet_protocols=all",
		"biff=no",
		"append_dot_mydomain=no",
		"readme_directory=no",
		"recipient_delimiter=+",
		"smtpd_banner=$myhostname ESMTP $mail_name",
		"mailbox_size_limit=" + cmp.Or(cfg.MailboxSizeLimit, "0"),
		"message_size_limit=" + cmp.Or(cfg.MessageSizeLimit, "0"),
		"relayhost=" + cfg.Relayhost,

		// Virtual hosting: everything is looked up in dbispconfig.
		"virtual_mailbox_base=" + cmp.Or(strings.TrimSuffix(cfg.HomedirPath, "/"), "/var/vmail"),
		"virtual_mailbox_domains=" + m("virtual_domains"),
		"virtual_mailbox_maps=" + m("virtual_mailboxes"),
		"virtual_alias_domains=" + m("virtual_alias_domains"),
		"virtual_alias_maps=" + strings.Join([]string{
			m("virtual_forwardings"), m("virtual_alias_maps"), m("virtual_email2email"),
		}, ", "),
		"virtual_uid_maps=" + m("virtual_uids"),
		"virtual_gid_maps=" + m("virtual_gids"),
		"virtual_transport=" + dovecotLMTPTransport,
		"transport_maps=" + m("virtual_transports"),
		"relay_domains=" + m("virtual_relaydomains"),
		"relay_recipient_maps=" + m("virtual_relayrecipientmaps"),
		"smtpd_sender_login_maps=" + m("virtual_sender_login_maps"),
		// proxymap opens the maps once for all smtpd processes; every map
		// consulted through proxy: has to be listed here or it is refused.
		"proxy_read_maps=$local_recipient_maps $mydestination $virtual_alias_maps " +
			"$virtual_alias_domains $virtual_mailbox_maps $virtual_mailbox_domains " +
			"$relay_recipient_maps $relay_domains $canonical_maps $sender_canonical_maps " +
			"$recipient_canonical_maps $relocated_maps $transport_maps $mynetworks " +
			"$smtpd_sender_login_maps $virtual_uid_maps $virtual_gid_maps",

		// SASL is Dovecot's; the socket lives in the Postfix spool.
		"smtpd_sasl_type=dovecot",
		"smtpd_sasl_path=private/auth",
		"smtpd_sasl_auth_enable=yes",
		"smtpd_sasl_authenticated_header=yes",
		"broken_sasl_auth_clients=yes",

		"smtpd_tls_cert_file=" + st.postfixTLSCert(),
		"smtpd_tls_key_file=" + st.postfixTLSKey(),
		"smtpd_tls_security_level=may",
		"smtpd_tls_protocols=!SSLv2,!SSLv3",
		"smtp_tls_security_level=may",
		"smtp_tls_CApath=/etc/ssl/certs",

		"smtpd_helo_required=yes",
		"smtpd_relay_restrictions=permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination",
		"smtpd_recipient_restrictions=permit_mynetworks, permit_sasl_authenticated, " +
			"reject_unauth_destination, check_policy_service unix:private/quota-status",
		"smtpd_sender_restrictions=permit_mynetworks, permit_sasl_authenticated, reject_non_fqdn_sender",
		// Forwarded and getmail-injected mail keeps envelope senders that
		// are not local; rejecting them would break both.
		"smtpd_reject_unlisted_sender=no",
	}
	if cfg.ContentFilter == "rspamd" {
		// rspamd's milter. accept-on-failure: a stopped scanner must not
		// stop mail (the rspamd step runs after this one on a fresh host).
		settings = append(settings,
			"smtpd_milters=inet:localhost:11332",
			"non_smtpd_milters=inet:localhost:11332",
			"milter_protocol=6",
			"milter_default_action=accept",
			"milter_mail_macros=i {mail_addr} {client_addr} {client_name} {auth_authen}",
		)
	}
	if _, err := st.Exec.Run(ctx, nil, "postconf", append([]string{"-e"}, settings...)...); err != nil {
		return fmt.Errorf("postconf -e: %w", err)
	}
	return nil
}

// postfixTLSCert/postfixTLSKey prefer the panel certificate and fall back to
// the snakeoil pair every Debian/Ubuntu ships, so smtpd always has a usable
// STARTTLS key even before ACME ran.
func (st *State) postfixTLSCert() string {
	return firstExisting(filepath.Join(st.PostfixConfigDir, "smtpd.cert"),
		"/etc/ssl/certs/ssl-cert-snakeoil.pem")
}

func (st *State) postfixTLSKey() string {
	return firstExisting(filepath.Join(st.PostfixConfigDir, "smtpd.key"),
		"/etc/ssl/private/ssl-cert-snakeoil.key")
}

// firstExisting returns the first path that exists, or the last candidate
// when none do.
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[len(paths)-1]
}

// postfixSubmission is the master.cf service set mail clients connect to:
// 587 with mandatory STARTTLS and 465 implicit TLS, both authenticated.
// `postconf -M/-P` is used instead of appending to master.cf so a re-run
// (or a host where the operator already uncommented submission) converges
// instead of producing a duplicate service.
var postfixSubmission = []struct {
	service    string
	definition string
	overrides  []string
}{
	{"submission/inet", "submission inet n - y - - smtpd", []string{
		"syslog_name=postfix/submission",
		"smtpd_tls_security_level=encrypt",
		"smtpd_sasl_auth_enable=yes",
		"smtpd_client_restrictions=permit_sasl_authenticated,reject",
	}},
	{"submissions/inet", "submissions inet n - y - - smtpd", []string{
		"syslog_name=postfix/submissions",
		"smtpd_tls_wrappermode=yes",
		"smtpd_sasl_auth_enable=yes",
		"smtpd_client_restrictions=permit_sasl_authenticated,reject",
	}},
}

// applyPostfixSubmission converges the two submission services.
func (st *State) applyPostfixSubmission(ctx context.Context) error {
	for _, svc := range postfixSubmission {
		if _, err := st.Exec.Run(ctx, nil, "postconf", "-M", "-e",
			svc.service+"="+svc.definition); err != nil {
			return fmt.Errorf("postconf -M %s: %w", svc.service, err)
		}
		args := []string{"-P"}
		for _, o := range svc.overrides {
			args = append(args, svc.service+"/"+o)
		}
		if _, err := st.Exec.Run(ctx, nil, "postconf", args...); err != nil {
			return fmt.Errorf("postconf -P %s: %w", svc.service, err)
		}
	}
	return nil
}
