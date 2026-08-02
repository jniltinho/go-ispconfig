package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// jsonHandler returns an httptest server that records the request and
// replies with the given raw body.
func jsonHandler(t *testing.T, status int, body string, gotReq *http.Request, gotBody *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotReq != nil {
			*gotReq = *r
		}
		if gotBody != nil {
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			*gotBody = b
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https URL", "https://panel.example.com:8080", false},
		{"http URL", "http://panel.example.com", false},
		{"trailing slash trimmed", "https://panel.example.com/", false},
		{"missing scheme", "panel.example.com", true},
		{"unsupported scheme", "ftp://panel.example.com", true},
		{"empty", "", true},
		{"credentials in URL rejected", "https://user:pw@panel.example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(Options{URL: tt.url})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotContains(t, c.endpoint, "//remote")
			require.True(t, len(c.endpoint) > len("/remote/json.php"))
			require.Equal(t, "/remote/json.php", c.endpoint[len(c.endpoint)-len("/remote/json.php"):])
		})
	}
}

func TestCallTransport(t *testing.T) {
	t.Run("posts method in query and named params in body", func(t *testing.T) {
		var req http.Request
		var body []byte
		srv := jsonHandler(t, http.StatusOK, `{"code":"ok","message":"","response":true}`, &req, &body)

		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		c.sessionID = "stestsession"
		require.NoError(t, c.call(context.Background(), "dns_zone_get", map[string]any{"primary_id": -1}, nil))

		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "/remote/json.php", req.URL.Path)
		require.Equal(t, "dns_zone_get", req.URL.RawQuery)
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		var params map[string]any
		require.NoError(t, json.Unmarshal(body, &params))
		require.Equal(t, float64(-1), params["primary_id"])
	})

	t.Run("nil params send an empty JSON object", func(t *testing.T) {
		var body []byte
		srv := jsonHandler(t, http.StatusOK, `{"code":"ok","message":"","response":true}`, nil, &body)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		require.NoError(t, c.call(context.Background(), "login", nil, nil))
		require.JSONEq(t, `{}`, string(body))
	})

	t.Run("ok response decodes payload", func(t *testing.T) {
		srv := jsonHandler(t, http.StatusOK, `{"code":"ok","message":"","response":{"domain":"example.com","extra_unknown":"x"}}`, nil, nil)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		c.sessionID = "stestsession"
		var out struct {
			Domain string `json:"domain"`
		}
		require.NoError(t, c.call(context.Background(), "sites_web_domain_get", nil, &out))
		require.Equal(t, "example.com", out.Domain)
	})

	t.Run("false response leaves out untouched", func(t *testing.T) {
		srv := jsonHandler(t, http.StatusOK, `{"code":"ok","message":"","response":false}`, nil, nil)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		c.sessionID = "stestsession"
		var out []string
		require.NoError(t, c.call(context.Background(), "client_get_all", nil, &out))
		require.Empty(t, out)
	})
}

func TestCallFaults(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
		wantMsg  string
	}{
		{
			"remote fault",
			`{"code":"remote_fault","message":"The login failed. Username or password wrong.","response":false}`,
			"remote_fault",
			"The login failed. Username or password wrong.",
		},
		{
			"permission denied",
			`{"code":"permission_denied","message":"You do not have the permissions to access this function.","response":false}`,
			"permission_denied",
			"You do not have the permissions to access this function.",
		},
		{
			"invalid method",
			`{"code":"invalid_method","message":"Method foo does not exist","response":false}`,
			"invalid_method",
			"Method foo does not exist",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := jsonHandler(t, http.StatusOK, tt.body, nil, nil)
			c, err := New(Options{URL: srv.URL})
			require.NoError(t, err)
			c.sessionID = "stestsession"

			err = c.call(context.Background(), "some_method", nil, nil)
			f, ok := IsFault(err)
			require.True(t, ok, "expected a *Fault, got %v", err)
			require.Equal(t, tt.wantCode, f.Code)
			require.Equal(t, tt.wantMsg, f.Message)
			require.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestCallTransportErrors(t *testing.T) {
	t.Run("non-2xx status is a transport error, not a fault", func(t *testing.T) {
		srv := jsonHandler(t, http.StatusBadGateway, "Bad Gateway", nil, nil)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		err = c.call(context.Background(), "login", nil, nil)
		require.Error(t, err)
		_, ok := IsFault(err)
		require.False(t, ok)
		require.Contains(t, err.Error(), "502")
	})

	t.Run("non-JSON body is a transport error with a snippet", func(t *testing.T) {
		srv := jsonHandler(t, http.StatusOK, "Remote API is disabled in security settings.", nil, nil)
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		err = c.call(context.Background(), "login", nil, nil)
		require.Error(t, err)
		_, ok := IsFault(err)
		require.False(t, ok)
		require.Contains(t, err.Error(), "Remote API is disabled")
	})

	t.Run("unreachable endpoint is a transport error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close() // now unreachable
		c, err := New(Options{URL: srv.URL})
		require.NoError(t, err)
		err = c.call(context.Background(), "login", nil, nil)
		require.Error(t, err)
		_, ok := IsFault(err)
		require.False(t, ok)
	})
}
