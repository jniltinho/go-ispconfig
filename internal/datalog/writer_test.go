package datalog

import (
	"context"
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

	t.Run("update carries full old and new records", func(t *testing.T) {
		// PHP datalogSave parity: unchanged fields (id, zone, origin, ...)
		// stay in the payload — daemon plugins address related rows by them.
		oldMap := map[string]any{"domain": "a.tld", "active": "y"}
		newMap := map[string]any{"domain": "a.tld", "active": "n"}
		diff, changed := buildDiff("u", oldMap, newMap)
		require.True(t, changed)
		require.Equal(t, oldMap, diff.Old)
		require.Equal(t, newMap, diff.New)
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

func TestNotifyAfterCommit(t *testing.T) {
	var fired []uint32
	SetReadyNotifier(func(id uint32) { fired = append(fired, id) })
	t.Cleanup(func() { SetReadyNotifier(nil) })

	t.Run("deferred until flush, deduplicated per server", func(t *testing.T) {
		ctx, flush := NotifyAfterCommit(context.Background())
		markReady(ctx, 1)
		markReady(ctx, 1)
		markReady(ctx, 2)
		require.Empty(t, fired, "nothing fires before commit")
		flush()
		require.ElementsMatch(t, []uint32{1, 2}, fired)

		fired = nil
		flush()
		require.Empty(t, fired, "flush is one-shot")
	})

	t.Run("immediate without collector", func(t *testing.T) {
		fired = nil
		markReady(context.Background(), 7)
		require.Equal(t, []uint32{7}, fired)
	})
}
