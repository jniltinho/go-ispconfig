package installer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ispconfig/init/systemd"
)

func TestEmbeddedAssets(t *testing.T) {
	assert.Contains(t, namedConfOptions, `directory "/var/cache/bind";`)
	assert.Contains(t, namedConfOptions, "allow-transfer {none;};")
	assert.Contains(t, nginxSitesInclude, "include /etc/nginx/sites-enabled/*;")

	assert.Contains(t, systemd.ServeUnit, "User=go-ispconfig")
	assert.Contains(t, systemd.ServeUnit, "ExecStart=/usr/local/bin/go-ispconfig serve")
	assert.Contains(t, systemd.DaemonUnit, "User=root")
	assert.Contains(t, systemd.DaemonUnit, "ExecStart=/usr/local/bin/go-ispconfig daemon")
}
