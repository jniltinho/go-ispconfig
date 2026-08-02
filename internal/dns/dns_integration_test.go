//go:build integration

// Package dns integration suite: the datalog-to-bind pipeline against a
// real MariaDB — repository write → sys_datalog row → daemon cycle → dns
// module table hook → bind plugin → zone file + named.conf.local +
// rendered_zone cache + delayed bind reload. The OS seam (chown,
// named-checkzone, systemctl) stays mocked: integration here means the
// database and datalog plumbing, not the operating system.
package dns

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// dnsServerConfig builds a server.config INI with the [dns] section
// pointing into the test's temp directory.
func dnsServerConfig(base string) string {
	return fmt.Sprintf(`[dns]
bind_user=bind
bind_group=bind
bind_zonefiles_dir=%s
bind_keyfiles_dir=%s
bind_zonefiles_masterprefix=pri.
bind_zonefiles_slaveprefix=slave/sec.
named_conf_path=%s/named.conf
named_conf_local_path=%s/named.conf.local
disable_bind_log=n
`, base, base, base, base)
}

func newSoa(origin string) *model.DNSSoa {
	return &model.DNSSoa{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Origin: origin,
		NS: "ns1." + strings.TrimSuffix(origin, "."), Mbox: "admin.example.com.",
		Serial: 2026080101, Refresh: 7200, Retry: 540,
		Expire: 604800, Minimum: 3600, TTL: 3600,
		Active: "Y", DNSSECInitialized: "N", DNSSECWanted: "N",
		DNSSECAlgo: "ECDSAP256SHA256",
	}
}

func newRR(zone uint32, name, typ, data string, aux uint32) *model.DNSRr {
	return &model.DNSRr{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Zone: zone, Name: name, Type: typ,
		Data: data, Aux: aux, TTL: 3600, Active: "Y",
	}
}

func TestDatalogToBindPipeline(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "dns")
	database.MariaDBExec(t, name, "CREATE DATABASE dns CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/dns?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	base := t.TempDir()
	require.NoError(t, db.Exec("UPDATE server SET config = ? WHERE server_id = 1", dnsServerConfig(base)).Error)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	soaRepo, err := repository.New[model.DNSSoa](db)
	require.NoError(t, err)
	rrRepo, err := repository.New[model.DNSRr](db)
	require.NoError(t, err)

	exec := &recordingExecutor{}
	services := engine.NewServices(&BindExecutor{Inner: exec, UnitExists: func(string) bool { return true }}, nil)
	RegisterServices(services)
	runner := &fakeRunner{}
	plugin := NewPlugin(db, services, runner, "", 1, nil)
	caa := true
	plugin.caaProbed = &caa

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, services, nil)
	require.NoError(t, err)

	soa := newSoa("example.com.")
	zoneFile := base + "/pri.example.com"
	namedConf := base + "/named.conf.local"

	t.Run("recordless zone is skipped", func(t *testing.T) {
		require.NoError(t, soaRepo.Insert(ctx, admin, soa))
		require.NoError(t, daemon.RunCycle(ctx))
		assert.NoFileExists(t, zoneFile)
		assert.Empty(t, exec.runs, "no reload for a recordless zone (PHP parity)")
	})

	t.Run("rr insert regenerates the whole zone", func(t *testing.T) {
		require.NoError(t, rrRepo.Insert(ctx, admin, newRR(soa.ID, "", "NS", "ns1.example.com.", 0)))
		require.NoError(t, rrRepo.Insert(ctx, admin, newRR(soa.ID, "", "A", "192.0.2.1", 0)))
		require.NoError(t, daemon.RunCycle(ctx))

		content, err := os.ReadFile(zoneFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "2026080101       ; serial")
		assert.Contains(t, string(content), "@ 3600      NS         ns1.example.com.")
		assert.Contains(t, string(content), "@ 3600      A          192.0.2.1")

		var rendered string
		require.NoError(t, db.Table("dns_soa").Where("id = ?", soa.ID).
			Pluck("rendered_zone", &rendered).Error)
		assert.Equal(t, string(content), rendered, "rendered_zone equals the file bytes")

		named, err := os.ReadFile(namedConf)
		require.NoError(t, err)
		assert.Contains(t, string(named), `zone "example.com" {`)
		assert.Contains(t, string(named), `file "`+zoneFile+`";`)

		assert.True(t, runner.has("named-checkzone", "example.com.", zoneFile))
		assert.True(t, runner.has("chown", "bind:bind", zoneFile))
		assert.Equal(t, [][2]string{{"bind9", "reload"}}, exec.runs, "exactly one delayed reload per batch")
		exec.runs = nil
	})

	t.Run("rr update rewrites the zone file", func(t *testing.T) {
		var rec model.DNSRr
		require.NoError(t, db.Where("zone = ? AND type = 'A'", soa.ID).First(&rec).Error)
		require.NoError(t, rrRepo.Get(ctx, admin, rec.ID, &rec))
		rec.Data = "192.0.2.99"
		require.NoError(t, rrRepo.Update(ctx, admin, &rec))
		require.NoError(t, daemon.RunCycle(ctx))

		content, err := os.ReadFile(zoneFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "192.0.2.99")
		assert.NotContains(t, string(content), "192.0.2.1\n")
		exec.runs = nil
	})

	t.Run("invalid zone is quarantined and datalog row records the error", func(t *testing.T) {
		runner.failCmd = "named-checkzone"
		runner.failOut = "dns_rr_load: bad owner name"
		defer func() { runner.failCmd = "" }()

		var rec model.DNSRr
		require.NoError(t, db.Where("zone = ? AND type = 'A'", soa.ID).First(&rec).Error)
		require.NoError(t, rrRepo.Get(ctx, admin, rec.ID, &rec))
		rec.Data = "192.0.2.100"
		require.NoError(t, rrRepo.Update(ctx, admin, &rec))
		require.NoError(t, daemon.RunCycle(ctx))

		content, err := os.ReadFile(zoneFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "192.0.2.99", "previous zone restored")
		quarantined, err := os.ReadFile(zoneFile + ".err")
		require.NoError(t, err)
		assert.Contains(t, string(quarantined), "192.0.2.100", "bad render quarantined")

		var dlRow model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_rr'").Order("datalog_id DESC").First(&dlRow).Error)
		assert.Equal(t, "error", dlRow.Status)
		assert.Contains(t, dlRow.Error, "bad owner name")
		exec.runs = nil
	})

	t.Run("rr events without a parent SOA are no-ops", func(t *testing.T) {
		before := readFile(t, zoneFile)
		orphan := newRR(99999, "", "A", "192.0.2.7", 0)
		require.NoError(t, rrRepo.Insert(ctx, admin, orphan))
		require.NoError(t, daemon.RunCycle(ctx))
		require.NoError(t, rrRepo.Delete(ctx, admin, orphan.ID))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.Equal(t, before, readFile(t, zoneFile), "no zone touched")
		assert.NoFileExists(t, base+"/pri.")
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
