package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryPrintsFreshCredentialsOnce(t *testing.T) {
	st, _, out := testState(t)
	st.AdminPassword = "freshpw123"
	require.NoError(t, os.MkdirAll(filepath.Dir(st.CredentialsFile), 0o700))

	require.NoError(t, summaryStep{}.Run(context.Background(), st))
	assert.Contains(t, out.String(), "admin / freshpw123")
	assert.Contains(t, out.String(), "https://srv1.example.com:8080/")
	assert.NoFileExists(t, st.CredentialsFile, "no credentials file without --write-credentials")
}

func TestSummaryRerunDoesNotReprint(t *testing.T) {
	st, _, out := testState(t)
	st.AdminPassword = "" // re-run: seed did not run
	st.WriteCredentials = true
	require.NoError(t, os.MkdirAll(filepath.Dir(st.CredentialsFile), 0o700))

	require.NoError(t, summaryStep{}.Run(context.Background(), st))
	assert.Contains(t, out.String(), "unchanged")
	assert.NotContains(t, out.String(), "admin /")
	assert.NoFileExists(t, st.CredentialsFile, "nothing persisted when no fresh password exists")
}

func TestSummaryWriteCredentialsOptIn(t *testing.T) {
	st, _, out := testState(t)
	st.AdminPassword = "freshpw123"
	st.WriteCredentials = true
	require.NoError(t, os.MkdirAll(filepath.Dir(st.CredentialsFile), 0o700))

	require.NoError(t, summaryStep{}.Run(context.Background(), st))
	content, err := os.ReadFile(st.CredentialsFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "freshpw123")
	info, _ := os.Stat(st.CredentialsFile)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, out.String(), "delete this file")
}
