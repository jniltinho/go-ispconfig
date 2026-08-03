//go:build integration

// End-to-end pipeline (task 7.1): repository write → sys_datalog row →
// daemon cycle → dns module hook → powerdns plugin → rows in the powerdns
// schema, with pdns_control/pdnsutil stubbed. Both databases live in the
// same MariaDB container, like a real single-host install.
package powerdns

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/dns"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

func TestDatalogToPowerDNSPipeline(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "pdnse2e")
	database.MariaDBExec(t, container, "CREATE DATABASE panel CHARACTER SET utf8mb4")
	database.MariaDBExec(t, container, "CREATE DATABASE powerdns CHARACTER SET utf8mb4")

	panelDB, err := database.Open(dsnPrefix + "/panel?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(panelDB)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(panelDB, "server1.test", "test-password-123")
	require.NoError(t, err)

	pdnsDB, err := database.Open(dsnPrefix + "/powerdns?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	require.NoError(t, ApplySchema(pdnsDB))

	runner := &recordingRunner{}
	services := engine.NewServices(&noopExecutor{}, nil)
	RegisterServices(services)
	plugin := NewPlugin(panelDB, pdnsDB, services, runner, 1, nil)
	// Skip binary discovery: pretend a supported PowerDNS 4 is installed.
	plugin.SetToolsForTest("pdns_control", "pdnsutil", "4.8.0")

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{dns.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(panelDB, reg, services, nil, 0)
	require.NoError(t, err)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	soaRepo, err := repository.New[model.DNSSoa](panelDB)
	require.NoError(t, err)
	rrRepo, err := repository.New[model.DNSRr](panelDB)
	require.NoError(t, err)

	soa := &model.DNSSoa{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Origin: "example.com.", NS: "ns1.example.com.",
		Mbox: "hostmaster.example.com.", Serial: 2026080101,
		Refresh: 7200, Retry: 540, Expire: 604800, Minimum: 3600, TTL: 3600,
		Active: "Y", DNSSECWanted: "N", DNSSECInitialized: "N",
	}
	require.NoError(t, soaRepo.Insert(ctx, admin, soa))
	require.NoError(t, rrRepo.Insert(ctx, admin, &model.DNSRr{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Zone: soa.ID, Name: "www", Type: "A", Data: "192.0.2.1",
		TTL: 3600, Active: "Y",
	}))
	require.NoError(t, daemon.RunCycle(ctx))

	var dom Domain
	require.NoError(t, pdnsDB.Where("name = ?", "example.com").Take(&dom).Error)
	assert.Equal(t, "MASTER", dom.Type)
	assert.Equal(t, int(soa.ID), dom.ISPConfigID)

	var records []Record
	require.NoError(t, pdnsDB.Where("domain_id = ?", dom.ID).Order("type").Find(&records).Error)
	require.Len(t, records, 2)
	assert.Equal(t, "www.example.com", *records[0].Name)
	assert.Equal(t, "A", *records[0].Type)
	assert.Equal(t, "192.0.2.1", *records[0].Content)
	assert.Equal(t, "SOA", *records[1].Type)
	assert.Contains(t, *records[1].Content, "ns1.example.com hostmaster.example.com")

	var calls []string
	for _, c := range runner.calls {
		calls = append(calls, strings.Join(c, " "))
	}
	joined := strings.Join(calls, "|")
	assert.Contains(t, joined, "pdns_control rediscover")
	assert.Contains(t, joined, "pdns_control notify example.com")
	assert.Contains(t, joined, "pdnsutil rectify-zone example.com")

	// Deleting the zone drops the domain and its records.
	require.NoError(t, soaRepo.Delete(ctx, admin, soa.ID))
	require.NoError(t, daemon.RunCycle(ctx))
	var domains int64
	require.NoError(t, pdnsDB.Model(&Domain{}).Count(&domains).Error)
	assert.Zero(t, domains)
	var left int64
	require.NoError(t, pdnsDB.Model(&Record{}).Count(&left).Error)
	assert.Zero(t, left)
}

// noopExecutor accepts every delayed service action (no systemctl here).
type noopExecutor struct{}

func (noopExecutor) Run(context.Context, string, string) error { return nil }
