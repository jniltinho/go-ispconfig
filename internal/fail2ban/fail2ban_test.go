package fail2ban

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJailList(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []string
	}{
		{
			name: "two jails",
			out:  "Status\n|- Number of jail:\t2\n`- Jail list:\tdovecot, sshd\n",
			want: []string{"dovecot", "sshd"},
		},
		{
			name: "single jail",
			out:  "Status\n|- Number of jail:\t1\n`- Jail list:\tsshd\n",
			want: []string{"sshd"},
		},
		{
			name: "no jails",
			out:  "Status\n|- Number of jail:\t0\n`- Jail list:\n",
			want: nil,
		},
		{
			name: "unrecognised output degrades to empty",
			out:  "some unexpected banner\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseJailList(tt.out))
		})
	}
}

// sshdStatus is a captured `fail2ban-client status sshd` output.
const sshdStatus = "Status for the jail: sshd\n" +
	"|- Filter\n" +
	"|  |- Currently failed:\t1\n" +
	"|  |- Total failed:\t12\n" +
	"|  `- File list:\t/var/log/auth.log\n" +
	"`- Actions\n" +
	"   |- Currently banned:\t2\n" +
	"   |- Total banned:\t5\n" +
	"   `- Banned IP list:\t203.0.113.7 198.51.100.4\n"

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want Status
	}{
		{
			name: "full status",
			out:  sshdStatus,
			want: Status{CurrentlyFailed: 1, TotalFailed: 12, CurrentlyBanned: 2, TotalBanned: 5,
				BannedIPs: []string{"203.0.113.7", "198.51.100.4"}},
		},
		{
			name: "no bans",
			out: "Status for the jail: dovecot\n|- Filter\n|  |- Currently failed:\t0\n" +
				"|  `- Total failed:\t0\n`- Actions\n   |- Currently banned:\t0\n" +
				"   |- Total banned:\t0\n   `- Banned IP list:\n",
			want: Status{BannedIPs: []string{}},
		},
		{
			name: "unparsable output yields zero counters, not an error",
			out:  "ERROR   NOK: ('sshd',)\n",
			want: Status{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseStatus(tt.out))
		})
	}
}

func TestValidJailName(t *testing.T) {
	for _, name := range []string{"sshd", "pure-ftpd", "nginx-http-auth", "a.b_c"} {
		assert.True(t, ValidJailName(name), name)
	}
	for _, name := range []string{"", "../../etc", "sshd; rm -rf /", "ssh d", "a/b"} {
		assert.False(t, ValidJailName(name), name)
	}
}

func TestJails(t *testing.T) {
	names := func(jails []Jail) []string {
		out := make([]string, len(jails))
		for i, j := range jails {
			out[i] = j.Name
		}
		return out
	}
	base := []string{"sshd", "pure-ftpd", "dovecot", "postfix"}
	tests := []struct {
		webServer string
		want      []string
	}{
		{"nginx", append(append([]string{}, base...), "nginx-http-auth")},
		{"apache", append(append([]string{}, base...), "apache-auth")},
		{"apache2", append(append([]string{}, base...), "apache-auth")},
		{"", base},
	}
	for _, tt := range tests {
		t.Run("web="+tt.webServer, func(t *testing.T) {
			assert.Equal(t, tt.want, names(Jails(tt.webServer)))
		})
	}
}

func TestJailRender(t *testing.T) {
	tests := []struct {
		name     string
		jail     Jail
		contains []string
	}{
		{
			name: "sshd",
			jail: Jails("nginx")[0],
			contains: []string{
				"# Managed by go-ispconfig",
				"[sshd]", "enabled  = true", "filter   = sshd",
				"logpath  = /var/log/auth.log",
				`action   = iptables-multiport[name=sshd, port="ssh", protocol=tcp]`,
				"ignoreip = 127.0.0.1/8 ::1",
				"maxretry = 3", "findtime = 1200", "bantime  = 1200",
			},
		},
		{
			name: "dovecot keeps the ISPConfig port set",
			jail: Jails("nginx")[2],
			contains: []string{"[dovecot]",
				`port="pop3,pop3s,imap,imaps"`, "logpath  = /var/log/mail.log", "maxretry = 20"},
		},
		{
			name:     "nginx web jail",
			jail:     Jails("nginx")[4],
			contains: []string{"[nginx-http-auth]", "logpath  = /var/log/nginx/error.log", `port="http,https"`},
		},
		{
			name:     "apache web jail",
			jail:     Jails("apache")[4],
			contains: []string{"[apache-auth]", "logpath  = /var/log/apache2/error.log"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.jail.Render()
			for _, want := range tt.contains {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jail.d")

	changed, err := Write(dir, Jails("nginx"))
	require.NoError(t, err)
	assert.True(t, changed)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 5)
	assert.FileExists(t, filepath.Join(dir, "ispconfig-nginx-http-auth.local"))

	changed, err = Write(dir, Jails("nginx"))
	require.NoError(t, err)
	assert.False(t, changed, "second write must not change anything")

	// An operator drop-in with a foreign name is left alone.
	other := filepath.Join(dir, "local-extra.conf")
	require.NoError(t, os.WriteFile(other, []byte("[custom]\n"), 0o644))
	_, err = Write(dir, Jails("nginx"))
	require.NoError(t, err)
	content, err := os.ReadFile(other)
	require.NoError(t, err)
	assert.Equal(t, "[custom]\n", string(content))
}
