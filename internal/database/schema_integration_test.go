//go:build integration

package database

import (
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// TestSchemaIdentity runs migrate against an empty database and imports the
// original ispconfig3.sql with the mariadb client into a second database,
// then requires SHOW CREATE TABLE to be identical for every table (core-
// database spec, scenario "Migration creates identical schema").
func TestSchemaIdentity(t *testing.T) {
	dsnPrefix, name := StartMariaDB(t, "schema")

	MariaDBExec(t, name, "CREATE DATABASE migrated CHARACTER SET utf8mb4; CREATE DATABASE original CHARACTER SET utf8mb4")

	// Path 1: go-ispconfig migrate.
	db, err := Open(dsnPrefix + "/migrated?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed, "expected DDL execution on empty database")

	// Path 2: original dump imported by the stock mariadb client.
	dump, err := os.Open("ispconfig3.sql")
	require.NoError(t, err)
	defer func() { _ = dump.Close() }()
	importCmd := exec.Command("docker", "exec", "-i", name, "mariadb", "-uroot", "-proot", "original")
	importCmd.Stdin = dump
	out, err := importCmd.CombinedOutput()
	require.NoError(t, err, "importing original dump: %s", out)

	orig, err := Open(dsnPrefix + "/original?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)

	migratedTables := listTables(t, db)
	originalTables := listTables(t, orig)
	require.Equal(t, originalTables, migratedTables, "table sets differ")
	require.NotEmpty(t, migratedTables)

	for _, table := range originalTables {
		assert.Equalf(t, showCreate(t, orig, table), showCreate(t, db, table),
			"SHOW CREATE TABLE differs for %s", table)
	}
	t.Logf("schema identical for %d tables", len(originalTables))
}

// TestSeed covers the post-DDL seed: admin password actually replaced with a
// verifiable bcrypt hash, orphan group reference repaired, server row created
// at the supported dbversion — plus the interrupted-install recovery path of
// Migrate and NULL-config tolerance of getconf.
func TestSeed(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "seed")

	MariaDBExec(t, container, "CREATE DATABASE seedtest CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/seedtest?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)

	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	// Interrupted install: schema complete but server table still empty —
	// a second migrate must ask for the seed again instead of aborting.
	needSeed, err = Migrate(db)
	require.NoError(t, err)
	assert.True(t, needSeed, "complete schema with empty server table must resume the seed")

	password, err := Seed(db, "server1.example.com", "")
	require.NoError(t, err)
	require.NotEmpty(t, password)
	assert.NotEqual(t, "xxx", password)

	var admin model.SysUser
	require.NoError(t, db.Take(&admin, 1).Error)
	assert.NotEqual(t, "xxx", admin.Passwort, "dump placeholder password must be replaced")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(admin.Passwort), []byte(password)),
		"stored hash must verify against the returned password")
	assert.Equal(t, "1", admin.Groups, "orphan group 2 from the dump must be dropped")

	var server model.Server
	require.NoError(t, db.Take(&server, 1).Error)
	assert.Equal(t, uint32(MinDBVersion), server.DBVersion)
	assert.Equal(t, "server1.example.com", server.ServerName)
	assert.Equal(t, int8(1), server.WebServer)
	assert.Equal(t, int8(1), server.DNSServer)

	// The seeded config must parse into the typed [web]/[dns] sections.
	cfg, err := getconf.GetServerConfig(db, 1)
	require.NoError(t, err)
	assert.Equal(t, "nginx", cfg.Web.ServerType)
	assert.Equal(t, "/etc/php/8.3/fpm/pool.d", cfg.Web.PHPFPMPoolDir)
	assert.Equal(t, "/etc/bind", cfg.DNS.BindZonefilesDir)

	// After the seed, migrate adopts the database without touching it.
	needSeed, err = Migrate(db)
	require.NoError(t, err)
	assert.False(t, needSeed)

	// Adopted ISPConfig databases can hold NULL in server.config and
	// sys_ini.config; getconf must read them as empty INIs, not fail.
	require.NoError(t, db.Exec("UPDATE server SET config = NULL WHERE server_id = 1").Error)
	require.NoError(t, db.Exec("UPDATE sys_ini SET config = NULL WHERE sysini_id = 1").Error)
	cfg, err = getconf.GetServerConfig(db, 1)
	require.NoError(t, err, "NULL server.config must not fail")
	assert.Empty(t, cfg.Web.ServerType)
	global, err := getconf.GetGlobalConfig(db)
	require.NoError(t, err, "NULL sys_ini.config must not fail")
	assert.Empty(t, global)
}

func listTables(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var tables []string
	err := db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name").
		Scan(&tables).Error
	require.NoError(t, err)
	return tables
}

var autoIncRe = regexp.MustCompile(` AUTO_INCREMENT=\d+`)

func showCreate(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var row struct {
		Table       string `gorm:"column:Table"`
		CreateTable string `gorm:"column:Create Table"`
	}
	err := db.Raw("SHOW CREATE TABLE `" + table + "`").Scan(&row).Error
	require.NoError(t, err)
	// The AUTO_INCREMENT counter is row state, not schema: it moves with the
	// dump's own INSERTs and server bookkeeping, so it is normalized away
	// before comparing (neither database is seeded in this test).
	return autoIncRe.ReplaceAllString(row.CreateTable, "")
}
