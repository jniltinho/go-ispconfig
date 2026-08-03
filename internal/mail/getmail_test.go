package mail

// Golden-file and handler tests for the getmail plugin (port of
// getmail_plugin.inc.php + run-getmail.sh). The golden file is the
// rendered rc of the fixture below; regenerate an INTENDED change with
//
//	go test ./internal/mail -run TestGetmailGolden -update-getmail

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

var updateGetmail = flag.Bool("update-getmail", false, "rewrite the getmail golden file")

// getmailRow is a full mail_get payload.
func getmailRow(server, username string) map[string]any {
	return map[string]any{
		"mailget_id": float64(1), "server_id": float64(1), "type": "pop3",
		"source_server": server, "source_username": username,
		"source_password": "s3cr3t", "source_delete": "y",
		"source_read_all": "n", "destination": "user1@example.com",
		"active": "y",
	}
}

// testGetmail builds a plugin whose config dir is a temp directory.
func testGetmail(t *testing.T) (*GetmailPlugin, *fakeRunner, string) {
	t.Helper()
	dir := t.TempDir()
	runner := &fakeRunner{}
	base := NewPlugin(nil, nil, runner, 1, nil)
	g := NewGetmailPlugin(base, "")
	cfg := getconf.DefaultGetmailConfig()
	cfg.ConfigDir = dir
	g.LoadConfig = func(context.Context) (getconf.GetmailConfig, error) { return cfg, nil }
	return g, runner, dir
}

func TestGetmailGolden(t *testing.T) {
	g, _, dir := testGetmail(t)
	require.NoError(t, g.update(context.Background(),
		engine.Data{New: getmailRow("mail.example.com", "john.doe@example.com")}))

	rc := filepath.Join(dir, "mail_example_com_john_doe_example_com.conf")
	got, err := os.ReadFile(rc)
	require.NoError(t, err)

	golden := "golden/getmail.conf"
	if *updateGetmail {
		require.NoError(t, os.WriteFile(golden, got, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))

	// The rc holds a cleartext third-party password (design D4/D7).
	fi, err := os.Stat(rc)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestGetmailChownsToGetmailUser(t *testing.T) {
	g, runner, dir := testGetmail(t)
	require.NoError(t, g.update(context.Background(),
		engine.Data{New: getmailRow("pop.example.com", "bob")}))
	assert.Contains(t, runner.all(),
		"chown getmail:getmail "+filepath.Join(dir, "pop_example_com_bob.conf"))
}

func TestGetmailRetrieverTypes(t *testing.T) {
	for typ, class := range retrievers {
		g, _, dir := testGetmail(t)
		r := getmailRow("srv", "u")
		r["type"] = typ
		require.NoError(t, g.update(context.Background(), engine.Data{New: r}))
		body, err := os.ReadFile(filepath.Join(dir, "srv_u.conf"))
		require.NoError(t, err)
		assert.Contains(t, string(body), "type = "+class)
	}
}

func TestGetmailUnknownTypeWritesNothing(t *testing.T) {
	g, _, dir := testGetmail(t)
	r := getmailRow("srv", "u")
	r["type"] = "exchange"
	require.NoError(t, g.update(context.Background(), engine.Data{New: r}))
	assert.NoFileExists(t, filepath.Join(dir, "srv_u.conf"))
}

func TestGetmailInactiveRemovesFile(t *testing.T) {
	g, _, dir := testGetmail(t)
	rc := filepath.Join(dir, "srv_u.conf")
	require.NoError(t, g.update(context.Background(), engine.Data{New: getmailRow("srv", "u")}))
	require.FileExists(t, rc)

	off := getmailRow("srv", "u")
	off["active"] = "n"
	require.NoError(t, g.update(context.Background(),
		engine.Data{Old: getmailRow("srv", "u"), New: off}))
	assert.NoFileExists(t, rc)
}

func TestGetmailRenameLeavesNoOrphan(t *testing.T) {
	g, _, dir := testGetmail(t)
	require.NoError(t, g.update(context.Background(),
		engine.Data{New: getmailRow("srv", "old@example.com")}))
	require.NoError(t, g.update(context.Background(), engine.Data{
		Old: getmailRow("srv", "old@example.com"),
		New: getmailRow("srv", "new@example.com"),
	}))
	assert.NoFileExists(t, filepath.Join(dir, "srv_old_example_com.conf"))
	assert.FileExists(t, filepath.Join(dir, "srv_new_example_com.conf"))
}

func TestGetmailDeleteIsIdempotent(t *testing.T) {
	g, _, _ := testGetmail(t)
	assert.NoError(t, g.delete(context.Background(),
		engine.Data{Old: getmailRow("srv", "gone")}))
}

func TestGetmailRefusesFakedPath(t *testing.T) {
	for _, bad := range []string{"../../etc/passwd", "a;b", "a|b", "a$b"} {
		// clean() neutralises the characters, so the guard is asserted on
		// the assembled path directly.
		_, err := rcPath("/etc/getmail", row{"source_server": "srv", "source_username": bad})
		assert.NoError(t, err, "clean() must neutralise %q", bad)
	}
	_, err := rcPath("/etc/getmail", row{"source_server": "..", "source_username": "x"})
	assert.NoError(t, err)
	// A dir that itself escapes is refused by the containment check.
	_, err = rcPath("/etc/getmail/..", row{"source_server": "a", "source_username": "b"})
	assert.Error(t, err)
}

func TestGetmailMissingConfigDirIsNoOp(t *testing.T) {
	g, runner, dir := testGetmail(t)
	cfg := getconf.DefaultGetmailConfig()
	cfg.ConfigDir = filepath.Join(dir, "absent")
	g.LoadConfig = func(context.Context) (getconf.GetmailConfig, error) { return cfg, nil }

	require.NoError(t, g.update(context.Background(), engine.Data{New: getmailRow("srv", "u")}))
	assert.NoDirExists(t, cfg.ConfigDir, "the daemon must never create the config dir")
	assert.Empty(t, runner.all())
}

func TestGetmailRelativeConfigDirIsRefused(t *testing.T) {
	g, _, _ := testGetmail(t)
	g.LoadConfig = func(context.Context) (getconf.GetmailConfig, error) {
		return getconf.GetmailConfig{ConfigDir: "relative", User: "getmail"}, nil
	}
	assert.NoError(t, g.update(context.Background(), engine.Data{New: getmailRow("srv", "u")}))
}

func TestGetmailFetchJobArgv(t *testing.T) {
	g, runner, dir := testGetmail(t)
	for _, name := range []string{"b.conf", "a.conf", "oldmail-b", "sub"} {
		if name == "sub" {
			require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
			continue
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}
	g.LookupUser = func(string) (string, string, error) { return "999", "999", nil }

	require.NoError(t, g.fetchJob(context.Background()))
	require.Len(t, runner.all(), 1)
	assert.Equal(t,
		"setpriv --reuid 999 --regid 999 --clear-groups /usr/bin/getmail -g "+dir+
			" -r "+filepath.Join(dir, "a.conf")+" -r "+filepath.Join(dir, "b.conf"),
		runner.all()[0])
}

func TestGetmailFetchJobSkipsEmptyDir(t *testing.T) {
	g, runner, _ := testGetmail(t)
	g.LookupUser = func(string) (string, string, error) { return "999", "999", nil }
	require.NoError(t, g.fetchJob(context.Background()))
	assert.Empty(t, runner.all())
}

func TestGetmailFetchJobUnknownUserFails(t *testing.T) {
	g, runner, dir := testGetmail(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.conf"), nil, 0o600))
	g.LookupUser = func(string) (string, string, error) { return "", "", os.ErrNotExist }
	assert.Error(t, g.fetchJob(context.Background()))
	assert.Empty(t, runner.all())
}

func TestGetmailFetchJobSingleFlight(t *testing.T) {
	g, runner, dir := testGetmail(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.conf"), nil, 0o600))
	g.LookupUser = func(string) (string, string, error) { return "999", "999", nil }

	g.fetching.Lock()
	require.NoError(t, g.fetchJob(context.Background()))
	assert.Empty(t, runner.all(), "an overlapping activation must start no process")
	g.fetching.Unlock()

	require.NoError(t, g.fetchJob(context.Background()))
	assert.Len(t, runner.all(), 1, "the guard is released again")
}
