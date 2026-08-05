package acme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The token must land exactly where the vhost serves it from, with the key
// authorization as the whole body — a mismatch fails validation with no
// diagnosis from the CA.
func TestWebrootSolverPresentAndCleanUp(t *testing.T) {
	root := t.TempDir()
	s := NewWebrootSolver(root)

	require.NoError(t, s.Present("example.com", "tok123", "tok123.keyauth"))

	path := filepath.Join(root, ".well-known", "acme-challenge", "tok123")
	assert.Equal(t, filepath.Join(root, filepath.FromSlash(http01.ChallengePath("tok123"))), path,
		"the path lego advertises is the path we write")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "tok123.keyauth", string(body))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "the web server must be able to read it")

	require.NoError(t, s.CleanUp("example.com", "tok123", "tok123.keyauth"))
	assert.NoFileExists(t, path)
}

// Cleaning up something already gone is not an error: a retried cycle must not
// fail on a token an earlier run removed.
func TestWebrootSolverCleanUpIsIdempotent(t *testing.T) {
	s := NewWebrootSolver(t.TempDir())
	assert.NoError(t, s.CleanUp("example.com", "missing", ""))
}
