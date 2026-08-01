//go:build integration

package database

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// startMariaDB runs a throwaway mariadb:11 container on an ephemeral local
// port and returns the root DSN prefix ("root:root@tcp(127.0.0.1:PORT)").
func startMariaDB(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("goisp-schema-test-%d", os.Getpid())

	out, err := exec.Command("docker", "run", "--rm", "-d", "--name", name,
		"-e", "MARIADB_ROOT_PASSWORD=root", "-p", "127.0.0.1::3306", "mariadb:11").CombinedOutput()
	require.NoError(t, err, "docker run: %s", out)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", name, "mariadb", "-uroot", "-proot", "-e", "SELECT 1").Run() == nil {
			portOut, err := exec.Command("docker", "port", name, "3306/tcp").Output()
			require.NoError(t, err)
			addr := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
			return "root:root@tcp(" + addr + ")"
		}
		time.Sleep(time.Second)
	}
	t.Fatal("mariadb container did not become ready in 60s")
	return ""
}

func mariadbExec(t *testing.T, sql string) {
	t.Helper()
	name := fmt.Sprintf("goisp-schema-test-%d", os.Getpid())
	out, err := exec.Command("docker", "exec", name, "mariadb", "-uroot", "-proot", "-e", sql).CombinedOutput()
	require.NoError(t, err, "mariadb -e %q: %s", sql, out)
}

// TestSchemaIdentity runs migrate against an empty database and imports the
// original ispconfig3.sql with the mariadb client into a second database,
// then requires SHOW CREATE TABLE to be identical for every table (core-
// database spec, scenario "Migration creates identical schema").
func TestSchemaIdentity(t *testing.T) {
	dsnPrefix := startMariaDB(t)
	name := fmt.Sprintf("goisp-schema-test-%d", os.Getpid())

	mariadbExec(t, "CREATE DATABASE migrated CHARACTER SET utf8mb4; CREATE DATABASE original CHARACTER SET utf8mb4")

	// Path 1: go-ispconfig migrate.
	db, err := Open(dsnPrefix + "/migrated?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	created, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, created, "expected DDL execution on empty database")

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
	// The AUTO_INCREMENT counter is state, not schema; it may differ because
	// the migrated database was seeded.
	return autoIncRe.ReplaceAllString(row.CreateTable, "")
}
