package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAnswersFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "answers.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestResolveAnswersPrecedenceFlagOverFile(t *testing.T) {
	file := writeAnswersFile(t, "panel_port = 8080\nhostname = \"file.example.com\"\n")
	a, err := ResolveAnswers(ResolveOptions{
		Flags:       map[string]string{"panel-port": "9443"},
		AnswersFile: file,
	})
	require.NoError(t, err)
	assert.Equal(t, 9443, a.PanelPort, "flag beats answers file")
	assert.Equal(t, "file.example.com", a.Hostname, "file beats default")
	assert.Equal(t, "dbispconfig", a.DBName, "default fills the rest")
	assert.True(t, a.EnableWeb)
	assert.True(t, a.EnableDNS)
	assert.False(t, a.InstallAcme, "acme defaults off")
}

func TestResolveAnswersPromptLayer(t *testing.T) {
	// Prompt order: hostname, panel-port, db-name, db-user, web, dns,
	// dns-backend, php-fpm, acme, acme-client, admin-email.
	in := strings.NewReader("prompted.example.com\n\n\n\n\n\n\nn\n\n\n\n")
	var out strings.Builder
	a, err := ResolveAnswers(ResolveOptions{Interactive: true, In: in, Out: &out})
	require.NoError(t, err)
	assert.Equal(t, "prompted.example.com", a.Hostname, "typed answer wins")
	assert.Equal(t, 8080, a.PanelPort, "empty input keeps default")
	assert.False(t, a.InstallPHPFPM, "typed n overrides default y")
	assert.Contains(t, out.String(), "[8080]", "prompt shows the default")
}

func TestResolveAnswersYesMissingRequired(t *testing.T) {
	old := hostnameDefault
	hostnameDefault = func() string { return "" }
	t.Cleanup(func() { hostnameDefault = old })

	_, err := ResolveAnswers(ResolveOptions{Interactive: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--hostname", "error names the missing flag")
}

func TestResolveAnswersValidation(t *testing.T) {
	_, err := ResolveAnswers(ResolveOptions{Flags: map[string]string{"panel-port": "notaport", "hostname": "h.example"}})
	require.ErrorContains(t, err, "--panel-port")

	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{"web": "maybe", "hostname": "h.example"}})
	require.ErrorContains(t, err, "--web")

	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{"acme-client": "lego", "hostname": "h.example"}})
	require.ErrorContains(t, err, "--acme-client")

	_, err = ResolveAnswers(ResolveOptions{AnswersFile: "does/not/exist.toml"})
	require.ErrorContains(t, err, "answers file")

	// Shell metacharacters must never pass into the acme sh -c line.
	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{
		"hostname": "h.example", "admin-email": "x@y.com;rm -rf /"}})
	require.ErrorContains(t, err, "--admin-email")

	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{
		"hostname": "h.example", "admin-email": "$(id)@y.com"}})
	require.ErrorContains(t, err, "--admin-email")

	// Privileged ports cannot be bound by the unprivileged serve unit.
	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{
		"hostname": "h.example", "panel-port": "443"}})
	require.ErrorContains(t, err, "unprivileged")
}

func TestResolveAnswersFileBooleans(t *testing.T) {
	file := writeAnswersFile(t, "acme = \"y\"\nacme_client = \"certbot\"\ndns = \"n\"\n")
	a, err := ResolveAnswers(ResolveOptions{
		Flags:       map[string]string{"hostname": "h.example.com"},
		AnswersFile: file,
	})
	require.NoError(t, err)
	assert.True(t, a.InstallAcme)
	assert.Equal(t, "certbot", a.AcmeClient)
	assert.False(t, a.EnableDNS)
}
