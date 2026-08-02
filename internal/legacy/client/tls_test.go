package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// tlsLoginServer is an httptest TLS server (self-signed, untrusted cert)
// that always answers a successful login.
func tlsLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"ok","message":"","response":"` + testSession + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTLS(t *testing.T) {
	t.Run("untrusted certificate rejected by default", func(t *testing.T) {
		srv := tlsLoginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)

		err = c.Login(context.Background())
		require.Error(t, err)
		_, isFault := IsFault(err)
		require.False(t, isFault, "certificate failure must be a transport error, not a legacy fault")
		require.Contains(t, err.Error(), "certificate")
		require.False(t, c.Insecure())
		require.False(t, c.PlainHTTP())
	})

	t.Run("insecure override connects and marks the session", func(t *testing.T) {
		srv := tlsLoginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword, Insecure: true})
		require.NoError(t, err)

		require.NoError(t, c.Login(context.Background()))
		require.True(t, c.Insecure(), "insecure marker must be surfaced for reporting")
		require.False(t, c.PlainHTTP())
	})

	t.Run("plain http is marked for warning", func(t *testing.T) {
		srv, _, _ := loginServer(t)
		c, err := New(Options{URL: srv.URL, Username: "migrator", Password: testPassword})
		require.NoError(t, err)

		require.NoError(t, c.Login(context.Background()))
		require.True(t, c.PlainHTTP())
		require.False(t, c.Insecure())
	})
}
