//go:build integration

// Package nginx integration suite (task 8.1): the full panel-to-disk
// pipeline against real docker MariaDB and Redis — repository write →
// sys_datalog row → datalog:ready wake over the queue → daemon cycle → web
// module table hook → nginx plugin → vhost + enabled symlink + PHP-FPM pool
// + site tree on a temp filesystem, with `nginx -t` guarding activation and
// exactly one delayed httpd reload per batch. The OS seam (useradd, nginx,
// systemctl) stays faked: integration here means database, queue and
// datalog plumbing, not the operating system.
package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/queue"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/web"
)

// syncExecutor records service actions thread-safely: the queue worker
// flushes delayed actions from its own goroutine while the test polls.
type syncExecutor struct {
	mu   sync.Mutex
	runs [][2]string
}

func (e *syncExecutor) Run(_ context.Context, service, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = append(e.runs, [2]string{service, action})
	return nil
}

func (e *syncExecutor) Reset() [][2]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.runs
	e.runs = nil
	return out
}

// webServerConfig builds a server.config INI with the [web] section pointing
// into the test's temp directory (Debian layout, temp-dir prefixed).
func webServerConfig(base string) string {
	return fmt.Sprintf(`[web]
server_type=nginx
website_basedir=%[1]s
nginx_vhost_conf_dir=%[1]s/sites-available
nginx_vhost_conf_enabled_dir=%[1]s/sites-enabled
security_level=20
nginx_user=www-data
nginx_group=www-data
php_fpm_init_script=php8.3-fpm
php_fpm_ini_path=%[1]s/php.ini
php_fpm_pool_dir=%[1]s/pool.d
php_fpm_start_port=9010
php_fpm_socket_dir=%[1]s/php-sockets
`, base)
}

// e2eDomain builds an insertable php-fpm vhost row rooted in base.
func e2eDomain(base string) *model.WebDomain {
	return &model.WebDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Domain: "e2e.example", Type: "vhost", Subdomain: "www",
		DocumentRoot: filepath.Join(base, "clients/client1/web1"),
		SystemUser:   "web1", SystemGroup: "client1", Errordocs: 1,
		PHP: "php-fpm", PHPFPMUseSocket: "y",
		CGI: "n", SSI: "n", Suexec: "y",
		Ruby: "n", Python: "n", Perl: "n",
		SSL: "n", SSLLetsencrypt: "n", SSLLetsencryptExclude: "n",
		RewriteToHTTPS: "n", EnablePagespeed: "n",
		PHPFPMChroot: "n", PM: "ondemand", BackupEncrypt: "n",
		Active: "y", TrafficQuotaLock: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
	}
}

func TestDatalogToNginxPipeline(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "nginx")
	database.MariaDBExec(t, name, "CREATE DATABASE nginx CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/nginx?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	base := t.TempDir()
	require.NoError(t, db.Exec("UPDATE server SET config = ? WHERE server_id = 1", webServerConfig(base)).Error)
	vhostDir := filepath.Join(base, "sites-available")
	enabledDir := filepath.Join(base, "sites-enabled")
	require.NoError(t, os.MkdirAll(vhostDir, 0o755))
	require.NoError(t, os.MkdirAll(enabledDir, 0o755))

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	repo, err := repository.New[model.WebDomain](db)
	require.NoError(t, err)

	runner := newFakeRunner()
	exec := &syncExecutor{}
	services := engine.NewServices(web.GuardedExecutor{Inner: exec, Runner: runner}, nil)
	web.RegisterServices(services, "php8.3-fpm")
	plugin := NewPlugin(db, services, runner, "", nil)
	plugin.nginxVersion = "1.24.0"
	plugin.logBaseDir = filepath.Join(base, "httpd-logs")

	reg := engine.NewRegistry(nil)
	// Daemon-shaped load: the nginx plugin subscribes client_delete,
	// announced by the client module (as in cmd/daemon).
	require.NoError(t, reg.Load([]engine.Module{web.NewModule(), clients.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, services, nil, 0)
	require.NoError(t, err)

	rec := e2eDomain(base)
	docroot := rec.DocumentRoot
	vhostFile := filepath.Join(vhostDir, "e2e.example.vhost")
	enabledLink := filepath.Join(enabledDir, "100-e2e.example.vhost")

	t.Run("insert over the queue wake lands vhost, pool and site tree", func(t *testing.T) {
		// Real Redis carries the datalog:ready wake: no RunCycle is called
		// here, only the worker may process the row (design D12).
		addr := queue.StartRedis(t, "nginx")
		qcfg := config.QueueConfig{Addr: addr}
		client := queue.NewClient(qcfg, nil)
		t.Cleanup(func() { _ = client.Close() })
		worker := queue.NewWorker(qcfg, daemon.ServerID(), nil)
		worker.Handle(queue.TypeDatalogReady, func(ctx context.Context, _ []byte) error {
			return daemon.Wake(ctx)
		})
		wctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- worker.Run(wctx) }()
		t.Cleanup(func() {
			cancel()
			require.NoError(t, <-done)
		})
		datalog.SetReadyNotifier(client.ReadyNotifier(daemon.ServerID()))
		t.Cleanup(func() { datalog.SetReadyNotifier(nil) })

		require.NoError(t, repo.Insert(ctx, admin, rec))
		require.Eventually(t, func() bool {
			_, err := os.Stat(vhostFile)
			return err == nil && len(exec.Reset()) > 0
		}, 30*time.Second, 200*time.Millisecond, "wake must render the vhost and flush the delayed reloads")

		content := readFile(t, vhostFile)
		assert.Contains(t, content, "server_name e2e.example www.e2e.example")
		poolSock := fmt.Sprintf("%s/php-sockets/web%d.sock", base, rec.DomainID)
		assert.Contains(t, content, "fastcgi_pass unix:"+poolSock+";")

		target, err := os.Readlink(enabledLink)
		require.NoError(t, err)
		assert.Equal(t, vhostFile, target, "enabled symlink points at the vhost")

		pool := readFile(t, filepath.Join(base, "pool.d", fmt.Sprintf("web%d.conf", rec.DomainID)))
		assert.Contains(t, pool, fmt.Sprintf("[web%d]", rec.DomainID))
		assert.Contains(t, pool, "listen = "+poolSock)
		assert.Contains(t, pool, "pm = ondemand")

		for _, rel := range []string{"web", "log", "ssl", "tmp", "private", "cgi-bin"} {
			assert.DirExists(t, filepath.Join(docroot, rel))
		}
		assert.True(t, runner.users["web1"], "system user created")
		assert.True(t, runner.groups["client1"], "system group created")
		assert.Contains(t, runner.commands(), "nginx -t", "activation is nginx -t guarded")
	})

	t.Run("one cycle flushes exactly one httpd reload and one fpm restart", func(t *testing.T) {
		require.NoError(t, repo.Get(ctx, admin, rec.DomainID, rec))
		rec.HdQuota = 512
		require.NoError(t, repo.Update(ctx, admin, rec))
		rec.HdQuota = 1024
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))
		assert.ElementsMatch(t, [][2]string{{"nginx", "reload"}, {"php8.3-fpm", "restart"}},
			exec.Reset(), "two changes in one batch dedup to one action per service")
	})

	t.Run("redirect update re-renders the vhost", func(t *testing.T) {
		require.NoError(t, repo.Get(ctx, admin, rec.DomainID, rec))
		rec.RedirectType = "permanent"
		rec.RedirectPath = "http://other.example.org/"
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))

		content := readFile(t, vhostFile)
		assert.Contains(t, content, "other.example.org", "redirect target rendered")
		assert.Contains(t, content, "permanent;")
		exec.Reset()
	})

	t.Run("nginx -t failure rolls back, quarantines and records the datalog error", func(t *testing.T) {
		before := readFile(t, vhostFile)
		runner.failCmd = "nginx"
		runner.failOut = `nginx: [emerg] unknown directive "bogus"`
		defer func() { runner.failCmd = "" }()

		require.NoError(t, repo.Get(ctx, admin, rec.DomainID, rec))
		rec.RedirectType = ""
		rec.RedirectPath = ""
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.Equal(t, before, readFile(t, vhostFile), "previous vhost restored")
		quarantined := readFile(t, vhostFile+".err")
		assert.NotContains(t, quarantined, "other.example.org", "failed render quarantined as .err")

		var dlRow model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'web_domain'").Order("datalog_id DESC").First(&dlRow).Error)
		assert.Equal(t, "error", dlRow.Status)
		assert.Contains(t, dlRow.Error, "unknown directive")

		for _, run := range exec.Reset() {
			assert.NotEqual(t, "nginx", run[0], "no nginx reload with a broken config")
		}
	})

	t.Run("delete removes vhost, symlink, pool, site tree and user", func(t *testing.T) {
		require.NoError(t, repo.Delete(ctx, admin, rec.DomainID))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.NoFileExists(t, vhostFile)
		assert.NoFileExists(t, vhostFile+".err")
		_, err := os.Lstat(enabledLink)
		assert.True(t, os.IsNotExist(err), "enabled symlink removed")
		assert.NoFileExists(t, filepath.Join(base, "pool.d", fmt.Sprintf("web%d.conf", rec.DomainID)))
		assert.NoDirExists(t, docroot)
		assert.NoDirExists(t, filepath.Join(base, "httpd-logs", "e2e.example"))
		assert.Contains(t, runner.commands(), "userdel web1")
		assert.ElementsMatch(t, [][2]string{{"nginx", "reload"}, {"php8.3-fpm", "restart"}},
			exec.Reset(), "one reload for nginx plus one restart of the version owning the removed pool")
	})
}

// readFile returns the content of path, failing the test when unreadable.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
