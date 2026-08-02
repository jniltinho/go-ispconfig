package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/legacy/legacytest"
)

// noDB fails the test if the local database is opened: connection-stage
// failures must abort before any local access.
func noDB(t *testing.T) func() (*gorm.DB, error) {
	return func() (*gorm.DB, error) {
		t.Fatal("openDB must not be called on this failure path")
		return nil, nil
	}
}

// run invokes the migrate-from flow against the mock, capturing output.
func run(t *testing.T, opts migrateFromOpts, openDB func() (*gorm.DB, error)) (string, string, error) {
	t.Helper()
	var out, errw strings.Builder
	err := runMigrateFrom(context.Background(), opts, openDB, &out, &errw)
	return out.String(), errw.String(), err
}

func TestParseSelection(t *testing.T) {
	tests := []struct {
		only    string
		wantErr bool
		clients bool
		sites   bool
		dns     bool
	}{
		{"clients,sites,dns", false, true, true, true},
		{"clients,dns", false, true, false, true},
		{"dns", false, false, false, true},
		{" sites , dns ", false, false, true, true},
		{"", true, false, false, false},
		{"mail", true, false, false, false},
	}
	for _, tt := range tests {
		sel, err := parseSelection(tt.only)
		if tt.wantErr {
			require.Error(t, err, tt.only)
			continue
		}
		require.NoError(t, err, tt.only)
		require.Equal(t, tt.clients, sel.Clients)
		require.Equal(t, tt.sites, sel.Sites)
		require.Equal(t, tt.dns, sel.DNS)
	}
}

func TestMigrateFromLoginFailure(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)

	out, errw, err := run(t, migrateFromOpts{
		url: s.URL, user: s.Username, password: "wrong-pw-value", only: "clients",
	}, noDB(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "legacy login failed")
	require.Contains(t, err.Error(), "Username or password wrong")

	// Redaction: the password never appears anywhere.
	for _, text := range []string{out, errw, err.Error()} {
		require.NotContains(t, text, "wrong-pw-value")
	}
}

func TestMigrateFromMissingGrants(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	var granted []string
	for _, fn := range s.Functions {
		if fn != "dns_zone_get" && fn != "sites_web_domain_get" {
			granted = append(granted, fn)
		}
	}
	s.Functions = granted

	_, _, err := run(t, migrateFromOpts{
		url: s.URL, user: s.Username, password: s.Password, only: "clients,sites,dns",
	}, noDB(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "dns_zone_get")
	require.Contains(t, err.Error(), "sites_web_domain_get")

	// The preflight failed before any data fetch.
	for _, call := range s.Calls {
		require.NotContains(t, []string{"client_get", "client_get_all", "sites_web_domain_get", "dns_zone_get"},
			call.Method, "no data may be fetched after a failed preflight")
	}
}

func TestMigrateFromMultiServerGuard(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	s.Servers = append(s.Servers, legacytest.Rec{"server_id": "2", "server_name": "legacy2"})

	t.Run("aborts without the confirmation flag", func(t *testing.T) {
		_, _, err := run(t, migrateFromOpts{
			url: s.URL, user: s.Username, password: s.Password, only: "clients",
		}, noDB(t))
		require.Error(t, err)
		require.Contains(t, err.Error(), "legacy1")
		require.Contains(t, err.Error(), "legacy2")
		require.Contains(t, err.Error(), "--map-all-to-local-server")
	})

	t.Run("explicit flag proceeds past the guard", func(t *testing.T) {
		dbOpened := false
		_, _, err := run(t, migrateFromOpts{
			url: s.URL, user: s.Username, password: s.Password, only: "clients",
			mapAllToLocal: true,
		}, func() (*gorm.DB, error) {
			dbOpened = true
			return nil, context.Canceled // stop the flow here
		})
		require.Error(t, err)
		require.True(t, dbOpened, "with the flag the flow must reach the local database stage")
	})
}

func TestMigrateFromEntitySubset(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)

	// Selection clients,dns: sites must never be fetched. The flow stops
	// at the (test-stubbed) database.
	_, _, err := run(t, migrateFromOpts{
		url: s.URL, user: s.Username, password: s.Password, only: "clients,dns",
	}, func() (*gorm.DB, error) { return nil, context.Canceled })
	require.Error(t, err)
	for _, call := range s.Calls {
		require.NotContains(t, call.Method, "sites_", "sites must not be fetched with --only clients,dns")
	}
}

func TestMigrateFromInsecureWarnings(t *testing.T) {
	t.Run("insecure flag warns and connects to untrusted TLS", func(t *testing.T) {
		s := legacytest.NewTLS()
		t.Cleanup(s.Close)
		_, errw, err := run(t, migrateFromOpts{
			url: s.URL, user: s.Username, password: s.Password, only: "clients", insecure: true,
		}, func() (*gorm.DB, error) { return nil, context.Canceled })
		require.Error(t, err) // stopped at the stubbed database, after the legacy stage
		require.Contains(t, errw, "TLS certificate verification is DISABLED")
	})

	t.Run("untrusted TLS without the flag fails", func(t *testing.T) {
		s := legacytest.NewTLS()
		t.Cleanup(s.Close)
		_, errw, err := run(t, migrateFromOpts{
			url: s.URL, user: s.Username, password: s.Password, only: "clients",
		}, noDB(t))
		require.Error(t, err)
		require.NotContains(t, errw, "DISABLED")
	})

	t.Run("plain http warns", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		_, errw, err := run(t, migrateFromOpts{
			url: s.URL, user: s.Username, password: s.Password, only: "clients",
		}, func() (*gorm.DB, error) { return nil, context.Canceled })
		require.Error(t, err)
		require.Contains(t, errw, "plain http")
	})
}
