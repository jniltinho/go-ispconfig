package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreflightOK(t *testing.T) {
	st, _, _ := testState(t)
	require.NoError(t, preflightStep{}.Run(context.Background(), st))
}

func TestPreflightNonRoot(t *testing.T) {
	st, _, _ := testState(t)
	st.Euid = 1000
	err := preflightStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "root is required")
}

func TestPreflightNoSystemd(t *testing.T) {
	st, _, _ := testState(t)
	st.SystemdMarker = filepath.Join(t.TempDir(), "missing")
	require.ErrorContains(t, preflightStep{}.Run(context.Background(), st), "systemd")
}

func TestPreflightNoApt(t *testing.T) {
	st, mock, _ := testState(t)
	mock.missing["apt-get"] = true
	require.ErrorContains(t, preflightStep{}.Run(context.Background(), st), "apt-get")
}

func TestPreflightExistingPHPInstall(t *testing.T) {
	st, _, _ := testState(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(st.LegacyMarker), 0o755))
	require.NoError(t, os.WriteFile(st.LegacyMarker, []byte("<?php"), 0o644))
	err := preflightStep{}.Run(context.Background(), st)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "legacy-migration", "error points to the migration change")
}
