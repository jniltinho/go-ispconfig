package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcquireLockSingleInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")

	first, err := acquireLock(path)
	require.NoError(t, err)

	_, err = acquireLock(path)
	require.Error(t, err, "second instance must be refused while the lock is held")

	require.NoError(t, first.Close())
	third, err := acquireLock(path)
	require.NoError(t, err, "lock must be reacquirable after release")
	require.NoError(t, third.Close())
}
