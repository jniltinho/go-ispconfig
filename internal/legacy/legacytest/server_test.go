package legacytest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/legacy/legacytest"
)

// connect returns a logged-in client for the mock panel.
func connect(t *testing.T, s *legacytest.Server) *client.Client {
	t.Helper()
	c, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: s.Password})
	require.NoError(t, err)
	require.NoError(t, c.Login(context.Background()))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// callCount counts recorded calls of one method.
func callCount(s *legacytest.Server, method string) int {
	n := 0
	for _, call := range s.Calls {
		if call.Method == method {
			n++
		}
	}
	return n
}

func TestMockFullFlow(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	ctx := context.Background()
	c := connect(t, s)

	require.NoError(t, c.Preflight(ctx))

	ids, err := c.ClientGetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, ids)

	reseller, err := c.ClientGet(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "reseller1", reseller["username"])
	require.Equal(t, 0, reseller.Int("parent_client_id"))

	child, err := c.ClientGet(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, 1, child.Int("parent_client_id"), "reseller hierarchy fixture")

	domains, err := c.SitesWebDomainGet(ctx, client.Filter{"type": "vhost"})
	require.NoError(t, err)
	require.Len(t, domains, 1200)
	require.Equal(t, 3, callCount(s, "sites_web_domain_get"),
		"1200 vhosts at page size 500 must take exactly 3 paged calls")
	require.Equal(t, "y", domains[0]["ssl"])
	require.Equal(t, "y", domains[0]["ssl_letsencrypt"])

	subs, err := c.SitesWebDomainGet(ctx, client.Filter{"type": "vhostsubdomain"})
	require.NoError(t, err)
	require.Len(t, subs, 1)

	folders, err := c.SitesWebFolderGet(ctx, nil)
	require.NoError(t, err)
	require.Len(t, folders, 1)

	folderUsers, err := c.SitesWebFolderUserGet(ctx, nil)
	require.NoError(t, err)
	require.Len(t, folderUsers, 1)
	require.Equal(t, legacytest.Hash6, folderUsers[0]["password"],
		"crypt $6$ hash must arrive verbatim")

	zones, err := c.DNSZoneGetAll(ctx)
	require.NoError(t, err)
	require.Len(t, zones, 2)
	require.Equal(t, "example.com.", zones[0]["origin"])

	total := 0
	for _, zone := range zones {
		rrs, err := c.DNSRRGetAllByZone(ctx, zone.Int("id"))
		require.NoError(t, err)
		for _, rr := range rrs {
			require.Equal(t, zone["id"], rr["zone"])
		}
		total += len(rrs)
	}
	require.Equal(t, 4, total)

	slaves, err := c.DNSSlaveGetAll(ctx)
	require.NoError(t, err)
	require.Len(t, slaves, 1)

	templates, err := c.DNSTemplateZoneGetAll(ctx)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	servers, err := c.ServerGetAll(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, "legacy1", servers[0]["server_name"])

	conf, err := c.ServerGet(ctx, 1, "server")
	require.NoError(t, err)
	require.Equal(t, "legacy1.example.com", conf["hostname"])
}

func TestMockFilters(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	ctx := context.Background()
	c := connect(t, s)

	t.Run("equality filter", func(t *testing.T) {
		got, err := c.SitesWebDomainGet(ctx, client.Filter{"domain": "site42.example.com"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "42", got[0]["domain_id"])
	})

	t.Run("LIKE filter", func(t *testing.T) {
		got, err := c.SitesWebDomainGet(ctx, client.Filter{"domain": "site1200.%"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "site1200.example.com", got[0]["domain"])
	})
}

func TestMockLoginFaults(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong password", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		c, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: "nope"})
		require.NoError(t, err)
		err = c.Login(ctx)
		f, ok := client.IsFault(err)
		require.True(t, ok)
		require.Equal(t, "remote_fault", f.Code)
		require.Contains(t, f.Message, "Username or password wrong")
		require.Equal(t, 1, s.LoginAttempts)
	})

	t.Run("attempts limit reached", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		s.LoginAttempts = 10
		c, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: s.Password})
		require.NoError(t, err)
		err = c.Login(ctx)
		f, ok := client.IsFault(err)
		require.True(t, ok)
		require.Contains(t, f.Message, "login failure limit")
	})

	t.Run("ten wrong logins trip the limit", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		bad, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: "nope"})
		require.NoError(t, err)
		for i := 0; i < 10; i++ {
			require.Error(t, bad.Login(ctx))
		}
		good, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: s.Password})
		require.NoError(t, err)
		err = good.Login(ctx)
		f, ok := client.IsFault(err)
		require.True(t, ok)
		require.Contains(t, f.Message, "login failure limit")
	})
}

func TestMockSessions(t *testing.T) {
	ctx := context.Background()

	t.Run("expired session", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		c := connect(t, s)
		for sid := range s.Sessions {
			delete(s.Sessions, sid)
		}
		_, err := c.ClientGetAll(ctx)
		f, ok := client.IsFault(err)
		require.True(t, ok)
		require.Contains(t, f.Message, "Session is expired")
	})

	t.Run("logout invalidates the session", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		c := connect(t, s)
		require.Len(t, s.Sessions, 1)
		require.NoError(t, c.Logout(ctx))
		require.Empty(t, s.Sessions)
	})
}

func TestMockGrants(t *testing.T) {
	ctx := context.Background()

	t.Run("preflight reports missing grants", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		var granted []string
		for _, fn := range s.Functions {
			if fn != "dns_zone_get" {
				granted = append(granted, fn)
			}
		}
		s.Functions = granted
		c := connect(t, s)

		err := c.Preflight(ctx)
		var missErr *client.MissingGrantsError
		require.ErrorAs(t, err, &missErr)
		require.Equal(t, []string{"dns_zone_get"}, missErr.Missing)
	})

	t.Run("ungranted method faults", func(t *testing.T) {
		s := legacytest.New()
		t.Cleanup(s.Close)
		s.Functions = []string{"get_function_list"}
		c := connect(t, s)
		_, err := c.ClientGetAll(ctx)
		f, ok := client.IsFault(err)
		require.True(t, ok)
		require.Contains(t, f.Message, "permissions")
	})
}

func TestMockTLS(t *testing.T) {
	ctx := context.Background()
	s := legacytest.NewTLS()
	t.Cleanup(s.Close)

	t.Run("untrusted certificate rejected by default", func(t *testing.T) {
		c, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: s.Password})
		require.NoError(t, err)
		require.Error(t, c.Login(ctx))
	})

	t.Run("insecure override connects", func(t *testing.T) {
		c, err := client.New(client.Options{URL: s.URL, Username: s.Username, Password: s.Password, Insecure: true})
		require.NoError(t, err)
		require.NoError(t, c.Login(ctx))
		require.True(t, c.Insecure())
	})
}

func TestMockMultiServerFixture(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	s.Servers = append(s.Servers, legacytest.Rec{"server_id": "2", "server_name": "legacy2"})
	c := connect(t, s)

	servers, err := c.ServerGetAll(context.Background())
	require.NoError(t, err)
	require.Len(t, servers, 2, "engine tests can extend the fixture to a multi-server panel")
}
