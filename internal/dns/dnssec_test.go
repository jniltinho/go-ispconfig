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

	"go-ispconfig/internal/engine"
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

// TestDNSSECLifecycleDisableRemovesSigned covers "Disabling DNSSEC
// removes the signed file" without touching keys.
func TestDNSSECLifecycleDisableRemovesSigned(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	signed := zoneFilePath(cfg, "example.com.") + ".signed"
	keyFile := filepath.Join(base, "Kexample.com.+013+11111.key")
	require.NoError(t, os.WriteFile(signed, []byte("signed"), 0o644))
	require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0o644))
	p := NewPlugin(nil, nil, &scriptRunner{}, "", 1, nil)

	data := engine.Data{
		Old: map[string]any{"origin": "example.com.", "dnssec_initialized": "Y", "dnssec_wanted": "Y", "dnssec_algo": "ECDSAP256SHA256"},
		New: map[string]any{"origin": "example.com.", "dnssec_initialized": "Y", "dnssec_wanted": "N", "dnssec_algo": "ECDSAP256SHA256", "id": 1},
	}
	require.NoError(t, p.dnssecLifecycle(context.Background(), cfg, data))
	assert.NoFileExists(t, signed)
	assert.FileExists(t, keyFile, "keys stay until zone delete")
}

// TestDNSSECDeleteRemovesAllMaterials covers "Zone delete removes all
// DNSSEC materials".
func TestDNSSECDeleteRemovesAllMaterials(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	signed := zoneFilePath(cfg, "example.com.") + ".signed"
	files := []string{
		filepath.Join(base, "Kexample.com.+013+11111.key"),
		filepath.Join(base, "Kexample.com.+013+11111.private"),
		filepath.Join(base, "Kexample.com.+007+22222.key"),
		signed,
		dssetPath(cfg, "example.com"),
	}
	for _, f := range files {
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	}
	unrelated := filepath.Join(base, "Kother.example.+013+9.key")
	require.NoError(t, os.WriteFile(unrelated, []byte("x"), 0o644))
	p := NewPlugin(nil, nil, &scriptRunner{}, "", 1, nil)

	require.NoError(t, p.dnssecDelete(context.Background(), cfg, "example.com.", 0, false))
	for _, f := range files {
		assert.NoFileExists(t, f)
	}
	assert.FileExists(t, unrelated, "other domains' keys untouched")
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

func TestInjectIncludes(t *testing.T) {
	zone := "@ IN SOA ...\n"
	out, n := injectIncludes(zone, []string{"/etc/bind/Kx.+013+1.key", "/etc/bind/Kx.+013+2.key"})
	assert.Equal(t, 2, n)
	assert.Contains(t, out, "\n$INCLUDE /etc/bind/Kx.+013+1.key\n")
	assert.Contains(t, out, "\n$INCLUDE /etc/bind/Kx.+013+2.key\n")

	again, n := injectIncludes(out, []string{"/etc/bind/Kx.+013+1.key", "/etc/bind/Kx.+013+2.key"})
	assert.Equal(t, 2, n)
	assert.Equal(t, out, again, "existing $INCLUDE lines are not duplicated")
}

func TestBuildDNSSECInfo(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	require.NoError(t, os.WriteFile(dssetPath(cfg, "example.com"),
		[]byte("example.com. IN DS 12345 13 2 ABCDEF\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "Kexample.com.+013+11111.key"),
		[]byte("example.com. IN DNSKEY 256 3 13 zsk\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "Kexample.com.+013+22222.key"),
		[]byte("example.com. IN DNSKEY 257 3 13 ksk\n"), 0o644))

	info, err := buildDNSSECInfo(cfg, "example.com", []string{"ECDSAP256SHA256"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(info, "DS-Records:\nexample.com. IN DS 12345 13 2 ABCDEF\n"))
	assert.Contains(t, info, "\n------------------------------------\n\nDNSKEY-Records:\n")
	assert.Contains(t, info, "DNSKEY 256 3 13 zsk")
	assert.Contains(t, info, "DNSKEY 257 3 13 ksk")
}

// TestDNSSECUpdateBrokenZoneBlocksResign covers the bind-dnssec scenario:
// a zone file failing named-checkzone aborts the re-sign (no signzone
// call) without escalating to a datalog error.
func TestDNSSECUpdateBrokenZoneBlocksResign(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	require.NoError(t, os.WriteFile(zoneFilePath(cfg, "example.com."), []byte("zone"), 0o644))
	require.NoError(t, os.WriteFile(dssetPath(cfg, "example.com"), []byte("ds"), 0o644))
	runner := &scriptRunner{fakeRunner: fakeRunner{failCmd: "named-checkzone", failOut: "broken"}}
	p := NewPlugin(nil, nil, runner, "", 1, nil)

	soa := row{"origin": "example.com.", "dnssec_algo": "ECDSAP256SHA256", "id": 1}
	require.NoError(t, p.dnssecUpdate(context.Background(), cfg, soa, soa, false))
	assert.True(t, runner.has("named-checkzone", "example.com"), "checkzone gate uses the domain without trailing dot")
	assert.False(t, runner.has("dnssec-signzone"), "broken zone must not be signed")
}

// TestDNSSECCreateSkipsWithoutZoneFile: no rendered zone file means no
// key generation at all.
func TestDNSSECCreateSkipsWithoutZoneFile(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &scriptRunner{}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	soa := row{"origin": "example.com.", "dnssec_algo": "ECDSAP256SHA256", "id": 1}
	require.NoError(t, p.dnssecCreate(context.Background(), cfg, row{}, soa))
	assert.Empty(t, runner.calls)
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
