package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// Every map main.cf references must exist as a section in the asset,
// otherwise Postfix opens a lookup table that was never written and defers
// all mail for that class of recipient.
func TestParsePostfixMapsHasEveryReferencedMap(t *testing.T) {
	maps := parsePostfixMaps(postfixMySQLMaps)

	for _, name := range []string{
		"virtual_domains", "virtual_mailboxes", "virtual_forwardings",
		"virtual_email2email", "virtual_alias_domains", "virtual_alias_maps",
		"virtual_uids", "virtual_gids", "virtual_transports",
		"virtual_sender_login_maps", "virtual_relaydomains",
		"virtual_relayrecipientmaps",
	} {
		assert.Contains(t, maps, name)
		assert.NotEmpty(t, strings.TrimSpace(maps[name]), "%s has an empty query", name)
	}
	// The header comment above the first section is not a map.
	assert.Len(t, maps, 12)
}

// The rendered map carries the credentials and this host's server_id: a
// leftover {server_id} would make the query match no row at all.
func TestRenderPostfixMapSubstitutes(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"

	got := st.renderPostfixMap(parsePostfixMaps(postfixMySQLMaps)["virtual_domains"], 7)

	assert.Contains(t, got, "user = ispconfig")
	assert.Contains(t, got, "password = s3cr3t")
	assert.Contains(t, got, "dbname = dbispconfig")
	assert.Contains(t, got, "hosts = 127.0.0.1")
	assert.Contains(t, got, "server_id = 7")
	assert.NotContains(t, got, "{server_id}")
}

// The map files hold the panel database password, so they must never be
// world-readable.
func TestWritePostfixMapsArePrivate(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"

	require.NoError(t, st.writePostfixMaps(1))

	path := filepath.Join(st.PostfixConfigDir, "mysql-virtual_mailboxes.cf")
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm())

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "password = s3cr3t")
}

// main.cf must route virtual mail into Dovecot's LMTP socket and read every
// virtual table from MySQL — this is the setting that makes a mailbox
// deliverable at all.
func TestApplyPostfixMainCf(t *testing.T) {
	st, mock, _ := testState(t)
	cfg := getconf.DefaultMailConfig()

	require.NoError(t, st.applyPostfixMainCf(context.Background(), cfg))

	require.Len(t, mock.calls, 1)
	call := mock.calls[0]
	assert.True(t, strings.HasPrefix(call, "postconf -e "))
	assert.Contains(t, call, "virtual_transport="+dovecotLMTPTransport)
	assert.Contains(t, call, "virtual_mailbox_maps=proxy:mysql:"+st.PostfixConfigDir+"/mysql-virtual_mailboxes.cf")
	assert.Contains(t, call, "virtual_mailbox_base=/var/vmail")
	assert.Contains(t, call, "smtpd_sasl_type=dovecot")
	assert.Contains(t, call, "myhostname=srv1.example.com")
	// Default content filter is rspamd, so the milter is wired up.
	assert.Contains(t, call, "smtpd_milters=inet:localhost:11332")
}

// Turning the content filter off must not leave Postfix talking to a milter
// that nothing is listening on.
func TestApplyPostfixMainCfWithoutRspamd(t *testing.T) {
	st, mock, _ := testState(t)
	cfg := getconf.DefaultMailConfig()
	cfg.ContentFilter = ""

	require.NoError(t, st.applyPostfixMainCf(context.Background(), cfg))

	assert.NotContains(t, mock.calls[0], "smtpd_milters")
}

// Submission is converged through postconf -M/-P rather than appended, so a
// second run cannot produce a duplicate master.cf service.
func TestApplyPostfixSubmission(t *testing.T) {
	st, mock, _ := testState(t)

	require.NoError(t, st.applyPostfixSubmission(context.Background()))

	assert.Contains(t, mock.calls, "postconf -M -e submission/inet=submission inet n - y - - smtpd")
	assert.Contains(t, mock.calls, "postconf -M -e submissions/inet=submissions inet n - y - - smtpd")
	assert.True(t, mock.called("postconf -P submission/inet/syslog_name=postfix/submission"))
	// 465 is implicit TLS, 587 mandatory STARTTLS.
	assert.True(t, mock.called("postconf -P submissions/inet/syslog_name=postfix/submissions"))
	assert.Contains(t, strings.Join(mock.calls, "\n"), "submissions/inet/smtpd_tls_wrappermode=yes")
	assert.Contains(t, strings.Join(mock.calls, "\n"), "submission/inet/smtpd_tls_security_level=encrypt")
}

// A host that already has both packages must not run apt at all.
func TestInstallPostfixIdempotent(t *testing.T) {
	st, mock, _ := testState(t)
	mock.output["dpkg-query"] = "install ok installed"

	already, err := installPostfix(context.Background(), st)

	require.NoError(t, err)
	assert.True(t, already)
	assert.False(t, mock.called("apt-get"))
}
