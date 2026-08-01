package repository

import (
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

func TestCanInsert(t *testing.T) {
	repo, err := New[model.WebDomain](dryDB(t))
	require.NoError(t, err)

	user := &Identity{UserID: 3, Typ: "user", Groups: []uint32{4}}
	admin := &Identity{UserID: 1, Typ: "admin"}

	rec := func(uid, gid uint32, pu, pg, po string) *model.WebDomain {
		return &model.WebDomain{SysUserID: uid, SysGroupID: gid,
			SysPermUser: pu, SysPermGroup: pg, SysPermOther: po}
	}

	require.True(t, repo.canInsert(admin, rec(9, 9, "", "", "")), "admin bypass")
	require.True(t, repo.canInsert(user, rec(3, 4, "riud", "riud", "")), "owner with i")
	require.False(t, repo.canInsert(user, rec(3, 4, "rud", "rud", "")), "owner without i")
	require.True(t, repo.canInsert(user, rec(9, 4, "", "riud", "")), "group member with i")
	require.False(t, repo.canInsert(user, rec(9, 5, "", "riud", "")), "not in group")
	require.False(t, repo.canInsert(user, rec(9, 9, "riud", "riud", "")), "foreign record")
	require.True(t, repo.canInsert(user, rec(9, 9, "", "", "ri")), "perm_other grants i")
	require.True(t, repo.canInsert(user, rec(0, 0, "riud", "riud", "")), "0/0 preset open insert")
	require.False(t, repo.canInsert(user, rec(0, 0, "r", "r", "")), "0/0 preset without i")
}

func TestIdentityInGroup(t *testing.T) {
	id := &Identity{Groups: []uint32{2, 5}}
	require.True(t, id.InGroup(5))
	require.False(t, id.InGroup(3))
}
