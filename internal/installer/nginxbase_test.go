package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeNginxConf(t *testing.T, st *State, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(st.Profile.NginxConfigDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(st.Profile.NginxConfigDir, "nginx.conf"), []byte(content), 0o644))
}

func TestNginxBaseStockDebianSkips(t *testing.T) {
	st, mock, _ := testState(t)
	writeNginxConf(t, st, "http {\n\tinclude /etc/nginx/sites-enabled/*;\n}\n")

	err := nginxBaseStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "already includes")
	assert.DirExists(t, st.Profile.NginxVhostConfDir)
	assert.DirExists(t, st.Profile.NginxVhostEnabledDir)
	assert.DirExists(t, st.AcmeWebroot)
	assert.False(t, mock.called("nginx -t"), "no write, no validation, no reload")
	assert.False(t, mock.called("systemctl reload"))
}

func TestNginxBaseWritesMissingIncludeAndReloads(t *testing.T) {
	st, mock, _ := testState(t)
	// A commented-out include must not count as present.
	writeNginxConf(t, st, "http {\n\t# include /etc/nginx/sites-enabled/*;\n}\n")

	require.NoError(t, nginxBaseStep{}.Run(context.Background(), st))
	include := filepath.Join(st.Profile.NginxConfigDir, "conf.d", "go-ispconfig-sites.conf")
	assert.FileExists(t, include)
	assert.True(t, mock.called("nginx -t"))
	assert.True(t, mock.called("systemctl reload nginx"))
}

func TestNginxBaseValidationFailureRestores(t *testing.T) {
	st, mock, _ := testState(t)
	writeNginxConf(t, st, "http {\n}\n")
	mock.fail["nginx -t"] = "config test failed"

	err := nginxBaseStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "nginx -t")
	include := filepath.Join(st.Profile.NginxConfigDir, "conf.d", "go-ispconfig-sites.conf")
	assert.NoFileExists(t, include, "failed validation restores the previous state")
	assert.False(t, mock.called("systemctl reload"), "no reload after failed validation")
}

func TestNginxBaseDisabledByAnswer(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.EnableWeb = false
	err := nginxBaseStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "disabled")
	assert.Empty(t, mock.calls)
}
