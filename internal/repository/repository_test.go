package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"

	"go-ispconfig/internal/model"
)

// dryDB returns a connection-less GORM handle (DryRun) good enough for
// schema parsing and SQL generation; real MySQL behavior is covered by the
// integration suite.
func dryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	return db
}

func TestParseGroupCSV(t *testing.T) {
	require.Equal(t, []uint32{1, 2}, parseGroupCSV("1,2"))
	require.Equal(t, []uint32{5}, parseGroupCSV(" 5 "))
	require.Equal(t, []uint32{3, 7}, parseGroupCSV("3,,x,0,7"))
	require.Nil(t, parseGroupCSV(""))
}

func TestWithPermSQL(t *testing.T) {
	db := dryDB(t)

	t.Run("admin bypasses the filter", func(t *testing.T) {
		admin := &Identity{UserID: 1, Typ: "admin", Groups: []uint32{1}}
		stmt := db.Session(&gorm.Session{DryRun: true}).Model(&model.WebDomain{}).
			Scopes(WithPerm(admin, PermRead)).Find(&[]model.WebDomain{}).Statement
		require.NotContains(t, stmt.SQL.String(), "sys_perm_user")
	})

	t.Run("user filter carries all three riud branches", func(t *testing.T) {
		user := &Identity{UserID: 3, Typ: "user", Groups: []uint32{4, 9}}
		stmt := db.Session(&gorm.Session{DryRun: true}).Model(&model.WebDomain{}).
			Scopes(WithPerm(user, PermUpdate)).Find(&[]model.WebDomain{}).Statement
		sql := stmt.SQL.String()
		require.Contains(t, sql, "sys_userid = ? AND sys_perm_user LIKE ?")
		require.Contains(t, sql, "sys_groupid IN (?,?) AND sys_perm_group LIKE ?")
		require.Contains(t, sql, "sys_perm_other LIKE ?")
		require.Contains(t, stmt.Vars, "%u%")
	})

	t.Run("groupless user has no group branch", func(t *testing.T) {
		user := &Identity{UserID: 3, Typ: "user"}
		stmt := db.Session(&gorm.Session{DryRun: true}).Model(&model.WebDomain{}).
			Scopes(WithPerm(user, PermRead)).Find(&[]model.WebDomain{}).Statement
		require.NotContains(t, stmt.SQL.String(), "sys_groupid")
	})
}

func TestNewRejectsUnpermissionedTables(t *testing.T) {
	db := dryDB(t)
	_, err := New[model.SysGroup](db) // sys_group has no sys_perm_* columns
	require.Error(t, err)
	_, err = New[model.WebDomain](db)
	require.NoError(t, err)
}

// lockedDomain overrides the insert permission preset with one that denies
// inserting.
type lockedDomain struct{ model.WebDomain }

// PermPreset denies the i flag for everyone.
func (lockedDomain) PermPreset() (string, string, string) { return "r", "r", "" }

// TableName keeps the embedded table mapping.
func (lockedDomain) TableName() string { return "web_domain" }

func TestCanInsert(t *testing.T) {
	repo, err := New[model.WebDomain](dryDB(t))
	require.NoError(t, err)

	user := &Identity{UserID: 3, Typ: "user", Groups: []uint32{4}}
	admin := &Identity{UserID: 1, Typ: "admin"}

	// canInsert consults the entity preset only, never the record body:
	// Insert stamps ownership server-side before this check.
	require.True(t, repo.canInsert(admin, &model.WebDomain{}), "admin bypass")
	require.True(t, repo.canInsert(user, &model.WebDomain{}), "default preset riud/riud grants i")
	require.False(t, repo.canInsert(nil, &model.WebDomain{}), "nil identity denied")

	locked, err := New[lockedDomain](dryDB(t))
	require.NoError(t, err)
	require.False(t, locked.canInsert(user, &lockedDomain{}), "preset without i denies non-admin")
	require.True(t, locked.canInsert(admin, &lockedDomain{}), "admin bypasses preset")
}

func TestStampOwnership(t *testing.T) {
	repo, err := New[model.WebDomain](dryDB(t))
	require.NoError(t, err)
	ctx := context.Background()
	user := &Identity{UserID: 3, Typ: "user", Groups: []uint32{4, 9}, DefaultGroup: 4}

	t.Run("payload ownership and perms are overwritten", func(t *testing.T) {
		rec := &model.WebDomain{SysUserID: 99, SysGroupID: 77,
			SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "riud"}
		require.NoError(t, repo.stampOwnership(ctx, user, rec))
		require.Equal(t, uint32(3), rec.SysUserID)
		require.Equal(t, uint32(4), rec.SysGroupID, "foreign group replaced by default group")
		require.Equal(t, "riud", rec.SysPermUser)
		require.Equal(t, "riud", rec.SysPermGroup)
		require.Equal(t, "", rec.SysPermOther, "perm_other comes from the preset, not the payload")
	})

	t.Run("requested group kept when the caller belongs to it", func(t *testing.T) {
		rec := &model.WebDomain{SysGroupID: 9}
		require.NoError(t, repo.stampOwnership(ctx, user, rec))
		require.Equal(t, uint32(9), rec.SysGroupID)
	})

	t.Run("groupless identity is denied", func(t *testing.T) {
		rec := &model.WebDomain{}
		err := repo.stampOwnership(ctx, &Identity{UserID: 3, Typ: "user"}, rec)
		require.ErrorIs(t, err, ErrPermissionDenied)
	})
}

func TestWithPermNilIdentity(t *testing.T) {
	db := dryDB(t)
	stmt := db.Session(&gorm.Session{DryRun: true}).Model(&model.WebDomain{}).
		Scopes(WithPerm(nil, PermRead)).Find(&[]model.WebDomain{}).Statement
	require.Contains(t, stmt.SQL.String(), "1 = 0", "nil identity must match nothing")
}

func TestHashIPStripsPort(t *testing.T) {
	require.Equal(t, HashIP("203.0.113.9"), HashIP("203.0.113.9:54321"),
		"ephemeral ports must not split the lockout counter")
	require.Equal(t, HashIP("2001:db8::1"), HashIP("[2001:db8::1]:443"))
	require.NotEqual(t, HashIP("203.0.113.9"), HashIP("203.0.113.10"))
}

func TestIdentityInGroup(t *testing.T) {
	id := &Identity{Groups: []uint32{2, 5}}
	require.True(t, id.InGroup(5))
	require.False(t, id.InGroup(3))
}
