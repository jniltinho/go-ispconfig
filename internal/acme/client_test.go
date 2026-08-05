package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/lego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfSigned writes a certificate covering names, valid for d, into the store
// so the precondition can be exercised without a CA.
func selfSigned(t *testing.T, s *Store, lineage string, names []string, d time.Duration) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(d),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	buf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, s.Save(lineage, Material{
		Cert: buf, Chain: nil, Fullchain: buf, Privkey: []byte("k"),
	}, names, ""))
}

// A certificate with plenty of life covering the same names is reused: no CA
// call, which is what keeps a re-apply of an unchanged site off the rate limit.
func TestIssueReusesValidCertificate(t *testing.T) {
	root := t.TempDir()
	c := New(Config{Root: root, Webroot: t.TempDir(), CADirURL: "http://127.0.0.1:1/unused"})
	selfSigned(t, c.store, "example.com", []string{"example.com", "www.example.com"}, 90*24*time.Hour)

	res, err := c.Issue([]string{"example.com", "www.example.com"}, "rsa")
	require.NoError(t, err, "must not have called the CA")
	assert.True(t, res.Reused)
	assert.Equal(t, "example.com", res.Lineage)
}

// Adding an alias must re-issue. Keyed on the domain set, not the main domain:
// a check on the main name alone would serve a certificate missing the alias.
func TestIssueReIssuesWhenDomainSetChanges(t *testing.T) {
	c := New(Config{Root: t.TempDir(), Webroot: t.TempDir(), CADirURL: "http://127.0.0.1:1/unused"})
	selfSigned(t, c.store, "example.com", []string{"example.com"}, 90*24*time.Hour)

	// The CA is unreachable, so a re-issue attempt is observable as an error
	// rather than as a silent reuse.
	_, err := c.Issue([]string{"example.com", "new.example.com"}, "rsa")
	require.Error(t, err, "an added alias must reach the CA")
}

// Inside the renew window the certificate is replaced even though the names
// match.
func TestIssueReIssuesInsideRenewWindow(t *testing.T) {
	c := New(Config{Root: t.TempDir(), Webroot: t.TempDir(), CADirURL: "http://127.0.0.1:1/unused"})
	selfSigned(t, c.store, "example.com", []string{"example.com"}, 10*24*time.Hour)

	_, err := c.Issue([]string{"example.com"}, "rsa")
	require.Error(t, err, "a certificate inside the window must reach the CA")
}

// The production directory URL must appear in exactly one place, and no test
// may point at it.
func TestProductionURLIsNotTheTestDefault(t *testing.T) {
	c := New(Config{Root: t.TempDir()})
	assert.Equal(t, lego.LEDirectoryProduction, c.caURL(), "empty config means production")

	staging := New(Config{Root: t.TempDir(), CADirURL: lego.LEDirectoryStaging})
	assert.Equal(t, lego.LEDirectoryStaging, staging.caURL())
}

// The account key is generated once and reused: a second load must not mint a
// new one, or the rate-limit history attached to it is orphaned.
func TestAccountKeyIsReused(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadOrCreateAccount(dir, "admin@example.com")
	require.NoError(t, err)
	require.NotNil(t, a.GetPrivateKey())

	raw, err := os.ReadFile(dir + "/account.key")
	require.NoError(t, err)

	b, err := LoadOrCreateAccount(dir, "other@example.com")
	require.NoError(t, err)
	again, err := os.ReadFile(dir + "/account.key")
	require.NoError(t, err)
	assert.Equal(t, raw, again, "the key on disk is not regenerated")
	assert.Equal(t, a.GetPrivateKey(), b.GetPrivateKey())

	info, err := os.Stat(dir + "/account.key")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// splitChain must separate the leaf from the intermediates, which is what lets
// cert.pem and chain.pem be stored the way certbot does.
func TestSplitChain(t *testing.T) {
	one := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("leaf")})
	two := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("inter")})
	leaf, chain := splitChain(append(append([]byte{}, one...), two...))
	assert.Equal(t, string(one), string(leaf))
	assert.Equal(t, string(two), string(chain))

	leaf, chain = splitChain([]byte("not pem"))
	assert.Equal(t, "not pem", string(leaf), "a non-PEM body is kept whole rather than dropped")
	assert.Nil(t, chain)
}

// A failed issuance must not be retried immediately: Let's Encrypt counts
// failed validations against the account, and a retry loop over many sites is
// how an operator gets locked out for a week.
func TestBackoffRefusesLocallyAfterFailure(t *testing.T) {
	c := New(Config{Root: t.TempDir(), Webroot: t.TempDir(), CADirURL: "http://127.0.0.1:1/unused"})

	_, err := c.Issue([]string{"example.com"}, "rsa")
	require.Error(t, err, "the CA is unreachable")

	_, err = c.Issue([]string{"example.com"}, "rsa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not retrying until",
		"the second attempt is refused locally, without reaching the CA")
}

// The backoff doubles and is capped, so a persistently broken site backs off to
// daily rather than for ever.
func TestBackoffDoublesAndCaps(t *testing.T) {
	c := New(Config{Root: t.TempDir()})
	now := time.Now()

	c.recordFailure("example.com", now, "boom")
	_, until := c.blocked("example.com", now)
	assert.WithinDuration(t, now.Add(backoffMin), until, time.Second)

	c.recordFailure("example.com", now, "boom")
	_, until = c.blocked("example.com", now)
	assert.WithinDuration(t, now.Add(2*backoffMin), until, time.Second)

	for i := 0; i < 20; i++ {
		c.recordFailure("example.com", now, "boom")
	}
	_, until = c.blocked("example.com", now)
	assert.WithinDuration(t, now.Add(backoffMax), until, time.Second, "capped at a day")
}

// A success forgets the history: one bad afternoon must not slow the next month.
func TestBackoffClearedOnSuccess(t *testing.T) {
	c := New(Config{Root: t.TempDir()})
	now := time.Now()
	c.recordFailure("example.com", now, "boom")
	blocked, _ := c.blocked("example.com", now)
	require.True(t, blocked)

	c.clearFailure("example.com")
	blocked, _ = c.blocked("example.com", now)
	assert.False(t, blocked)
}

// The backoff expires on its own.
func TestBackoffExpires(t *testing.T) {
	c := New(Config{Root: t.TempDir()})
	now := time.Now()
	c.recordFailure("example.com", now, "boom")

	blocked, _ := c.blocked("example.com", now.Add(backoffMin+time.Minute))
	assert.False(t, blocked)
}
