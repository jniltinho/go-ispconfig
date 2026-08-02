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

// TestExistingClientAdoptsPendingParent covers the review finding: a
// client that already exists locally whose reseller parent is created in
// the same run must end up referencing the new parent id — never 0.
func TestExistingClientAdoptsPendingParent(t *testing.T) {
	db := setupDB(t)
	srv := legacytest.New()
	t.Cleanup(srv.Close)
	c := connect(t, srv)
	ctx := context.Background()

	// Pre-seed client2 locally (no reseller): matches the legacy client2
	// whose parent_client_id points at reseller1.
	require.NoError(t, db.Create(&model.SysGroup{Name: "client2", ClientID: 0}).Error)
	var g model.SysGroup
	require.NoError(t, db.Where("name = ?", "client2").First(&g).Error)
	require.NoError(t, db.Create(&model.SysUser{
		SysUserID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		Username: "client2", Passwort: "x", Typ: "user", Active: 1,
		Groups: "0", DefaultGroup: g.GroupID,
	}).Error)
	// Raw insert with named columns so the schema's enum defaults apply.
	require.NoError(t, db.Exec(
		"INSERT INTO client (username, contact_name, email, sys_perm_user, sys_perm_group, parent_client_id) "+
			"VALUES ('client2', 'Client Two', 'client2@example.com', 'riud', 'riud', 0)").Error)
	var seed model.Client
	require.NoError(t, db.Where("username = ?", "client2").First(&seed).Error)
	require.NoError(t, db.Model(&model.SysGroup{}).Where("groupid = ?", g.GroupID).
		Update("client_id", seed.ClientID).Error)
	require.NoError(t, db.Model(&model.SysUser{}).Where("username = ?", "client2").
		Update("client_id", seed.ClientID).Error)

	snap, err := importer.FetchSnapshot(ctx, c, importer.Selection{Clients: true})
	require.NoError(t, err)
	plan, err := importer.BuildPlan(ctx, db, snap, importer.Options{
		Selection: importer.Selection{Clients: true}, TargetServerID: 1,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Conflicts())

	_, err = importer.Apply(ctx, db, plan)
	require.NoError(t, err)

	var reseller, child model.Client
	require.NoError(t, db.Where("username = ?", "reseller1").First(&reseller).Error)
	require.NoError(t, db.Where("username = ?", "client2").First(&child).Error)
	require.Equal(t, seed.ClientID, child.ClientID, "existing client updated, not duplicated")
	require.NotZero(t, reseller.ClientID)
	require.Equal(t, reseller.ClientID, child.ParentClientID,
		"pending reseller parent must resolve on the update path (never 0)")
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
