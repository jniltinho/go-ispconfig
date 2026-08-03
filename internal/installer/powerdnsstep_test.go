package installer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnswersDNSBackend(t *testing.T) {
	a, err := ResolveAnswers(ResolveOptions{Flags: map[string]string{"hostname": "srv1.example.com"}})
	require.NoError(t, err)
	assert.Equal(t, "bind", a.DNSBackend, "default backend")

	a, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{
		"hostname": "srv1.example.com", "dns-backend": " PowerDNS ",
	}})
	require.NoError(t, err)
	assert.Equal(t, "powerdns", a.DNSBackend)

	_, err = ResolveAnswers(ResolveOptions{Flags: map[string]string{
		"hostname": "srv1.example.com", "dns-backend": "nsd",
	}})
	require.ErrorContains(t, err, "invalid --dns-backend")
}

func TestPackageSetSwapsBindForPowerDNS(t *testing.T) {
	st, _, _ := testState(t)
	assert.Contains(t, st.packageSet(), "bind9")
	assert.Equal(t, "named", st.DNSService())

	st.Answers.DNSBackend = "powerdns"
	pkgs := st.packageSet()
	assert.NotContains(t, pkgs, "bind9")
	assert.Contains(t, pkgs, "pdns-server")
	assert.Contains(t, pkgs, "pdns-backend-mysql")
	assert.Contains(t, pkgs, "mariadb-server", "non-DNS packages unchanged")
	assert.Equal(t, "pdns", st.DNSService())

	// dns disabled keeps the bind package set (backend is irrelevant).
	st.Answers.EnableDNS = false
	assert.Contains(t, st.packageSet(), "bind9")
}

func TestPackagesStepInstallsPowerDNS(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.DNSBackend = "powerdns"
	require.NoError(t, packagesStep{}.Run(context.Background(), st))
	calls := strings.Join(mock.calls, "|")
	assert.Contains(t, calls, "pdns-server")
	assert.NotContains(t, calls, "bind9")
	assert.True(t, mock.called("systemctl enable --now mariadb redis-server nginx pdns php8.2-fpm"))
}

func TestBindBaseSkippedOnPowerDNS(t *testing.T) {
	st, _, _ := testState(t)
	st.Answers.DNSBackend = "powerdns"
	require.ErrorContains(t, bindBaseStep{}.Run(context.Background(), st), "powerdns")
}

func TestPowerDNSStepSkippedOnBind(t *testing.T) {
	st, mock, _ := testState(t)
	require.ErrorContains(t, powerDNSStep{}.Run(context.Background(), st), "bind")
	assert.Empty(t, mock.calls)
}

func TestRenderPdnsLocal(t *testing.T) {
	st, _, _ := testState(t)
	st.DBAddr = "127.0.0.1:3307"
	st.DBPassword = "s3cret"
	got := st.renderPdnsLocal()
	assert.Contains(t, got, "gmysql-host=127.0.0.1")
	assert.Contains(t, got, "gmysql-port=3307")
	assert.Contains(t, got, "gmysql-user=ispconfig")
	assert.Contains(t, got, "gmysql-password=s3cret")
	assert.Contains(t, got, "gmysql-dbname=powerdns")
	assert.NotContains(t, got, "{mysql_server", "every placeholder replaced")
}

func TestSetDNSBackendKey(t *testing.T) {
	assert.Equal(t, "[dns]\ndns_backend=powerdns\nbind_user=root\n",
		setDNSBackendKey("[dns]\ndns_backend=bind\nbind_user=root\n", "powerdns"))
	// Missing key: inserted into the existing [dns] section.
	assert.Equal(t, "[server]\nx=1\n\n[dns]\ndns_backend=powerdns\nbind_user=root\n",
		setDNSBackendKey("[server]\nx=1\n\n[dns]\nbind_user=root\n", "powerdns"))
	// Missing section: appended.
	assert.Equal(t, "[server]\nx=1\n\n[dns]\ndns_backend=powerdns\n",
		setDNSBackendKey("[server]\nx=1\n", "powerdns"))
}
