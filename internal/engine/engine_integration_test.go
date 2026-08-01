//go:build integration

// Package engine integration suite: the full sys_datalog pipeline against a
// real MariaDB (task 4.6) — repository write → datalog row → daemon cycle →
// module table-hook → named event → plugin → delayed service restart — plus
// the quarantine, per-row crash recovery, dedup-restart and startup guard
// scenarios of the sys-datalog spec.
package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// recorderExec records service actions instead of calling systemctl.
type recorderExec struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorderExec) Run(_ context.Context, service, action string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, service+"/"+action)
	return nil
}

func (r *recorderExec) Reset() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = nil
	return out
}

// webModule is the fake module: web_domain table hook translated into named
// events, exactly like a real nginx module will.
type webModule struct{}

func (webModule) Name() string { return "web_module" }

func (webModule) OnLoad(r *engine.Registry) error {
	r.AnnounceEvents("web_module", "web_domain_insert", "web_domain_update", "web_domain_delete")
	r.RegisterTableHook("web_domain", func(table, action string, data engine.Data) error {
		return r.RaiseEvent(engine.EventName(table, action), data)
	})
	return nil
}

// webPlugin is the fake plugin: records received events and asks for a
// delayed nginx reload on each, like a real vhost plugin.
type webPlugin struct {
	services *engine.Services
	mu       sync.Mutex
	events   []string
	last     engine.Data
}

func (p *webPlugin) Name() string { return "web_plugin" }

func (p *webPlugin) OnLoad(r *engine.Registry) error {
	for _, ev := range []string{"web_domain_insert", "web_domain_update", "web_domain_delete"} {
		if err := r.RegisterEvent(ev, p.record); err != nil {
			return err
		}
	}
	return nil
}

func (p *webPlugin) record(event string, data engine.Data) error {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.last = data
	p.mu.Unlock()
	p.services.RestartServiceDelayed("nginx", engine.ActionReload)
	return nil
}

func (p *webPlugin) Reset() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.events
	p.events = nil
	return out
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

func serverUpdated(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	var srv model.Server
	require.NoError(t, db.Where("server_id = 1").First(&srv).Error)
	return srv.Updated
}

func lastDatalogID(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	var maxID uint64
	require.NoError(t, db.Model(&model.SysDatalog{}).Select("COALESCE(MAX(datalog_id),0)").Scan(&maxID).Error)
	return maxID
}

func TestEngineE2E(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "engine")
	database.MariaDBExec(t, name, "CREATE DATABASE engine CHARACTER SET utf8mb4")

	db, err := database.Open(dsnPrefix + "/engine?charset=utf8mb4&parseTime=True&loc=Local")
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

	exec := &recorderExec{}
	services := engine.NewServices(exec, nil)
	services.Register("nginx")
	plugin := &webPlugin{services: services}

	// observed collects server.updated as seen from inside a table hook —
	// the daemon must advance it only after the hook (per-row semantics).
	var observed []uint64
	reg := engine.NewRegistry(nil)
	reg.RegisterTableHook("web_domain", func(string, string, engine.Data) error {
		observed = append(observed, serverUpdated(t, db))
		return nil
	})
	require.NoError(t, reg.Load([]engine.Module{webModule{}}, []engine.Plugin{plugin}))

	sched := engine.NewScheduler(db, nil)
	daemon, err := engine.NewDaemon(db, reg, services, sched, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, daemon.ServerID())

	var domainID uint32

	t.Run("repository insert writes datalog row", func(t *testing.T) {
		rec := newDomain("e2e.example")
		require.NoError(t, repo.Insert(ctx, admin, rec))
		domainID = rec.DomainID

		var row model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'web_domain'").Order("datalog_id DESC").First(&row).Error)
		require.Equal(t, "i", row.Action)
		require.Equal(t, fmt.Sprintf("domain_id:%d", domainID), row.DBIdx)
		require.EqualValues(t, 1, row.ServerID)
		require.Equal(t, "admin", row.User)
		var data engine.Data
		require.NoError(t, json.Unmarshal([]byte(row.Data), &data))
		require.Equal(t, "e2e.example", data.New["domain"])
		require.Empty(t, data.Old)
	})

	t.Run("failed transaction leaves no datalog row", func(t *testing.T) {
		before := lastDatalogID(t, db)
		dup := newDomain("dup.example")
		dup.DomainID = domainID // duplicate PK forces the INSERT to fail
		require.Error(t, repo.Insert(ctx, admin, dup))
		require.Equal(t, before, lastDatalogID(t, db), "rollback must remove the datalog row too")
	})

	t.Run("daemon cycle dispatches hook, event and one reload", func(t *testing.T) {
		require.NoError(t, daemon.RunCycle(ctx))
		require.Equal(t, []string{"web_domain_insert"}, plugin.Reset())
		require.Equal(t, "e2e.example", plugin.last.New["domain"])
		require.Equal(t, []string{"nginx/reload"}, exec.Reset())
		require.Equal(t, lastDatalogID(t, db), serverUpdated(t, db))
	})

	t.Run("update journals only changed fields", func(t *testing.T) {
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, admin, domainID, &rec))
		rec.Active = "n"
		require.NoError(t, repo.Update(ctx, admin, &rec))

		var row model.SysDatalog
		require.NoError(t, db.Order("datalog_id DESC").First(&row).Error)
		require.Equal(t, "u", row.Action)
		var data engine.Data
		require.NoError(t, json.Unmarshal([]byte(row.Data), &data))
		require.Equal(t, map[string]any{"active": "y"}, data.Old)
		require.Equal(t, map[string]any{"active": "n"}, data.New)

		require.NoError(t, daemon.RunCycle(ctx))
		require.Equal(t, []string{"web_domain_update"}, plugin.Reset())
		exec.Reset()
	})

	t.Run("ten changes cause exactly one reload", func(t *testing.T) {
		for i := range 10 {
			var rec model.WebDomain
			require.NoError(t, repo.Get(ctx, admin, domainID, &rec))
			rec.HdQuota = int64(100 + i)
			require.NoError(t, repo.Update(ctx, admin, &rec))
		}
		require.NoError(t, daemon.RunCycle(ctx))
		require.Len(t, plugin.Reset(), 10, "every change raises its event")
		require.Equal(t, []string{"nginx/reload"}, exec.Reset(), "spec: bind/nginx reloaded exactly once per batch")
	})

	t.Run("server.updated advances per row", func(t *testing.T) {
		base := serverUpdated(t, db)
		observed = nil
		var ids []uint64
		for i := range 3 {
			rec := newDomain(fmt.Sprintf("row%d.example", i))
			require.NoError(t, repo.Insert(ctx, admin, rec))
			ids = append(ids, lastDatalogID(t, db))
		}
		require.NoError(t, daemon.RunCycle(ctx))
		// Inside the hook for row N the daemon has only advanced through
		// row N-1: a crash mid-batch resumes at the first unprocessed row.
		require.Equal(t, []uint64{base, ids[0], ids[1]}, observed)
		require.Equal(t, ids[2], serverUpdated(t, db))
		plugin.Reset()
		exec.Reset()
	})

	t.Run("non-JSON legacy row is quarantined without crash", func(t *testing.T) {
		phpSerialized := `a:2:{s:3:"old";a:0:{}s:3:"new";a:1:{s:6:"domain";s:11:"php.example";}}`
		require.NoError(t, db.Exec(
			"INSERT INTO sys_datalog (server_id, dbtable, dbidx, action, tstamp, user, data, status) VALUES (1, 'web_domain', 'domain_id:999', 'u', ?, 'admin', ?, 'ok')",
			time.Now().Unix(), phpSerialized).Error)
		badID := lastDatalogID(t, db)

		require.NoError(t, daemon.RunCycle(ctx), "daemon must not crash on a PHP-serialize payload")
		require.Equal(t, badID, serverUpdated(t, db), "updated advances past the quarantined row")

		var row model.SysDatalog
		require.NoError(t, db.Where("datalog_id = ?", badID).First(&row).Error)
		require.Equal(t, "error", row.Status, "quarantine marker set")
		require.Contains(t, row.Error, "datalogError")

		var logRow model.SysLog
		require.NoError(t, db.Where("datalog_id = ?", badID).First(&logRow).Error)
		require.Contains(t, logRow.Message, "datalogError")
		require.EqualValues(t, 2, logRow.Loglevel)
		require.Empty(t, plugin.Reset(), "no event for a quarantined row")
		exec.Reset()
	})

	t.Run("empty diff row is logged and skipped", func(t *testing.T) {
		require.NoError(t, db.Exec(
			"INSERT INTO sys_datalog (server_id, dbtable, dbidx, action, tstamp, user, data, status) VALUES (1, 'web_domain', 'domain_id:998', 'u', ?, 'admin', '{\"old\":{},\"new\":{}}', 'ok')",
			time.Now().Unix()).Error)
		emptyID := lastDatalogID(t, db)
		require.NoError(t, daemon.RunCycle(ctx))
		require.Equal(t, emptyID, serverUpdated(t, db))
		var logRow model.SysLog
		require.NoError(t, db.Where("datalog_id = ?", emptyID).First(&logRow).Error)
		require.Contains(t, logRow.Message, "empty")
		require.Empty(t, plugin.Reset())
		exec.Reset()
	})

	t.Run("pending remote action is dispatched and state stored", func(t *testing.T) {
		got := ""
		reg.RegisterAction("os_update", func(action, param string) (string, error) {
			got = param
			return engine.StateOK, nil
		})
		require.NoError(t, db.Exec(
			"INSERT INTO sys_remoteaction (server_id, tstamp, action_type, action_param, action_state) VALUES (1, ?, 'os_update', 'now', 'pending')",
			time.Now().Unix()).Error)
		require.NoError(t, daemon.RunCycle(ctx))

		require.Equal(t, "now", got)
		var a model.SysRemoteAction
		require.NoError(t, db.Where("action_type = 'os_update'").First(&a).Error)
		require.Equal(t, "ok", a.ActionState)
		exec.Reset()
	})

	t.Run("scheduler runs due job and persists state", func(t *testing.T) {
		ran := 0
		require.NoError(t, sched.Register("e2e_job", "* * * * *", func(context.Context) error {
			ran++
			return nil
		}))
		past := time.Now().Add(-2 * time.Minute).Format(time.RFC3339)
		require.NoError(t, db.Exec(
			"REPLACE INTO sys_config (`group`, `name`, `value`) VALUES ('scheduler', 'e2e_job_last_run', ?)", past).Error)

		require.NoError(t, daemon.RunCycle(ctx))
		require.Equal(t, 1, ran, "due job runs exactly once")
		require.NoError(t, daemon.RunCycle(ctx))
		require.Equal(t, 1, ran, "job does not rerun before the next activation")

		var status model.SysConfig
		require.NoError(t, db.Where("`group` = 'scheduler' AND `name` = 'e2e_job_status'").First(&status).Error)
		require.Equal(t, "ok", status.Value)

		jobs := sched.Jobs(ctx)
		require.Len(t, jobs, 1)
		require.Equal(t, "e2e_job", jobs[0].Name)
		require.NotEmpty(t, jobs[0].LastRun)
		exec.Reset()
	})

	t.Run("startup guard refuses multi-server and mirror setups", func(t *testing.T) {
		require.NoError(t, db.Exec("UPDATE server SET mirror_server_id = 2 WHERE server_id = 1").Error)
		_, err := engine.GuardServer(db)
		require.ErrorContains(t, err, "mirror")
		require.NoError(t, db.Exec("UPDATE server SET mirror_server_id = 0 WHERE server_id = 1").Error)

		require.NoError(t, db.Exec(
			"INSERT INTO server (sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other, server_name, config, active) VALUES (1,1,'riud','riud','r','server2.test','',1)").Error)
		_, err = engine.GuardServer(db)
		require.ErrorContains(t, err, "multi-server")
		require.NoError(t, db.Exec("DELETE FROM server WHERE server_name = 'server2.test'").Error)

		_, err = engine.GuardServer(db)
		require.NoError(t, err)
	})
}
