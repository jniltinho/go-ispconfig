package installer

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// A host without the vmail user gets group and user with the exact uid/gid
// the mail plugin chowns maildirs to — otherwise every chown fails and the
// mailboxes stay root-owned.
func TestProvisionVmailCreatesUser(t *testing.T) {
	st, mock, _ := testState(t)
	mock.fail["getent group vmail"] = "not found"
	mock.fail["id -u vmail"] = "no such user"
	dir := t.TempDir() + "/vmail"
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = dir + "/" // the trailing slash must be trimmed

	require.NoError(t, provisionVmail(context.Background(), st, cfg))

	assert.Contains(t, mock.calls, "groupadd --system --gid 5000 vmail")
	assert.Contains(t, mock.calls, "useradd --system --gid vmail --home-dir "+dir+
		" --no-create-home --shell /usr/sbin/nologin "+
		"--comment Virtual mail handler --uid 5000 vmail")
	assert.Contains(t, mock.calls, "chown -R vmail:vmail "+dir)

	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), fi.Mode().Perm())
}

// Converging over an existing install must not try to recreate anything.
func TestProvisionVmailIdempotent(t *testing.T) {
	st, mock, _ := testState(t)
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = t.TempDir() + "/vmail"
	require.NoError(t, os.MkdirAll(cfg.HomedirPath, 0o750))

	require.NoError(t, provisionVmail(context.Background(), st, cfg))

	assert.False(t, mock.called("groupadd"))
	assert.False(t, mock.called("useradd"))
	assert.True(t, mock.called("chown -R vmail:vmail"))
}

// With virtual uid/gid maps the maildirs belong to the web system users;
// a recursive chown would take them away from their owners.
func TestProvisionVmailKeepsVirtualUIDMaps(t *testing.T) {
	st, mock, _ := testState(t)
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = t.TempDir() + "/vmail"
	cfg.MailboxVirtualUidgidMaps = "y"

	require.NoError(t, provisionVmail(context.Background(), st, cfg))

	assert.False(t, mock.called("chown -R"))
	assert.Contains(t, mock.calls, "chown vmail:vmail "+cfg.HomedirPath)
}

// A [mail] section with the name keys blanked out must not produce a
// "chown :" command.
func TestProvisionVmailFallsBackToDefaults(t *testing.T) {
	st, mock, _ := testState(t)
	dir := t.TempDir() + "/vmail"

	require.NoError(t, provisionVmail(context.Background(), st, getconf.MailConfig{HomedirPath: dir}))

	assert.Contains(t, mock.calls, "chown -R vmail:vmail "+dir)
	// No uid/gid configured: the flags are omitted, never passed empty.
	assert.Contains(t, mock.calls, "getent group vmail")
}
