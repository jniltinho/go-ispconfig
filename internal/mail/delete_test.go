package mail

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

func TestSafeMailPath(t *testing.T) {
	home := "/var/vmail"
	assert.True(t, safeMailPath("/var/vmail/example.com/user", home))
	for _, bad := range []string{
		"/var/vmail",        // homedir itself
		"/etc/passwd",       // outside
		"/var/vmail/../etc", // traversal
		"/var/vmail//x",     // double slash
		"/var/vmail/a*",     // glob
		"/var/vmail/a&b",    // shell meta
		"/var/vmailevil/x",  // prefix trick
		"/var/vm",           // too short + outside
	} {
		assert.False(t, safeMailPath(bad, home), bad)
	}
}

func TestUserDeleteHardAndSoft(t *testing.T) {
	home := t.TempDir()
	maildir := home + "/example.com/user1"
	require.NoError(t, os.MkdirAll(maildir, 0o700))

	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)
	require.NoError(t, p.userDelete(context.Background(), engine.Data{
		Old: map[string]any{"maildir": maildir},
	}))
	assert.Contains(t, runner.all(), "rm -rf "+maildir)

	// Soft delete renames with the timestamp suffix and touches it.
	cfg.MailboxSoftDelete = "30"
	p, runner = testPlugin(t, cfg)
	require.NoError(t, p.userDelete(context.Background(), engine.Data{
		Old: map[string]any{"maildir": maildir},
	}))
	cmds := runner.all()
	require.Len(t, cmds, 2)
	assert.True(t, strings.HasPrefix(cmds[0], "mv "+maildir+" "+maildir+"-deleted-"), cmds[0])
	assert.True(t, strings.HasPrefix(cmds[1], "touch "+maildir+"-deleted-"), cmds[1])
}

func TestUserDeleteRefusesUnsafePath(t *testing.T) {
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = t.TempDir()
	p, runner := testPlugin(t, cfg)
	require.NoError(t, p.userDelete(context.Background(), engine.Data{
		Old: map[string]any{"maildir": "/etc/passwd-looking-path"},
	}))
	assert.Empty(t, runner.all(), "no filesystem command may run for an unsafe path")
}

func TestDomainDeleteRemovesTrees(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(home+"/example.com", 0o700))
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)

	require.NoError(t, p.domainDelete(context.Background(), engine.Data{
		Old: map[string]any{"domain": "example.com"},
	}))
	cmds := runner.all()
	assert.Contains(t, cmds, "rm -rf "+home+"/example.com")
	assert.Contains(t, cmds, "rm -rf "+home+"/mailfilters/example.com")

	// Empty domain refused entirely.
	p, runner = testPlugin(t, cfg)
	require.NoError(t, p.domainDelete(context.Background(), engine.Data{
		Old: map[string]any{"domain": ""},
	}))
	assert.Empty(t, runner.all())
}
