package firewall

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

func TestEffectivePorts(t *testing.T) {
	tcp, udp := EffectivePorts("80,443", "53", []string{"22", "8080"})
	assert.Equal(t, "80,443,22,8080", tcp)
	assert.Equal(t, "53", udp)

	// Already present protected ports are not duplicated.
	tcp, _ = EffectivePorts("22,80", "", []string{"22", "8080"})
	assert.Equal(t, "22,80,8080", tcp)

	// Empty record still gets protected ports.
	tcp, udp = EffectivePorts("", "", []string{"22", "8080"})
	assert.Equal(t, "22,8080", tcp)
	assert.Equal(t, "", udp)

	// Invalid protected tokens ignored.
	tcp, _ = EffectivePorts("80", "", []string{"abc", "0", "22"})
	assert.Equal(t, "80,22", tcp)
}

// simulateUFWEnabledProtected walks a command sequence and reports whether
// the final state is "UFW enabled without every protected TCP port allowed".
// Used by the mandatory lock-out invariant tests (design D6 / task 2.4).
func simulateUFWEnabledWithoutProtected(calls []string, protected []string) bool {
	allowed := map[string]struct{}{}
	enabled := false
	for _, c := range calls {
		switch {
		case c == "ufw --force reset":
			allowed = map[string]struct{}{}
		case c == "ufw --force enable":
			enabled = true
		case c == "ufw enable":
			enabled = true
		case c == "ufw disable" || c == "ufw --force disable":
			enabled = false
		case strings.HasPrefix(c, "ufw allow ") && strings.HasSuffix(c, "/tcp"):
			port := strings.TrimSuffix(strings.TrimPrefix(c, "ufw allow "), "/tcp")
			allowed[port] = struct{}{}
		case strings.HasPrefix(c, "ufw delete allow ") && strings.HasSuffix(c, "/tcp"):
			port := strings.TrimSuffix(strings.TrimPrefix(c, "ufw delete allow "), "/tcp")
			delete(allowed, port)
		}
	}
	if !enabled {
		return false
	}
	for _, p := range protected {
		if _, ok := allowed[p]; !ok {
			return true // broken: enabled without this protected port
		}
	}
	return false
}

func assertLockOutOK(t *testing.T, calls []string, protected ...string) {
	t.Helper()
	if simulateUFWEnabledWithoutProtected(calls, protected) {
		t.Fatalf("lock-out invariant broken: UFW ended enabled without protected ports %v\ncalls:\n  %s",
			protected, strings.Join(calls, "\n  "))
	}
	// Also assert no delete of protected while sequence ends enabled.
	enabledAtEnd := false
	for _, c := range calls {
		if c == "ufw --force enable" || c == "ufw enable" {
			enabledAtEnd = true
		}
		if c == "ufw disable" || c == "ufw --force disable" {
			enabledAtEnd = false
		}
	}
	if enabledAtEnd {
		for _, p := range protected {
			del := "ufw delete allow " + p + "/tcp"
			for _, c := range calls {
				assert.NotEqual(t, del, c, "must not delete protected port %s while ending enabled", p)
			}
		}
	}
}

// TestLockOutInsertEmpty: insert with empty tcp_port + active=y still
// opens panel + SSH before force-enable.
func TestLockOutInsertEmpty(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1) // panel 8080, ssh 22
	data := fwData(1, "", "", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))

	got := r.joined()
	assert.Contains(t, got, "ufw allow 22/tcp")
	assert.Contains(t, got, "ufw allow 8080/tcp")
	assert.Contains(t, got, "ufw --force enable")
	// Protected allows must appear before enable.
	enableIdx := indexOf(got, "ufw --force enable")
	require.Greater(t, enableIdx, indexOf(got, "ufw allow 22/tcp"))
	require.Greater(t, enableIdx, indexOf(got, "ufw allow 8080/tcp"))
	assertLockOutOK(t, got, "22", "8080")
}

// TestLockOutUpdateRemovesSSH: admin drops 22 from tcp_port while active;
// plugin must not delete 22/tcp (and may re-allow it).
func TestLockOutUpdateRemovesSSH(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22,80,443", "80,443", "", "", "y", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))

	got := r.joined()
	assert.NotContains(t, got, "ufw delete allow 22/tcp")
	assertLockOutOK(t, got, "22", "8080")
	// Panel was not in old list — must be allowed.
	assert.Contains(t, got, "ufw allow 8080/tcp")
}

// TestLockOutCustomPanelPort: config.toml server.port = 9443 is protected.
func TestLockOutCustomPanelPort(t *testing.T) {
	r := &recordingRunner{}
	p := NewPlugin(r, 1, 9443, nil)
	data := fwData(1, "", "80", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))

	got := r.joined()
	assert.Contains(t, got, "ufw allow 9443/tcp")
	assert.Contains(t, got, "ufw allow 22/tcp")
	assert.NotContains(t, got, "ufw allow 8080/tcp")
	assertLockOutOK(t, got, "22", "9443")
}

// TestLockOutCustomSSHPort: server.config ssh_port override is respected.
func TestLockOutCustomSSHPort(t *testing.T) {
	r := &recordingRunner{}
	p := NewPlugin(r, 1, 8080, nil)
	p.sshPort = 2222
	data := fwData(1, "", "", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))
	assert.Contains(t, r.joined(), "ufw allow 2222/tcp")
	assertLockOutOK(t, r.joined(), "2222", "8080")
}

// TestLockOutDeletePath: delete ends with UFW disabled (lock-out cannot
// occur). Sequence still force-resets then disables.
func TestLockOutDeletePath(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := engine.Data{
		Old: map[string]any{"server_id": float64(1), "tcp_port": "22,80", "active": "y"},
	}
	require.NoError(t, p.ufwDelete(context.Background(), "firewall_delete", data))
	got := r.joined()
	assert.Contains(t, got, "ufw --force reset")
	assert.Contains(t, got, "ufw disable")
	// Disabled final state → lock-out simulator must not flag.
	assertLockOutOK(t, got, "22", "8080")
	assert.False(t, simulateUFWEnabledWithoutProtected(got, []string{"22", "8080"}))
}

// TestLockOutInvariantRejectsBrokenSequence documents that the helper
// itself fails a sequence that enables UFW without SSH (mandatory CI
// coverage of the invariant checker).
func TestLockOutInvariantRejectsBrokenSequence(t *testing.T) {
	broken := []string{
		"ufw --force reset",
		"ufw allow 80/tcp",
		"ufw --force enable",
	}
	assert.True(t, simulateUFWEnabledWithoutProtected(broken, []string{"22", "8080"}),
		"checker must flag enable without SSH/panel")

	good := []string{
		"ufw --force reset",
		"ufw allow 22/tcp",
		"ufw allow 8080/tcp",
		"ufw --force enable",
	}
	assert.False(t, simulateUFWEnabledWithoutProtected(good, []string{"22", "8080"}))
}

// TestLockOutInactiveDoesNotForceProtected: when active=n the plugin
// disables UFW and may delete ports that would be protected when enabled.
func TestLockOutInactiveDoesNotForceProtected(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	// Drop 22 while deactivating — delete of 22 is OK because UFW ends disabled.
	data := fwData(1, "22,80", "80", "", "", "y", "n")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))
	got := r.joined()
	assert.Contains(t, got, "ufw disable")
	assert.NotContains(t, got, "ufw --force enable")
	// No forced allow of 8080 when disabling.
	assert.NotContains(t, got, "ufw allow 8080/tcp")
	assertLockOutOK(t, got, "22", "8080")
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}
