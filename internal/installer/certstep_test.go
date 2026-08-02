package installer

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTLSCertStepGeneratesAndSkips(t *testing.T) {
	st, mock, _ := testState(t)
	ctx := context.Background()

	require.NoError(t, tlsCertStep{}.Run(ctx, st))
	certFile := filepath.Join(st.ConfigDir, "ssl", "panel.crt")
	keyFile := filepath.Join(st.ConfigDir, "ssl", "panel.key")

	data, err := os.ReadFile(certFile)
	require.NoError(t, err)
	block, _ := pem.Decode(data)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, "srv1.example.com", leaf.Subject.CommonName)
	assert.Contains(t, leaf.DNSNames, "srv1.example.com")
	assert.Greater(t, leaf.NotAfter.Year(), leaf.NotBefore.Year()+8, "10-year validity")

	info, _ := os.Stat(keyFile)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.True(t, mock.called("chown -R go-ispconfig:go-ispconfig"), "ssl dir handed to panel user")

	// Re-run: valid pair kept.
	before, _ := os.ReadFile(certFile)
	err = tlsCertStep{}.Run(ctx, st)
	require.ErrorContains(t, err, "already present")
	after, _ := os.ReadFile(certFile)
	assert.Equal(t, before, after, "existing valid cert not regenerated")
}
