//go:build integration

package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// setupDB starts a migrated MariaDB (admin seed included via the dump).
func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "clients")
	database.MariaDBExec(t, container, "CREATE DATABASE clients CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/clients?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	created, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, created)
	// Seed the local server row (web+dns roles) and the admin password;
	// the limit-hook API tests create real zones/sites against it.
	_, err = database.Seed(db, "panel.test", "seed-admin-pw")
	require.NoError(t, err)
	return db
}

// insertClient writes a minimal valid client row (raw SQL so enum
// defaults apply) and returns it loaded.
func insertClient(t *testing.T, db *gorm.DB, username string, limitClient int32, parent uint32) *model.Client {
	t.Helper()
	require.NoError(t, db.Exec(
		"INSERT INTO client (username, contact_name, email, language, sys_userid, sys_groupid, sys_perm_user, sys_perm_group, limit_client, parent_client_id) "+
			"VALUES (?, ?, ?, 'en', 1, 1, 'riud', 'riud', ?, ?)",
		username, username+" Contact", username+"@example.com", limitClient, parent).Error)
	var c model.Client
	require.NoError(t, db.Where("username = ?", username).Take(&c).Error)
	return &c
}

func TestProvisionerLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	hash, err := auth.HashPassword("s3cret-pw!X")
	require.NoError(t, err)

	// --- create: reseller then a client under it ---
	reseller := insertClient(t, db, "resell1", -1, 0)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ProvisionIdentity(ctx, tx, reseller, hash, "admin")
	}))

	var rGroup model.SysGroup
	require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&rGroup).Error)
	require.Equal(t, "resell1", rGroup.Name)
	var rUser model.SysUser
	require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&rUser).Error)
	require.Equal(t, "resell1", rUser.Username)
	require.Equal(t, hash, rUser.Passwort)
	require.Equal(t, rGroup.GroupID, rUser.DefaultGroup)
	require.Contains(t, rUser.Modules, "client", "resellers must carry the client module")
	require.Equal(t, "dashboard", rUser.Startmodule)
	require.EqualValues(t, 1, rUser.Active)

	child := insertClient(t, db, "acme", 0, reseller.ClientID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ProvisionIdentity(ctx, tx, child, hash, "admin")
	}))
	var cGroup model.SysGroup
	require.NoError(t, db.Where("client_id = ?", child.ClientID).Take(&cGroup).Error)
	var cUser model.SysUser
	require.NoError(t, db.Where("client_id = ?", child.ClientID).Take(&cUser).Error)
	require.NotContains(t, cUser.Modules, "client", "plain clients get no client module")

	// Parent reseller gained the child group; child row re-owned by the
	// reseller identity (design D3.5).
	require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&rUser).Error)
	require.Contains(t, splitCSV(rUser.Groups), itoa(cGroup.GroupID))
	var childRow model.Client
	require.NoError(t, db.Take(&childRow, child.ClientID).Error)
	require.Equal(t, rUser.UserID, childRow.SysUserID)
	require.Equal(t, rGroup.GroupID, childRow.SysGroupID)

	// --- update: rename, password, lock, promote to reseller ---
	newHash, err := auth.HashPassword("new-pw-Y!7")
	require.NoError(t, err)
	old := childRow
	updated := childRow
	updated.Username = "acme2"
	updated.Locked = "y"
	updated.LimitClient = 3
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SyncIdentity(ctx, tx, &old, &updated, newHash)
	}))
	require.NoError(t, db.Where("client_id = ?", child.ClientID).Take(&cUser).Error)
	require.Equal(t, "acme2", cUser.Username)
	require.Equal(t, newHash, cUser.Passwort)
	require.NotNil(t, cUser.LastPasswordChange)
	require.EqualValues(t, 0, cUser.Active, "locked client login deactivated")
	require.Contains(t, cUser.Modules, "client", "promotion to reseller adds the module")
	require.NoError(t, db.Where("client_id = ?", child.ClientID).Take(&cGroup).Error)
	require.Equal(t, "acme2", cGroup.Name)

	// Unlock + demote again.
	old2 := updated
	updated2 := updated
	updated2.Locked = "n"
	updated2.LimitClient = 0
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SyncIdentity(ctx, tx, &old2, &updated2, "")
	}))
	require.NoError(t, db.Where("client_id = ?", child.ClientID).Take(&cUser).Error)
	require.EqualValues(t, 1, cUser.Active)
	require.NotContains(t, cUser.Modules, "client")
	require.Equal(t, newHash, cUser.Passwort, "no password change without a new hash")

	// --- parent reassignment to admin (0) detaches the reseller ---
	old3 := updated2
	updated3 := updated2
	updated3.ParentClientID = 0
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SyncIdentity(ctx, tx, &old3, &updated3, "")
	}))
	require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&rUser).Error)
	require.NotContains(t, splitCSV(rUser.Groups), itoa(cGroup.GroupID))
	require.NoError(t, db.Take(&childRow, child.ClientID).Error)
	require.Equal(t, uint32(1), childRow.SysUserID, "admin-owned after reparent to 0")
	require.Equal(t, uint32(1), childRow.SysGroupID)

	// --- delete: identity rows gone, reseller CSV untouched-by-now ---
	// Re-attach first so the delete path exercises the detach.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return attachGroupToParent(ctx, tx, reseller.ClientID, cGroup.GroupID)
	}))
	require.NoError(t, db.Exec("INSERT INTO client_template_assigned (client_id, client_template_id) VALUES (?, 1)", child.ClientID).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := DeprovisionIdentity(ctx, tx, &updated3, "admin"); err != nil {
			return err
		}
		return tx.Delete(&model.Client{}, child.ClientID).Error
	}))
	var n int64
	require.NoError(t, db.Model(&model.SysUser{}).Where("client_id = ?", child.ClientID).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Model(&model.SysGroup{}).Where("client_id = ?", child.ClientID).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Model(&model.ClientTemplateAssigned{}).Where("client_id = ?", child.ClientID).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&rUser).Error)
	require.NotContains(t, splitCSV(rUser.Groups), itoa(cGroup.GroupID), "deleted group detached from the reseller")

	// UsernameTaken sees both tables.
	taken, err := UsernameTaken(ctx, db, "resell1", 0)
	require.NoError(t, err)
	require.True(t, taken)
	taken, err = UsernameTaken(ctx, db, "resell1", reseller.ClientID)
	require.NoError(t, err)
	require.False(t, taken, "a client's own rows do not collide with itself")
	taken, err = UsernameTaken(ctx, db, "ghost", 0)
	require.NoError(t, err)
	require.False(t, taken)
}

// itoa is a tiny uint32 to string helper for assertions.
func itoa(v uint32) string {
	return splitCSV(addGroupToCSV("", v))[0]
}
