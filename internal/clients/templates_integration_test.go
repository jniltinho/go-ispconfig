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

	// Legacy slash-list migration merges on top and clears the column.
	require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", cid).
		Update("template_additional", "0/x/"+itoa(uint32(tplB))+"/"+itoa(uint32(tplB))).Error)
	var withLegacy model.Client
	require.NoError(t, db.Take(&withLegacy, cid).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		migrated, err := MigrateLegacyAdditional(ctx, tx, &withLegacy)
		require.True(t, migrated)
		return err
	}))
	require.ElementsMatch(t, []int32{tplA, tplB, tplB}, assignedIDs(t, db, cid))
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
	require.NoError(t, db.Where("template_id = ?", tplB).Delete(&model.ClientTemplate{}).Error)
	tpls, err := AssignedTemplates(ctx, db, cid)
	require.NoError(t, err)
	require.Len(t, tpls, 1)
	require.Equal(t, "A", tpls[0].TemplateName)
}
