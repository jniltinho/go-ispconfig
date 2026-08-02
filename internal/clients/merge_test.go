package clients

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// tpl builds a template with the tested fields; the zero value of every
// other column is fine for the pure merge.
func tpl(mut func(*model.ClientTemplate)) *model.ClientTemplate {
	t := &model.ClientTemplate{}
	if mut != nil {
		mut(t)
	}
	return t
}

func TestMergeTemplates(t *testing.T) {
	t.Run("master copies limits and additional adds numeric", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitWebDomain = 5; m.LimitDNSZone = 10 })
		add := tpl(func(m *model.ClientTemplate) { m.LimitWebDomain = 3 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, int32(8), limits["limit_web_domain"])
		require.Equal(t, int32(10), limits["limit_dns_zone"])
	})

	t.Run("additional -1 promotes to unlimited", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitMailbox = 10 })
		add := tpl(func(m *model.ClientTemplate) { m.LimitMailbox = -1 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, int32(-1), limits["limit_mailbox"])
	})

	t.Run("master -1 ignores additions", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitMailbox = -1 })
		add := tpl(func(m *model.ClientTemplate) { m.LimitMailbox = 5 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, int32(-1), limits["limit_mailbox"])
	})

	t.Run("cron frequency takes the minimum with floor 1", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitCronFrequency = 5 })
		addLow := tpl(func(m *model.ClientTemplate) { m.LimitCronFrequency = 0 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{addLow}, false)
		require.NoError(t, err)
		require.Equal(t, int32(1), limits["limit_cron_frequency"])
	})

	t.Run("y/n flags: y wins except force_suexec", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) {
			m.LimitSSL = "n"
			m.ForceSuexec = "y"
		})
		add := tpl(func(m *model.ClientTemplate) {
			m.LimitSSL = "y"
			m.ForceSuexec = "n"
		})
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, "y", limits["limit_ssl"])
		require.Equal(t, "n", limits["force_suexec"], "n is less restrictive for force_suexec")
	})

	t.Run("checkboxarray and server lists union", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) {
			m.WebPHPOptions = "php-fpm,mod"
			m.WebServers = "1"
		})
		add := tpl(func(m *model.ClientTemplate) {
			m.WebPHPOptions = "php-fpm,fast-cgi"
			m.WebServers = "2,1"
		})
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, "php-fpm,mod,fast-cgi", limits["web_php_options"])
		require.Equal(t, "1,2", limits["web_servers"])
	})

	t.Run("cron type picks the lower tform index", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitCronType = "url" })
		add := tpl(func(m *model.ClientTemplate) { m.LimitCronType = "chrooted" })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, "chrooted", limits["limit_cron_type"])
	})

	t.Run("additional default server does not override master", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.DefaultSlaveDNSServer = 3 })
		add := tpl(func(m *model.ClientTemplate) { m.DefaultSlaveDNSServer = 9 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, int32(3), limits["default_slave_dnsserver"])
	})

	t.Run("zero default servers are omitted from the write set", func(t *testing.T) {
		limits, err := MergeTemplates(tpl(nil), nil, false)
		require.NoError(t, err)
		_, hasSlave := limits["default_slave_dnsserver"]
		require.False(t, hasSlave)
	})

	t.Run("non-reseller never receives limit_client", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitClient = 10 })
		limits, err := MergeTemplates(master, nil, false)
		require.NoError(t, err)
		_, has := limits["limit_client"]
		require.False(t, has)
	})

	t.Run("reseller master with zero limit_client promotes to -1", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitClient = 0 })
		limits, err := MergeTemplates(master, nil, true)
		require.NoError(t, err)
		require.Equal(t, int32(-1), limits["limit_client"])
	})

	t.Run("reseller additional limit_client adds", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitClient = 5 })
		add := tpl(func(m *model.ClientTemplate) { m.LimitClient = 3 })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, true)
		require.NoError(t, err)
		require.Equal(t, int32(8), limits["limit_client"])
	})

	t.Run("limit_web_ip is master-only", func(t *testing.T) {
		master := tpl(func(m *model.ClientTemplate) { m.LimitWebIP = "10.0.0.1" })
		add := tpl(func(m *model.ClientTemplate) { m.LimitWebIP = "10.0.0.9" })
		limits, err := MergeTemplates(master, []*model.ClientTemplate{add}, false)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", limits["limit_web_ip"])
	})
}
