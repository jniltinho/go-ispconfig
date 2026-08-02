package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testPassword = "sup3r-secret-pw"
	testSession  = "s0123456789abcdef0123456789abcdef012345678"
)

// loginServer mocks a panel that accepts testPassword and records every
// request method and decoded body.
func loginServer(t *testing.T) (*httptest.Server, *[]string, *[]map[string]any) {
	t.Helper()
	var methods []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var params map[string]any
		require.NoError(t, json.Unmarshal(raw, &params))
		methods = append(methods, r.URL.RawQuery)
		bodies = append(bodies, params)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RawQuery {
		case "login":
			if params["password"] == testPassword {
				_, _ = w.Write([]byte(`{"code":"ok","message":"","response":"` + testSession + `"}`))
			} else {
				_, _ = w.Write([]byte(`{"code":"remote_fault","message":"The login failed. Username or password wrong.","response":false}`))
			}
		case "logout":
			_, _ = w.Write([]byte(`{"code":"ok","message":"","response":true}`))
		default:
			_, _ = w.Write([]byte(`{"code":"ok","message":"","response":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &methods, &bodies
}

func TestLogin(t *testing.T) {
	t.Run("success stores session and injects it into calls", func(t *testing.T) {
		srv, methods, bodies := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)

		require.NoError(t, c.Login(context.Background()))
		require.Equal(t, testSession, c.sessionID)

		require.NoError(t, c.call(context.Background(), "dns_zone_get", map[string]any{"primary_id": -1}, nil))
		require.Equal(t, []string{"login", "dns_zone_get"}, *methods)
		require.Equal(t, testSession, (*bodies)[1]["session_id"])
	})

	t.Run("failure returns the fault without retrying", func(t *testing.T) {
		srv, methods, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: "wrong-pw"})
		require.NoError(t, err)

		err = c.Login(context.Background())
		f, ok := IsFault(err)
		require.True(t, ok)
		require.Equal(t, "remote_fault", f.Code)
		require.Len(t, *methods, 1, "login must not be retried")
		require.Empty(t, c.sessionID)
	})

	t.Run("empty session id is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":"ok","message":"","response":""}`))
		}))
		t.Cleanup(srv.Close)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)
		require.Error(t, c.Login(context.Background()))
	})

	t.Run("call before login returns ErrNotLoggedIn", func(t *testing.T) {
		srv, methods, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)
		err = c.call(context.Background(), "dns_zone_get", nil, nil)
		require.ErrorIs(t, err, ErrNotLoggedIn)
		require.Empty(t, *methods, "no request may be sent without a session")
	})
}

func TestLogout(t *testing.T) {
	t.Run("close logs out with the stored session id", func(t *testing.T) {
		srv, methods, bodies := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)
		require.NoError(t, c.Login(context.Background()))

		require.NoError(t, c.Close())
		require.Equal(t, []string{"login", "logout"}, *methods)
		require.Equal(t, testSession, (*bodies)[1]["session_id"])
		require.Empty(t, c.sessionID)
	})

	t.Run("logout without login is a no-op", func(t *testing.T) {
		srv, methods, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)
		require.NoError(t, c.Logout(context.Background()))
		require.Empty(t, *methods)
	})
}

// TestRedaction is the hard requirement of the spec's credential-hygiene
// scenario: no error string may ever contain the password or session id.
func TestRedaction(t *testing.T) {
	collect := func(errs ...error) string {
		var sb strings.Builder
		for _, err := range errs {
			if err != nil {
				sb.WriteString(err.Error())
				sb.WriteString("\n")
			}
		}
		return sb.String()
	}

	t.Run("login fault", func(t *testing.T) {
		srv, _, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: "wrong-" + testPassword})
		require.NoError(t, err)
		out := collect(c.Login(context.Background()))
		require.NotContains(t, out, "wrong-"+testPassword)
	})

	t.Run("transport error after login", func(t *testing.T) {
		srv, _, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)
		require.NoError(t, c.Login(context.Background()))
		srv.Close() // force a transport error carrying the request context

		out := collect(
			c.call(context.Background(), "dns_zone_get", nil, nil),
			c.Logout(context.Background()),
		)
		require.NotContains(t, out, testPassword)
		require.NotContains(t, out, testSession)
	})

	t.Run("fault errors carry no session id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":"remote_fault","message":"The Session is expired or does not exist.","response":false}`))
		}))
		t.Cleanup(srv.Close)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		c.sessionID = testSession
		err = c.call(context.Background(), "dns_zone_get", nil, nil)
		var f *Fault
		require.True(t, errors.As(err, &f))
		require.NotContains(t, err.Error(), testSession)
	})
}
