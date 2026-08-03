//go:build integration

package cron

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// TestCronDatalogToRunnerFlow covers task 3.9 without the REST layer:
// insert/update/delete of a cron row + sys_datalog → daemon cycle →
// ClientJobRunner membership; no files under a temp crontab_dir.
// Full HTTP API coverage lands with task 4.x.
func TestCronDatalogToRunnerFlow(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "cronflow")
	database.MariaDBExec(t, container, "CREATE DATABASE cronflow CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/cronflow?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "cronflow.example.com", "smoke-test-pw")
	require.NoError(t, err)

	parent := seedCronParent(t, db)
	crontabDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(crontabDir, "ispc_web1"), []byte("# legacy\n"), 0o644))

	// Cutover first (same order as daemon bootstrap).
	removed, err := RemoveLegacyCrontabs(crontabDir, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"ispc_web1"}, removed)

	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	plugin := NewPlugin(db, 1, runner, nil)
	plugin.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{
			Domain: parent.Domain, DocumentRoot: parent.DocumentRoot,
			SystemUser: parent.SystemUser, SystemGroup: parent.SystemGroup,
		}, true, nil
	}

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nopExec{}, nil), nil)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, daemon.RunCycle(ctx)) // drain seed backlog

	// --- insert active ---
	job := model.Cron{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: 1, ParentDomainID: parent.DomainID,
		Type: model.CronTypeURL, Command: "https://{DOMAIN}/cron.php",
		RunMin: "*/5", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
		Log: "n", Active: "y",
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		return datalog.LogInsert(tx, &job, "admin")
	}))
	require.NotZero(t, job.ID)
	require.NoError(t, daemon.RunCycle(ctx))
	assert.True(t, runner.Has(job.ID), "active insert must register the job")

	// --- update inactive ---
	old := job
	job.Active = "n"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&job).Error; err != nil {
			return err
		}
		return datalog.LogUpdate(tx, &old, &job, "admin")
	}))
	require.NoError(t, daemon.RunCycle(ctx))
	assert.False(t, runner.Has(job.ID), "inactive update must remove the job")

	// --- re-activate then delete ---
	old = job
	job.Active = "y"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&job).Error; err != nil {
			return err
		}
		return datalog.LogUpdate(tx, &old, &job, "admin")
	}))
	require.NoError(t, daemon.RunCycle(ctx))
	assert.True(t, runner.Has(job.ID), "re-activate must register again")

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&job).Error; err != nil {
			return err
		}
		return datalog.LogDelete(tx, &job, "admin")
	}))
	require.NoError(t, daemon.RunCycle(ctx))
	assert.False(t, runner.Has(job.ID), "delete must remove the job")

	// No new files under crontab_dir after the whole flow.
	entries, err := os.ReadDir(crontabDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "plugin must not create files under crontab_dir")
}

type nopExec struct{}

func (nopExec) Run(context.Context, string, string) error { return nil }

func seedCronParent(t *testing.T, db *gorm.DB) model.WebDomain {
	t.Helper()
	parent := model.WebDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: 1, IPAddress: "*", Domain: "flow.example.com", Type: "vhost",
		DocumentRoot: "/var/www/clients/client1/web1",
		SystemUser:   "web1", SystemGroup: "client1",
		CGI: "n", SSI: "n", Suexec: "y", Ruby: "n", Python: "n", Perl: "n",
		SSLLetsencryptExclude: "n", PHPFPMChroot: "n", BackupEncrypt: "n",
		TrafficQuotaLock: "n", EnablePagespeed: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
		PHP: "php-fpm", PHPFPMUseSocket: "y", PM: "dynamic",
		PMMaxChildren: 10, PMStartServers: 2, PMMinSpareServers: 1, PMMaxSpareServers: 5,
		PMProcessIdleTimeout: 10,
		SSL:                  "n", SSLLetsencrypt: "n", RewriteToHTTPS: "n",
		SeoRedirect: "non_www_to_www", Subdomain: "www", Active: "y",
		HTTPPort: 80, HTTPSPort: 443,
	}
	require.NoError(t, db.Create(&parent).Error)
	return parent
}
