package mail

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

func TestRetentionDays(t *testing.T) {
	assert.Equal(t, 0, retentionDays(""))
	assert.Equal(t, 0, retentionDays("0"))
	assert.Equal(t, 0, retentionDays("n"))
	assert.Equal(t, 30, retentionDays("y"))
	assert.Equal(t, 14, retentionDays("14"))
	assert.Equal(t, 0, retentionDays("garbage"))
}

func TestSoftDeleteTimestamp(t *testing.T) {
	ts, ok := softDeleteTimestamp("user-deleted-20260101120000")
	require.True(t, ok)
	assert.Equal(t, 2026, ts.Year())
	_, ok = softDeleteTimestamp("plain")
	assert.False(t, ok)
}

func TestPurgeJobRemovesOldTrees(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	cfg.MailboxSoftDelete = "7"
	p, runner := testPlugin(t, cfg)

	old := time.Now().AddDate(0, 0, -30).Format("20060102150405")
	recent := time.Now().AddDate(0, 0, -1).Format("20060102150405")
	// Domain-level soft delete directly under homedir.
	require.NoError(t, os.MkdirAll(home+"/gone.example-deleted-"+old, 0o700))
	// Mailbox-level soft delete under a live domain dir.
	require.NoError(t, os.MkdirAll(home+"/live.example/user-deleted-"+old, 0o700))
	require.NoError(t, os.MkdirAll(home+"/live.example/fresh-deleted-"+recent, 0o700))
	// A live mailbox must be untouched.
	require.NoError(t, os.MkdirAll(home+"/live.example/keep/Maildir/new", 0o700))

	require.NoError(t, p.purgeJob(context.Background()))

	rm := runner.all()
	assert.Contains(t, rm, "rm -rf "+home+"/gone.example-deleted-"+old)
	assert.Contains(t, rm, "rm -rf "+home+"/live.example/user-deleted-"+old)
	for _, c := range rm {
		assert.NotContains(t, c, "fresh-deleted-"+recent, "recent soft-delete kept")
		assert.NotContains(t, c, "/keep", "live mailbox kept")
	}
}

func TestPurgeJobDisabled(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	cfg.MailboxSoftDelete = "0"
	p, runner := testPlugin(t, cfg)
	old := time.Now().AddDate(0, 0, -30).Format("20060102150405")
	require.NoError(t, os.MkdirAll(home+"/x.example-deleted-"+old, 0o700))
	require.NoError(t, p.purgeJob(context.Background()))
	assert.Empty(t, runner.all(), "purge is a no-op when soft delete is off")
}
