package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stepNames(steps []Step) []string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name()
	}
	return names
}

func TestInstallStepsOrder(t *testing.T) {
	assert.Equal(t, []string{
		"preflight", "packages", "mariadb", "server-ips", "panel-user",
		"config-toml", "tls-cert", "nginx-base", "apache2", "bind-base", "powerdns", "pure-ftpd",
		"fail2ban", "vmail", "postfix", "dovecot", "rspamd", "getmail",
		"systemd-units", "summary",
	}, stepNames(InstallSteps()))
}

func TestUpdateStepsNeverTouchDBOrCredentials(t *testing.T) {
	names := stepNames(UpdateSteps())
	assert.Equal(t, []string{"preflight", "nginx-base", "apache2", "bind-base", "systemd-units"}, names)
	for _, forbidden := range []string{"mariadb", "config-toml", "tls-cert", "summary", "server-ips"} {
		assert.NotContains(t, names, forbidden)
	}
}

// TestUpdateRunLeavesDataUntouched runs the full --update pipeline against
// a fake installed host and asserts config.toml, certs and credentials file
// stay byte-identical while units/base configs re-render.
func TestUpdateRunLeavesDataUntouched(t *testing.T) {
	st, mock, _ := testState(t)
	st.Update = true
	ctx := context.Background()

	// Fake an installed host.
	require.NoError(t, os.MkdirAll(st.ConfigDir+"/ssl", 0o755))
	configPath := filepath.Join(st.ConfigDir, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[database]\ndsn = \"ispconfig:oldpw@tcp(127.0.0.1:3306)/dbispconfig\"\n"), 0o640))
	certPath := filepath.Join(st.ConfigDir, "ssl", "panel.crt")
	require.NoError(t, os.WriteFile(certPath, []byte("OLDCERT"), 0o600))
	writeNginxConf(t, st, "http {\n\tinclude /etc/nginx/sites-enabled/*;\n}\n")
	require.NoError(t, os.MkdirAll(st.Profile.BindZonefilesDir, 0o755))

	require.NoError(t, Run(ctx, st, UpdateSteps()))

	config, _ := os.ReadFile(configPath)
	assert.Contains(t, string(config), "oldpw", "update never touches config.toml credentials")
	cert, _ := os.ReadFile(certPath)
	assert.Equal(t, "OLDCERT", string(cert), "update never touches certificates")
	assert.NoFileExists(t, st.CredentialsFile)
	assert.FileExists(t, st.Profile.NamedConfOptionsPath, "base configs re-rendered")
	assert.FileExists(t, filepath.Join(st.SystemdDir, ServeUnitName))
	assert.True(t, mock.called("systemctl restart"), "update restarts units")
	assert.False(t, mock.called("systemctl enable"))
}
