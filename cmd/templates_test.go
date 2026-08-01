package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/mastertpl"
)

func TestListTemplatesMarksOverrides(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "nginx_vhost.conf.master"), []byte("custom"), 0o644))

	var b strings.Builder
	require.NoError(t, listTemplates(&b, dir))

	out := b.String()
	require.Contains(t, out, "nginx_vhost.conf.master (overridden)")
	require.Contains(t, out, "bind_pri.domain.master\n")
	require.NotContains(t, out, "bind_pri.domain.master (overridden)")
	require.Len(t, strings.Split(strings.TrimSpace(out), "\n"), len(mastertpl.Names()))
}

func TestExportTemplateAndRefuseOverwrite(t *testing.T) {
	dir := t.TempDir()
	const name = "nginx_vhost.conf.master"

	var b strings.Builder
	require.NoError(t, exportTemplates(&b, dir, []string{name}, false))
	require.Contains(t, b.String(), "exported "+filepath.Join(dir, name))

	embedded, err := mastertpl.Templates.ReadFile("templates/" + name)
	require.NoError(t, err)
	exported, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	require.Equal(t, embedded, exported)

	// Second export without --force refuses and keeps the customized file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("edited"), 0o644))
	err = exportTemplates(&b, dir, []string{name}, false)
	require.ErrorContains(t, err, "refusing to overwrite")
	kept, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	require.Equal(t, []byte("edited"), kept)

	// --force overwrites with the embedded original again.
	require.NoError(t, exportTemplates(&b, dir, []string{name}, true))
	restored, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	require.Equal(t, embedded, restored)
}

func TestExportAllTemplates(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	require.NoError(t, exportTemplates(&b, dir, mastertpl.Names(), false))
	for _, name := range mastertpl.Names() {
		require.FileExists(t, filepath.Join(dir, name))
	}
}

func TestExportUnknownTemplateErrors(t *testing.T) {
	var b strings.Builder
	err := exportTemplates(&b, t.TempDir(), []string{"nope.master"}, false)
	require.ErrorContains(t, err, "unknown template")
}
