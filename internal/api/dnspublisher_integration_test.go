//go:build integration

package api_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/model"
)

// TestDNSPublisher covers the DKIM TXT publication contract (add-mail-
// module task 5.1): upsert into a managed zone with SOA-derived
// ownership + serial bump + datalog, parent-zone matching for
// subdomains, replace-on-upsert, delete with serial bump, and the
// no-zone unpublished result.
func TestDNSPublisher(t *testing.T) {
	db, _, _, _ := newClientsTestEnv(t)
	ctx := context.Background()

	soa := model.DNSSoa{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Origin: "example.com.", NS: "ns1.example.com.",
		Mbox: "hostmaster.example.com.", Serial: 2026080101,
		Refresh: 7200, Retry: 540, Expire: 604800, Minimum: 3600, TTL: 3600,
		Active: "Y", DNSSECWanted: "N", DNSSECInitialized: "N",
	}
	require.NoError(t, db.Create(&soa).Error)

	pub := api.NewDNSPublisher(db)
	name := "default._domainkey.mail.example.com."
	data := "v=DKIM1; t=s; p=AAAA"

	t.Run("upsert publishes into the parent zone", func(t *testing.T) {
		published, err := pub.UpsertTXT(ctx, db, name, data, 3600)
		require.NoError(t, err)
		assert.True(t, published)

		var rr model.DNSRr
		require.NoError(t, db.Where("name = ?", name).Take(&rr).Error)
		assert.Equal(t, "TXT", rr.Type)
		assert.Equal(t, data, rr.Data)
		assert.Equal(t, soa.ID, rr.Zone)
		assert.Equal(t, soa.SysGroupID, rr.SysGroupID, "ownership from the SOA")

		var gotSoa model.DNSSoa
		require.NoError(t, db.Take(&gotSoa, soa.ID).Error)
		assert.Greater(t, gotSoa.Serial, soa.Serial, "SOA serial bumped")

		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = 'dns_rr' AND action = 'i'").Count(&n).Error)
		assert.EqualValues(t, 1, n, "insert journaled")
	})

	t.Run("upsert replaces the previous record", func(t *testing.T) {
		published, err := pub.UpsertTXT(ctx, db, name, "v=DKIM1; t=s; p=BBBB", 3600)
		require.NoError(t, err)
		assert.True(t, published)
		var rows []model.DNSRr
		require.NoError(t, db.Where("name = ?", name).Find(&rows).Error)
		require.Len(t, rows, 1, "old record replaced, not duplicated")
		assert.Contains(t, rows[0].Data, "p=BBBB")
	})

	t.Run("delete withdraws and journals", func(t *testing.T) {
		require.NoError(t, pub.DeleteTXT(ctx, db, name, "v=DKIM1"))
		var n int64
		require.NoError(t, db.Model(&model.DNSRr{}).Where("name = ?", name).Count(&n).Error)
		assert.Zero(t, n)
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = 'dns_rr' AND action = 'd'").Count(&n).Error)
		assert.GreaterOrEqual(t, n, int64(1))
	})

	t.Run("no matching zone is unpublished success", func(t *testing.T) {
		published, err := pub.UpsertTXT(ctx, db, "default._domainkey.nozone.test.", data, 3600)
		require.NoError(t, err)
		assert.False(t, published)
		var n int64
		require.NoError(t, db.Model(&model.DNSRr{}).
			Where("name LIKE ?", "%nozone%").Count(&n).Error)
		assert.Zero(t, n)
	})
}
