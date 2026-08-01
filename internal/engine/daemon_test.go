package engine

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunCycleSkipsWhenBusy covers the "Concurrent processing prevented"
// scenario: a tick arriving while a cycle is in flight is skipped, never
// queued. With busy held the cycle body is never entered, so the nil db is
// proof no processing happened.
func TestRunCycleSkipsWhenBusy(t *testing.T) {
	d := &Daemon{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.busy.Lock()
	defer d.busy.Unlock()
	require.NoError(t, d.RunCycle(context.Background()))
}

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
