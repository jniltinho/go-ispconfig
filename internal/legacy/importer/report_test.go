package importer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyHost(t *testing.T) {
	require.Equal(t, "legacy.example.com", LegacyHost("https://legacy.example.com:8080"))
	require.Equal(t, "legacy.example.com", LegacyHost("http://legacy.example.com/panel"))
	require.Equal(t, "10.0.0.5", LegacyHost("https://10.0.0.5"))
	require.Equal(t, "::1", LegacyHost("https://[::1]:8080"), "IPv6 hosts must not break")
}

func TestBuildReport(t *testing.T) {
	snap := testSnapshot()
	snap.Domains[0]["ssl"] = "y"
	snap.Domains[0]["ssl_letsencrypt"] = "y"
	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{Clients: true, Sites: true, DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)
	counts := plan.Counts()

	r := BuildReport(plan, counts, ReportInput{
		LegacyHost: "legacy.example.com",
		Insecure:   true,
		PlainHTTP:  true,
	})

	t.Run("counts and reset list", func(t *testing.T) {
		require.Equal(t, counts, r.Counts)
		require.Equal(t, []string{"reseller1", "client2"}, r.ResetRequired)
		require.Empty(t, r.Conflicts)
	})

	t.Run("warnings", func(t *testing.T) {
		joined := strings.Join(r.Warnings, "\n")
		require.Contains(t, joined, "TLS certificate verification was DISABLED")
		require.Contains(t, joined, "plain http://")
		require.Contains(t, joined, "certificates must be re-issued")
		require.NotContains(t, joined, "multiple servers")
	})

	t.Run("rsync suggestions with uid/gid remap, vhosts only", func(t *testing.T) {
		require.Len(t, r.RsyncSuggestions, 1, "one suggestion per vhost; subdomains share the docroot")
		s := r.RsyncSuggestions[0]
		require.Contains(t, s, "rsync -a")
		require.Contains(t, s, "--usermap=*:web10")
		require.Contains(t, s, "--groupmap=*:client2")
		require.Contains(t, s, "legacy.example.com:/var/www/clients/client2/web10/")
		require.Contains(t, s, "chown -R web10:client2")
	})

	t.Run("operational order: files, then SSL, then DNS cutover", func(t *testing.T) {
		order := strings.Join(r.OperationalOrder, "\n")
		files := strings.Index(order, "rsync")
		ssl := strings.Index(order, "Let's Encrypt")
		dns := strings.Index(order, "DNS TTLs")
		require.True(t, files >= 0 && ssl > files && dns > ssl,
			"order must be files -> SSL -> DNS cutover: %s", order)
	})

	t.Run("multi-server warning", func(t *testing.T) {
		r := BuildReport(plan, counts, ReportInput{MultiServer: true})
		require.Contains(t, strings.Join(r.Warnings, "\n"), "multiple servers")
	})

	t.Run("conflicts listed with reasons", func(t *testing.T) {
		sub := testSnapshot()
		sub.Domains = sub.Domains[1:] // orphan subdomain
		p2, err := buildPlan(sub, newLocalState(), Options{
			Selection:      Selection{Clients: true, Sites: true},
			TargetServerID: 1,
		})
		require.NoError(t, err)
		r2 := BuildReport(p2, p2.Counts(), ReportInput{})
		require.Len(t, r2.Conflicts, 3, "missing parent cascades to folder and folder user")
		require.Contains(t, r2.Conflicts[0].Reason, "parent domain")
		require.Contains(t, r2.Conflicts[1].Reason, "parent web_domain")
		require.Contains(t, r2.Conflicts[2].Reason, "parent web_folder")
	})
}
