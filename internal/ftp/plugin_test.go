package ftp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// fakeRunner records commands without executing anything: the plugin runs
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

// testPlugin builds a plugin whose parent site is a temp docroot owned by
// web1:client1, with web_folder_protection enabled.
func testPlugin(t *testing.T) (*Plugin, *fakeRunner, string) {
	t.Helper()
	// The docroot must survive checkPath: no symlinked component. On macOS
	// t.TempDir() is under a symlinked /var, so resolve it first.
	docroot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	docroot = filepath.Join(docroot, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))

	runner := &fakeRunner{}
	p := NewPlugin(nil, runner, nil)
	p.LoadWeb = func(int64) (row, error) {
		return row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"system_user": "web1", "system_group": "client1",
		}, nil
	}
	p.LoadWebConfig = func(uint32) (*getconf.WebConfig, error) {
		return &getconf.WebConfig{WebFolderProtection: "y"}, nil
	}
	return p, runner, docroot
}

func ftpUser(dir string) map[string]any {
	return map[string]any{
		"ftp_user_id": float64(5), "username": "web1ftp", "dir": dir,
		"parent_domain_id": float64(1),
	}
}

func TestInsertCreatesDirAsSiteUser(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	dir := filepath.Join(docroot, "files", "uploads")

	require.NoError(t, p.ftpUserInsert(context.Background(), "ftp_user_insert",
		engine.Data{New: ftpUser(dir)}))

	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	assert.Equal(t, []string{
		"chattr -i " + docroot,
		"chown web1:client1 " + filepath.Join(docroot, "files"),
		"chown web1:client1 " + dir,
		"chattr +i " + docroot,
	}, runner.all(), "docroot unlocked, every new component owned by the site, then relocked")
}

func TestInsertExistingDirDoesNothing(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	dir := filepath.Join(docroot, "web")
	require.NoError(t, os.Mkdir(dir, 0o700))

	require.NoError(t, p.ftpUserInsert(context.Background(), "ftp_user_insert",
		engine.Data{New: ftpUser(dir)}))

	assert.Empty(t, runner.all(), "no chattr dance for a directory that is already there")
	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm(), "existing mode left alone")
}

func TestInsertOutsideDocrootIsRefused(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	// Sibling directory sharing the docroot prefix: accepted by the PHP
	// string compare, rejected here.
	dir := docroot + "2/files"

	require.NoError(t, p.ftpUserInsert(context.Background(), "ftp_user_insert",
		engine.Data{New: ftpUser(dir)}))

	assert.NoDirExists(t, dir)
	assert.Empty(t, runner.all())
}

func TestInsertKeepsProtectionWhenMkdirFails(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	// A regular file where a path component must be created.
	require.NoError(t, os.WriteFile(filepath.Join(docroot, "files"), nil, 0o644))

	err := p.ftpUserInsert(context.Background(), "ftp_user_insert",
		engine.Data{New: ftpUser(filepath.Join(docroot, "files", "up"))})

	require.Error(t, err)
	assert.Equal(t, "chattr +i "+docroot, runner.all()[len(runner.all())-1],
		"docroot is relocked even when the mkdir failed")
}

func TestUpdateMovesDirAndDropsOldQuota(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	oldDir, newDir := filepath.Join(docroot, "old"), filepath.Join(docroot, "new")
	require.NoError(t, os.Mkdir(oldDir, 0o755))
	quota := filepath.Join(oldDir, quotaFile)
	require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))

	require.NoError(t, p.ftpUserUpdate(context.Background(), "ftp_user_update",
		engine.Data{Old: ftpUser(oldDir), New: ftpUser(newDir)}))

	assert.DirExists(t, newDir)
	assert.NoFileExists(t, quota, "the quota state of the previous location is stale")
	assert.Contains(t, runner.all(), "chown web1:client1 "+newDir)
}

func TestUpdateSameDirKeepsQuota(t *testing.T) {
	p, _, docroot := testPlugin(t)
	dir := filepath.Join(docroot, "files")
	require.NoError(t, os.Mkdir(dir, 0o755))
	quota := filepath.Join(dir, quotaFile)
	require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))

	require.NoError(t, p.ftpUserUpdate(context.Background(), "ftp_user_update",
		engine.Data{Old: ftpUser(dir), New: ftpUser(dir)}))

	assert.FileExists(t, quota)
}

func TestDeleteRemovesQuotaOnly(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	dir := filepath.Join(docroot, "files")
	require.NoError(t, os.Mkdir(dir, 0o755))
	quota := filepath.Join(dir, quotaFile)
	require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))
	content := filepath.Join(dir, "index.html")
	require.NoError(t, os.WriteFile(content, []byte("<h1>"), 0o644))

	require.NoError(t, p.ftpUserDelete(context.Background(), "ftp_user_delete",
		engine.Data{Old: ftpUser(dir)}))

	assert.NoFileExists(t, quota)
	assert.FileExists(t, content, "site content is never touched")
	assert.DirExists(t, dir)
	assert.Empty(t, runner.all(), "no useradd/userdel: FTP accounts are virtual")
}

func TestDeleteMissingQuotaIsNotAnError(t *testing.T) {
	p, _, docroot := testPlugin(t)
	require.NoError(t, p.ftpUserDelete(context.Background(), "ftp_user_delete",
		engine.Data{Old: ftpUser(filepath.Join(docroot, "gone"))}))
}

func TestDeleteWithGoneParentKeepsQuota(t *testing.T) {
	p, _, docroot := testPlugin(t)
	p.LoadWeb = func(int64) (row, error) { return nil, nil }
	dir := filepath.Join(docroot, "files")
	require.NoError(t, os.Mkdir(dir, 0o755))
	quota := filepath.Join(dir, quotaFile)
	require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))

	require.NoError(t, p.ftpUserDelete(context.Background(), "ftp_user_delete",
		engine.Data{Old: ftpUser(dir)}))

	assert.FileExists(t, quota, "without a docroot to check against, nothing is unlinked")
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
		assert.Equal(t, tt.want, underDocroot(tt.dir, docroot), tt.dir)
	}
	assert.False(t, underDocroot("/var/www/web1", ""), "an empty docroot matches nothing")
}

func TestCheckPathRejectsSymlinkAndExoticChars(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	real := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(real, 0o755))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(real, link))

	assert.True(t, checkPath(real))
	assert.False(t, checkPath(link), "a symlinked component could redirect a root chattr")
	assert.False(t, checkPath(filepath.Join(link, "sub")), "symlink anywhere along the path")
	assert.False(t, checkPath("relative/path"))
	assert.False(t, checkPath("/var/www/web1; rm -rf /"))
}

func TestWebFolderProtectionRules(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	ctx := context.Background()
	off := &getconf.WebConfig{}

	require.NoError(t, p.webFolderProtection(ctx, off, docroot, false))
	assert.Equal(t, []string{"chattr -i " + docroot}, runner.all(),
		"unlocking is unconditional, so a server that just disabled the option still unlocks")

	require.NoError(t, p.webFolderProtection(ctx, off, docroot, true))
	assert.Len(t, runner.all(), 1, "no relock while web_folder_protection is off")

	require.NoError(t, p.webFolderProtection(ctx, off, "/", false))
	require.NoError(t, p.webFolderProtection(ctx, off, "/var", false))
	assert.Len(t, runner.all(), 1, "/ and short paths are never chattr'd")
}
