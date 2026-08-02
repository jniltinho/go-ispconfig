package importer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/legacy/importer"
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

func TestFetchSnapshotAndInventory(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	c := connect(t, s)

	snap, err := importer.FetchSnapshot(context.Background(),
		c, importer.Selection{Clients: true, Sites: true, DNS: true})
	require.NoError(t, err)

	inv := snap.Inventory()
	require.Equal(t, 3, inv.Clients)
	require.Equal(t, 1201, inv.WebDomains, "1200 vhosts + 1 vhostsubdomain")
	require.Equal(t, 1, inv.WebFolders)
	require.Equal(t, 1, inv.WebFolderUsers)
	require.Equal(t, 2, inv.DNSZones)
	require.Equal(t, 4, inv.DNSRecords)
	require.Equal(t, 1, inv.DNSSlaveZones)
	require.Equal(t, 1, inv.DNSTemplates)
	require.Len(t, inv.Servers, 1)
	require.False(t, inv.MultiServer)

	// vhost pass comes before the subdomain pass (parents first).
	require.Equal(t, "vhost", snap.Domains[0]["type"])
	require.Equal(t, "vhostsubdomain", snap.Domains[1200]["type"])
	require.Equal(t, "reseller1", snap.Clients[1]["username"])
	require.Len(t, snap.RRs[1], 3)
	require.Len(t, snap.RRs[2], 1)
}

func TestFetchSnapshotSelection(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	c := connect(t, s)

	snap, err := importer.FetchSnapshot(context.Background(),
		c, importer.Selection{DNS: true})
	require.NoError(t, err)

	require.Len(t, snap.Clients, 3, "clients always fetched for owner resolution")
	require.Empty(t, snap.Domains, "sites not selected")
	require.Empty(t, snap.Folders)
	require.Len(t, snap.Zones, 2)

	// No sites method may have been called.
	for _, call := range s.Calls {
		require.NotContains(t, call.Method, "sites_", "deselected entities must not be fetched")
	}
}

func TestInventoryMultiServer(t *testing.T) {
	s := legacytest.New()
	t.Cleanup(s.Close)
	s.Servers = append(s.Servers, legacytest.Rec{"server_id": "2", "server_name": "legacy2"})
	c := connect(t, s)

	snap, err := importer.FetchSnapshot(context.Background(), c, importer.Selection{})
	require.NoError(t, err)
	require.True(t, snap.Inventory().MultiServer, "two servers must raise the multi-server guard flag")
}
