//go:build integration

package powerdns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
)

// TestDomainRecordRoundTrip applies embedded powerdns.sql on a throwaway
// MariaDB database and round-trips Domain + Record via GORM.
func TestDomainRecordRoundTrip(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "pdnsrt")
	database.MariaDBExec(t, container, "CREATE DATABASE powerdns")

	db, err := database.Open(dsnPrefix + "/powerdns?parseTime=true&charset=utf8mb4&loc=Local")
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, ApplySchema(db))

	// Schema present.
	for _, table := range []string{"domains", "records", "supermasters", "domainmetadata"} {
		var n int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table).Scan(&n).Error)
		assert.Equalf(t, int64(1), n, "table %s", table)
	}
	for _, col := range []struct{ table, col string }{
		{"domains", "ispconfig_id"},
		{"records", "ispconfig_id"},
	} {
		var n int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
			col.table, col.col).Scan(&n).Error)
		assert.Equalf(t, int64(1), n, "%s.%s", col.table, col.col)
	}

	serial := 2026080201
	dom := Domain{
		Name:           "example.com",
		Type:           "MASTER",
		NotifiedSerial: &serial,
		ISPConfigID:    42,
	}
	require.NoError(t, db.Create(&dom).Error)
	require.NotZero(t, dom.ID)

	var gotDom Domain
	require.NoError(t, db.First(&gotDom, "ispconfig_id = ?", 42).Error)
	assert.Equal(t, "example.com", gotDom.Name)
	assert.Equal(t, "MASTER", gotDom.Type)
	require.NotNil(t, gotDom.NotifiedSerial)
	assert.Equal(t, serial, *gotDom.NotifiedSerial)
	assert.Equal(t, 42, gotDom.ISPConfigID)

	name := "example.com"
	typ := "SOA"
	content := "ns1.example.com hostmaster.example.com 2026080201 7200 3600 1209600 86400"
	ttl := 86400
	prio := 0
	changeDate := int(time.Now().Unix())
	domainID := gotDom.ID
	rec := Record{
		DomainID:    &domainID,
		Name:        &name,
		Type:        &typ,
		Content:     &content,
		TTL:         &ttl,
		Prio:        &prio,
		ChangeDate:  &changeDate,
		ISPConfigID: 42,
	}
	require.NoError(t, db.Create(&rec).Error)
	require.NotZero(t, rec.ID)

	var gotRec Record
	require.NoError(t, db.First(&gotRec, "ispconfig_id = ? AND type = ?", 42, "SOA").Error)
	require.NotNil(t, gotRec.DomainID)
	assert.Equal(t, gotDom.ID, *gotRec.DomainID)
	require.NotNil(t, gotRec.Name)
	assert.Equal(t, "example.com", *gotRec.Name)
	require.NotNil(t, gotRec.Content)
	assert.Contains(t, *gotRec.Content, "ns1.example.com hostmaster.example.com 2026080201")
	require.NotNil(t, gotRec.TTL)
	assert.Equal(t, 86400, *gotRec.TTL)
	require.NotNil(t, gotRec.Prio)
	assert.Equal(t, 0, *gotRec.Prio)

	// A record round-trip.
	aName := "www.example.com"
	aType := "A"
	aContent := "1.2.3.4"
	aTTL := 3600
	aPrio := 0
	aRec := Record{
		DomainID:    &domainID,
		Name:        &aName,
		Type:        &aType,
		Content:     &aContent,
		TTL:         &aTTL,
		Prio:        &aPrio,
		ChangeDate:  &changeDate,
		ISPConfigID: 1001,
	}
	require.NoError(t, db.Create(&aRec).Error)

	var count int64
	require.NoError(t, db.Model(&Record{}).Where("domain_id = ?", domainID).Count(&count).Error)
	assert.Equal(t, int64(2), count)

	// Delete records then domain (plugin delete order).
	require.NoError(t, db.Where("domain_id = ?", domainID).Delete(&Record{}).Error)
	require.NoError(t, db.Delete(&Domain{}, domainID).Error)
	require.NoError(t, db.Model(&Domain{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}
