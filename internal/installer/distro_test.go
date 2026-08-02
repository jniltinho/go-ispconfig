package installer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectDistroSupported(t *testing.T) {
	tests := []struct {
		fixture string
		id      string
		php     string
	}{
		{"testdata/os-release-debian12", "debian12", "8.2"},
		{"testdata/os-release-ubuntu2404", "ubuntu24.04", "8.3"},
	}
	for _, tt := range tests {
		p, err := DetectDistro(tt.fixture)
		require.NoError(t, err, tt.fixture)
		assert.Equal(t, tt.id, p.ID)
		assert.Equal(t, tt.php, p.PHPVersion)
		assert.Equal(t, "php"+tt.php+"-fpm", p.PHPFPMPackage())
		assert.Equal(t, "/etc/php/"+tt.php+"/fpm/pool.d", p.PHPFPMPoolDir)
		// Debian-family paths shared by all profiles.
		assert.Equal(t, "/etc/nginx/sites-available", p.NginxVhostConfDir)
		assert.Equal(t, "/etc/bind/named.conf.options", p.NamedConfOptionsPath)
		assert.Contains(t, p.Packages, "mariadb-server")
		assert.Contains(t, p.Packages, "redis-server")
	}
}

func TestDetectDistroAllProfilesComplete(t *testing.T) {
	// Every supported id must produce a profile with a PHP version and
	// package set (catches profile-data drift, design risk 3).
	for _, id := range SupportedDistros() {
		p := profileFor(id, id)
		assert.NotEmpty(t, p.PHPVersion, id)
		assert.NotEmpty(t, p.Packages, id)
		assert.NotEmpty(t, p.BindService, id)
	}
	assert.Equal(t, []string{"debian11", "debian12", "debian13", "ubuntu22.04", "ubuntu24.04"}, SupportedDistros())
}

func TestDetectDistroUnsupported(t *testing.T) {
	_, err := DetectDistro("testdata/os-release-centos9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"centos"`)
	assert.Contains(t, err.Error(), "debian12")

	_, err = DetectDistro("testdata/os-release-debian10")
	require.Error(t, err, "too-old debian must be rejected")

	_, err = DetectDistro("testdata/does-not-exist")
	require.Error(t, err)
}
