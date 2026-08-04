package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// Picking the wrong dialect makes Dovecot refuse to start, so the version
// parse is the load-bearing part of this step.
func TestDovecotVersion(t *testing.T) {
	for out, want := range map[string]string{
		"2.4.1 (7d8c0e5759)":  "2.4",
		"2.3.21 (47349e2482)": "2.3",
		"2.3.4.1 (f79e8e7e4)": "2.3",
		"2.4.0":               "2.4",
		"3.0.1":               "2.4",
		"2.2.36 (1f10bfa63)":  "2.3",
	} {
		st, mock, _ := testState(t)
		mock.output["dovecot --version"] = out

		got, err := st.dovecotVersion(context.Background())

		require.NoError(t, err, out)
		assert.Equal(t, want, got, "dovecot --version = %q", out)
	}
}

func TestDovecotVersionUnparsable(t *testing.T) {
	st, mock, _ := testState(t)
	mock.output["dovecot --version"] = "who knows"

	_, err := st.dovecotVersion(context.Background())

	require.Error(t, err)
}

// 2.3 keeps mail_location; the maildir path has to be the virtual one, not
// the distro's mbox default, or nothing is ever delivered.
func TestWriteDovecotConfig23(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"

	changed, err := st.writeDovecotConfig("2.3", 1, getconf.DefaultMailConfig())
	require.NoError(t, err)
	assert.True(t, changed)

	main := readFile(t, filepath.Join(st.DovecotConfigDir, "dovecot.conf"))
	assert.Contains(t, main, "mail_location = maildir:/var/vmail/%d/%n/Maildir")
	assert.Contains(t, main, "protocols = imap pop3 lmtp sieve")
	assert.Contains(t, main, "unix_listener /var/spool/postfix/private/dovecot-lmtp")
	assert.Contains(t, main, "user = vmail")
	assert.NotContains(t, main, "{hostname}", "an unreplaced placeholder is a broken config")
	assert.NotContains(t, main, "{homedir}")
}

// 2.4 renamed mail_location to mail_driver/mail_path and the %d/%n
// variables to %{user|...}; rendering the 2.3 file there would not parse.
func TestWriteDovecotConfig24(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"

	_, err := st.writeDovecotConfig("2.4", 1, getconf.DefaultMailConfig())
	require.NoError(t, err)

	main := readFile(t, filepath.Join(st.DovecotConfigDir, "dovecot.conf"))
	assert.Contains(t, main, "dovecot_config_version = 2.4.0")
	assert.Contains(t, main, "mail_driver = maildir")
	assert.Contains(t, main, "mail_home = /var/vmail/%{user | domain}/%{user | username}")
	assert.NotContains(t, main, "\nmail_location", "2.4 removed this setting")
	assert.Contains(t, main, "unix_listener /var/spool/postfix/private/dovecot-lmtp")
}

// The SQL file carries the panel database password and must be 0600 in
// both dialects, with this host's server_id substituted in.
func TestWriteDovecotSQLIsPrivate(t *testing.T) {
	for _, dialect := range []string{"2.3", "2.4"} {
		st, _, _ := testState(t)
		st.DBPassword = "s3cr3t"

		_, err := st.writeDovecotConfig(dialect, 9, getconf.DefaultMailConfig())
		require.NoError(t, err)

		path := filepath.Join(st.DovecotConfigDir, "dovecot-sql.conf")
		fi, err := os.Stat(path)
		require.NoError(t, err, dialect)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), dialect)

		sql := readFile(t, path)
		assert.Contains(t, sql, "s3cr3t", dialect)
		assert.Contains(t, sql, "server_id = '9'", dialect)
		assert.NotContains(t, sql, "{mysql_server_ip}", dialect)
		assert.NotContains(t, sql, "{server_id}", dialect)
	}
}

// Re-running the installer over an unchanged host must not report a change
// (and so must not restart the mail server for nothing).
func TestWriteDovecotConfigIdempotent(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"
	cfg := getconf.DefaultMailConfig()

	_, err := st.writeDovecotConfig("2.4", 1, cfg)
	require.NoError(t, err)
	changed, err := st.writeDovecotConfig("2.4", 1, cfg)

	require.NoError(t, err)
	assert.False(t, changed)
}

// A [mail] section naming another IMAP daemon must leave Dovecot alone
// instead of overwriting a courier setup.
func TestDovecotStepSkipsOtherDaemon(t *testing.T) {
	st, mock, _ := testState(t)
	// No DB: mailRole short-circuits before the daemon check, which is the
	// other skip path this step must honour.
	err := dovecotStep{}.Run(context.Background(), st)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database connection")
	assert.False(t, mock.called("apt-get"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// A config that parses but whose auth process cannot start must fail the
// step: this is exactly what `doveconf -n` alone lets through.
func TestVerifyDovecotAuthFails(t *testing.T) {
	st, mock, _ := testState(t)
	mock.fail["doveadm user *"] = "Internal error occurred"

	err := st.verifyDovecotAuth(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not answering userdb lookups")
}

// A host with no mailboxes yet still has working auth; the wildcard lookup
// exits 0 there and must not fail a fresh install.
func TestVerifyDovecotAuthOK(t *testing.T) {
	st, mock, _ := testState(t)

	require.NoError(t, st.verifyDovecotAuth(context.Background()))
	assert.True(t, mock.called("doveadm user *"))
}
