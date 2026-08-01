package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// fakeRunner emulates the OS commands ensureSite issues: getent answers from
// the users/groups sets, useradd/groupadd mutate them, everything is
// recorded.
type fakeRunner struct {
	users, groups map[string]bool
	calls         [][]string
	failCmd       string // command name that fails when non-empty
	failOut       string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{users: map[string]bool{}, groups: map[string]bool{}}
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name == f.failCmd {
		return []byte(f.failOut), fmt.Errorf("exit status 1")
	}
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

func (f *fakeRunner) commands() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// testPlugin builds a plugin with temp dirs and a fake runner (no DB).
func testPlugin(t *testing.T, r *fakeRunner) (*Plugin, string) {
	t.Helper()
	base := t.TempDir()
	p := NewPlugin(nil, nil, r, "", nil)
	p.logBaseDir = filepath.Join(base, "httpd-logs")
	return p, base
}

func vhostRow(base string) row {
	return row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "type": "vhost",
		"document_root": filepath.Join(base, "clients/client1/web1"),
		"system_user":   "web1", "system_group": "client1",
		"errordocs": float64(1), "active": "y",
	}
}

func webCfg(base string) *getconf.WebConfig {
	return &getconf.WebConfig{WebsiteBasedir: base, SecurityLevel: "20"}
}

// TestEnsureSiteProvisionsTree covers "New vhost domain provisions the
// tree": directories with correct modes exist and user/group are created.
func TestEnsureSiteProvisionsTree(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	d := vhostRow(base)
	docroot := d.str("document_root")

	require.NoError(t, p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: d,
	}))

	for rel, mode := range map[string]os.FileMode{
		"web": 0o751, "web/error": 0o755, "log": 0o750, "ssl": 0o755,
		"tmp": os.ModeSticky | 0o777, "private": 0o710, "cgi-bin": 0o755,
	} {
		info, err := os.Stat(filepath.Join(docroot, rel))
		require.NoErrorf(t, err, "missing %s", rel)
		assert.Equalf(t, mode, info.Mode().Perm()|info.Mode()&os.ModeSticky, "mode of %s", rel)
	}
	info, err := os.Stat(filepath.Join(p.logBaseDir, "example.com"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())

	assert.True(t, r.groups["client1"], "group created")
	assert.True(t, r.users["web1"], "user created")
	cmds := r.commands()
	assert.Contains(t, cmds, "groupadd client1")
	assert.Contains(t, cmds, "useradd -d "+docroot+" -g client1 -s /bin/false web1")
	assert.Contains(t, cmds, "chown web1:client1 "+filepath.Join(docroot, "tmp"))
	assert.Contains(t, cmds, "chown root:root "+filepath.Join(docroot, "ssl"))
}

// TestEnsureSiteIdempotent covers "Re-running is a no-op": the second run
// succeeds and creates no user or group again.
func TestEnsureSiteIdempotent(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	s := site{cfg: webCfg(base), action: "update", old: vhostRow(base), new: vhostRow(base)}

	require.NoError(t, p.ensureSite(context.Background(), s))
	firstCalls := len(r.calls)
	require.NoError(t, p.ensureSite(context.Background(), s))

	var creations []string
	for _, c := range r.calls[firstCalls:] {
		if c[0] == "useradd" || c[0] == "groupadd" {
			creations = append(creations, c[0])
		}
	}
	assert.Empty(t, creations, "no user/group creation on second run")
}

// TestEnsureSiteMovesDocroot covers "Domain rename moves the docroot".
func TestEnsureSiteMovesDocroot(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)

	old := vhostRow(base)
	require.NoError(t, p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: old,
	}))
	marker := filepath.Join(old.str("document_root"), "web", "index.html")
	require.NoError(t, os.WriteFile(marker, []byte("hello"), 0o644))

	updated := vhostRow(base)
	updated["document_root"] = filepath.Join(base, "clients/client2/web1")
	updated["system_group"] = "client2"
	require.NoError(t, p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "update", old: old, new: updated,
	}))

	moved, err := os.ReadFile(filepath.Join(updated.str("document_root"), "web", "index.html"))
	require.NoError(t, err, "site data must follow the docroot move")
	assert.Equal(t, "hello", string(moved))
	_, err = os.Stat(old.str("document_root"))
	assert.True(t, os.IsNotExist(err), "old docroot is gone")

	cmds := r.commands()
	assert.Contains(t, cmds, "chown -R web1:client2 "+updated.str("document_root"))
	assert.Contains(t, cmds, "usermod --home "+updated.str("document_root")+" --gid client2 web1")
}

// TestEnsureSiteRefusesUnsafePaths: docroots outside website_basedir, equal
// to it, or with traversal are rejected before anything is created.
func TestEnsureSiteRefusesUnsafePaths(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)

	for _, docroot := range []string{
		"/etc/nginx", base, base + "/../evil", "relative/path", "/", "",
	} {
		d := vhostRow(base)
		d["document_root"] = docroot
		err := p.ensureSite(context.Background(), site{
			cfg: webCfg(base), action: "insert", old: row{}, new: d,
		})
		assert.Errorf(t, err, "docroot %q must be refused", docroot)
	}
	assert.Empty(t, r.calls, "no OS command may run for unsafe paths")
}

// TestEnsureSiteRefusesForbiddenUsers: root-owned sites are refused.
func TestEnsureSiteRefusesForbiddenUsers(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	d := vhostRow(base)
	d["system_user"] = "root"
	err := p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: d,
	})
	require.ErrorContains(t, err, "not allowed")
}

// TestEnsureSiteSubdomainTree: vhostsubdomain creates its web_folder and
// per-host log folder inside the parent docroot, but not ssl/tmp/private.
func TestEnsureSiteSubdomainTree(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	d := vhostRow(base)
	d["type"] = "vhostsubdomain"
	d["domain"] = "blog.example.com"
	d["web_folder"] = "blog"
	d["errordocs"] = float64(0)

	require.NoError(t, p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: d,
		parentDomain: "example.com",
	}))
	docroot := d.str("document_root")
	assert.DirExists(t, filepath.Join(docroot, "blog"))
	assert.DirExists(t, filepath.Join(docroot, "log", "blog"))
	assert.NoDirExists(t, filepath.Join(docroot, "tmp"))

	// Blacklisted web folder is refused.
	d["web_folder"] = "etc/x"
	err := p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: d, parentDomain: "example.com",
	})
	assert.ErrorContains(t, err, "blacklisted")
}

// TestEnsureSiteCommandFailureSurfaces: a failing useradd aborts with its
// output in the error.
func TestEnsureSiteCommandFailureSurfaces(t *testing.T) {
	r := newFakeRunner()
	r.failCmd = "useradd"
	r.failOut = "useradd: UID range exhausted"
	p, base := testPlugin(t, r)

	err := p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: vhostRow(base),
	})
	require.ErrorContains(t, err, "UID range exhausted")
}

// TestWebFolderOf pins the folder normalization rules.
func TestWebFolderOf(t *testing.T) {
	assert.Equal(t, "web", webFolderOf(row{"type": "vhost", "web_folder": ""}))
	assert.Equal(t, "web/sub", webFolderOf(row{"type": "vhost", "web_folder": "/sub/"}))
	assert.Equal(t, "blog", webFolderOf(row{"type": "vhostsubdomain", "web_folder": "blog"}))
}

// TestLogFolderOf pins the per-host log folder rules.
func TestLogFolderOf(t *testing.T) {
	assert.Equal(t, "log", logFolderOf(row{"type": "vhost"}, ""))
	assert.Equal(t, "log/blog",
		logFolderOf(row{"type": "vhostsubdomain", "domain": "blog.example.com"}, "example.com"))
	assert.Equal(t, "log/web7",
		logFolderOf(row{"type": "vhostalias", "domain": "other.org", "domain_id": float64(7)}, "example.com"))
}

// sanity: fakeRunner honors argv slices only (no shell).
func TestFakeRunnerRecordsArgv(t *testing.T) {
	r := newFakeRunner()
	_, _ = r.Run(context.Background(), "chown", "a b", "c")
	assert.True(t, slices.Equal([]string{"chown", "a b", "c"}, r.calls[0]))
}
