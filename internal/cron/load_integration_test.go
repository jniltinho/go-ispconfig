//go:build integration

package cron

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// TestLoadActiveJobsFromMariaDB covers task 3.2: daemon-start load of
// active cron rows for this server into the client-job runner.
func TestLoadActiveJobsFromMariaDB(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "cronload")
	database.MariaDBExec(t, container, "CREATE DATABASE cronload CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/cronload?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	const thisServer uint32 = 1
	const otherServer uint32 = 2

	parent := model.WebDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: thisServer, IPAddress: "*", Domain: "load.example.com", Type: "vhost",
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

	mk := func(serverID uint32, active, runMin string) model.Cron {
		return model.Cron{
			SysUserID: 1, SysGroupID: 1,
			SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
			ServerID: serverID, ParentDomainID: parent.DomainID,
			Type: model.CronTypeURL, Command: "https://load.example.com/cron.php",
			RunMin: runMin, RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
			Log: "n", Active: active,
		}
	}
	activeLocal := mk(thisServer, "y", "*/5")
	inactiveLocal := mk(thisServer, "n", "0")
	activeOther := mk(otherServer, "y", "15")
	require.NoError(t, db.Create(&activeLocal).Error)
	require.NoError(t, db.Create(&inactiveLocal).Error)
	require.NoError(t, db.Create(&activeOther).Error)

	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)

	loadedIDs := map[uint32]bool{}
	n, err := LoadActiveJobs(context.Background(), db, thisServer, runner, func(job model.Cron) JobFunc {
		loadedIDs[job.ID] = true
		return func(context.Context) {}
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only active rows on this server")
	assert.True(t, runner.Has(activeLocal.ID), "active local job registered")
	assert.False(t, runner.Has(inactiveLocal.ID), "inactive job not registered")
	assert.False(t, runner.Has(activeOther.ID), "other server job not registered")
	assert.True(t, loadedIDs[activeLocal.ID])
	assert.Equal(t, "*/5 * * * *", ComposeExpression(
		activeLocal.RunMin, activeLocal.RunHour, activeLocal.RunMday,
		activeLocal.RunMonth, activeLocal.RunWday,
	))

	// Second load replaces / re-arms the same id without error (self-healing).
	n2, err := LoadActiveJobs(context.Background(), db, thisServer, runner, func(job model.Cron) JobFunc {
		return func(context.Context) {}
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n2)
	assert.True(t, runner.Has(activeLocal.ID))
	assert.Equal(t, 1, runner.Len(), fmt.Sprintf("still one entry after reload, got %d", runner.Len()))
}
