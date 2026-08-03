package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner records commands without executing anything: these helpers run
// chown/chattr as root, which no test may do.
type fakeRunner struct {
	mu   sync.Mutex
	runs [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, append([]string{name}, args...))
	return nil, nil
}

func (f *fakeRunner) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.runs))
	for _, r := range f.runs {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

func TestRowAccessors(t *testing.T) {
	r := Row{
		"s": "web1", "b": []byte("web2"), "f": float64(42), "i": 7,
		"u": uint32(9), "nil": nil, "numstr": " 13 ",
	}
	assert.Equal(t, "web1", r.Str("s"))
	assert.Equal(t, "web2", r.Str("b"))
	assert.Equal(t, "42", r.Str("f"), "a json number renders without the .0 tail")
	assert.Equal(t, "", r.Str("nil"))
	assert.Equal(t, "", r.Str("missing"))

	assert.Equal(t, int64(42), r.Num("f"))
	assert.Equal(t, int64(7), r.Num("i"))
	assert.Equal(t, int64(9), r.Num("u"))
	assert.Equal(t, int64(13), r.Num("numstr"), "PHP-era payloads carry numbers as strings")
	assert.Equal(t, int64(0), r.Num("s"))
	assert.Equal(t, int64(0), r.Num("missing"))
}

func TestUnderDocroot(t *testing.T) {
	const docroot = "/var/www/web1"
	tests := []struct {
		dir  string
		want bool
	}{
		{"/var/www/web1", true},
		{"/var/www/web1/", true},
		{"/var/www/web1/files", true},
		{"/var/www/web12", false},        // sibling sharing the prefix
		{"/var/www/web1files", false},    // no path boundary
		{"/var/www/web1/../web2", false}, // traversal
		{"/var/www/./web1", false},
		{"/var/www", false},
		{"var/www/web1", false}, // relative
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, UnderDocroot(tt.dir, docroot), tt.dir)
	}
	assert.False(t, UnderDocroot("/var/www/web1", ""), "an empty docroot matches nothing")
}

func TestCheckPathRejectsSymlinkAndExoticChars(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	real := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(real, 0o755))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(real, link))

	assert.True(t, CheckPath(real))
	assert.False(t, CheckPath(link), "a symlinked component could redirect a root chattr")
	assert.False(t, CheckPath(filepath.Join(link, "sub")), "symlink anywhere along the path")
	assert.False(t, CheckPath("relative/path"))
	assert.False(t, CheckPath("/var/www/web1; rm -rf /"))
}

func TestIsAllowedPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/var/www/clients/client1/web1", true},
		{"/home/web1", true},
		{"/srv/www/web1", true},
		{"/", false},
		{"/etc", false},
		{"/etc/ssh", false},
		{"/proc/1", false},
		{"/sys", false},
		{"/dev/null", false},
		{"/tmp/x", false},
		{"/run/lock", false},
		{"/boot", false},
		{"/root", false},
		{"/var", false},
		{"/var/", false},
		{"/var/backup", false},
		{"/var/backups/web1", false},
		// only /var itself and /var/backup(s) are refused
		{"/var/www", true},
		{"//var//www//web1", true},
		{"/var/www/web1/", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, IsAllowedPath(tt.path), tt.path)
	}
}

func TestIsAllowedPathResolvesSymlinkIntoSystemDir(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := filepath.Join(base, "sneaky")
	require.NoError(t, os.Symlink("/etc", link))

	assert.False(t, IsAllowedPath(link),
		"an existing path is resolved first, so a link into /etc is caught")
}

func TestMkdirPathCreatesEveryComponent(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	runner := &fakeRunner{}
	target := filepath.Join(base, "web1", "home", "web1ftp")

	require.NoError(t, MkdirPath(context.Background(), runner, target, 0o750, "web1", "client1"))

	for _, dir := range []string{filepath.Join(base, "web1"), filepath.Dir(target), target} {
		fi, err := os.Stat(dir)
		require.NoError(t, err, dir)
		assert.Equal(t, os.FileMode(0o750), fi.Mode().Perm(), dir+" ignores the process umask")
	}
	assert.Equal(t, []string{
		"chown web1:client1 " + filepath.Join(base, "web1"),
		"chown web1:client1 " + filepath.Dir(target),
		"chown web1:client1 " + target,
	}, runner.all(), "only the components actually created are chowned")
}

func TestMkdirPathSkipsExistingAndRefusesFiles(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	runner := &fakeRunner{}
	require.NoError(t, os.Mkdir(filepath.Join(base, "web1"), 0o700))

	require.NoError(t, MkdirPath(context.Background(), runner, filepath.Join(base, "web1", "home"), 0o755, "web1", "client1"))
	assert.Equal(t, []string{"chown web1:client1 " + filepath.Join(base, "web1", "home")}, runner.all(),
		"the pre-existing component is left alone, mode and owner included")

	require.NoError(t, os.WriteFile(filepath.Join(base, "file"), nil, 0o644))
	err = MkdirPath(context.Background(), runner, filepath.Join(base, "file", "sub"), 0o755, "", "")
	assert.ErrorContains(t, err, "is not a directory")
}

func TestChownArgs(t *testing.T) {
	ctx := context.Background()
	runner := &fakeRunner{}
	require.NoError(t, Chown(ctx, runner, "/srv/web1", "web1", "client1", false))
	require.NoError(t, Chown(ctx, runner, "/srv/web1/.ssh", "web1", "client1", true))
	require.NoError(t, Chown(ctx, runner, "/srv/web1/home", "root", "", false))

	assert.Equal(t, []string{
		"chown web1:client1 /srv/web1",
		"chown -R web1:client1 /srv/web1/.ssh",
		"chown root /srv/web1/home",
	}, runner.all())
}

func TestWebFolderProtectionRules(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	docroot := filepath.Join(base, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))
	ctx := context.Background()
	runner := &fakeRunner{}

	require.NoError(t, WebFolderProtection(ctx, runner, nil, docroot, false, false))
	assert.Equal(t, []string{"chattr -i " + docroot}, runner.all(),
		"unlocking is unconditional, so a server that just disabled the option still unlocks")

	require.NoError(t, WebFolderProtection(ctx, runner, nil, docroot, true, false))
	assert.Len(t, runner.all(), 1, "no relock while web_folder_protection is off")

	require.NoError(t, WebFolderProtection(ctx, runner, nil, docroot, true, true))
	assert.Equal(t, "chattr +i "+docroot, runner.all()[1])

	require.NoError(t, WebFolderProtection(ctx, runner, nil, "/", false, true))
	require.NoError(t, WebFolderProtection(ctx, runner, nil, "/var", false, true))
	assert.Len(t, runner.all(), 2, "/ and short paths are never chattr'd")
}
