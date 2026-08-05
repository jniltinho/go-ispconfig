package acme

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateStoreRecordsSuccessAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStateStore(path)
	now := time.Now()

	require.NoError(t, s.RecordSuccess("example.com", ChallengeHTTP, now, "", ""))
	st := s.Get("example.com")
	assert.Equal(t, ChallengeHTTP, st.Provider)
	assert.WithinDuration(t, now, st.LastRenewal, time.Second)
	assert.Empty(t, st.LastError)

	require.NoError(t, s.RecordError("example.com", ChallengeHTTP, "boom"))
	st = s.Get("example.com")
	assert.Equal(t, "boom", st.LastError)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "example.com")
}

func TestIssueForSiteLinksSiteCerts(t *testing.T) {
	root := t.TempDir()
	webroot := t.TempDir()
	docroot := filepath.Join(t.TempDir(), "web1")
	mgr := NewManager(ManagerConfig{
		Client: Config{
			Root:     root,
			Webroot:  webroot,
			ServerID: 1,
			CADirURL: "http://127.0.0.1:1/unused",
		},
		StatePath: filepath.Join(root, "state.json"),
	})

	// Pre-seed a valid certificate so Issue reuses without calling the CA.
	selfSigned(t, mgr.client.store, "example.com", []string{"example.com"}, 90*24*time.Hour)

	keyFile := filepath.Join(docroot, "ssl", "example.com-le.key")
	crtFile := filepath.Join(docroot, "ssl", "example.com-le.crt")
	require.NoError(t, os.MkdirAll(filepath.Dir(keyFile), 0o755))
	res, err := mgr.IssueForSite([]string{"example.com"}, "rsa", keyFile, crtFile, ChallengeHTTP)
	require.NoError(t, err)
	assert.True(t, res.Reused)
	assert.FileExists(t, keyFile)
	assert.FileExists(t, crtFile)

	target, err := os.Readlink(crtFile)
	require.NoError(t, err)
	assert.Contains(t, target, "fullchain.pem")
}

func TestDomainsFromRenewal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.com.conf")
	require.NoError(t, os.WriteFile(path, []byte("domains = www.example.com, example.com\n"), 0o644))
	got, err := domainsFromRenewal(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"www.example.com", "example.com"}, got)
}

func TestReadLedgerRejectsCorruptFile(t *testing.T) {
	root := t.TempDir()
	c := New(Config{Root: root, ServerID: 1})
	path := c.ledgerPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	_, err := c.readLedger()
	require.Error(t, err)
}

func TestRecordSuccessSkipsUnchangedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewStateStore(path)
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.RecordSuccess("example.com", ChallengeHTTP, now, "/k", "/c"))
	info, err := os.Stat(path)
	require.NoError(t, err)
	firstMod := info.ModTime()

	require.NoError(t, s.RecordSuccess("example.com", ChallengeHTTP, now, "/k", "/c"))
	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, firstMod, info.ModTime())
}
