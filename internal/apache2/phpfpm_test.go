package apache2

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// TestManagePoolSocketOwnership: Apache dials the pool socket as the web
// server user, so the pool must declare listen.owner/group/mode and the
// socket's parent dir has to exist — PHP-FPM refuses to start without it,
// taking every other pool on the host down.
func TestManagePoolSocketOwnership(t *testing.T) {
	base := t.TempDir()
	poolDir := filepath.Join(base, "etc/php/8.4/fpm/pool.d")
	socketDir := filepath.Join(base, "var/lib/php8.4-fpm")

	p := NewPlugin(nil, nil, nil, "", nil)
	cfg := &getconf.WebConfig{SecurityLevel: "20", User: "www-data", Group: "www-data"}
	d := row{
		"domain_id": float64(1), "domain": "example.com",
		"document_root": filepath.Join(base, "web1"),
		"system_user":   "web1", "system_group": "client1",
		"php": "php-fpm", "php_fpm_use_socket": "y", "pm": "ondemand",
	}
	fpm := fpmInfo{
		poolName: "web1", poolDir: poolDir, socketDir: socketDir,
		socketPath: socketDir + "/web1.sock", useSocket: true,
		initScript: "php8.4-fpm",
	}

	require.NoError(t, p.managePool(cfg, d, fpm))

	require.DirExists(t, socketDir)
	out, err := os.ReadFile(filepath.Join(poolDir, "web1.conf"))
	require.NoError(t, err)
	require.Contains(t, string(out), "listen.owner = web1")
	require.Contains(t, string(out), "listen.group = www-data")
	require.Contains(t, string(out), "listen.mode = 0660")
}
