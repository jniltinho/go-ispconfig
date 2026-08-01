package datalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"

	"go-ispconfig/internal/model"
)

func dryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	return db
}

func TestIsTracked(t *testing.T) {
	require.True(t, IsTracked(&model.WebDomain{}))
	require.True(t, IsTracked(&model.DNSSoa{}))
	require.False(t, IsTracked(&model.SysUser{}), "sys_user must not be datalogged")
	require.False(t, IsTracked(&model.Server{}), "server must not be datalogged")
}

func TestBuildDiff(t *testing.T) {
	t.Run("insert carries full new record", func(t *testing.T) {
		diff, changed := buildDiff("i", nil, map[string]any{"domain": "a.tld", "active": "y"})
		require.True(t, changed)
		require.Empty(t, diff.Old)
		require.Equal(t, "a.tld", diff.New["domain"])
	})

	t.Run("delete carries full old record", func(t *testing.T) {
		diff, changed := buildDiff("d", map[string]any{"domain": "a.tld"}, nil)
		require.True(t, changed)
		require.Empty(t, diff.New)
		require.Equal(t, "a.tld", diff.Old["domain"])
	})

	t.Run("update carries changed fields only", func(t *testing.T) {
		oldMap := map[string]any{"domain": "a.tld", "active": "y"}
		newMap := map[string]any{"domain": "a.tld", "active": "n"}
		diff, changed := buildDiff("u", oldMap, newMap)
		require.True(t, changed)
		require.Equal(t, map[string]any{"active": "y"}, diff.Old)
		require.Equal(t, map[string]any{"active": "n"}, diff.New)
	})

	t.Run("no-op update writes nothing", func(t *testing.T) {
		m := map[string]any{"domain": "a.tld"}
		_, changed := buildDiff("u", m, m)
		require.False(t, changed)
	})
}

func TestServerID(t *testing.T) {
	require.Equal(t, uint32(3), serverID(map[string]any{"server_id": uint32(2)}, map[string]any{"server_id": uint32(3)}))
	require.Equal(t, uint32(2), serverID(map[string]any{"server_id": uint32(2)}, map[string]any{}))
	require.Equal(t, uint32(0), serverID(map[string]any{}, map[string]any{}), "no server_id column means broadcast")
}

func TestRecordMap(t *testing.T) {
	db := dryDB(t)
	s, err := parseSchema(db, &model.WebDomain{})
	require.NoError(t, err)

	m := recordMap(s, &model.WebDomain{DomainID: 7, Domain: "a.tld", ServerID: 1})
	require.Equal(t, uint32(7), m["domain_id"])
	require.Equal(t, "a.tld", m["domain"])
	require.Equal(t, uint32(1), m["server_id"])
	require.Empty(t, recordMap(s, nil))
}
