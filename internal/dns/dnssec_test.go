package dns

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptRunner extends fakeRunner with per-command behavior so tests can
// emulate the side effects of the BIND tools (key/dsset/signed files).
type scriptRunner struct {
	fakeRunner
	handler func(name string, args ...string) ([]byte, error)
}

func (s *scriptRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := s.fakeRunner.Run(ctx, name, args...)
	if err != nil || s.handler == nil {
		return out, err
	}
	return s.handler(name, args...)
}

// fakeKeygen creates plausible key files like dnssec-keygen would.
func fakeKeygen(t *testing.T, keyDir string) func(name string, args ...string) ([]byte, error) {
	t.Helper()
	tag := 10000
	return func(name string, args ...string) ([]byte, error) {
		if name != "dnssec-keygen" {
			return nil, nil
		}
		domain := args[len(args)-1]
		algo := "013"
		for i, a := range args {
			if a == "-a" && args[i+1] == "NSEC3RSASHA1" {
				algo = "007"
			}
		}
		tag++
		base := filepath.Join(keyDir, "K"+domain+".+"+algo+"+"+strconv.Itoa(tag))
		require.NoError(t, os.WriteFile(base+".key", []byte(domain+" IN DNSKEY 257 3 13 fake\n"), 0o644))
		require.NoError(t, os.WriteFile(base+".private", []byte("Private-key-format: v1.3\n"), 0o600))
		return []byte(base), nil
	}
}

func TestDNSSECAlgos(t *testing.T) {
	assert.Equal(t, []string{"ECDSAP256SHA256"}, dnssecAlgos("ECDSAP256SHA256"))
	assert.Equal(t, []string{"NSEC3RSASHA1", "ECDSAP256SHA256"}, dnssecAlgos("NSEC3RSASHA1,ECDSAP256SHA256"),
		"input (SET) order preserved like the PHP explode")
	assert.Empty(t, dnssecAlgos(""))
	assert.Empty(t, dnssecAlgos("RSASHA1"))
}

func TestDNSSECCreateKeysGeneratesPairPerAlgorithm(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &scriptRunner{handler: fakeKeygen(t, base)}
	p := NewPlugin(nil, nil, runner, "", 1, nil)

	require.NoError(t, p.dnssecCreateKeys(context.Background(), cfg,
		"example.com", []string{"ECDSAP256SHA256", "NSEC3RSASHA1"}))

	assert.Len(t, keyFiles(cfg, "example.com", "ECDSAP256SHA256", ".key"), 2, "ZSK+KSK for alg 13")
	assert.Len(t, keyFiles(cfg, "example.com", "NSEC3RSASHA1", ".key"), 2, "ZSK+KSK for alg 7")
	assert.Len(t, allKeyFiles(cfg, "example.com", ".key"), 4)
	assert.Equal(t, base+"/dsset-example.com.", dssetPath(cfg, "example.com"))

	var keygenCalls, kskCalls int
	for _, c := range runner.calls {
		if c[0] != "dnssec-keygen" {
			continue
		}
		keygenCalls++
		assert.Equal(t, []string{"-K", base}, c[1:3], "keys land in bind_keyfiles_dir")
		if strings.Contains(strings.Join(c, " "), "-f KSK") {
			kskCalls++
		}
	}
	assert.Equal(t, 4, keygenCalls)
	assert.Equal(t, 2, kskCalls)
}

// TestDNSSECCreateKeysPreservesExisting covers "Existing keys are
// preserved": key files present for the algorithm mean no keygen runs.
func TestDNSSECCreateKeysPreservesExisting(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	existing := filepath.Join(base, "Kexample.com.+013+11111.key")
	require.NoError(t, os.WriteFile(existing, []byte("existing"), 0o644))
	runner := &scriptRunner{handler: fakeKeygen(t, base)}
	p := NewPlugin(nil, nil, runner, "", 1, nil)

	require.NoError(t, p.dnssecCreateKeys(context.Background(), cfg,
		"example.com", []string{"ECDSAP256SHA256"}))

	assert.False(t, runner.has("dnssec-keygen"), "no keygen when keys exist")
	assert.Equal(t, []string{existing}, keyFiles(cfg, "example.com", "ECDSAP256SHA256", ".key"))
}
