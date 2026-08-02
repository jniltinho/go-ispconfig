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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// dnsToolsFake emulates dnssec-keygen and dnssec-signzone side effects
// (key files, dsset, .signed) for the pipeline test.
func dnsToolsFake(t *testing.T, keyDir string) func(name string, args ...string) ([]byte, error) {
	t.Helper()
	keygen := fakeKeygen(t, keyDir)
	return func(name string, args ...string) ([]byte, error) {
		switch name {
		case "dnssec-keygen":
			return keygen(name, args...)
		case "dnssec-signzone":
			var domain string
			for i, a := range args {
				if a == "-o" {
					domain = args[i+1]
				}
			}
			zonefile := args[len(args)-1]
			require.NoError(t, os.WriteFile(filepath.Join(keyDir, "dsset-"+domain+"."),
				[]byte(domain+". IN DS 12345 13 2 ABCDEF\n"), 0o644))
			require.NoError(t, os.WriteFile(zonefile+".signed", []byte("signed zone\n"), 0o644))
		}
		return nil, nil
	}
}

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
	runner := &scriptRunner{handler: dnsToolsFake(t, base)}
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
		exec.runs = nil
	})

	t.Run("secondary zone reaches named.conf and creates the slave dir", func(t *testing.T) {
		slaveRepo, err := repository.New[model.DNSSlave](db)
		require.NoError(t, err)
		slave := &model.DNSSlave{
			SysUserID: 1, SysGroupID: 1,
			SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Origin: "customer.net.",
			NS: "192.168.10.20", Active: "Y", Xfer: "",
		}
		require.NoError(t, slaveRepo.Insert(ctx, admin, slave))
		require.NoError(t, daemon.RunCycle(ctx))

		named := readFile(t, namedConf)
		assert.Contains(t, named, `zone "customer.net" {`)
		assert.Contains(t, named, "type slave;")
		assert.Contains(t, named, "masters {192.168.10.20;};")
		assert.Contains(t, named, "allow-transfer {none;};")

		info, err := os.Stat(base + "/slave")
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o770), info.Mode().Perm())
		assert.Equal(t, [][2]string{{"bind9", "reload"}}, exec.runs)
		exec.runs = nil

		// Delete: named.conf entry and transferred file are removed.
		require.NoError(t, os.WriteFile(base+"/slave/sec.customer.net", []byte("transferred"), 0o644))
		require.NoError(t, slaveRepo.Delete(ctx, admin, slave.ID))
		require.NoError(t, daemon.RunCycle(ctx))
		assert.NotContains(t, readFile(t, namedConf), "customer.net")
		assert.NoFileExists(t, base+"/slave/sec.customer.net")
		exec.runs = nil
	})

	t.Run("enabling dnssec generates keys, signs and publishes info", func(t *testing.T) {
		var z model.DNSSoa
		require.NoError(t, soaRepo.Get(ctx, admin, soa.ID, &z))
		z.DNSSECWanted = "Y"
		require.NoError(t, soaRepo.Update(ctx, admin, &z))
		require.NoError(t, daemon.RunCycle(ctx))

		keys, _ := filepath.Glob(base + "/Kexample.com.+013+*.key")
		assert.Len(t, keys, 2, "ZSK+KSK generated")
		assert.FileExists(t, zoneFile+".signed")
		assert.Contains(t, readFile(t, zoneFile), "$INCLUDE "+keys[0])

		require.NoError(t, db.Where("id = ?", soa.ID).First(&z).Error)
		assert.Equal(t, "Y", z.DNSSECInitialized)
		assert.Greater(t, z.DNSSECLastSigned, int64(0))
		assert.True(t, strings.HasPrefix(z.DNSSECInfo, "DS-Records:\n"), "dnssec_info starts with the DS block")
		assert.Contains(t, z.DNSSECInfo, "DNSKEY-Records:\n")

		assert.Contains(t, readFile(t, namedConf), zoneFile+".signed", "named.conf points at the signed file")
		exec.runs = nil
	})

	t.Run("dns_resign job re-signs only stale zones", func(t *testing.T) {
		sched := engine.NewScheduler(db, nil)
		require.NoError(t, plugin.RegisterResign(sched))

		// Fresh signature: nothing to do.
		runner.calls = nil
		require.NoError(t, sched.RunJob(ctx, "dns_resign"))
		assert.False(t, runner.has("dnssec-signzone"), "fresh signatures untouched")

		// Stale signature (6 days > 5-day threshold): re-sign + one reload.
		stale := time.Now().Add(-6 * 24 * time.Hour).Unix()
		require.NoError(t, db.Exec("UPDATE dns_soa SET dnssec_last_signed = ? WHERE id = ?", stale, soa.ID).Error)
		runner.calls = nil
		exec.runs = nil
		require.NoError(t, sched.RunJob(ctx, "dns_resign"))
		assert.True(t, runner.has("dnssec-signzone"))
		services.ProcessDelayedActions(ctx)
		assert.Equal(t, [][2]string{{"bind9", "reload"}}, exec.runs)

		var z model.DNSSoa
		require.NoError(t, db.Where("id = ?", soa.ID).First(&z).Error)
		assert.Greater(t, z.DNSSECLastSigned, stale, "dnssec_last_signed refreshed")

		jobs := sched.Jobs(ctx)
		require.Len(t, jobs, 1)
		assert.Equal(t, "dns_resign", jobs[0].Name)
		assert.Equal(t, "ok", jobs[0].Status, "job bookkeeping persisted")
		exec.runs = nil
	})

	t.Run("disabling dnssec removes the signed file", func(t *testing.T) {
		var z model.DNSSoa
		require.NoError(t, soaRepo.Get(ctx, admin, soa.ID, &z))
		z.DNSSECWanted = "N"
		require.NoError(t, soaRepo.Update(ctx, admin, &z))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.NoFileExists(t, zoneFile+".signed")
		named := readFile(t, namedConf)
		assert.Contains(t, named, `file "`+zoneFile+`";`, "named.conf back at the unsigned file")
		keys, _ := filepath.Glob(base + "/Kexample.com.*")
		assert.NotEmpty(t, keys, "keys stay until zone delete")
		exec.runs = nil
	})

	t.Run("zone delete removes file, dnssec materials and named.conf entry", func(t *testing.T) {
		require.NoError(t, soaRepo.Delete(ctx, admin, soa.ID))
		require.NoError(t, daemon.RunCycle(ctx))
		assert.NoFileExists(t, zoneFile)
		assert.NoFileExists(t, zoneFile+".err")
		assert.NotContains(t, readFile(t, namedConf), "example.com")
		leftovers, _ := filepath.Glob(base + "/Kexample.com.*")
		assert.Empty(t, leftovers, "key files removed on zone delete")
		assert.NoFileExists(t, base+"/dsset-example.com.")
		assert.Equal(t, [][2]string{{"bind9", "reload"}}, exec.runs)
		exec.runs = nil
	})

}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
