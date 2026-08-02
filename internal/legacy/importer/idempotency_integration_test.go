//go:build integration

package importer_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/legacy/legacytest"
	"go-ispconfig/internal/model"
)

// planTables are the local tables the importer may touch.
var planTables = []string{
	"client", "sys_group", "sys_user", "web_domain", "web_folder",
	"web_folder_user", "dns_soa", "dns_rr", "dns_slave", "dns_template",
	"sys_datalog",
}

// rowCounts snapshots the row count of every importer-touched table.
func rowCounts(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, table := range planTables {
		var n int64
		require.NoError(t, db.Table(table).Count(&n).Error)
		out[table] = n
	}
	return out
}

func TestImporterDryRunAndIdempotency(t *testing.T) {
	db := setupDB(t)
	srv := legacytest.New()
	t.Cleanup(srv.Close)
	c := connect(t, srv)
	ctx := context.Background()
	opts := importer.Options{Selection: selAll, TargetServerID: 1}

	fetch := func() *importer.Snapshot {
		snap, err := importer.FetchSnapshot(ctx, c, selAll)
		require.NoError(t, err)
		return snap
	}

	var afterFirst map[string]int64

	t.Run("dry-run writes nothing", func(t *testing.T) {
		before := rowCounts(t, db)
		plan, err := importer.BuildPlan(ctx, db, fetch(), opts)
		require.NoError(t, err)
		require.NotEmpty(t, plan.Items)
		require.Equal(t, before, rowCounts(t, db),
			"planning is the dry-run: zero local writes, zero datalog rows")
		require.Zero(t, before["sys_datalog"])
	})

	t.Run("first apply imports everything", func(t *testing.T) {
		plan, err := importer.BuildPlan(ctx, db, fetch(), opts)
		require.NoError(t, err)
		_, err = importer.Apply(ctx, db, plan)
		require.NoError(t, err)
		afterFirst = rowCounts(t, db)
		require.EqualValues(t, 1201, afterFirst["web_domain"])
	})

	t.Run("second run plans all-skip and changes nothing", func(t *testing.T) {
		plan, err := importer.BuildPlan(ctx, db, fetch(), opts)
		require.NoError(t, err)
		for _, it := range plan.Items {
			require.Equal(t, importer.ActionSkip, it.Action,
				"%s %s must be skip-identical on the second run (got %s: %s)",
				it.Table, it.Key, it.Action, it.Reason)
		}

		counts, err := importer.Apply(ctx, db, plan)
		require.NoError(t, err)
		for table, tally := range counts {
			require.Zero(t, tally.Created, "%s created on re-run", table)
			require.Zero(t, tally.Updated, "%s updated on re-run", table)
		}
		require.Equal(t, afterFirst, rowCounts(t, db),
			"apply-twice must leave identical row counts, including sys_datalog")
	})

	t.Run("changed legacy field re-plans as update of that field set", func(t *testing.T) {
		srv.Domains[41]["document_root"] = "/var/www/clients/moved/web42"

		plan, err := importer.BuildPlan(ctx, db, fetch(), opts)
		require.NoError(t, err)
		updates := 0
		for _, it := range plan.Items {
			if it.Action == importer.ActionUpdate {
				updates++
				require.Equal(t, "web_domain", it.Table)
			} else {
				require.Equal(t, importer.ActionSkip, it.Action)
			}
		}
		require.Equal(t, 1, updates)

		counts, err := importer.Apply(ctx, db, plan)
		require.NoError(t, err)
		require.Equal(t, 1, counts["web_domain"].Updated)

		var dom model.WebDomain
		require.NoError(t, db.Where("domain = ?", "site42.example.com").First(&dom).Error)
		require.Equal(t, "/var/www/clients/moved/web42", dom.DocumentRoot)

		// The update was journaled with the full old/new diff.
		var row model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = ?", "web_domain", "u").
			Order("datalog_id DESC").First(&row).Error)
		var diff struct {
			Old map[string]any `json:"old"`
			New map[string]any `json:"new"`
		}
		require.NoError(t, json.Unmarshal([]byte(row.Data), &diff))
		require.Equal(t, "/var/www/clients/moved/web42", diff.New["document_root"])
		require.NotEqual(t, diff.New["document_root"], diff.Old["document_root"])
		require.Equal(t, "site42.example.com", diff.New["domain"],
			"update diff carries the full record (daemon plugin parity)")
	})
}
