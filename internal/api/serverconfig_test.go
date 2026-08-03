package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// serverConfigDB gives server 2 a config with a section the panel renders
// (web) and a key it does not know about (web.custom_key), plus a second
// section that must survive an unrelated write.
func serverConfigDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := serverRegistryDB(t)
	require.NoError(t, db.Exec("ALTER TABLE server ADD COLUMN config TEXT").Error)
	require.NoError(t, db.Exec("UPDATE server SET config = ? WHERE server_id = 2",
		"[web]\nwebsite_basedir=/var/www\ncustom_key=keep-me\n\n[dns]\ndns_backend=bind\n").Error)
	return db
}

// serveServerConfig routes one request through the real handlers, skipping
// the admin guard (covered by requireAdmin's own test).
func serveServerConfig(t *testing.T, d *Deps, method, path, body string) (int, []byte) {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	e.GET("/api/servers/:id/config", serverConfigGetHandler(d))
	e.GET("/api/servers/:id/config/:section", serverConfigSectionHandler(d))
	e.PUT("/api/servers/:id/config/:section", serverConfigSaveHandler(d))

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func TestServerConfigAPI(t *testing.T) {
	d := &Deps{DB: serverConfigDB(t)}

	t.Run("reads one section", func(t *testing.T) {
		code, body := serveServerConfig(t, d, http.MethodGet, "/api/servers/2/config/web", "")
		require.Equal(t, http.StatusOK, code, "%s", body)
		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, "/var/www", got["website_basedir"])
	})

	t.Run("absent section reads empty, missing server is 404", func(t *testing.T) {
		code, body := serveServerConfig(t, d, http.MethodGet, "/api/servers/2/config/mail", "")
		require.Equal(t, http.StatusOK, code)
		require.JSONEq(t, "{}", string(body))

		code, _ = serveServerConfig(t, d, http.MethodGet, "/api/servers/99/config/web", "")
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("empty section is refused", func(t *testing.T) {
		code, _ := serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web", "{}")
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("names the INI grammar cannot express are refused", func(t *testing.T) {
		// Writing these would round trip into nothing, silently deleting
		// config on the next save.
		code, _ := serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/we-b", `{"a":"b"}`)
		require.Equal(t, http.StatusBadRequest, code, "section name")
		code, _ = serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web", `{"a b":"c"}`)
		require.Equal(t, http.StatusBadRequest, code, "key name")
		code, _ = serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web", `{"a":"b\n[evil]\nx=1"}`)
		require.Equal(t, http.StatusBadRequest, code, "newline injects a section")
	})

	t.Run("path params never leak into the section", func(t *testing.T) {
		code, body := serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web",
			`{"website_basedir":"/var/www"}`)
		require.Equal(t, http.StatusOK, code)
		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))
		require.NotContains(t, got, "id")
		require.NotContains(t, got, "section")
	})

	t.Run("write merges and keeps unknown keys and other sections", func(t *testing.T) {
		code, body := serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web",
			`{"website_basedir":"/srv/www"}`)
		require.Equal(t, http.StatusOK, code, "%s", body)

		var srv model.Server
		require.NoError(t, d.DB.Select("COALESCE(config, '') AS config").Take(&srv, 2).Error)
		stored := getconf.ParseINI(srv.Config)
		require.Equal(t, "/srv/www", stored["web"]["website_basedir"], "submitted key applied")
		require.Equal(t, "keep-me", stored["web"]["custom_key"], "unknown key survives the round trip")
		require.Equal(t, "bind", stored["dns"]["dns_backend"], "other sections untouched")
	})

	serverRows := func() []model.SysDatalog {
		var rows []model.SysDatalog
		require.NoError(t, d.DB.Where("dbtable = ?", "server").Order("datalog_id").Find(&rows).Error)
		return rows
	}

	t.Run("the write is journaled against that server", func(t *testing.T) {
		rows := serverRows()
		require.NotEmpty(t, rows)
		last := rows[len(rows)-1]
		require.EqualValues(t, 2, last.ServerID)
		require.Equal(t, "server_id:2", last.DBIdx)
		require.Equal(t, "u", last.Action)
		require.Contains(t, last.Data, "/srv/www")
	})

	t.Run("an unchanged write journals nothing new", func(t *testing.T) {
		before := len(serverRows())
		code, _ := serveServerConfig(t, d, http.MethodPut, "/api/servers/2/config/web",
			`{"website_basedir":"/srv/www"}`)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, serverRows(), before, "identical INI must not emit a datalog row")
	})
}
