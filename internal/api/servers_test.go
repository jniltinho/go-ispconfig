package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// serverRegistryDB reuses the sqlite server table of the monitor tests and
// fills it with a two-node registry: 1 = web+mail master, 2 = DNS node,
// 3 = a mirror of 2, 4 = registered but not yet installed.
func serverRegistryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := monitorTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE server_registry (
  server_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_name TEXT NOT NULL DEFAULT '',
  mail_server INTEGER NOT NULL DEFAULT 0,
  web_server INTEGER NOT NULL DEFAULT 0,
  dns_server INTEGER NOT NULL DEFAULT 0,
  mirror_server_id INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 0)`).Error)
	require.NoError(t, db.Exec("DROP TABLE server").Error)
	require.NoError(t, db.Exec("ALTER TABLE server_registry RENAME TO server").Error)
	// Raw inserts: the shared sqlite fixture only carries the columns the
	// monitor endpoints read, not every column of model.Server.
	rows := [][]any{
		{"master.test", 1, 1, 0, 0, 1},
		{"dns01.test", 0, 0, 1, 0, 1},
		{"dns02.test", 0, 0, 1, 2, 1},
		{"web02.test", 1, 0, 0, 0, 0},
	}
	for _, r := range rows {
		require.NoError(t, db.Exec(
			"INSERT INTO server (server_name, web_server, mail_server, dns_server, mirror_server_id, active)"+
				" VALUES (?,?,?,?,?,?)", r...).Error)
	}
	return db
}

// onPrepareCtx runs fn with a routed request context, the shape a Prepare
// hook sees: POST /server is a create, PUT /server/<id> an update of that
// row. The context only lives for the duration of the request, so fn runs
// inside the handler.
func onPrepareCtx(method, id string, fn func(*echo.Context) error) error {
	e := echo.New()
	var err error
	h := func(c *echo.Context) error { err = fn(c); return nil }
	e.Add(method, "/server", h)
	e.Add(method, "/server/:id", h)
	path := "/server"
	if id != "" {
		path += "/" + id
	}
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
	return err
}

func TestRequireTargetServer(t *testing.T) {
	d := &Deps{DB: serverRegistryDB(t)}
	check := requireTargetServer("dns_server")
	create := func(body map[string]any) error {
		return onPrepareCtx(http.MethodPost, "", func(c *echo.Context) error { return check(c, d, body) })
	}

	t.Run("accepts an active server carrying the role", func(t *testing.T) {
		require.NoError(t, create(map[string]any{"server_id": float64(2)}))
	})

	t.Run("rejects a server without the role", func(t *testing.T) {
		require.IsType(t, &ValidationError{}, create(map[string]any{"server_id": float64(1)}))
	})

	t.Run("rejects a mirror and an inactive server", func(t *testing.T) {
		require.Error(t, create(map[string]any{"server_id": float64(3)}))
		require.Error(t, onPrepareCtx(http.MethodPost, "", func(c *echo.Context) error {
			return requireTargetServer("web_server")(c, d, map[string]any{"server_id": float64(4)})
		}))
	})

	t.Run("create without server_id is refused, no silent server 1", func(t *testing.T) {
		require.IsType(t, &ValidationError{}, create(map[string]any{}))
	})

	t.Run("update without server_id keeps the stored value", func(t *testing.T) {
		require.NoError(t, onPrepareCtx(http.MethodPut, "7", func(c *echo.Context) error {
			return check(c, d, map[string]any{"active": "n"})
		}))
	})
}

func TestServerPrepareMirrorRules(t *testing.T) {
	d := &Deps{DB: serverRegistryDB(t)}

	cases := []struct {
		name, id string
		mirror   float64
		want     any
	}{
		{"self-mirror is dropped", "2", 2, float64(0)},
		{"server 1 is never a mirror", "1", 2, float64(0)},
		{"mirror of a mirror is dropped", "4", 3, float64(0)},
		{"valid mirror target is kept", "4", 2, float64(2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"mirror_server_id": tc.mirror}
			require.NoError(t, onPrepareCtx(http.MethodPut, tc.id, func(c *echo.Context) error {
				return serverPrepare(c, d, nil, body)
			}))
			require.Equal(t, tc.want, body["mirror_server_id"])
		})
	}

	t.Run("caller-supplied server_id is discarded", func(t *testing.T) {
		body := map[string]any{"server_id": float64(7), "server_name": "  WEB03.Test "}
		require.NoError(t, onPrepareCtx(http.MethodPost, "", func(c *echo.Context) error {
			return serverPrepare(c, d, nil, body)
		}))
		require.NotContains(t, body, "server_id")
		require.Equal(t, "web03.test", body["server_name"])
	})
}
