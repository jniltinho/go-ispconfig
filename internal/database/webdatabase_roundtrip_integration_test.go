//go:build integration

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestWebDatabaseModelsRoundTrip inserts one realistic web_database_user and
// web_database row into a migrated MariaDB schema and reads them back
// (add-database-module task 1.1). Nullable columns (database_quota,
// last_quota_notification, database_user_id, database_ro_user_id) must
// survive both as NULL and as values; enum defaults must land.
func TestWebDatabaseModelsRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "dbrt")
	MariaDBExec(t, container, "CREATE DATABASE dbrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/dbrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	user := model.WebDatabaseUser{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID:           1,
		DatabaseUser:       "c1_app",
		DatabaseUserPrefix: "c1_",
		DatabasePassword:   "*2470C0C06DEE42FD1618BB99005ADCA2EC9D1E19",
	}
	require.NoError(t, db.Create(&user).Error)
	require.NotZero(t, user.DatabaseUserID)

	quota := int32(100)
	wdb := model.WebDatabase{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID:           1,
		ParentDomainID:     7,
		Type:               "mysql",
		DatabaseName:       "c1_app",
		DatabaseNamePrefix: "c1_",
		DatabaseQuota:      &quota,
		QuotaExceeded:      "n",
		DatabaseUserID:     &user.DatabaseUserID,
		DatabaseCharset:    "utf8mb4",
		RemoteAccess:       "y",
		RemoteIps:          "10.0.0.5,10.0.0.6",
		BackupInterval:     "none",
		BackupCopies:       1,
		Active:             "y",
	}
	require.NoError(t, db.Create(&wdb).Error)
	require.NotZero(t, wdb.DatabaseID)

	var got model.WebDatabase
	require.NoError(t, db.Take(&got, wdb.DatabaseID).Error)
	assert.Equal(t, "mysql", got.Type)
	assert.Equal(t, "c1_app", got.DatabaseName)
	assert.Equal(t, "c1_", got.DatabaseNamePrefix)
	require.NotNil(t, got.DatabaseQuota)
	assert.EqualValues(t, 100, *got.DatabaseQuota)
	assert.Nil(t, got.LastQuotaNotification, "date NULL default survives")
	require.NotNil(t, got.DatabaseUserID)
	assert.Equal(t, user.DatabaseUserID, *got.DatabaseUserID)
	assert.Nil(t, got.DatabaseROUserID)
	assert.Equal(t, "y", got.RemoteAccess)
	assert.Equal(t, "10.0.0.5,10.0.0.6", got.RemoteIps)
	assert.Equal(t, "n", got.QuotaExceeded)

	var gotUser model.WebDatabaseUser
	require.NoError(t, db.Take(&gotUser, user.DatabaseUserID).Error)
	assert.Equal(t, "c1_app", gotUser.DatabaseUser)
	assert.Equal(t, "*2470C0C06DEE42FD1618BB99005ADCA2EC9D1E19", gotUser.DatabasePassword)
	assert.Empty(t, gotUser.DatabasePasswordSha2)
}
