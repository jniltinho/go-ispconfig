package mail

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// fakeRunner records commands without executing anything.
type fakeRunner struct {
	mu   sync.Mutex
	runs [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, append([]string{name}, args...))
	return nil, nil
}

func (f *fakeRunner) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.runs {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

// testPlugin builds a plugin with a fixed config and fake runner (no DB:
// uid/gid already resolved in the fixtures).
func testPlugin(t *testing.T, cfg getconf.MailConfig) (*Plugin, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{}
	p := NewPlugin(nil, nil, runner, 1, nil)
	p.LoadConfig = func(context.Context) (getconf.MailConfig, error) { return cfg, nil }
	return p, runner
}

func TestUserInsertCreatesDovecotMaildirLayout(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)

	maildir := home + "/example.com/user1"
	err := p.userInsert(context.Background(), engine.Data{New: map[string]any{
		"mailuser_id": float64(3), "email": "user1@example.com",
		"uid": float64(5000), "gid": float64(5000),
		"maildir": maildir, "maildir_format": "maildir", "quota": float64(0),
		"server_id": float64(1),
	}})
	require.NoError(t, err)

	// Dovecot layout: <maildir>/Maildir with cur/new/tmp + dot folders.
	for _, d := range []string{
		maildir + "/Maildir/cur", maildir + "/Maildir/new", maildir + "/Maildir/tmp",
		maildir + "/Maildir/.Sent/new", maildir + "/Maildir/.Drafts/cur",
		maildir + "/Maildir/.Trash/tmp", maildir + "/Maildir/.Junk/new",
	} {
		assert.DirExists(t, d)
	}
	// Base domain dir exists 0770.
	fi, err := os.Stat(home + "/example.com")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o770), fi.Mode().Perm())
	// Mailbox dirs are 0700.
	fi, err = os.Stat(maildir + "/Maildir")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())

	// Subscriptions file lists the standard folders once each.
	sub, err := os.ReadFile(maildir + "/Maildir/subscriptions")
	require.NoError(t, err)
	assert.Equal(t, "Sent\nDrafts\nTrash\nJunk\n", string(sub))

	cmds := runner.all()
	assert.Contains(t, cmds, "chown "+cfg.MailuserName+":"+cfg.MailuserGroup+" "+home+"/example.com")
	assert.Contains(t, cmds, "chown -R 5000:5000 "+maildir, "recursive ownership at the end")
}

func TestUserInsertQuarantinesCorruptMaildir(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)

	// Existing Maildir dir without new/cur → quarantine then recreate.
	maildir := home + "/example.com/broken"
	require.NoError(t, os.MkdirAll(maildir+"/Maildir/whatever", 0o700))

	err := p.userInsert(context.Background(), engine.Data{New: map[string]any{
		"mailuser_id": float64(9), "email": "broken@example.com",
		"uid": float64(5000), "gid": float64(5000),
		"maildir": maildir, "maildir_format": "maildir", "server_id": float64(1),
	}})
	require.NoError(t, err)

	assert.DirExists(t, home+"/corrupted/9")
	assert.Contains(t, runner.all(), "mv -f "+maildir+" "+home+"/corrupted/9")
}

func TestUserInsertMdboxUsesDoveadm(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)

	err := p.userInsert(context.Background(), engine.Data{New: map[string]any{
		"mailuser_id": float64(4), "email": "box@example.com",
		"uid": float64(5000), "gid": float64(5000),
		"maildir": home + "/example.com/box", "maildir_format": "mdbox",
		"server_id": float64(1),
	}})
	require.NoError(t, err)

	cmds := runner.all()
	assert.Contains(t, cmds, "doveadm mailbox create -u box@example.com INBOX")
	assert.Contains(t, cmds, "doveadm mailbox subscribe -u box@example.com Drafts")
	assert.NoDirExists(t, home+"/example.com/box/Maildir", "mdbox never builds a maildir layout")
}

func TestUserUpdateRefusesFormatChangeAndMoves(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	p, runner := testPlugin(t, cfg)

	oldMaildir := home + "/example.com/olduser"
	newMaildir := home + "/example.com/newuser"
	require.NoError(t, os.MkdirAll(oldMaildir+"/Maildir/new", 0o700))
	require.NoError(t, os.MkdirAll(oldMaildir+"/Maildir/cur", 0o700))

	err := p.userUpdate(context.Background(), engine.Data{
		Old: map[string]any{
			"mailuser_id": float64(5), "email": "old@example.com",
			"maildir": oldMaildir, "maildir_format": "maildir",
		},
		New: map[string]any{
			"mailuser_id": float64(5), "email": "new@example.com",
			"uid": float64(5000), "gid": float64(5000),
			"maildir": newMaildir, "maildir_format": "mdbox", // attempted change
			"quota": float64(0), "server_id": float64(1),
		},
	})
	require.NoError(t, err)

	cmds := runner.all()
	// Old format wins: no doveadm calls, maildir layout provisioned.
	for _, c := range cmds {
		assert.NotContains(t, c, "doveadm", "format change must be refused")
	}
	assert.DirExists(t, newMaildir+"/Maildir/new", "structure provisioned at the new path")
	// Freshly built target replaced by the moved mailbox (PHP rm+mv).
	assert.Contains(t, cmds, "rm -fr "+newMaildir)
	assert.Contains(t, cmds, "mv -f "+oldMaildir+" "+newMaildir)
}

func TestUserUpdateQuotaOnlyOnNonDovecot(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home
	maildir := home + "/example.com/quotauser"

	run := func(daemon string, quota float64) []string {
		cfg.POP3IMAPDaemon = daemon
		p, runner := testPlugin(t, cfg)
		err := p.userUpdate(context.Background(), engine.Data{
			Old: map[string]any{"maildir": maildir, "maildir_format": "maildir"},
			New: map[string]any{
				"mailuser_id": float64(6), "email": "q@example.com",
				"uid": float64(5000), "gid": float64(5000),
				"maildir": maildir, "maildir_format": "maildir",
				"quota": quota, "server_id": float64(1),
			},
		})
		require.NoError(t, err)
		return runner.all()
	}

	for _, c := range run("dovecot", 12345) {
		assert.NotContains(t, c, "maildirmake -q", "dovecot quota is SQL-authoritative")
	}

	// Non-dovecot (courier-style layout at the maildir root) applies quota.
	require.NoError(t, os.RemoveAll(maildir))
	require.NoError(t, os.MkdirAll(maildir+"/new", 0o700))
	require.NoError(t, os.MkdirAll(maildir+"/cur", 0o700))
	assert.Contains(t, run("courier", 12345), "maildirmake -q 12345S "+maildir)

	// quota 0 removes maildirsize (unlimited).
	require.NoError(t, os.WriteFile(maildir+"/maildirsize", []byte("x"), 0o600))
	run("courier", 0)
	assert.NoFileExists(t, maildir+"/maildirsize")
}

func TestTransportEventsQueuePostfixReload(t *testing.T) {
	exec := &recordExec{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)
	p := NewPlugin(nil, services, &fakeRunner{}, 1, nil)

	for range 3 { // insert/update/delete all map to the same handler
		require.NoError(t, p.transportUpdate(context.Background(), engine.Data{}))
	}
	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, []string{"postfix/reload"}, exec.runs, "batched into one reload")
}

func TestWelcomeMailGatedAndRendered(t *testing.T) {
	home := t.TempDir()
	cfg := getconf.DefaultMailConfig()
	cfg.HomedirPath = home

	var sent []string
	mk := func(enabled string) *Plugin {
		p, _ := testPlugin(t, cfg)
		p.LoadGlobalConfig = func() (map[string]string, error) {
			return map[string]string{
				"enable_welcome_mail": enabled,
				"admin_mail":          "postmaster@panel.test",
				"admin_name":          "The Admin",
			}, nil
		}
		p.Sendmail = func(_ context.Context, from, to string, msg []byte) error {
			sent = append(sent, from+"|"+to+"|"+string(msg))
			return nil
		}
		return p
	}

	// Disabled: nothing sent.
	mk("n").sendWelcomeMail(context.Background(), "user@example.com")
	assert.Empty(t, sent)

	// Enabled: rendered with placeholders, utf-8 subject, envelope from.
	mk("y").sendWelcomeMail(context.Background(), "user@example.com")
	require.Len(t, sent, 1)
	assert.True(t, strings.HasPrefix(sent[0], "postmaster@panel.test|user@example.com|"), sent[0])
	assert.Contains(t, sent[0], "From: The Admin <postmaster@panel.test>")
	assert.Contains(t, sent[0], "To: user@example.com")
	assert.Contains(t, sent[0], "Subject: =?utf-8?B?")
	assert.Contains(t, sent[0], "Welcome to your new email account.")
}
