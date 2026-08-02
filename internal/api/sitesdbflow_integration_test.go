//go:build integration

package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/clientdb"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
)

// dsnHostPortFromPrefix returns "host:port" from a root DSN prefix
// ("user:pass@tcp(host:port)").
func dsnHostPortFromPrefix(dsnPrefix string) string {
	start := strings.Index(dsnPrefix, "tcp(")
	end := strings.LastIndex(dsnPrefix, ")")
	if start < 0 || end <= start {
		return "127.0.0.1:3306"
	}
	return dsnPrefix[start+4 : end]
}

// newDatabaseFlowEnv boots a single-server panel with a superadmin
// session plus the root DSN prefix of the same MariaDB container, which
// doubles as the client database server.
func newDatabaseFlowEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string, string) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "dbflow")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.example.com", "smoke-test-pw")
	require.NoError(t, err)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	ck, csrf := login(t, srv, "admin", "smoke-test-pw")
	return db, srv, ck, csrf, dsnPrefix
}

// nopDBExec satisfies engine.Executor; the database module never
// requests service restarts (design D2).
type nopDBExec struct{}

func (nopDBExec) Run(context.Context, string, string) error { return nil }

// TestDatabaseEndToEndFlow (task 7.1): API create user + database →
// sys_datalog → daemon cycle → physical CREATE DATABASE and GRANTs
// observable via information_schema / mysql.user; the remote_ips update
// and both delete paths run through the same pipeline.
func TestDatabaseEndToEndFlow(t *testing.T) {
	db, srv, adminCk, adminCsrf, dsnPrefix := newDatabaseFlowEnv(t)
	ctx := context.Background()

	// Daemon-shaped engine: database module + the real mysql_clientdb
	// plugin with a root admin connection into the same container.
	plugin := clientdb.NewPlugin(db, engine.ExecRunner{}, "", 1, nil)
	// Host/Port must match the container publish address for any CLI
	// tools the plugin may spawn (mysqldump); do not hardcode :3306.
	host, portStr, _ := strings.Cut(dsnHostPortFromPrefix(dsnPrefix), ":")
	port, _ := strconv.Atoi(portStr)
	adminCfg := clientdb.Config{Host: host, Port: port, User: "root", Password: "root"}
	plugin.OpenAdmin = func(context.Context) (*sql.DB, clientdb.Config, error) {
		adminDB, err := sql.Open("mysql", dsnPrefix+"/")
		return adminDB, adminCfg, err
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{clientdb.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nopDBExec{}, nil), nil)
	require.NoError(t, err)

	// Direct admin connection for physical-state assertions.
	adminDB, err := sql.Open("mysql", dsnPrefix+"/")
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()
	schemaExists := func(name string) bool {
		var s string
		err := adminDB.QueryRowContext(ctx,
			"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&s)
		return err == nil
	}
	userHosts := func(user string) []string {
		rows, err := adminDB.QueryContext(ctx, "SELECT Host FROM mysql.user WHERE User = ?", user)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var hosts []string
		for rows.Next() {
			var h string
			require.NoError(t, rows.Scan(&h))
			hosts = append(hosts, h)
		}
		require.NoError(t, rows.Err())
		return hosts
	}
	grantsOf := func(user, host string) string {
		rows, err := adminDB.QueryContext(ctx, "SHOW GRANTS FOR '"+user+"'@'"+host+"'")
		if err != nil {
			return ""
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var g string
			require.NoError(t, rows.Scan(&g))
			out = append(out, g)
		}
		return strings.Join(out, "\n")
	}

	// Drain the seed backlog so cycles below only see this test's rows.
	require.NoError(t, daemon.RunCycle(ctx))

	// Panel-side records through the real API.
	domainID := 0.0
	{
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", adminCk, adminCsrf,
			map[string]any{"server_id": 1, "domain": "dbflow.example.com", "type": "vhost"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		domainID = rec["domain_id"].(float64)
	}
	status, data := call(t, srv, http.MethodPost, "/api/sites/database-users", adminCk, adminCsrf,
		map[string]any{"database_user": "dbflow_u", "database_password": "Fl0w-Secret-1!"})
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var userRec map[string]any
	require.NoError(t, json.Unmarshal(data, &userRec))
	userID := userRec["database_user_id"].(float64)

	var databaseID float64
	t.Run("create → physical database + localhost grant", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/databases", adminCk, adminCsrf,
			map[string]any{"server_id": 1, "parent_domain_id": domainID,
				"database_name": "dbflow_db", "database_user_id": userID})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		databaseID = rec["database_id"].(float64)

		require.NoError(t, daemon.RunCycle(ctx))
		assert.True(t, schemaExists("dbflow_db"), "daemon created the physical database")
		assert.Contains(t, userHosts("dbflow_u"), "localhost")
		assert.Contains(t, grantsOf("dbflow_u", "localhost"), "ALL PRIVILEGES ON `dbflow_db`.*")
	})

	t.Run("remote_ips update → grants for the new host", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), adminCk, adminCsrf,
			map[string]any{"remote_access": "y", "remote_ips": "10.0.0.5"})
		require.Equal(t, http.StatusOK, status, "%s", data)

		require.NoError(t, daemon.RunCycle(ctx))
		assert.Contains(t, userHosts("dbflow_u"), "10.0.0.5")
		assert.Contains(t, grantsOf("dbflow_u", "10.0.0.5"), "ALL PRIVILEGES ON `dbflow_db`.*")
	})

	t.Run("database delete → schema dropped, account gone", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusNoContent, status)

		require.NoError(t, daemon.RunCycle(ctx))
		assert.False(t, schemaExists("dbflow_db"), "daemon dropped the physical database")
		assert.Empty(t, userHosts("dbflow_u"), "no other database needs the account")
	})

	t.Run("user delete consumes cleanly", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/database-users/%d", int(userID)), adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusNoContent, status)
		require.NoError(t, daemon.RunCycle(ctx))
		assert.Empty(t, userHosts("dbflow_u"))
	})
}
