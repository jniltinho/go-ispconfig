//go:build integration

package installer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// integrationState wires a State at a throwaway MariaDB container: root via
// --db-root-password over TCP (no unix socket in the container), user host
// '%' because the client connects from the docker bridge.
func integrationState(t *testing.T, addr, configDir string) *State {
	st, _, _ := testState(t)
	st.ConfigDir = configDir
	st.Answers.DBRootPassword = "root"
	st.DBAddr = addr
	st.MySQLSocket = filepath.Join(t.TempDir(), "no.sock")
	st.DBUserHosts = []string{"%"}
	return st
}

func TestMariaDBStepIntegration(t *testing.T) {
	dsnPrefix, _ := database.StartMariaDB(t, "installer")
	addr := strings.TrimSuffix(strings.SplitN(dsnPrefix, "(", 2)[1], ")")
	// Deliberately absent: this step runs long before configTomlStep, so on a
	// fresh machine /etc/go-ispconfig does not exist yet. Handing it an
	// existing TempDir hid a bare os.WriteFile that aborted the whole install.
	configDir := filepath.Join(t.TempDir(), "etc", "go-ispconfig")
	ctx := context.Background()

	// Fresh install: db + user + schema + seed.
	st := integrationState(t, addr, configDir)
	require.NoError(t, mariadbStep{}.Run(ctx, st))
	require.NotEmpty(t, st.DBPassword)
	assert.NotEmpty(t, st.AdminPassword, "fresh install seeds the admin")
	assert.FileExists(t, filepath.Join(configDir, "mysql_clientdb.conf"),
		"the step must create its own parent directory")

	var tables int64
	require.NoError(t, st.DB.Raw(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'dbispconfig'").Scan(&tables).Error)
	assert.Greater(t, tables, int64(70), "full ISPConfig table set present (78 tables in ispconfig3.sql)")

	var servers int64
	require.NoError(t, st.DB.Raw("SELECT COUNT(*) FROM server WHERE web_server = 1 AND dns_server = 1").Scan(&servers).Error)
	assert.EqualValues(t, 1, servers, "seed created the local server row")

	// Simulate the config.toml the config step would write, then re-run
	// with a fresh State: password reused, schema adopted, no re-seed.
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte("[database]\ndsn = \""+st.ispconfigDSN()+"\"\n"), 0o600))

	st2 := integrationState(t, addr, configDir)
	require.NoError(t, mariadbStep{}.Run(ctx, st2))
	assert.Equal(t, st.DBPassword, st2.DBPassword, "existing credentials reused")
	assert.Empty(t, st2.AdminPassword, "re-run never regenerates the admin password")

	// server-ips step: detected addresses inserted once, re-run skips.
	st2.HostIPs = func() []net.IP {
		return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("2001:db8::10")}
	}
	require.NoError(t, serverIPStep{}.Run(ctx, st2))
	var rows []model.ServerIP
	require.NoError(t, st2.DB.Order("ip_address").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "IPv6", rows[0].IPType)
	assert.Equal(t, "2001:db8::10", rows[0].IPAddress)
	assert.Equal(t, "IPv4", rows[1].IPType)
	assert.Equal(t, "203.0.113.10", rows[1].IPAddress)
	assert.Equal(t, "y", rows[1].Virtualhost)

	rerunErr := serverIPStep{}.Run(ctx, st2)
	require.ErrorContains(t, rerunErr, "already recorded", "re-run skips existing IPs")
	var n int64
	require.NoError(t, st2.DB.Model(&model.ServerIP{}).Count(&n).Error)
	assert.EqualValues(t, 2, n, "no duplicate rows on re-run")

	// uninstall --purge-db drops database and user.
	st3 := integrationState(t, addr, configDir)
	require.NoError(t, os.MkdirAll(st3.Profile.NginxConfigDir, 0o755))
	require.NoError(t, Uninstall(ctx, st3, UninstallOptions{PurgeDB: true}))
	root, err := connectRoot(st3)
	require.NoError(t, err)
	defer closeDB(root)
	var dbs int64
	require.NoError(t, root.Raw(
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = 'dbispconfig'").Scan(&dbs).Error)
	assert.Zero(t, dbs, "database dropped by --purge-db")
	var users int64
	require.NoError(t, root.Raw(
		"SELECT COUNT(*) FROM mysql.user WHERE user = 'ispconfig'").Scan(&users).Error)
	assert.Zero(t, users, "db user dropped by --purge-db")
}
