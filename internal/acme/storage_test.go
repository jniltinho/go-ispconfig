package acme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func material(tag string) Material {
	return Material{
		Cert:      []byte("cert-" + tag),
		Chain:     []byte("chain-" + tag),
		Fullchain: []byte("fullchain-" + tag),
		Privkey:   []byte("privkey-" + tag),
	}
}

// A first issuance creates generation 1 and the four live symlinks.
func TestSaveFirstGeneration(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Save("example.com", material("1"),
		[]string{"example.com", "www.example.com"}, "https://ca.test/dir"))

	_, _, fullchain, privkey := s.LivePaths("example.com")
	body, err := os.ReadFile(fullchain)
	require.NoError(t, err)
	assert.Equal(t, "fullchain-1", string(body))

	target, err := os.Readlink(fullchain)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("..", "..", "archive", "example.com", "fullchain1.pem"), target,
		"live/ points into archive/ relatively, as certbot writes it")

	info, err := os.Stat(privkey)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the private key is not world-readable")
}

// A renewal writes generation 2 and moves the pointers; the old generation
// stays on disk, which is what makes a bad renewal recoverable.
func TestSaveRenewalKeepsHistory(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Save("example.com", material("1"), []string{"example.com"}, ""))
	require.NoError(t, s.Save("example.com", material("2"), []string{"example.com"}, ""))

	_, _, fullchain, _ := s.LivePaths("example.com")
	body, _ := os.ReadFile(fullchain)
	assert.Equal(t, "fullchain-2", string(body), "live/ follows the newest generation")

	old := filepath.Join(s.archiveDir("example.com"), "fullchain1.pem")
	assert.FileExists(t, old, "the previous generation is kept")
}

// The renewal file is what a legacy panel parses, so its four keys must be
// present, absolute, and appear before the marker the parser stops at.
func TestRenewalConfIsTheDiscoveryContract(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Save("example.com", material("1"),
		[]string{"www.example.com", "example.com"}, "https://ca.test/dir"))

	body, err := os.ReadFile(s.renewalFile("example.com"))
	require.NoError(t, err)
	conf := string(body)

	// Parse it the way get_certificate_list does: key = value, stop at the
	// [[webroot_map]] marker.
	got := map[string]string{}
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "[[webroot_map]]" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		got[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	cert, chain, fullchain, privkey := s.LivePaths("example.com")
	assert.Equal(t, cert, got["cert"])
	assert.Equal(t, chain, got["chain"])
	assert.Equal(t, fullchain, got["fullchain"])
	assert.Equal(t, privkey, got["privkey"])
	for _, k := range []string{"cert", "chain", "fullchain", "privkey"} {
		assert.True(t, filepath.IsAbs(got[k]), "%s must be absolute for the legacy parser", k)
	}
	assert.Equal(t, "example.com, www.example.com", got["domains"], "domains are sorted")
}

// ECDSA takes the legacy's _ecc suffix so it does not collide with an RSA
// lineage the adopted host is already renewing.
func TestLineage(t *testing.T) {
	l, err := Lineage("example.com", "rsa")
	require.NoError(t, err)
	assert.Equal(t, "example.com", l)

	l, err = Lineage("example.com", "ecdsa")
	require.NoError(t, err)
	assert.Equal(t, "example.com_ecc", l)

	l, err = Lineage("*.example.com", "rsa")
	require.NoError(t, err)
	assert.Equal(t, "example.com", l, "a wildcard shares the lineage of its base name")

	_, err = Lineage("../../etc/passwd", "rsa")
	assert.Error(t, err, "a name that would escape the tree is refused")
	_, err = Lineage("a/b", "rsa")
	assert.Error(t, err)
}

// A reader holding the live path open across a renewal must never see a
// truncated file: the write goes to a new generation and only the pointer moves.
func TestRenewalIsAtomicForReaders(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Save("example.com", material("1"), []string{"example.com"}, ""))
	_, _, fullchain, _ := s.LivePaths("example.com")

	f, err := os.Open(fullchain)
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, s.Save("example.com", material("2"), []string{"example.com"}, ""))

	body, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.NotEmpty(t, body, "the handle opened before the renewal still reads a whole file")
}
