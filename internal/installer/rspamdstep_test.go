package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rspamd auto-loads local.d/<module>.conf only for real module names, and
// "users" is not one: without the explicit include every per-identity
// settings file the mail plugin writes is dead config.
func TestEnsureRspamdUsersInclude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rspamd.conf")
	require.NoError(t, os.WriteFile(path, []byte(".include \"$CONFDIR/logging.inc\""), 0o644))

	changed, err := ensureRspamdUsersInclude(path)
	require.NoError(t, err)
	assert.True(t, changed)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), rspamdUsersInclude)
	assert.Contains(t, string(got), "logging.inc", "distribution config is kept")
	assert.Equal(t, 2, strings.Count(string(got), "\n"), "newline added before the appended line")

	// A second converge is a no-op, not a duplicate include.
	changed, err = ensureRspamdUsersInclude(path)
	require.NoError(t, err)
	assert.False(t, changed)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(got), string(again))
}

// The stanzas legacy relies on must survive: authenticated senders skip
// RBL/SPF, role addresses are never filtered.
func TestRspamdUsersConfCarriesLegacyStanzas(t *testing.T) {
	for _, want := range []string{
		`groups_disabled = ["rbl", "spf"];`,
		`rcpt = "postmaster";`,
		`want_spam = yes;`,
		`"$LOCAL_CONFDIR/local.d/users/*.conf"`,
	} {
		assert.Contains(t, rspamdUsersConf, want)
	}
}
