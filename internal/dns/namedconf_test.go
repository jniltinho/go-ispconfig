package dns

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func soaZone(origin, xfer, alsoNotify, updateACL, dnssec string) map[string]any {
	return map[string]any{
		"origin": origin, "xfer": xfer, "also_notify": alsoNotify,
		"update_acl": updateACL, "dnssec_wanted": dnssec,
	}
}

func always(string) bool { return true }

func TestPrimaryZoneRowsOptions(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	rows := primaryZoneRows(cfg, []map[string]any{
		soaZone("example.com.", "1.2.3.4,5.6.7.8", "", "", "N"),
	}, always)
	require.Len(t, rows, 1)
	assert.Equal(t, "example.com", rows[0]["zone"])
	assert.Equal(t, "/etc/bind/pri.example.com", rows[0]["zonefile_path"])
	assert.Equal(t, "        allow-transfer {1.2.3.4;5.6.7.8;};\n", rows[0]["options"])
}

func TestPrimaryZoneRowsSignedPathAndFullOptions(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	rows := primaryZoneRows(cfg, []map[string]any{
		soaZone("example.org.", "10.0.0.1", "10.0.0.2", "10.0.0.3", "Y"),
	}, always)
	require.Len(t, rows, 1)
	assert.Equal(t, "/etc/bind/pri.example.org.signed", rows[0]["zonefile_path"])
	assert.Equal(t,
		"        allow-transfer {10.0.0.1;};\n"+
			"        also-notify {10.0.0.2;};\n"+
			"        allow-update {10.0.0.3;};\n",
		rows[0]["options"])
}

// TestPrimaryZoneRowsSkipsMissingFile covers "Recordless zone excluded":
// a zone whose file does not exist on disk never reaches named.conf.local.
func TestPrimaryZoneRowsSkipsMissingFile(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	rows := primaryZoneRows(cfg, []map[string]any{
		soaZone("norecords.com.", "", "", "", "N"),
		soaZone("example.com.", "", "", "", "N"),
	}, func(path string) bool { return path == "/etc/bind/pri.example.com" })
	require.Len(t, rows, 1)
	assert.Equal(t, "example.com", rows[0]["zone"])
}

func TestSecondaryZoneRows(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	rows := secondaryZoneRows(cfg, []map[string]any{
		{"origin": "customer.net.", "ns": "192.168.10.20,192.168.10.21", "xfer": ""},
		{"origin": "other.net.", "ns": "192.168.10.20", "xfer": "10.0.0.9"},
	})
	require.Len(t, rows, 2)
	assert.Equal(t, "/etc/bind/slave/sec.customer.net", rows[0]["zonefile_path"])
	assert.Equal(t,
		"        masters {192.168.10.20;192.168.10.21;};\n"+
			"        allow-transfer {none;};\n",
		rows[0]["options"])
	assert.Equal(t,
		"        masters {192.168.10.20;};\n"+
			"        allow-transfer {10.0.0.9;};\n",
		rows[1]["options"])
}

// TestGoldenNamedConfLocal renders a realistic primary+slave mix and
// compares byte-for-byte against the PHP-shaped golden file.
func TestGoldenNamedConfLocal(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	primary := primaryZoneRows(cfg, []map[string]any{
		soaZone("example.com.", "192.168.10.6,192.168.10.7", "192.168.10.6", "", "N"),
		soaZone("example.org.", "", "", "", "Y"),
		soaZone("dynamic.example.", "", "", "10.0.0.5", "N"),
	}, always)
	secondary := secondaryZoneRows(cfg, []map[string]any{
		{"origin": "customer.net.", "ns": "192.168.10.20", "xfer": ""},
		{"origin": "partner.example.", "ns": "192.168.10.30,192.168.10.31", "xfer": "192.168.10.6"},
	})
	got, err := renderNamedConf(primary, secondary, "")
	require.NoError(t, err)
	checkGolden(t, "named_conf_local", got)
}

func TestAtomicWrite(t *testing.T) {
	path := t.TempDir() + "/named.conf.local"
	require.NoError(t, atomicWrite(path, "zone content\n"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "zone content\n", string(data))
	require.NoError(t, atomicWrite(path, "v2\n"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "v2\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}
