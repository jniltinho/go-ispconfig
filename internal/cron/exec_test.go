package cron

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestExpandCommandFull(t *testing.T) {
	site := SiteContext{
		Domain:       "example.com",
		DocumentRoot: "/var/www/clients/client1/web1",
		PHPCLIBinary: "/usr/bin/php8.3",
	}
	got, err := ExpandCommand("{SITE_PHP} {DOCROOT_CLIENT}/cron.php {DOMAIN}", model.CronTypeFull, site)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/php8.3 /var/www/clients/client1/web1/web/cron.php example.com", got)

	got, err = ExpandCommand("[web_root]/job.php", model.CronTypeFull, site)
	require.NoError(t, err)
	assert.Equal(t, "/var/www/clients/client1/web1/web/job.php", got)
}

func TestExpandCommandChrootedStripsDocroot(t *testing.T) {
	site := SiteContext{
		Domain:       "example.com",
		DocumentRoot: "/var/www/clients/client1/web1",
	}
	cmd := "/var/www/clients/client1/web1/web/cron.php"
	got, err := ExpandCommand(cmd, model.CronTypeChrooted, site)
	require.NoError(t, err)
	assert.Equal(t, "/web/cron.php", got)

	// Placeholders resolve to in-jail /web path.
	got, err = ExpandCommand("{SITE_PHP} {DOCROOT_CLIENT}/x.php", model.CronTypeChrooted, site)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/php /web/x.php", got)
}

func TestExpandCommandRejectsInsecure(t *testing.T) {
	_, err := ExpandCommand("echo\nok", model.CronTypeFull, SiteContext{})
	require.Error(t, err)
}

func TestSplitArgv(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "simple", in: "/usr/bin/php script.php", want: []string{"/usr/bin/php", "script.php"}},
		{name: "double quotes", in: `/usr/bin/php "my script.php" arg`, want: []string{"/usr/bin/php", "my script.php", "arg"}},
		{name: "single quotes", in: `/bin/echo 'a b'`, want: []string{"/bin/echo", "a b"}},
		{name: "empty", in: "   ", wantErr: true},
		{name: "unclosed", in: `echo "foo`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitArgv(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildExecSpec(t *testing.T) {
	site := SiteContext{
		Domain:       "example.com",
		DocumentRoot: "/var/www/clients/client1/web1",
		PHPCLIBinary: "/usr/bin/php",
	}
	spec, err := BuildExecSpec("{SITE_PHP} cron.php", model.CronTypeFull, site)
	require.NoError(t, err)
	assert.Equal(t, []string{"/usr/bin/php", "cron.php"}, spec.Argv)
	assert.Equal(t, "/var/www/clients/client1/web1/web", spec.Cwd)
	assert.Equal(t, model.CronTypeFull, spec.Type)
}

func TestProcessRunnerRunsTrue(t *testing.T) {
	// Use cwd that exists; absolute /bin/true needs no PATH.
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	require.NoError(t, os.MkdirAll(web, 0o755))

	site := SiteContext{DocumentRoot: dir, Domain: "t.example"}
	spec, err := BuildExecSpec("/bin/true", model.CronTypeFull, site)
	require.NoError(t, err)

	p := &ProcessRunner{Timeout: 5 * time.Second}
	res := p.Run(context.Background(), spec, site)
	assert.Equal(t, StatusOK, res.Status, "err=%v out=%s", res.Err, res.Output)
	assert.Equal(t, 0, res.ExitCode)
}

func TestProcessRunnerExitCode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "web"), 0o755))
	site := SiteContext{DocumentRoot: dir}
	spec, err := BuildExecSpec("/bin/false", model.CronTypeFull, site)
	require.NoError(t, err)
	res := (&ProcessRunner{Timeout: 5 * time.Second}).Run(context.Background(), spec, site)
	assert.Equal(t, StatusExit, res.Status)
	assert.NotEqual(t, 0, res.ExitCode)
}
