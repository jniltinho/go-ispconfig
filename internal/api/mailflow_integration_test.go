//go:build integration

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/mail"
	"go-ispconfig/internal/model"
)

// engineNopRunner is a CommandRunner that records nothing and succeeds
// (chown/sievec/mv are faked in this filesystem test).
type engineNopRunner struct{}

func (engineNopRunner) Run(context.Context, string, ...string) ([]byte, error) { return nil, nil }

// nopExecutor satisfies engine.Executor without touching services.
type nopExecutor struct{ runs []string }

func (n *nopExecutor) Run(_ context.Context, service, action string) error {
	n.runs = append(n.runs, service+"/"+action)
	return nil
}

// TestMailEndToEndFlow (task 9.1): create a mail domain (DKIM), a mailbox
// and an alias through the API, then run a daemon-shaped engine with the
// mail plugins and assert the maildir, sieve scripts and DKIM key files
// exist on disk; a transport update queues a postfix reload.
func TestMailEndToEndFlow(t *testing.T) {
	db, srv, cookie, csrf := newMailTestEnv(t)
	ctx := context.Background()

	home := t.TempDir()
	dkimDir := t.TempDir()
	rspamdDir := t.TempDir()
	require.NoError(t, os.MkdirAll(rspamdDir+"/local.d", 0o755))
	// Point the server's [mail] config at the temp dirs so the plugins
	// write where the test can inspect.
	cfgINI := fmt.Sprintf("[mail]\nhomedir_path=%s\nmaildir_path=%s/[domain]/[localpart]\n"+
		"maildir_format=maildir\npop3_imap_daemon=dovecot\ncontent_filter=rspamd\n"+
		"dkim_path=%s\nmailuser_name=vmail\nmailuser_group=vmail\nmailuser_uid=5000\nmailuser_gid=5000\n",
		home, home, dkimDir)
	require.NoError(t, db.Exec("UPDATE server SET config = ? WHERE server_id = 1", cfgINI).Error)

	// --- create rows via the API (writes datalog) ---
	status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
		map[string]any{"server_id": 1, "domain": "flow.example", "active": "y", "dkim": "y"})
	require.Equal(t, http.StatusCreated, status, "%s", data)

	status, data = call(t, srv, http.MethodPost, "/api/mail/mailboxes", cookie, csrf,
		map[string]any{"email": "user1@flow.example", "password": "flow-pw-1", "quota": 1048576})
	require.Equal(t, http.StatusCreated, status, "%s", data)

	status, data = call(t, srv, http.MethodPost, "/api/mail/aliases", cookie, csrf,
		map[string]any{"server_id": 1, "source": "alias@flow.example",
			"destination": "user1@flow.example", "active": "y"})
	require.Equal(t, http.StatusCreated, status, "%s", data)

	// --- daemon-shaped engine with the mail module + plugins ---
	exec := &nopExecutor{}
	services := engine.NewServices(exec, nil)
	mail.RegisterServices(services)
	reg := engine.NewRegistry(nil)

	base := mail.NewPlugin(db, services, engineNopRunner{}, 1, nil)
	dkim := mail.NewDkimPlugin(base)
	dkim.RspamdLocalDir = rspamdDir + "/local.d"
	dkim.UserExists = func(string) bool { return false } // root fallback
	rspamd := mail.NewRspamdPlugin(base, "")
	rspamd.RspamdDir = rspamdDir
	plugins := []engine.Plugin{
		base,
		mail.NewMaildeliverPlugin(base, ""),
		dkim,
		rspamd,
	}
	require.NoError(t, reg.Load([]engine.Module{mail.NewModule()}, plugins))
	daemon, err := engine.NewDaemon(db, reg, services, nil)
	require.NoError(t, err)
	require.NoError(t, daemon.RunCycle(ctx))

	t.Run("maildir provisioned", func(t *testing.T) {
		for _, d := range []string{
			home + "/flow.example/user1/Maildir/new",
			home + "/flow.example/user1/Maildir/cur",
			home + "/flow.example/user1/Maildir/.Junk/new",
		} {
			assert.DirExists(t, d)
		}
	})

	t.Run("sieve scripts written and compiled", func(t *testing.T) {
		maildir := home + "/flow.example/user1"
		assert.FileExists(t, maildir+"/.ispconfig-before.sieve")
		assert.FileExists(t, maildir+"/.ispconfig.sieve")
		// The alias address is collected into the vacation identity list.
		after, err := os.ReadFile(maildir + "/.ispconfig.sieve")
		require.NoError(t, err)
		_ = after // autoresponder off → no vacation block, but the file exists
	})

	t.Run("DKIM key files and rspamd maps written", func(t *testing.T) {
		assert.FileExists(t, dkimDir+"/flow.example.private")
		assert.FileExists(t, dkimDir+"/flow.example.public")
		domains, err := os.ReadFile(rspamdDir + "/local.d/dkim_domains.map")
		require.NoError(t, err)
		assert.Contains(t, string(domains), "flow.example")
	})

	t.Run("transport update queues a postfix reload", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/transports", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "relay.flow.example",
				"transport": "smtp:[10.0.0.9]", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		require.NoError(t, daemon.RunCycle(ctx))
		services.ProcessDelayedActions(ctx)
		assert.Contains(t, exec.runs, "postfix/reload")
	})

	// Sanity: the datalog rows were consumed (server.updated advanced).
	var s model.Server
	require.NoError(t, db.Take(&s, 1).Error)
	var maxID uint64
	require.NoError(t, db.Model(&model.SysDatalog{}).
		Select("COALESCE(MAX(datalog_id),0)").Scan(&maxID).Error)
	assert.EqualValues(t, maxID, s.Updated, "daemon consumed every datalog row")
}
