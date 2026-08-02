//go:build integration

package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// insertTemplate stores a minimal client_template via raw SQL (enum
// defaults apply) and returns its id.
func insertTemplate(t *testing.T, db *gorm.DB, name, typ string, cols map[string]any) uint32 {
	t.Helper()
	set := "sys_userid, sys_groupid, sys_perm_user, sys_perm_group, template_name, template_type"
	vals := []any{1, 1, "riud", "riud", name, typ}
	ph := "?, ?, ?, ?, ?, ?"
	for col, v := range cols {
		set += ", " + col
		ph += ", ?"
		vals = append(vals, v)
	}
	require.NoError(t, db.Exec("INSERT INTO client_template ("+set+") VALUES ("+ph+")", vals...).Error)
	var tpl model.ClientTemplate
	require.NoError(t, db.Where("template_name = ?", name).Take(&tpl).Error)
	return tpl.TemplateID
}

func assignedIDs(t *testing.T, db *gorm.DB, clientID int64) []int32 {
	t.Helper()
	var out []int32
	require.NoError(t, db.Model(&model.ClientTemplateAssigned{}).
		Where("client_id = ?", clientID).Order("assigned_template_id").
		Pluck("client_template_id", &out).Error)
	return out
}

func TestTemplateAssignmentStore(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	tplA := int32(insertTemplate(t, db, "A", "a", nil))
	tplB := int32(insertTemplate(t, db, "B", "a", nil))

	c := insertClient(t, db, "tplclient", 0, 0)
	cid := int64(c.ClientID)

	// Assign as a multiset, including a duplicate.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetAssignedTemplates(ctx, tx, cid, []int32{tplA, tplB, tplA})
	}))
	require.ElementsMatch(t, []int32{tplA, tplA, tplB}, assignedIDs(t, db, cid))

	// Reconcile down to a single A: one A and the B are removed.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetAssignedTemplates(ctx, tx, cid, []int32{tplA})
	}))
	require.Equal(t, []int32{tplA}, assignedIDs(t, db, cid))

	// Legacy slash-list migration reconciles to EXACTLY the list (PHP
	// old-style branch: no merge with existing rows) and clears the column.
	require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", cid).
		Update("template_additional", "0/x/"+itoa(uint32(tplB))+"/"+itoa(uint32(tplB))).Error)
	var withLegacy model.Client
	require.NoError(t, db.Take(&withLegacy, cid).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		migrated, err := MigrateLegacyAdditional(ctx, tx, &withLegacy)
		require.True(t, migrated)
		return err
	}))
	require.ElementsMatch(t, []int32{tplB, tplB}, assignedIDs(t, db, cid))
	var after model.Client
	require.NoError(t, db.Take(&after, cid).Error)
	require.Empty(t, after.TemplateAdditional)

	// No-op migration when the column is already empty.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		migrated, err := MigrateLegacyAdditional(ctx, tx, &after)
		require.False(t, migrated)
		return err
	}))

	// AssignedTemplates resolves rows and skips deleted templates.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetAssignedTemplates(ctx, tx, cid, []int32{tplA, tplB})
	}))
	require.NoError(t, db.Where("template_id = ?", tplB).Delete(&model.ClientTemplate{}).Error)
	tpls, err := AssignedTemplates(ctx, db, cid)
	require.NoError(t, err)
	require.Len(t, tpls, 1)
	require.Equal(t, "A", tpls[0].TemplateName)
}

func TestApplyTemplatesDB(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	master := insertTemplate(t, db, "Master", "m", map[string]any{
		"limit_web_domain": 5, "limit_dns_zone": 10, "limit_ssl": "y",
		"web_php_options": "php-fpm",
	})
	add := insertTemplate(t, db, "Addon", "a", map[string]any{
		"limit_web_domain": 3, "limit_dns_zone": -1,
		"web_php_options": "fast-cgi",
	})

	c := insertClient(t, db, "applyclient", 0, 0)
	require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", c.ClientID).
		Update("template_master", master).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetAssignedTemplates(ctx, tx, int64(c.ClientID), []int32{int32(add)})
	}))

	require.NoError(t, db.Take(c, c.ClientID).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ApplyTemplates(ctx, tx, c)
	}))

	require.Equal(t, int32(8), c.LimitWebDomain, "5 + 3 materialized")
	require.Equal(t, int32(-1), c.LimitDNSZone, "additional -1 promotes")
	require.Equal(t, "y", c.LimitSSL)
	require.Equal(t, "php-fpm,fast-cgi", c.WebPHPOptions)
	require.Equal(t, int32(0), c.LimitClient, "non-reseller keeps limit_client 0")

	// Custom master (0) never re-materializes.
	require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", c.ClientID).
		Updates(map[string]any{"template_master": 0, "limit_web_domain": 99}).Error)
	require.NoError(t, db.Take(c, c.ClientID).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ApplyTemplates(ctx, tx, c)
	}))
	require.Equal(t, int32(99), c.LimitWebDomain, "custom limits untouched")
}
