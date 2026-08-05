package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-acme/lego/v4/registration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateAccountDropsStaleRegistrationWhenKeyMissing(t *testing.T) {
	dir := t.TempDir()
	reg := &registration.Resource{URI: "https://acme.example/acct/1"}
	raw, err := json.Marshal(&Account{Email: "admin@example.com", Registration: reg})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "account.json"), raw, 0o600))

	acc, err := LoadOrCreateAccount(dir, "admin@example.com")
	require.NoError(t, err)
	assert.Nil(t, acc.Registration, "partial account dir must not reuse a registration without its key")
	assert.NotNil(t, acc.GetPrivateKey())
}

func TestLoadOrCreateAccountHonoursConfigEmail(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "account.key"), keyPEM, 0o600))

	raw, err := json.Marshal(&Account{Email: "old@example.com"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "account.json"), raw, 0o600))

	acc, err := LoadOrCreateAccount(dir, "new@example.com")
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", acc.Email)
}
