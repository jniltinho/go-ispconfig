package api

// Firewall entity unit tests (add-firewall-module task 1.3): the
// declarative entity must mirror firewall.tform.php defaults, the
// port-list REGEX and the UNIQUE on server_id; the BeforeUpdate hook
// must reject any server_id change (PHP
// firewall_edit.php::onBeforeUpdate "The Server can not be changed.").

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// TestFirewallEntityFields asserts the firewall entity exposes the
// columns ISPConfig3 firewalls always carry, in the right shape.
func TestFirewallEntityFields(t *testing.T) {
	ent := firewallEntity()
	assert.Equal(t, "firewall", ent.Name)
	assert.Equal(t, "firewall_edit_title", ent.Title)
	assert.Equal(t, "admin_allow_firewall_config", ent.Policy)
	assert.True(t, ent.AdminOnly, "PHP: firewall form is admin-only")
	require.Len(t, ent.Tabs, 1)
	tab := ent.Tabs[0]
	assert.Equal(t, "firewall", tab.Name)
	fields := map[string]Field{}
	for _, f := range tab.Fields {
		fields[f.Name] = f
	}
	// server_id with UNIQUE + ISPOSITIVE.
	srv, ok := fields["server_id"]
	require.True(t, ok, "server_id present")
	assert.Equal(t, "SELECT", srv.Formtype)
	var unique, positive bool
	for _, r := range srv.Validators {
		if r.Type == "UNIQUE" {
			unique = true
			assert.Equal(t, firewallErrorUnique, r.ErrKey)
		}
		if r.Type == "ISPOSITIVE" {
			positive = true
		}
	}
	assert.True(t, unique, "server_id UNIQUE validator")
	assert.True(t, positive, "server_id ISPOSITIVE validator")
	// tcp_port / udp_port REGEX validator.
	for _, col := range []string{"tcp_port", "udp_port"} {
		f, ok := fields[col]
		require.True(t, ok, "%s present", col)
		require.Len(t, f.Validators, 1)
		assert.Equal(t, "REGEX", f.Validators[0].Type)
		assert.Equal(t, firewallTCPPortsRegex, f.Validators[0].Regex)
	}
	// tcp_port / udp_port / active defaults come from firewall.tform.php.
	assert.Equal(t, firewallDefaultTCPPort, fields["tcp_port"].Default)
	assert.Equal(t, firewallDefaultUDPPort, fields["udp_port"].Default)
	assert.Equal(t, firewallDefaultActive, fields["active"].Default)
}

// TestFirewallServerImmutable: a real server_id change must abort with
// firewall_error_server_immutable, but a full-object PUT that re-sends the
// unchanged server_id (and an update without server_id) must pass — the
// Vue form always submits the whole row (M3 review, task 3.1).
func TestFirewallServerImmutable(t *testing.T) {
	ctx := context.Background()
	id := &repository.Identity{}

	// Real change: body moves the row to a different server → reject.
	old := &model.Firewall{ServerID: 1}
	changed := &model.Firewall{ServerID: 7}
	err := firewallServerImmutable(ctx, nil, id, map[string]any{"server_id": uint32(7)}, old, changed)
	require.Error(t, err)
	ve, ok := err.(*ValidationError)
	require.True(t, ok, "ValidationError returned")
	assert.Contains(t, ve.Fields["server_id"], firewallErrorServerImm)

	// Full-object PUT re-sending the unchanged server_id → pass.
	same := &model.Firewall{ServerID: 1}
	assert.NoError(t, firewallServerImmutable(ctx, nil, id,
		map[string]any{"server_id": uint32(1), "tcp_port": "22,80"}, old, same))

	// Body without server_id → pass.
	assert.NoError(t, firewallServerImmutable(ctx, nil, id,
		map[string]any{"tcp_port": "22,80"}, old, old))

	// Defensive: server_id present but records unavailable → reject.
	err = firewallServerImmutable(ctx, nil, id, map[string]any{"server_id": uint32(7)}, nil, nil)
	require.Error(t, err)
}

// TestFirewallPortRegexMatches: the regex byte-identical to the PHP
// original (see internal/firewall/ports.go NOTE for the Go RE2 vs PHP
// PCRE acceptance divergence) — exercise the accepted/rejected sets
// documented in ports_test.go.
func TestFirewallPortRegexMatches(t *testing.T) {
	ent := firewallEntity()
	tcp := ent.Tabs[0].Fields[0] // server_id (placeholder; tcp is index 1)
	_ = tcp
	var tcpField Field
	for _, f := range ent.Tabs[0].Fields {
		if f.Name == "tcp_port" {
			tcpField = f
		}
	}
	require.NotEmpty(t, tcpField.Default)
	// 22 → matches the default tcp_port; 40110:40210 (range) too.
	assert.Regexp(t, firewallTCPPortsRegex, "22")
	assert.Regexp(t, firewallTCPPortsRegex, "40110:40210")
	assert.Regexp(t, firewallTCPPortsRegex, "")
	// Letters are still rejected.
	assert.NotRegexp(t, firewallTCPPortsRegex, "abc")
}
