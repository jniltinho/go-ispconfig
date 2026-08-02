package installer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackagesInstallsMissing(t *testing.T) {
	st, mock, _ := testState(t)
	// nginx already installed, everything else missing.
	mock.output["dpkg-query -W -f=${Status} nginx"] = "install ok installed"
	mock.fail["dpkg-query -W -f=${Status} bind9"] = "no packages found"

	require.NoError(t, packagesStep{}.Run(context.Background(), st))

	var install string
	for _, c := range mock.calls {
		if strings.Contains(c, "apt-get") && strings.Contains(c, "install") {
			install = c
		}
	}
	require.NotEmpty(t, install, "apt-get install must run")
	assert.NotContains(t, install, "nginx", "installed package is skipped")
	assert.Contains(t, install, "bind9")
	assert.Contains(t, install, "mariadb-server")
	assert.Contains(t, install, "redis-server")
	assert.Contains(t, install, "php8.2-fpm", "php-fpm answered yes on debian12")
	assert.Contains(t, install, "Dpkg::Options::=--force-confold")
	assert.Contains(t, install, "DPkg::Lock::Timeout=600")
	assert.True(t, mock.called("apt-get") && strings.Contains(strings.Join(mock.calls, "|"), "update"),
		"apt-get update runs before install")
}

func TestPackagesSkipsPHPFPMWhenDisabled(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.InstallPHPFPM = false
	require.NoError(t, packagesStep{}.Run(context.Background(), st))
	assert.NotContains(t, strings.Join(mock.calls, "|"), "php8.2-fpm")
}

func TestPackagesAllInstalledSkips(t *testing.T) {
	st, mock, _ := testState(t)
	mock.output["dpkg-query"] = "install ok installed"
	err := packagesStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "already installed")
	assert.False(t, mock.called("apt-get"), "no apt call when everything is installed")
	assert.True(t, mock.called("systemctl enable --now mariadb redis-server nginx bind9 php8.2-fpm"),
		"services ensured active even when packages were present")
}

func TestPackagesInstallFailure(t *testing.T) {
	st, mock, _ := testState(t)
	mock.fail["apt-get -o Dpkg::Options::=--force-confold -o DPkg::Lock::Timeout=600 install"] = "dpkg lock timeout"
	err := packagesStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "apt-get install")
}
