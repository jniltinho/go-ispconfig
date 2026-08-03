package installer

import (
	"context"
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
