package site

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// fakeRunner emulates the OS commands Ensure issues: getent answers from the
// users/groups sets, useradd/groupadd mutate them, everything is recorded.
type fakeRunner struct {
	users, groups map[string]bool
	calls         [][]string
}

// newFakeRunner returns a runner with no pre-existing users or groups.
func newFakeRunner() *fakeRunner {
	return &fakeRunner{users: map[string]bool{}, groups: map[string]bool{}}
}

// Run records the argv and fakes getent/useradd/groupadd.
func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch name {
	case "getent":
		kind, key := args[0], args[1]
		if (kind == "passwd" && f.users[key]) || (kind == "group" && f.groups[key]) {
			return nil, nil
		}
		return nil, fmt.Errorf("exit status 2")
	case "useradd":
		f.users[args[len(args)-1]] = true
	case "groupadd":
		f.groups[args[len(args)-1]] = true
	}
	return nil, nil
}

// commands returns every recorded call as a single line.
func (f *fakeRunner) commands() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func vhostRow(base string) Row {
	return Row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "type": "vhost",
		"document_root": filepath.Join(base, "clients/client1/web1"),
		"system_user":   "web1", "system_group": "client1",
		"errordocs": float64(1), "active": "y",
	}
}

// TestEnsureBothWebServers pins the layout both plugins get out of Ensure and
// the one knob that differs: the worker user joined to the client group.
func TestEnsureBothWebServers(t *testing.T) {
	for _, tc := range []struct {
		tag, worker string
	}{
		{"nginx", "www-data"},
		{"apache2", "apache"},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			base, r := t.TempDir(), newFakeRunner()
			logs := filepath.Join(base, "logs-"+tc.tag)
			d := vhostRow(base)

			require.NoError(t, Ensure(context.Background(), Request{
				Tag: tc.tag, WorkerUser: tc.worker, LogBaseDir: logs,
				Cfg:    &getconf.WebConfig{WebsiteBasedir: base, SecurityLevel: "20"},
				Action: "insert", Old: Row{}, New: d, Runner: r,
			}))

			docroot := d.Str("document_root")
			for rel, mode := range map[string]os.FileMode{
				"web": 0o751, "web/error": 0o755, "log": 0o750, "ssl": 0o755,
				"tmp": os.ModeSticky | 0o777, "private": 0o710, "cgi-bin": 0o755,
			} {
				info, err := os.Stat(filepath.Join(docroot, rel))
				require.NoErrorf(t, err, "missing %s", rel)
				assert.Equalf(t, mode, info.Mode().Perm()|info.Mode()&os.ModeSticky, "mode of %s", rel)
			}
			assert.DirExists(t, filepath.Join(logs, "example.com"))
			assert.FileExists(t, filepath.Join(docroot, "web", "standard_index.html"))

			cmds := r.commands()
			assert.Contains(t, cmds, "groupadd client1")
			assert.Contains(t, cmds, "useradd -d "+docroot+" -g client1 -s /bin/false web1")
			assert.Contains(t, cmds, "usermod -a -G client1 "+tc.worker,
				"the web server worker must be able to read the site")
		})
	}
}

// TestEnsureRejectsUnsafeInput: nothing is created and no command runs for a
// docroot outside website_basedir or a forbidden system user.
func TestEnsureRejectsUnsafeInput(t *testing.T) {
	base := t.TempDir()
	cfg := &getconf.WebConfig{WebsiteBasedir: base, SecurityLevel: "20"}

	for name, mutate := range map[string]func(Row){
		"outside basedir":  func(d Row) { d["document_root"] = "/etc/apache2" },
		"basedir itself":   func(d Row) { d["document_root"] = base },
		"traversal":        func(d Row) { d["document_root"] = base + "/../evil" },
		"root user":        func(d Row) { d["system_user"] = "root" },
		"option-like user": func(d Row) { d["system_user"] = "-u0" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newFakeRunner()
			d := vhostRow(base)
			mutate(d)
			err := Ensure(context.Background(), Request{
				Tag: "apache2", LogBaseDir: filepath.Join(base, "logs"), Cfg: cfg,
				Action: "insert", Old: Row{}, New: d, Runner: r,
			})
			require.Error(t, err)
			assert.Empty(t, r.calls, "no OS command may run for unsafe input")
		})
	}
}

// TestWebAndLogFolder pins the folder normalization rules shared by both
// plugins' vhost templates.
func TestWebAndLogFolder(t *testing.T) {
	assert.Equal(t, "web", WebFolder(Row{"type": "vhost", "web_folder": ""}))
	assert.Equal(t, "web/sub", WebFolder(Row{"type": "vhost", "web_folder": "/sub/"}))
	assert.Equal(t, "blog", WebFolder(Row{"type": "vhostsubdomain", "web_folder": "blog"}))

	assert.Equal(t, "log", LogFolder(Row{"type": "vhost"}, ""))
	assert.Equal(t, "log/blog",
		LogFolder(Row{"type": "vhostsubdomain", "domain": "blog.example.com"}, "example.com"))
	assert.Equal(t, "log/web7",
		LogFolder(Row{"type": "vhostalias", "domain": "other.org", "domain_id": float64(7)}, "example.com"))
}
