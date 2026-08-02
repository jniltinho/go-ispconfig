//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/firewall"
)

// newFirewallFlowEnv boots a SINGLE-server panel (the daemon refuses
// multi-server setups) with a superadmin session, for the end-to-end
// API → datalog → daemon flow.
func newFirewallFlowEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "firewallflow")
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
	return db, srv, ck, csrf
}

// recordingRunner captures every ufw argv and answers `ufw --version`.
type recordingRunner struct{ calls []string }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "ufw" && len(args) == 1 && args[0] == "--version" {
		return []byte("ufw 0.36.1\n"), nil
	}
	return []byte("ok"), nil
}

func (r *recordingRunner) has(cmd string) bool {
	for _, c := range r.calls {
		if c == cmd {
			return true
		}
	}
	return false
}

func (r *recordingRunner) index(cmd string) int {
	for i, c := range r.calls {
		if c == cmd {
			return i
		}
	}
	return -1
}

// nopFwExec satisfies engine.Executor (service restarts) without touching
// the system; the firewall plugin drives ufw through its own runner.
type nopFwExec struct{}

func (nopFwExec) Run(context.Context, string, string) error { return nil }

// TestFirewallEndToEndFlow (task 5.1): API create/update/delete → sys_datalog
// → the daemon's firewall module raises the event → the UFW plugin runs the
// expected command sequence through a recording runner, including the
// protected panel + SSH ports that the lock-out guard force-allows.
func TestFirewallEndToEndFlow(t *testing.T) {
	db, srv, adminCk, adminCsrf := newFirewallFlowEnv(t)
	ctx := context.Background()

	// Daemon-shaped engine: the firewall module + the real UFW plugin bound
	// to server_id 1 with the panel on 8080 (SSH defaults to 22).
	rec := &recordingRunner{}
	reg := engine.NewRegistry(nil)
	plugin := firewall.NewPlugin(rec, 1, 8080, nil)
	require.NoError(t, reg.Load([]engine.Module{firewall.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nopFwExec{}, nil), nil)
	require.NoError(t, err)

	// Drain the seed backlog so assertions see only this test's events.
	require.NoError(t, daemon.RunCycle(ctx))
	rec.calls = nil

	var fwID float64
	t.Run("create → insert baseline + allow ports incl. protected + enable", func(t *testing.T) {
		// tcp_port deliberately omits 22 and 8080 to prove the lock-out
		// guard force-allows the panel and SSH ports anyway.
		status, data := call(t, srv, http.MethodPost, "/api/firewall", adminCk, adminCsrf,
			map[string]any{"server_id": 1, "tcp_port": "80,443", "udp_port": "53", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec2 map[string]any
		require.NoError(t, json.Unmarshal(data, &rec2))
		fwID = rec2["firewall_id"].(float64)

		require.NoError(t, daemon.RunCycle(ctx))

		// Insert baseline, in order, before any allow.
		for _, c := range []string{
			"ufw --force disable", "ufw --force reset",
			"ufw default deny incoming", "ufw default allow outgoing",
		} {
			assert.True(t, rec.has(c), "baseline step missing: %s", c)
		}
		// Declared ports.
		assert.True(t, rec.has("ufw allow 80/tcp"), "80/tcp allowed")
		assert.True(t, rec.has("ufw allow 443/tcp"), "443/tcp allowed")
		assert.True(t, rec.has("ufw allow 53/udp"), "53/udp allowed")
		// Protected ports force-added by the lock-out guard.
		assert.True(t, rec.has("ufw allow 22/tcp"), "SSH port protected")
		assert.True(t, rec.has("ufw allow 8080/tcp"), "panel port protected")
		// Enabled last, after the baseline.
		assert.True(t, rec.has("ufw --force enable"), "firewall enabled")
		assert.Greater(t, rec.index("ufw --force enable"), rec.index("ufw --force reset"),
			"enable comes after the baseline reset")
	})

	t.Run("update → differential allow/delete, protected ports kept", func(t *testing.T) {
		rec.calls = nil
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf,
			map[string]any{"server_id": 1, "tcp_port": "80", "udp_port": "53", "active": "y"})
		require.Equal(t, http.StatusOK, status, "%s", data)

		require.NoError(t, daemon.RunCycle(ctx))

		// 443 removed; 80 stays (no re-add), protected ports never deleted.
		assert.True(t, rec.has("ufw delete allow 443/tcp"), "443/tcp removed")
		assert.False(t, rec.has("ufw delete allow 22/tcp"), "SSH never deleted")
		assert.False(t, rec.has("ufw delete allow 8080/tcp"), "panel never deleted")
		// Reload (active unchanged y→y), not a fresh enable.
		assert.True(t, rec.has("ufw reload"), "reload on unchanged-active update")
	})

	t.Run("delete → reset + disable", func(t *testing.T) {
		rec.calls = nil
		status, _ := call(t, srv, http.MethodDelete, fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusNoContent, status)

		require.NoError(t, daemon.RunCycle(ctx))

		assert.True(t, rec.has("ufw --force reset"), "reset on delete")
		assert.True(t, rec.has("ufw disable"), "disabled on delete")
	})
}
