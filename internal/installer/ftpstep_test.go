package installer

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageSetInstallsPureFTPdWithWeb(t *testing.T) {
	st, _, _ := testState(t)
	assert.Contains(t, st.packageSet(), PureFTPdPackage)

	st.Answers.EnableWeb = false
	assert.NotContains(t, st.packageSet(), PureFTPdPackage)
}

func TestRenderPureFTPdMySQL(t *testing.T) {
	st, _, _ := testState(t)
	st.DBPassword = "s3cr3t"
	conf := st.renderPureFTPdMySQL(3)

	assert.Contains(t, conf, "MYSQLServer     127.0.0.1", "host only, no port")
	assert.Contains(t, conf, "MYSQLUser       ispconfig")
	assert.Contains(t, conf, "MYSQLPassword   s3cr3t")
	assert.Contains(t, conf, "MYSQLDatabase   dbispconfig")
	assert.Contains(t, conf, "MYSQLCrypt      crypt", "matches the API's crypt() hashes")
	assert.Contains(t, conf, `SELECT password FROM ftp_user WHERE active = 'y' AND server_id = '3'`)
	assert.NotContains(t, conf, "{", "no placeholder left unfilled")
}

func TestEnablePureFTPdVirtualChroot(t *testing.T) {
	st, _, _ := testState(t)
	require.NoError(t, os.MkdirAll(st.PureFTPdDefaults[:len(st.PureFTPdDefaults)-len("/pure-ftpd-common")], 0o755))
	require.NoError(t, os.WriteFile(st.PureFTPdDefaults,
		[]byte("STANDALONE_OR_INETD=inetd\nVIRTUALCHROOT=false\nUPLOADSCRIPT=\n"), 0o644))

	changed, err := enablePureFTPdVirtualChroot(st.PureFTPdDefaults)
	require.NoError(t, err)
	assert.True(t, changed)
	got, err := os.ReadFile(st.PureFTPdDefaults)
	require.NoError(t, err)
	assert.Equal(t, "STANDALONE_OR_INETD=standalone\nVIRTUALCHROOT=true\nUPLOADSCRIPT=\n", string(got))

	// Idempotent, and a missing file is not an error (non-Debian layouts).
	changed, err = enablePureFTPdVirtualChroot(st.PureFTPdDefaults)
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = enablePureFTPdVirtualChroot(st.PureFTPdDefaults + ".absent")
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestFTPStepSkips(t *testing.T) {
	st, _, _ := testState(t)
	require.ErrorContains(t, ftpStep{}.Run(context.Background(), st), "no database connection")

	st.Answers.EnableWeb = false
	require.ErrorContains(t, ftpStep{}.Run(context.Background(), st), "web module disabled")
}
