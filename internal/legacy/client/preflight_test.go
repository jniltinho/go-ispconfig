package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// preflightClient returns a logged-in client whose panel serves the given
// get_function_list response body (raw JSON) for every request.
func preflightClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{URL: srv.URL})
	require.NoError(t, err)
	c.sessionID = testSession
	return c
}

func TestPreflight(t *testing.T) {
	t.Run("all required functions granted", func(t *testing.T) {
		granted, err := json.Marshal(append([]string{"login", "logout", "get_function_list", "mail_domain_get"},
			RequiredFunctions...))
		require.NoError(t, err)
		c := preflightClient(t, `{"code":"ok","message":"","response":`+string(granted)+`}`)
		require.NoError(t, c.Preflight(context.Background()))
	})

	t.Run("missing functions listed by exact name", func(t *testing.T) {
		var subset []string
		for _, fn := range RequiredFunctions {
			if fn != "dns_zone_get" && fn != "sites_web_domain_get" {
				subset = append(subset, fn)
			}
		}
		granted, err := json.Marshal(subset)
		require.NoError(t, err)
		c := preflightClient(t, `{"code":"ok","message":"","response":`+string(granted)+`}`)

		err = c.Preflight(context.Background())
		var missErr *MissingGrantsError
		require.ErrorAs(t, err, &missErr)
		require.Equal(t, []string{"sites_web_domain_get", "dns_zone_get"}, missErr.Missing)
		require.Contains(t, err.Error(), "dns_zone_get")
		require.Contains(t, err.Error(), "sites_web_domain_get")
	})

	t.Run("empty grant list misses everything", func(t *testing.T) {
		c := preflightClient(t, `{"code":"ok","message":"","response":[]}`)
		err := c.Preflight(context.Background())
		var missErr *MissingGrantsError
		require.ErrorAs(t, err, &missErr)
		require.Equal(t, RequiredFunctions, missErr.Missing)
	})

	t.Run("fault from get_function_list propagates", func(t *testing.T) {
		c := preflightClient(t,
			`{"code":"remote_fault","message":"You do not have the permissions to access this function.","response":false}`)
		err := c.Preflight(context.Background())
		f, ok := IsFault(err)
		require.True(t, ok)
		require.Equal(t, "remote_fault", f.Code)
	})

	t.Run("preflight requires a session", func(t *testing.T) {
		c := preflightClient(t, `{"code":"ok","message":"","response":[]}`)
		c.sessionID = ""
		require.ErrorIs(t, c.Preflight(context.Background()), ErrNotLoggedIn)
	})
}
