package mail

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// sievecRunner fakes sievec by creating the .svbin next to the input.
type sievecRunner struct {
	fakeRunner
}

func (f *sievecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "sievec" && len(args) == 1 {
		svbin := args[0]
		svbin = svbin[:len(svbin)-len(".sieve")] + ".svbin"
		_ = os.WriteFile(svbin, []byte("BIN"), 0o644)
	}
	return f.fakeRunner.Run(ctx, name, args...)
}

func testMaildeliver(t *testing.T, cfg getconf.MailConfig) (*MaildeliverPlugin, *sievecRunner) {
	t.Helper()
	runner := &sievecRunner{}
	base := NewPlugin(nil, nil, runner, 1, nil)
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) { return cfg, nil }
	return NewMaildeliverPlugin(base, ""), runner
}

// mailboxRow is a full sieve-relevant payload.
func mailboxRow(maildir string) map[string]any {
	return map[string]any{
		"mailuser_id": float64(1), "email": "user1@example.com",
		"maildir": maildir, "move_junk": "y", "imap_prefix": "",
		"autoresponder": "n", "autoresponder_subject": "s",
		"autoresponder_text": "", "autoresponder_start_date": "",
		"autoresponder_end_date": "", "custom_mailfilter": "",
		"forward_in_lda": "n", "cc": "", "quota": float64(0),
	}
}

func TestMaildeliverWritesAndCompiles(t *testing.T) {
	home := t.TempDir()
	maildir := home + "/example.com/user1"
	require.NoError(t, os.MkdirAll(maildir, 0o700))
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	m, runner := testMaildeliver(t, cfg)

	err := m.update(context.Background(), engine.Data{New: mailboxRow(maildir)})
	require.NoError(t, err)

	for _, f := range []string{
		maildir + "/.ispconfig-before.sieve", maildir + "/.ispconfig-before.svbin",
		maildir + "/.ispconfig.sieve", maildir + "/.ispconfig.svbin",
	} {
		assert.FileExists(t, f)
		fi, err := os.Stat(f)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), f)
	}
	assert.DirExists(t, maildir+"/sieve")

	before, err := os.ReadFile(maildir + "/.ispconfig-before.sieve")
	require.NoError(t, err)
	assert.Contains(t, string(before), `fileinto :create "Junk";`)
	assert.Contains(t, string(before), "execute after this")
	assert.Contains(t, runner.all(), "sievec "+maildir+"/.ispconfig-before.sieve")
	assert.Contains(t, runner.all(), "chown vmail:vmail "+maildir+"/.ispconfig.svbin")
}

func TestMaildeliverSkipsWhenIrrelevantChange(t *testing.T) {
	home := t.TempDir()
	maildir := home + "/example.com/user1"
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	m, runner := testMaildeliver(t, cfg)

	newRow := mailboxRow(maildir)
	oldRow := mailboxRow(maildir)
	oldRow["quota"] = float64(999) // only quota differs
	require.NoError(t, m.update(context.Background(), engine.Data{Old: oldRow, New: newRow}))
	assert.Empty(t, runner.all())
	assert.NoFileExists(t, maildir+"/.ispconfig.sieve")
}

func TestMaildeliverDeleteRemovesArtifacts(t *testing.T) {
	home := t.TempDir()
	maildir := home + "/example.com/user1"
	require.NoError(t, os.MkdirAll(maildir, 0o700))
	for _, f := range sieveArtifacts(maildir) {
		if f == maildir+"/sieve/ispconfig.sieve" {
			require.NoError(t, os.MkdirAll(maildir+"/sieve", 0o700))
		}
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
	}
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	m, _ := testMaildeliver(t, cfg)

	require.NoError(t, m.delete(context.Background(), engine.Data{
		Old: map[string]any{"maildir": maildir},
	}))
	for _, f := range sieveArtifacts(maildir) {
		assert.NoFileExists(t, f)
	}
}
