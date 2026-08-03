//go:build integration

// Queue integration suite (task 4b.4): per-server routing, retry, periodic
// activation and the datalog:ready instant-wake path with its Redis-down
// degradation, against real docker Redis and MariaDB containers.
package queue

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// startWorker runs w in the background and stops it on test cleanup.
func startWorker(t *testing.T, w *Worker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
}

func TestQueueIntegration(t *testing.T) {
	addr := StartRedis(t, "queue")
	cfg := config.QueueConfig{Addr: addr}
	client := NewClient(cfg, nil)
	t.Cleanup(func() { _ = client.Close() })

	t.Run("job routed to owning server only", func(t *testing.T) {
		var seen1, seen2 atomic.Int32
		w1 := NewWorker(cfg, 1, nil)
		w1.Handle("test:routed", func(context.Context, []byte) error {
			seen1.Add(1)
			return nil
		})
		startWorker(t, w1)

		_, err := client.Enqueue(2, asynq.NewTask("test:routed", nil))
		require.NoError(t, err)
		_, err = client.Enqueue(1, asynq.NewTask("test:routed", nil))
		require.NoError(t, err)

		require.Eventually(t, func() bool { return seen1.Load() == 1 }, 10*time.Second, 100*time.Millisecond)
		time.Sleep(2 * time.Second)
		require.EqualValues(t, 1, seen1.Load(), "server 1's worker must never run server 2's job")

		w2 := NewWorker(cfg, 2, nil)
		w2.Handle("test:routed", func(context.Context, []byte) error {
			seen2.Add(1)
			return nil
		})
		startWorker(t, w2)
		require.Eventually(t, func() bool { return seen2.Load() == 1 }, 10*time.Second, 100*time.Millisecond,
			"server 2's job stays queued until server 2's worker consumes it")
	})

	t.Run("failed task is retried", func(t *testing.T) {
		var attempts atomic.Int32
		w := NewWorker(cfg, 3, nil)
		w.retryDelay = func(int, error, *asynq.Task) time.Duration { return 200 * time.Millisecond }
		w.Handle("test:flaky", func(context.Context, []byte) error {
			if attempts.Add(1) == 1 {
				return errors.New("transient failure")
			}
			return nil
		})
		startWorker(t, w)

		_, err := client.Enqueue(3, asynq.NewTask("test:flaky", nil), asynq.MaxRetry(3))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return attempts.Load() >= 2 }, 15*time.Second, 100*time.Millisecond,
			"task must be retried after the first failure")
	})

	t.Run("periodic task fires once per activation", func(t *testing.T) {
		var runs atomic.Int32
		w := NewWorker(cfg, 4, nil)
		w.HandleSchedulerJobs(func(_ context.Context, name string) error {
			require.Equal(t, "beat", name)
			runs.Add(1)
			return nil
		})
		w.RegisterPeriodic("@every 1s", "beat")
		startWorker(t, w)

		require.Eventually(t, func() bool { return runs.Load() >= 1 }, 20*time.Second, 100*time.Millisecond)
		base := runs.Load()
		time.Sleep(3 * time.Second)
		delta := runs.Load() - base
		require.GreaterOrEqual(t, delta, int32(1), "periodic task keeps firing per activation")
		require.LessOrEqual(t, delta, int32(4),
			"one execution per activation: ~3 activations in 3s must not run more than 4 times")
	})

	// The remaining scenarios exercise the full datalog path and need MariaDB.
	dsnPrefix, dbName := database.StartMariaDB(t, "queue")
	database.MariaDBExec(t, dbName, "CREATE DATABASE queue CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/queue?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	repo, err := repository.New[model.WebDomain](db)
	require.NoError(t, err)
	daemon, err := engine.NewDaemon(db, engine.NewRegistry(nil), nil, nil, 0)
	require.NoError(t, err)
	t.Cleanup(func() { datalog.SetReadyNotifier(nil) })

	updated := func() uint64 {
		var srv model.Server
		require.NoError(t, db.Where("server_id = 1").First(&srv).Error)
		return srv.Updated
	}
	lastDatalogID := func() uint64 {
		var maxID uint64
		require.NoError(t, db.Model(&model.SysDatalog{}).Select("COALESCE(MAX(datalog_id),0)").Scan(&maxID).Error)
		return maxID
	}

	t.Run("datalog:ready wakes processing before any tick", func(t *testing.T) {
		w := NewWorker(cfg, daemon.ServerID(), nil)
		w.Handle(TypeDatalogReady, func(ctx context.Context, _ []byte) error {
			return daemon.Wake(ctx)
		})
		startWorker(t, w)
		datalog.SetReadyNotifier(client.ReadyNotifier(daemon.ServerID()))

		// No daemon ticker runs at all: only the wake can process the row.
		require.NoError(t, repo.Insert(ctx, admin, newDomain("wake.example")))
		want := lastDatalogID()
		require.Eventually(t, func() bool { return updated() == want }, 15*time.Second, 100*time.Millisecond,
			"instant wake must process the datalog without a poll tick")
	})

	t.Run("redis down: write succeeds with warning, poll picks up", func(t *testing.T) {
		var logBuf bytes.Buffer
		deadClient := NewClient(config.QueueConfig{Addr: "127.0.0.1:1"},
			slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { _ = deadClient.Close() })
		datalog.SetReadyNotifier(deadClient.ReadyNotifier(daemon.ServerID()))

		require.NoError(t, repo.Insert(ctx, admin, newDomain("outage.example")),
			"business write must never fail because Redis is down")
		require.Contains(t, logBuf.String(), "failed to enqueue datalog:ready")

		want := lastDatalogID()
		require.Greater(t, want, updated(), "change is pending until the poll tick")
		require.NoError(t, daemon.RunCycle(ctx)) // the fallback tick
		require.Equal(t, want, updated(), "poll tick applies the change")
	})
}

// newDomain builds an insertable web_domain record with valid enum members.
func newDomain(domain string) *model.WebDomain {
	return &model.WebDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Domain: domain, Type: "vhost",
		CGI: "n", SSI: "n", Suexec: "y", Subdomain: "none",
		Ruby: "n", Python: "n", Perl: "n",
		SSL: "n", SSLLetsencrypt: "n", SSLLetsencryptExclude: "n",
		RewriteToHTTPS: "n", PHPFPMUseSocket: "y", EnablePagespeed: "n",
		PHPFPMChroot: "n", PM: "ondemand", BackupEncrypt: "n",
		Active: "y", TrafficQuotaLock: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
	}
}
