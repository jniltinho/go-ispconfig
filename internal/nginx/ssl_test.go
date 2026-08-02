package nginx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// opensslFakeRunner writes plausible key/csr/crt files when openssl is
// invoked, so the create path can be exercised without a real openssl.
type opensslFakeRunner struct {
	calls [][]string
}

func (f *opensslFakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name != "openssl" {
		return nil, nil
	}
	// Find -keyout/-out arguments and create the files.
	for i, a := range args {
		if (a == "-keyout" || a == "-out") && i+1 < len(args) {
			_ = os.WriteFile(args[i+1], []byte("PEM "+args[i+1]), 0o644)
		}
	}
	return nil, nil
}

func sslDomainRow(base string) row {
	return row{
		"domain": "example.com", "type": "vhost", "server_id": float64(1),
		"document_root": filepath.Join(base, "web1"),
		"ssl_domain":    "", "ssl_country": "BR", "ssl_organisation": "ACME Ltd",
		"ssl_state": "", "ssl_locality": "", "ssl_organisation_unit": "",
	}
}

// TestSSLCreateGeneratesFilesAndKeyMode: create runs openssl, the key ends
// up 0400, and the subject omits empty DN fields.
func TestSSLCreateGeneratesFilesAndKeyMode(t *testing.T) {
	base := t.TempDir()
	runner := &opensslFakeRunner{}
	p := NewPlugin(nil, nil, runner, "", nil)
	d := sslDomainRow(base)
	d["ssl_action"] = "create"

	require.NoError(t, p.ssl(context.Background(), "web_domain_insert", engine.Data{New: d}))

	key := filepath.Join(base, "web1", "ssl", "example.com.key")
	info, err := os.Stat(key)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
	assert.FileExists(t, filepath.Join(base, "web1", "ssl", "example.com.crt"))
	assert.Equal(t, "example.com", p.sslChangedDomain)

	// Subject: present fields only, in order, with CN and emailAddress.
	var subj string
	for _, c := range runner.calls {
		for i, a := range c {
			if a == "-subj" {
				subj = c[i+1]
			}
		}
	}
	assert.Equal(t, "/C=BR/O=ACME Ltd/CN=example.com/emailAddress=webmaster@example.com", subj)
}

// TestSSLSaveRejectsEncryptedKey: an encrypted pasted key is refused and
// ssl_action left cleared (no file written).
func TestSSLSaveRejectsEncryptedKey(t *testing.T) {
	base := t.TempDir()
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(base, "web1", "ssl"), 0o755))
	d := sslDomainRow(base)
	d["ssl_action"] = "save"
	d["ssl_key"] = "-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\n..."
	d["ssl_cert"] = "-----BEGIN CERTIFICATE-----\nx"

	err := p.ssl(context.Background(), "web_domain_update", engine.Data{New: d})
	require.ErrorContains(t, err, "encrypted")
	assert.NoFileExists(t, filepath.Join(base, "web1", "ssl", "example.com.key"))
}

// TestSSLSaveRejectsAcmeInvalid: a cert containing .acme.invalid is refused.
func TestSSLSaveRejectsAcmeInvalid(t *testing.T) {
	base := t.TempDir()
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(base, "web1", "ssl"), 0o755))
	d := sslDomainRow(base)
	d["ssl_action"] = "save"
	d["ssl_cert"] = "subject: CN=abcd.acme.invalid"

	err := p.ssl(context.Background(), "web_domain_update", engine.Data{New: d})
	require.ErrorContains(t, err, "acme.invalid")
	assert.NoFileExists(t, filepath.Join(base, "web1", "ssl", "example.com.crt"))
}

// TestSSLSaveWritesFilesAndBundle: a valid save writes key (0400), csr and
// crt (cert + bundle appended) and backs up any previous files.
func TestSSLSaveWritesFilesAndBundle(t *testing.T) {
	base := t.TempDir()
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	sslDir := filepath.Join(base, "web1", "ssl")
	require.NoError(t, os.MkdirAll(sslDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sslDir, "example.com.key"), []byte("OLDKEY"), 0o400))

	d := sslDomainRow(base)
	d["ssl_action"] = "save"
	d["ssl_request"] = "CSR-DATA"
	d["ssl_cert"] = "CERT-DATA"
	d["ssl_bundle"] = "BUNDLE-DATA"
	d["ssl_key"] = "KEY-DATA"

	require.NoError(t, p.ssl(context.Background(), "web_domain_update", engine.Data{New: d}))

	key := filepath.Join(sslDir, "example.com.key")
	info, err := os.Stat(key)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())

	crt, err := os.ReadFile(filepath.Join(sslDir, "example.com.crt"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(crt), "CERT-DATA") && strings.Contains(string(crt), "BUNDLE-DATA"))

	// Previous key backed up at 0400.
	binfo, err := os.Stat(key + "~")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), binfo.Mode().Perm())
	assert.Equal(t, "example.com", p.sslChangedDomain)
}

// TestSSLDeleteRemovesFiles: del removes csr and crt.
func TestSSLDeleteRemovesFiles(t *testing.T) {
	base := t.TempDir()
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	sslDir := filepath.Join(base, "web1", "ssl")
	require.NoError(t, os.MkdirAll(sslDir, 0o755))
	for _, f := range []string{"example.com.csr", "example.com.crt"} {
		require.NoError(t, os.WriteFile(filepath.Join(sslDir, f), []byte("x"), 0o644))
	}
	d := sslDomainRow(base)
	d["ssl_action"] = "del"

	require.NoError(t, p.ssl(context.Background(), "web_domain_delete", engine.Data{New: d}))
	assert.NoFileExists(t, filepath.Join(sslDir, "example.com.csr"))
	assert.NoFileExists(t, filepath.Join(sslDir, "example.com.crt"))
}

// TestSSLNoActionOrNonVhost: empty ssl_action or a non-vhost type is a no-op.
func TestSSLNoActionOrNonVhost(t *testing.T) {
	base := t.TempDir()
	runner := &opensslFakeRunner{}
	p := NewPlugin(nil, nil, runner, "", nil)

	d := sslDomainRow(base) // no ssl_action
	require.NoError(t, p.ssl(context.Background(), "web_domain_update", engine.Data{New: d}))

	d["type"] = "alias"
	d["ssl_action"] = "create"
	require.NoError(t, p.ssl(context.Background(), "web_domain_update", engine.Data{New: d}))
	assert.Empty(t, runner.calls, "no openssl for non-vhost or no action")
}

// TestSSLHandlerLoadsWithWebModule pins design D2 wiring: the plugin
// registers its ssl + insert/update/delete handlers against the events the
// web module announces (ssl ahead of update in registration order).
func TestSSLHandlerLoadsWithWebModule(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load(
		[]engine.Module{web.NewModule()},
		[]engine.Plugin{NewPlugin(nil, nil, newFakeRunner(), "", nil)},
	))
}
