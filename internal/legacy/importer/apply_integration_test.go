//go:build integration

// Importer integration suite: full snapshot → plan → apply runs from the
// legacytest mock panel into a real MariaDB with the embedded ISPConfig3
// schema, asserting remapped rows and daemon-consumable sys_datalog
// emission (tasks 3.4/3.7).
package importer_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/legacy/legacytest"
	"go-ispconfig/internal/model"
)

// setupDB starts a MariaDB container with the embedded ISPConfig3 schema.
func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsnPrefix, name := database.StartMariaDB(t, "importer")
	database.MariaDBExec(t, name, "CREATE DATABASE importer CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/importer?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	created, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, created)
	return db
}

// selAll selects every entity.
var selAll = importer.Selection{Clients: true, Sites: true, DNS: true}

func TestApplyEndToEnd(t *testing.T) {
	db := setupDB(t)
	srv := legacytest.New()
	t.Cleanup(srv.Close)
	c := connect(t, srv)
	ctx := context.Background()

	snap, err := importer.FetchSnapshot(ctx, c, selAll)
	require.NoError(t, err)
	plan, err := importer.BuildPlan(ctx, db, snap, importer.Options{
		Selection: selAll, TargetServerID: 1,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Conflicts())

	counts, err := importer.Apply(ctx, db, plan)
	require.NoError(t, err)
	require.Equal(t, 3, counts["client"].Created)
	require.Equal(t, 3, counts["sys_group"].Created)
	require.Equal(t, 3, counts["sys_user"].Created)
	require.Equal(t, 1201, counts["web_domain"].Created)
	require.Equal(t, 1, counts["web_folder"].Created)
	require.Equal(t, 1, counts["web_folder_user"].Created)
	require.Equal(t, 2, counts["dns_soa"].Created)
	require.Equal(t, 4, counts["dns_rr"].Created)
	require.Equal(t, 1, counts["dns_slave"].Created)
	require.Equal(t, 1, counts["dns_template"].Created)

	t.Run("client hierarchy and self-ownership remapped", func(t *testing.T) {
		var reseller, child model.Client
		require.NoError(t, db.Where("username = ?", "reseller1").First(&reseller).Error)
		require.NoError(t, db.Where("username = ?", "client2").First(&child).Error)
		require.Equal(t, reseller.ClientID, child.ParentClientID,
			"reseller hierarchy must survive the id remap")

		var group model.SysGroup
		require.NoError(t, db.Where("name = ?", "reseller1").First(&group).Error)
		require.Equal(t, reseller.ClientID, group.ClientID)
		var user model.SysUser
		require.NoError(t, db.Where("username = ?", "reseller1").First(&user).Error)
		require.Equal(t, reseller.ClientID, user.ClientID)
		require.Equal(t, group.GroupID, user.DefaultGroup)
		require.Equal(t, user.UserID, reseller.SysUserID, "client row re-owned by its own user")
		require.Equal(t, group.GroupID, reseller.SysGroupID)
		require.Equal(t, importer.PlaceholderHash, user.Passwort,
			"panel users must be created unable to log in")
	})

	t.Run("web domains remapped to owner, server and parent", func(t *testing.T) {
		var n int64
		require.NoError(t, db.Model(&model.WebDomain{}).Count(&n).Error)
		require.EqualValues(t, 1201, n)

		var site1, sub model.WebDomain
		require.NoError(t, db.Where("domain = ?", "site1.example.com").First(&site1).Error)
		require.Equal(t, uint32(1), site1.ServerID)
		require.Equal(t, "y", site1.SSL, "SSL fields imported as-is")
		require.Equal(t, "y", site1.SSLLetsencrypt)

		var owner model.SysGroup
		require.NoError(t, db.Where("name = ?", "client2").First(&owner).Error)
		require.Equal(t, owner.GroupID, site1.SysGroupID, "ownership follows the remap")
		require.Equal(t, "riud", site1.SysPermUser, "riud strings verbatim")

		require.NoError(t, db.Where("domain = ?", "sub.site1.example.com").First(&sub).Error)
		require.Equal(t, site1.DomainID, sub.ParentDomainID, "subdomain parent follows the remap")
	})

	t.Run("folder chain and crypt hash", func(t *testing.T) {
		var folder model.WebFolder
		require.NoError(t, db.Where("path = ?", "protected").First(&folder).Error)
		var site1 model.WebDomain
		require.NoError(t, db.Where("domain = ?", "site1.example.com").First(&site1).Error)
		require.EqualValues(t, site1.DomainID, folder.ParentDomainID)

		var fu model.WebFolderUser
		require.NoError(t, db.Where("username = ?", "folderuser1").First(&fu).Error)
		require.EqualValues(t, folder.WebFolderID, fu.WebFolderID)
		require.Equal(t, legacytest.Hash6, fu.Password, "$6$ hash imported verbatim")
	})

	t.Run("dns records follow their zone", func(t *testing.T) {
		var zone model.DNSSoa
		require.NoError(t, db.Where("origin = ?", "example.com.").First(&zone).Error)
		var rrs []model.DNSRr
		require.NoError(t, db.Where("zone = ?", zone.ID).Find(&rrs).Error)
		require.Len(t, rrs, 3)
	})

	t.Run("datalog rows consumable by the daemon", func(t *testing.T) {
		var rows []model.SysDatalog
		require.NoError(t, db.Where("dbtable = ?", "dns_soa").Find(&rows).Error)
		require.Len(t, rows, 2)
		row := rows[0]
		require.Equal(t, "i", row.Action)
		require.Equal(t, uint32(1), row.ServerID)
		require.Equal(t, "admin", row.User)
		require.Regexp(t, `^id:\d+$`, row.DBIdx)

		var diff struct {
			Old map[string]any `json:"old"`
			New map[string]any `json:"new"`
		}
		require.NoError(t, json.Unmarshal([]byte(row.Data), &diff))
		require.Empty(t, diff.Old)
		require.NotEmpty(t, diff.New["origin"], "insert diff must carry the full new record")

		// Untracked tables must not be journaled; tracked ones must.
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = ?", "sys_user").Count(&n).Error)
		require.Zero(t, n)
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = ?", "dns_template").Count(&n).Error)
		require.Zero(t, n)
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = ?", "web_domain").Count(&n).Error)
		require.EqualValues(t, 1201, n)
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = ?", "client").Count(&n).Error)
		require.EqualValues(t, 3, n)
	})
}
