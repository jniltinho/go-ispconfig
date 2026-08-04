package acme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The swap must never leave the site without a certificate: unlinking both
// paths and then creating them means a crash, or a failure on the second link,
// leaves the vhost pointing at nothing and the next reload drops the site.
func TestLinkSiteCertsNeverLeavesTheSiteBare(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live")
	require.NoError(t, os.MkdirAll(live, 0o755))
	oldFull := filepath.Join(live, "fullchain0.pem")
	oldKey := filepath.Join(live, "privkey0.pem")
	require.NoError(t, os.WriteFile(oldFull, []byte("old-cert"), 0o644))
	require.NoError(t, os.WriteFile(oldKey, []byte("old-key"), 0o600))

	ssl := filepath.Join(dir, "ssl")
	require.NoError(t, os.MkdirAll(ssl, 0o755))
	crtFile := filepath.Join(ssl, "example.com-le.crt")
	keyFile := filepath.Join(ssl, "example.com-le.key")
	require.NoError(t, LinkSiteCerts(oldFull, oldKey, keyFile, crtFile))

	body, err := os.ReadFile(crtFile)
	require.NoError(t, err)
	assert.Equal(t, "old-cert", string(body))

	// Re-linking to a new generation replaces in place: both paths resolve at
	// every moment, and the second call is not a remove-then-create.
	newFull := filepath.Join(live, "fullchain1.pem")
	newKey := filepath.Join(live, "privkey1.pem")
	require.NoError(t, os.WriteFile(newFull, []byte("new-cert"), 0o644))
	require.NoError(t, os.WriteFile(newKey, []byte("new-key"), 0o600))
	require.NoError(t, LinkSiteCerts(newFull, newKey, keyFile, crtFile))

	body, err = os.ReadFile(crtFile)
	require.NoError(t, err)
	assert.Equal(t, "new-cert", string(body))
	body, err = os.ReadFile(keyFile)
	require.NoError(t, err)
	assert.Equal(t, "new-key", string(body))
}
