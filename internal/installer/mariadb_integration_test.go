//go:build integration

package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
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
	configDir := t.TempDir()
	ctx := context.Background()

	// Fresh install: db + user + schema + seed.
	st := integrationState(t, addr, configDir)
	require.NoError(t, mariadbStep{}.Run(ctx, st))
	require.NotEmpty(t, st.DBPassword)
	assert.NotEmpty(t, st.AdminPassword, "fresh install seeds the admin")

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
}
