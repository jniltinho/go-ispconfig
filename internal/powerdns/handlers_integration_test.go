//go:build integration

package powerdns

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
)

func setupPdnsDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "pdnsh")
	database.MariaDBExec(t, container, "CREATE DATABASE powerdns")
	db, err := database.Open(dsnPrefix + "/powerdns?parseTime=true&charset=utf8mb4&loc=Local")
	require.NoError(t, err)
	require.NoError(t, ApplySchema(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func testPlugin(pdns *gorm.DB) *Plugin {
	return NewPlugin(nil, pdns, nil, &recordingRunner{}, 1, nil)
}

func soaNew(id int, origin, active string) map[string]any {
	return map[string]any{
		"id":        float64(id),
		"server_id": float64(1),
		"origin":    origin,
		"ns":        "ns1." + stripTrailingDot(origin) + ".",
		"mbox":      "hostmaster." + stripTrailingDot(origin) + ".",
		"serial":    float64(2026080201),
		"refresh":   float64(7200),
		"retry":     float64(3600),
		"expire":    float64(1209600),
		"minimum":   float64(86400),
		"ttl":       float64(86400),
		"active":    active,
	}
}

// TestEventToSQLMasterRRSlave covers master insert/update/delete, RR mapping,
// and slave lifecycle against real MariaDB (task 2.6).
func TestEventToSQLMasterRRSlave(t *testing.T) {
	pdns := setupPdnsDB(t)
	p := testPlugin(pdns)
	ctx := context.Background()

	// --- Master insert ---
	require.NoError(t, p.handleSOAInsert(ctx, engine.Data{New: soaNew(10, "example.com.", "Y")}))

	var dom Domain
	require.NoError(t, pdns.Where("ispconfig_id = ? AND type = ?", 10, "MASTER").Take(&dom).Error)
	assert.Equal(t, "example.com", dom.Name)
	require.NotNil(t, dom.NotifiedSerial)
	assert.Equal(t, 2026080201, *dom.NotifiedSerial)

	var soaRec Record
	require.NoError(t, pdns.Where("ispconfig_id = ? AND type = ?", 10, "SOA").Take(&soaRec).Error)
	require.NotNil(t, soaRec.Content)
	assert.Contains(t, *soaRec.Content, "ns1.example.com hostmaster.example.com 2026080201")
	require.NotNil(t, soaRec.Name)
	assert.Equal(t, "example.com", *soaRec.Name)

	// Inactive insert is no-op
	require.NoError(t, p.handleSOAInsert(ctx, engine.Data{New: soaNew(11, "skip.com.", "N")}))
	var n int64
	require.NoError(t, pdns.Model(&Domain{}).Where("ispconfig_id = ?", 11).Count(&n).Error)
	assert.Equal(t, int64(0), n)

	// --- RR insert: relative A ---
	rrA := engine.Data{New: map[string]any{
		"id": float64(1001), "server_id": float64(1), "zone": float64(10),
		"name": "www", "type": "A", "data": "1.2.3.4", "aux": float64(0),
		"ttl": float64(3600), "active": "Y", "origin": "example.com.",
	}}
	require.NoError(t, p.handleRRInsert(ctx, rrA))
	var aRec Record
	require.NoError(t, pdns.Where("ispconfig_id = ?", 1001).Take(&aRec).Error)
	require.NotNil(t, aRec.Name)
	assert.Equal(t, "www.example.com", *aRec.Name)
	require.NotNil(t, aRec.Content)
	assert.Equal(t, "1.2.3.4", *aRec.Content)

	// Apex MX relative
	rrMX := engine.Data{New: map[string]any{
		"id": float64(1002), "server_id": float64(1), "zone": float64(10),
		"name": "", "type": "MX", "data": "mail", "aux": float64(10),
		"ttl": float64(3600), "active": "Y", "origin": "example.com.",
	}}
	require.NoError(t, p.handleRRInsert(ctx, rrMX))
	var mxRec Record
	require.NoError(t, pdns.Where("ispconfig_id = ?", 1002).Take(&mxRec).Error)
	require.NotNil(t, mxRec.Name)
	assert.Equal(t, "example.com", *mxRec.Name)
	require.NotNil(t, mxRec.Content)
	assert.Equal(t, "mail.example.com", *mxRec.Content)
	require.NotNil(t, mxRec.Prio)
	assert.Equal(t, 10, *mxRec.Prio)

	// Duplicate ispconfig_id skipped
	require.NoError(t, p.handleRRInsert(ctx, rrA))
	require.NoError(t, pdns.Model(&Record{}).Where("ispconfig_id = ?", 1001).Count(&n).Error)
	assert.Equal(t, int64(1), n)

	// RR before SOA skipped
	require.NoError(t, p.handleRRInsert(ctx, engine.Data{New: map[string]any{
		"id": float64(2000), "zone": float64(99), "name": "x", "type": "A",
		"data": "9.9.9.9", "aux": float64(0), "ttl": float64(60), "active": "Y",
		"origin": "missing.com.", "server_id": float64(1),
	}}))
	require.NoError(t, pdns.Model(&Record{}).Where("ispconfig_id = ?", 2000).Count(&n).Error)
	assert.Equal(t, int64(0), n)

	// Deactivate RR → delete (SOA untouched)
	require.NoError(t, p.handleRRUpdate(ctx, engine.Data{
		Old: map[string]any{"id": float64(1001), "active": "Y"},
		New: map[string]any{"id": float64(1001), "active": "N", "zone": float64(10)},
	}))
	require.NoError(t, pdns.Model(&Record{}).Where("ispconfig_id = ?", 1001).Count(&n).Error)
	assert.Equal(t, int64(0), n)
	require.NoError(t, pdns.Model(&Record{}).Where("type = ?", "SOA").Count(&n).Error)
	assert.Equal(t, int64(1), n)

	// RR delete never removes SOA
	require.NoError(t, p.handleRRDelete(ctx, engine.Data{
		Old: map[string]any{"id": float64(10)}, // same id as SOA ispconfig_id
	}))
	require.NoError(t, pdns.Model(&Record{}).Where("type = ? AND ispconfig_id = ?", "SOA", 10).Count(&n).Error)
	assert.Equal(t, int64(1), n)

	// Deactivate zone → purge
	require.NoError(t, p.handleSOAUpdate(ctx, engine.Data{
		Old: soaNew(10, "example.com.", "Y"),
		New: soaNew(10, "example.com.", "N"),
	}))
	require.NoError(t, pdns.Model(&Domain{}).Where("ispconfig_id = ?", 10).Count(&n).Error)
	assert.Equal(t, int64(0), n)
	require.NoError(t, pdns.Model(&Record{}).Count(&n).Error)
	assert.Equal(t, int64(0), n)

	// --- Slave ---
	require.NoError(t, p.handleSlaveInsert(ctx, engine.Data{New: map[string]any{
		"id": float64(20), "server_id": float64(1), "origin": "slave.example.",
		"ns": "1.2.3.4", "active": "Y",
	}}))
	var slave Domain
	require.NoError(t, pdns.Where("ispconfig_id = ? AND type = ?", 20, "SLAVE").Take(&slave).Error)
	assert.Equal(t, "slave.example", slave.Name)
	require.NotNil(t, slave.Master)
	assert.Equal(t, "1.2.3.4", *slave.Master)

	// Inject AXFR cache record (ispconfig_id=0) and update slave → purged
	domainID := slave.ID
	cacheName := "slave.example"
	cacheType := "A"
	cacheContent := "9.9.9.9"
	require.NoError(t, pdns.Create(&Record{
		DomainID:    &domainID,
		Name:        &cacheName,
		Type:        &cacheType,
		Content:     &cacheContent,
		ISPConfigID: 0,
	}).Error)
	require.NoError(t, p.handleSlaveUpdate(ctx, engine.Data{
		Old: map[string]any{"id": float64(20), "active": "Y", "origin": "slave.example.", "ns": "1.2.3.4"},
		New: map[string]any{"id": float64(20), "active": "Y", "origin": "slave.example.", "ns": "5.6.7.8"},
	}))
	require.NoError(t, pdns.Model(&Record{}).Where("domain_id = ? AND ispconfig_id = 0", domainID).Count(&n).Error)
	assert.Equal(t, int64(0), n)
	require.NoError(t, pdns.Where("ispconfig_id = ? AND type = ?", 20, "SLAVE").Take(&slave).Error)
	require.NotNil(t, slave.Master)
	assert.Equal(t, "5.6.7.8", *slave.Master)

	require.NoError(t, p.handleSlaveDelete(ctx, engine.Data{
		Old: map[string]any{"id": float64(20), "active": "Y"},
	}))
	require.NoError(t, pdns.Model(&Domain{}).Where("ispconfig_id = ?", 20).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

// TestControlCommandWiring covers task 3.2: rediscover/notify/rectify fire
// after an active SOA insert, retrieve after an active slave insert; a
// missing binary stays non-fatal on the same paths.
func TestControlCommandWiring(t *testing.T) {
	pdns := setupPdnsDB(t)
	ctx := context.Background()

	r := &pathRunner{bins: map[string]string{"pdns_control": "4.8.0", "pdnsutil": "ok"}}
	p := NewPlugin(nil, pdns, nil, r, 1, nil)
	p.SetToolsForTest("pdns_control", "pdnsutil", "4.8.0")

	require.NoError(t, p.handleSOAInsert(ctx, engine.Data{New: soaNew(30, "wired.com.", "Y")}))
	joined := strings.Join(r.log, "\n")
	assert.Contains(t, joined, "pdns_control rediscover")
	assert.Contains(t, joined, "pdns_control notify wired.com")
	assert.Contains(t, joined, "pdnsutil rectify-zone wired.com")

	r.log = nil
	require.NoError(t, p.handleSlaveInsert(ctx, engine.Data{New: map[string]any{
		"id": float64(31), "server_id": float64(1), "origin": "wired-slave.com.",
		"ns": "1.2.3.4", "active": "Y",
	}}))
	assert.Contains(t, strings.Join(r.log, "\n"), "pdns_control retrieve wired-slave.com")

	// Missing binaries: same events succeed without any command.
	rEmpty := &pathRunner{bins: map[string]string{}}
	pMissing := NewPlugin(nil, pdns, nil, rEmpty, 1, nil)
	pMissing.SetToolsForTest("", "", "")
	require.NoError(t, pMissing.handleSOAInsert(ctx, engine.Data{New: soaNew(32, "nobin.com.", "Y")}))
	assert.Empty(t, rEmpty.log)
}

// TestDelayedRestartDedup covers task 3.4: SOA and slave mutations in one run
// collapse into a single powerdns restart; pure RR mutations queue nothing.
func TestDelayedRestartDedup(t *testing.T) {
	pdns := setupPdnsDB(t)
	ctx := context.Background()

	exec := &recordingExecutor{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)
	p := NewPlugin(nil, pdns, services, &recordingRunner{}, 1, nil)
	p.SetToolsForTest("", "", "")

	// Several restart-queueing events in one daemon run.
	require.NoError(t, p.handleSOAInsert(ctx, engine.Data{New: soaNew(40, "dedup-a.com.", "Y")}))
	require.NoError(t, p.handleSOAInsert(ctx, engine.Data{New: soaNew(41, "dedup-b.com.", "Y")}))
	require.NoError(t, p.handleSlaveInsert(ctx, engine.Data{New: map[string]any{
		"id": float64(42), "server_id": float64(1), "origin": "dedup-slave.com.",
		"ns": "1.2.3.4", "active": "Y",
	}}))

	services.ProcessDelayedActions(ctx)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, [2]string{ServiceName, engine.ActionRestart}, exec.calls[0])

	// Pure RR mutation: no restart queued.
	exec.calls = nil
	require.NoError(t, p.handleRRInsert(ctx, engine.Data{New: map[string]any{
		"id": float64(4001), "server_id": float64(1), "zone": float64(40),
		"name": "www", "type": "A", "data": "1.2.3.4", "aux": float64(0),
		"ttl": float64(3600), "active": "Y", "origin": "dedup-a.com.",
	}}))
	services.ProcessDelayedActions(ctx)
	assert.Empty(t, exec.calls)
}
