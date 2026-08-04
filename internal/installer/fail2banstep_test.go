package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The HTTP jail must follow Answers.WebServer, not a hardcoded nginx: an
// apache2 node given the nginx jail watches a logpath that never exists.
func TestFail2banStepHTTPJailFollowsWebServer(t *testing.T) {
	for _, tc := range []struct {
		webServer string
		enableWeb bool
		want      string
		absent    string
	}{
		{webServer: WebServerApache, enableWeb: true, want: "ispconfig-apache-auth.local", absent: "ispconfig-nginx-http-auth.local"},
		{webServer: WebServerNginx, enableWeb: true, want: "ispconfig-nginx-http-auth.local", absent: "ispconfig-apache-auth.local"},
		{webServer: WebServerApache, enableWeb: false, absent: "ispconfig-apache-auth.local"},
	} {
		t.Run(tc.webServer+"/web="+map[bool]string{true: "y", false: "n"}[tc.enableWeb], func(t *testing.T) {
			st, _, _ := testState(t)
			st.Fail2banJailDir = t.TempDir()
			st.Answers.WebServer = tc.webServer
			st.Answers.EnableWeb = tc.enableWeb

			require.NoError(t, fail2banStep{}.Run(context.Background(), st))

			// The mail/ftp/ssh jails land regardless of the web module.
			assert.FileExists(t, filepath.Join(st.Fail2banJailDir, "ispconfig-dovecot.local"))
			if tc.want != "" {
				assert.FileExists(t, filepath.Join(st.Fail2banJailDir, tc.want))
			}
			assert.NoFileExists(t, filepath.Join(st.Fail2banJailDir, tc.absent))
		})
	}
}

// The sudo grant must be visudo-validated before it goes live: a malformed
// file in /etc/sudoers.d breaks sudo host-wide.
func TestFail2banStepWritesValidatedSudoers(t *testing.T) {
	st, mock, _ := testState(t)
	st.Fail2banJailDir = t.TempDir()

	require.NoError(t, fail2banStep{}.Run(context.Background(), st))

	path := filepath.Join(st.SudoersDir, Fail2banSudoersFile)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "NOPASSWD: /usr/bin/fail2ban-client status")
	assert.True(t, mock.called("visudo -c -f "+path+".new"), "visudo must run on the staging file")
	assert.NoFileExists(t, path+".new")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o440), info.Mode().Perm())
}

// A rejected sudoers file must never reach /etc/sudoers.d.
func TestFail2banStepSudoersVisudoFailure(t *testing.T) {
	st, mock, _ := testState(t)
	st.Fail2banJailDir = t.TempDir()
	mock.fail["visudo"] = "syntax error"

	require.Error(t, fail2banStep{}.Run(context.Background(), st))
	assert.NoFileExists(t, filepath.Join(st.SudoersDir, Fail2banSudoersFile))
}
