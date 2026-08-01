package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

func folderWebsite(base string) row {
	return row{
		"domain_id": float64(1), "domain": "example.com", "type": "vhost",
		"document_root": filepath.Join(base, "clients/client1/web1"),
		"system_user":   "web1", "system_group": "client1", "web_folder": "",
	}
}

// TestMaintainFolderAuthInsert covers "Folder user creates auth entry": the
// auth file holds the crypted password and the folder exists.
func TestMaintainFolderAuthInsert(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	website := folderWebsite(base)
	folder := row{"path": "/admin", "parent_domain_id": float64(1)}
	data := engine.Data{New: map[string]any{
		"username": "alice", "password": "$1$abc$crypted", "active": "y",
	}}

	require.NoError(t, p.maintainFolderAuth(context.Background(), "web_folder_user_insert", data, folder, website))

	htpasswd := filepath.Join(website.str("document_root"), "web/admin/.htpasswd")
	content, err := os.ReadFile(htpasswd)
	require.NoError(t, err)
	assert.Equal(t, "alice:$1$abc$crypted\n", string(content))
	assert.Contains(t, r.commands(), "chown web1:client1 "+htpasswd)
}

// TestMaintainFolderAuthUpdateRenameAndDeactivate: a renamed user loses the
// old line; a deactivated user is removed entirely.
func TestMaintainFolderAuthUpdateRenameAndDeactivate(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	website := folderWebsite(base)
	folder := row{"path": "admin"}
	htdir := filepath.Join(website.str("document_root"), "web/admin")
	require.NoError(t, os.MkdirAll(htdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(htdir, ".htpasswd"),
		[]byte("alice:oldhash\nbob:bobhash\n"), 0o640))

	// Rename alice -> alicia.
	data := engine.Data{
		Old: map[string]any{"username": "alice", "active": "y"},
		New: map[string]any{"username": "alicia", "password": "newhash", "active": "y"},
	}
	require.NoError(t, p.maintainFolderAuth(context.Background(), "web_folder_user_update", data, folder, website))
	content, err := os.ReadFile(filepath.Join(htdir, ".htpasswd"))
	require.NoError(t, err)
	assert.Equal(t, "bob:bobhash\nalicia:newhash\n", string(content))

	// Deactivate bob.
	data = engine.Data{
		Old: map[string]any{"username": "bob", "active": "y"},
		New: map[string]any{"username": "bob", "password": "bobhash", "active": "n"},
	}
	require.NoError(t, p.maintainFolderAuth(context.Background(), "web_folder_user_update", data, folder, website))
	content, err = os.ReadFile(filepath.Join(htdir, ".htpasswd"))
	require.NoError(t, err)
	assert.Equal(t, "alicia:newhash\n", string(content))
}

// TestMaintainFolderAuthDelete removes the user's line.
func TestMaintainFolderAuthDelete(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	website := folderWebsite(base)
	folder := row{"path": "admin"}
	htdir := filepath.Join(website.str("document_root"), "web/admin")
	require.NoError(t, os.MkdirAll(htdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(htdir, ".htpasswd"), []byte("alice:h\n"), 0o640))

	data := engine.Data{Old: map[string]any{"username": "alice", "active": "y"}}
	require.NoError(t, p.maintainFolderAuth(context.Background(), "web_folder_user_delete", data, folder, website))
	content, err := os.ReadFile(filepath.Join(htdir, ".htpasswd"))
	require.NoError(t, err)
	assert.Empty(t, string(content))
}

// TestFolderAuthPathSafety: traversal and out-of-docroot paths are refused.
func TestFolderAuthPathSafety(t *testing.T) {
	website := folderWebsite("/var/www")
	for _, bad := range []string{"../secret", "a/../../b", `a\b`, "./x"} {
		_, err := folderAuthPath(website, bad)
		assert.Errorf(t, err, "path %q must be refused", bad)
	}
	got, err := folderAuthPath(website, "/admin/")
	require.NoError(t, err)
	assert.Equal(t, website.str("document_root")+"/web/admin/", got)

	// Sub/alias sites protect folders below their own web_folder.
	sub := folderWebsite("/var/www")
	sub["type"] = "vhostsubdomain"
	sub["web_folder"] = "blog"
	got, err = folderAuthPath(sub, "wp-admin")
	require.NoError(t, err)
	assert.Equal(t, sub.str("document_root")+"/blog/wp-admin/", got)
}

// TestVhostRendersAuthLocation ties the folder to the vhost render: the
// protected location with auth_basic survives the merge pass.
func TestVhostRendersAuthLocation(t *testing.T) {
	d := goldenDomain()
	content, warnings, err := renderVhost(vhostInput{
		cfg: goldenCfg(), d: d, nginxVersion: "1.26.1", dummyFile: "/d.htm",
		folders: []row{{"path": "/admin", "active": "y"}},
	}, "")
	require.NoError(t, err)
	assert.Empty(t, warnings)
	merged, _ := mergeLocations(content)
	assert.Contains(t, merged, "location /admin/ {")
	assert.Contains(t, merged, `auth_basic "Members Only";`)
	assert.Contains(t, merged, "auth_basic_user_file /var/www/clients/client1/web1/web/admin/.htpasswd;")
}
