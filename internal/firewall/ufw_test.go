package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recordingRunner captures every argv and scripts ufw --version replies.
type recordingRunner struct {
	calls   [][]string
	version string
	missing bool
	failOn  map[string]bool // joined argv → error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	r.calls = append(r.calls, cmd)
	key := strings.Join(cmd, " ")
	if r.failOn[key] {
		return []byte("fail"), errors.New("forced failure")
	}
	if name == "ufw" && len(args) == 1 && args[0] == "--version" {
		if r.missing {
			return nil, errors.New("executable file not found in $PATH")
		}
		ver := r.version
		if ver == "" {
			ver = "0.36.1"
		}
		return []byte("ufw " + ver + "\nCopyright 2008-2023\n"), nil
	}
	return []byte("ok"), nil
}

func (r *recordingRunner) joined() []string {
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = strings.Join(c, " ")
	}
	return out
}

func (r *recordingRunner) hasPrefix(prefix string) bool {
	for _, c := range r.joined() {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (r *recordingRunner) countPrefix(prefix string) int {
	n := 0
	for _, c := range r.joined() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func testPlugin(r *recordingRunner, serverID uint32) *Plugin {
	return NewPlugin(r, serverID, DefaultPanelPort, nil)
}

func fwData(serverID uint32, oldTCP, newTCP, oldUDP, newUDP, oldActive, newActive string) engine.Data {
	return engine.Data{
		Old: map[string]any{
			"server_id": float64(serverID),
			"tcp_port":  oldTCP,
			"udp_port":  oldUDP,
			"active":    oldActive,
		},
		New: map[string]any{
			"server_id": float64(serverID),
			"tcp_port":  newTCP,
			"udp_port":  newUDP,
			"active":    newActive,
		},
	}
}

func TestUFWUpdateInsertBaseline(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "", "22,80", "", "53", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))

	got := r.joined()
	require.GreaterOrEqual(t, len(got), 6)
	assert.Equal(t, "ufw --version", got[0])
	assert.Equal(t, "ufw --force disable", got[1])
	assert.Equal(t, "ufw --force reset", got[2])
	assert.Equal(t, "ufw default deny incoming", got[3])
	assert.Equal(t, "ufw default allow outgoing", got[4])
	assert.Contains(t, got, "ufw allow 22/tcp")
	assert.Contains(t, got, "ufw allow 80/tcp")
	assert.Contains(t, got, "ufw allow 53/udp")
	assert.Contains(t, got, "ufw --force enable")
	assert.False(t, r.hasPrefix("ufw reload"), "fresh enable must not reload")
}

func TestUFWUpdateDoesNotReset(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22,80", "22,80,443", "53", "53", "y", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))

	assert.False(t, r.hasPrefix("ufw --force reset"), "update must not reset")
	assert.Equal(t, 1, r.countPrefix("ufw allow 443/tcp"))
	assert.False(t, r.hasPrefix("ufw allow 22/tcp"), "unchanged ports are not re-allowed")
	assert.Contains(t, r.joined(), "ufw reload")
	assert.False(t, r.hasPrefix("ufw --force enable"))
}

func TestUFWUpdateRemovesUDPPort(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22", "22", "53,123", "53", "y", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))

	assert.Contains(t, r.joined(), "ufw delete allow 123/udp")
	assert.False(t, r.hasPrefix("ufw delete allow 53/udp"))
}

func TestUFWUpdateRangeToken(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22", "22,40110:40210", "", "", "y", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))
	assert.Contains(t, r.joined(), "ufw allow 40110:40210/tcp")
}

func TestUFWUpdateActivateForceEnable(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22", "22", "", "", "n", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))
	assert.Contains(t, r.joined(), "ufw --force enable")
	assert.False(t, r.hasPrefix("ufw reload"))
}

func TestUFWUpdateDeactivate(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(1, "22", "22", "", "", "y", "n")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_update", data))
	assert.Contains(t, r.joined(), "ufw disable")
}

func TestUFWMissingSkipsApply(t *testing.T) {
	r := &recordingRunner{missing: true}
	p := testPlugin(r, 1)
	data := fwData(1, "", "22", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))
	assert.Equal(t, []string{"ufw --version"}, r.joined(), "only the probe runs when ufw is missing")
}

func TestUFWTooOldSkipsApply(t *testing.T) {
	r := &recordingRunner{version: "0.29"}
	p := testPlugin(r, 1)
	data := fwData(1, "", "22", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))
	assert.Equal(t, []string{"ufw --version"}, r.joined())
}

func TestUFWSkipNonLocalServer(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := fwData(9, "", "22", "", "", "", "y")
	require.NoError(t, p.ufwUpdate(context.Background(), "firewall_insert", data))
	assert.Empty(t, r.calls, "foreign server_id must not run any ufw command")
}

func TestParseUFWVersion(t *testing.T) {
	v, err := parseUFWVersion("ufw 0.36.1\nCopyright\n")
	require.NoError(t, err)
	assert.Equal(t, "0.36.1", v)

	_, err = parseUFWVersion("broken")
	require.Error(t, err)
}

func TestCompareVersion(t *testing.T) {
	assert.Equal(t, -1, compareVersion("0.29", "0.30"))
	assert.Equal(t, 0, compareVersion("0.30", "0.30"))
	assert.Equal(t, 1, compareVersion("0.36.1", "0.30"))
}
