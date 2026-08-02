package installer

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindBaseWritesValidatesReloads(t *testing.T) {
	st, mock, _ := testState(t)
	require.NoError(t, os.MkdirAll(st.Profile.BindZonefilesDir, 0o755))
	ctx := context.Background()

	require.NoError(t, bindBaseStep{}.Run(ctx, st))
	content, err := os.ReadFile(st.Profile.NamedConfOptionsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `directory "/var/cache/bind";`)
	assert.FileExists(t, st.Profile.NamedConfLocalPath, "named.conf.local ensured for the dns module")
	assert.True(t, mock.called("named-checkconf"))
	assert.True(t, mock.called("systemctl reload named"))

	// Re-run: identical content, no backup, no reload.
	mock.calls = nil
	err = bindBaseStep{}.Run(ctx, st)
	require.ErrorContains(t, err, "unchanged")
	assert.Empty(t, mock.calls)
	entries, _ := os.ReadDir(st.Profile.BindZonefilesDir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".bak-", "no backup churn on identical re-run")
	}
}

func TestBindBaseValidationFailureRestores(t *testing.T) {
	st, mock, _ := testState(t)
	require.NoError(t, os.MkdirAll(st.Profile.BindZonefilesDir, 0o755))
	require.NoError(t, os.WriteFile(st.Profile.NamedConfOptionsPath, []byte("options { previous; };\n"), 0o644))
	mock.fail["named-checkconf"] = "unknown option"

	err := bindBaseStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "named-checkconf")
	content, _ := os.ReadFile(st.Profile.NamedConfOptionsPath)
	assert.Equal(t, "options { previous; };\n", string(content), "original restored on failed validation")
	assert.False(t, mock.called("systemctl reload"))
}

func TestBindBaseDisabledByAnswer(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.EnableDNS = false
	err := bindBaseStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "disabled")
	assert.Empty(t, mock.calls)
}
