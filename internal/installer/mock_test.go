package installer

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockExec records every command and answers from canned responses; tests
// never touch the OS.
type mockExec struct {
	// calls is each invocation as "name arg1 arg2 ...".
	calls []string
	// fail maps a command prefix to an error message; a Run whose joined
	// command starts with the prefix fails.
	fail map[string]string
	// output maps a command prefix to canned stdout.
	output map[string]string
	// missing lists LookPath names that are not found.
	missing map[string]bool
}

func (m *mockExec) Run(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	m.calls = append(m.calls, cmd)
	for prefix, msg := range m.fail {
		if strings.HasPrefix(cmd, prefix) {
			return []byte(msg), fmt.Errorf("%s: exit 1: %s", cmd, msg)
		}
	}
	for prefix, out := range m.output {
		if strings.HasPrefix(cmd, prefix) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func (m *mockExec) LookPath(name string) (string, error) {
	if m.missing[name] {
		return "", fmt.Errorf("%s: not found", name)
	}
	return "/usr/bin/" + name, nil
}

// called reports whether any recorded call starts with prefix.
func (m *mockExec) called(prefix string) bool {
	for _, c := range m.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// testState builds a State wired to a mock executor and temp dirs.
func testState(t *testing.T) (*State, *mockExec, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	mock := &mockExec{fail: map[string]string{}, output: map[string]string{}, missing: map[string]bool{}}
	out := &bytes.Buffer{}
	st := NewState(profileFor("debian12", "Debian 12"), &Answers{
		Hostname: "srv1.example.com", PanelPort: 8080,
		DBName: "dbispconfig", DBUser: "ispconfig",
		EnableWeb: true, EnableDNS: true, InstallPHPFPM: true,
	})
	st.Exec = mock
	st.Out = out
	st.Euid = 0
	st.SystemdMarker = dir // exists => "systemd present"
	st.ConfigDir = dir + "/etc/go-ispconfig"
	st.SystemdDir = dir + "/etc/systemd/system"
	st.BinPath = dir + "/usr/local/bin/go-ispconfig"
	st.CredentialsFile = dir + "/root/.go-ispconfig-credentials"
	st.LegacyMarker = dir + "/usr/local/ispconfig/server/lib/config.inc.php"
	st.AcmeWebroot = dir + "/usr/local/ispconfig/interface/acme"

	// Point the profile's absolute paths into the temp dir.
	p := st.Profile
	p.NginxConfigDir = dir + "/etc/nginx"
	p.NginxVhostConfDir = dir + "/etc/nginx/sites-available"
	p.NginxVhostEnabledDir = dir + "/etc/nginx/sites-enabled"
	p.WebsiteBasedir = dir + "/var/www"
	p.BindZonefilesDir = dir + "/etc/bind"
	p.NamedConfPath = dir + "/etc/bind/named.conf"
	p.NamedConfLocalPath = dir + "/etc/bind/named.conf.local"
	p.NamedConfOptionsPath = dir + "/etc/bind/named.conf.options"
	return st, mock, out
}
