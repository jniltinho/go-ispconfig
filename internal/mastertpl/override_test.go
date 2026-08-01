package mastertpl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamesListsEmbeddedTemplates(t *testing.T) {
	names := Names()
	require.Contains(t, names, "nginx_vhost.conf.master")
	require.Contains(t, names, "bind_pri.domain.master")
	require.IsIncreasing(t, names)
}

func TestLoadCustomWins(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "nginx_vhost.conf.master"), []byte("# customized"), 0o644))

	src, source, err := Load("nginx_vhost.conf.master", dir)
	require.NoError(t, err)
	require.Equal(t, SourceCustom, source)
	require.Equal(t, "# customized", src)
}

func TestLoadFallsBackToEmbedded(t *testing.T) {
	src, source, err := Load("nginx_vhost.conf.master", t.TempDir())
	require.NoError(t, err)
	require.Equal(t, SourceEmbedded, source)
	embedded, err := Templates.ReadFile("templates/nginx_vhost.conf.master")
	require.NoError(t, err)
	require.Equal(t, string(embedded), src)
}

func TestLoadEmptyCustomDirUsesEmbedded(t *testing.T) {
	_, source, err := Load("bind_pri.domain.master", "")
	require.NoError(t, err)
	require.Equal(t, SourceEmbedded, source)
}

func TestLoadUnknownTemplateErrors(t *testing.T) {
	_, _, err := Load("../../etc/passwd", t.TempDir())
	require.ErrorContains(t, err, "unknown template")
}
