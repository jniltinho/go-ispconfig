package clients

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupCSVHelpers(t *testing.T) {
	tests := []struct {
		name string
		csv  string
		id   uint32
		add  string
		del  string
	}{
		{"append to list", "1,5", 7, "1,5,7", "1,5"},
		{"already present", "1,5,7", 7, "1,5,7", "1,5"},
		{"empty list", "", 7, "7", ""},
		{"messy spacing", " 1, ,5 ", 7, "1,5,7", "1,5"},
		{"no substring confusion", "12,112", 12, "12,112", "112"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.add, addGroupToCSV(tt.csv, tt.id))
			require.Equal(t, tt.del, removeGroupFromCSV(tt.csv, tt.id))
		})
	}
}

func TestModuleHelpers(t *testing.T) {
	require.True(t, hasModule("dashboard,sites,client", "client"))
	require.False(t, hasModule("dashboard,sites", "client"))
	require.False(t, hasModule("clientx,dashboard", "client"))
	require.Equal(t, "dashboard,sites", removeModule("dashboard,client,sites", "client"))
	require.Equal(t, "dashboard", removeModule("dashboard", "client"))
}

// The nav filters tabs by this CSV, so an omission here silently hides a
// whole module from every client that owns records in it.
func TestInterfaceModulesGrantsClientOwnedModules(t *testing.T) {
	for _, m := range []string{"dashboard", "mail", "sites", "dns", "tools", "help"} {
		require.True(t, hasModule(InterfaceModules, m), "module %q must be granted", m)
	}
	require.False(t, hasModule(InterfaceModules, "client"), "reseller-only")
	require.False(t, hasModule(InterfaceModules, "admin"))
}
