package installer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enableCalls returns every "systemctl enable ..." invocation the mock saw.
func enableCalls(m *mockExec) []string {
	var out []string
	for _, c := range m.calls {
		if strings.HasPrefix(c, "systemctl enable") {
			out = append(out, c)
		}
	}
	return out
}

// packageSet() drops the nginx package for an apache2 install, so the
// packages step must not enable nginx.service either — it does not exist.
// The Apache unit is not its job either: apache2Step runs later and installs
// the Apache packages itself.
func TestPackagesStepSkipsWebUnitForApache(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.WebServer = WebServerApache
	require.NotContains(t, st.packageSet(), "nginx")

	_ = packagesStep{}.Run(context.Background(), st)

	for _, call := range enableCalls(mock) {
		assert.NotContains(t, strings.Fields(call), st.Profile.NginxService)
		assert.NotContains(t, strings.Fields(call), st.Profile.ApacheService)
	}
}

// The nginx install keeps enabling nginx.service from the packages step.
func TestPackagesStepEnablesNginxUnit(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.WebServer = WebServerNginx

	_ = packagesStep{}.Run(context.Background(), st)

	assert.Contains(t, strings.Join(enableCalls(mock), "\n"), st.Profile.NginxService)
}

// apache2Step owns the Apache unit end to end.
func TestApache2StepEnablesTheUnit(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.WebServer = WebServerApache

	require.NoError(t, apache2Step{}.Run(context.Background(), st))

	var enabled bool
	for _, call := range enableCalls(mock) {
		f := strings.Fields(call)
		if f[len(f)-1] == st.Profile.ApacheService {
			enabled = true
		}
	}
	assert.True(t, enabled, "apache2 unit is enabled by its own step")
}

// TestSetINIKeyWebSection: the web server choice replaces an existing key,
// is inserted into an existing section and creates a missing one.
func TestSetINIKeyWebSection(t *testing.T) {
	assert.Equal(t, "[web]\nserver_type=apache\nuser=www-data\n",
		setINIKey("[web]\nserver_type=nginx\nuser=www-data\n", "[web]", "server_type", "apache"))
	assert.Equal(t, "[web]\nvhost_conf_dir=/etc/apache2/sites-available\nuser=www-data\n",
		setINIKey("[web]\nuser=www-data\n", "[web]", "vhost_conf_dir", "/etc/apache2/sites-available"),
		"a key the seeded INI never had is inserted, not dropped")
	assert.Equal(t, "[dns]\nx=1\n\n[web]\nserver_type=apache\n",
		setINIKey("[dns]\nx=1\n", "[web]", "server_type", "apache"))
}
