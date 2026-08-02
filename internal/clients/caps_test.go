package clients

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestCapToParent(t *testing.T) {
	t.Run("nil parent caps nothing", func(t *testing.T) {
		child := &model.Client{LimitWebDomain: 100}
		clamped, err := CapToParent(child, nil)
		require.NoError(t, err)
		require.Empty(t, clamped)
		require.Equal(t, int32(100), child.LimitWebDomain)
	})

	t.Run("numeric limits clamp to the parent", func(t *testing.T) {
		parent := &model.Client{LimitWebDomain: 5, LimitDNSZone: -1, LimitMailbox: 0}
		child := &model.Client{LimitWebDomain: 20, LimitDNSZone: 50, LimitMailbox: 3}
		clamped, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, int32(5), child.LimitWebDomain, "child cannot exceed parent")
		require.Equal(t, int32(50), child.LimitDNSZone, "unlimited parent does not cap")
		require.Equal(t, int32(0), child.LimitMailbox, "zero parent blocks entirely")
		require.Contains(t, clamped, "limit_web_domain")
		require.Contains(t, clamped, "limit_mailbox")
		require.NotContains(t, clamped, "limit_dns_zone")
	})

	t.Run("child unlimited clamps to a finite parent", func(t *testing.T) {
		parent := &model.Client{LimitWebDomain: 5}
		child := &model.Client{LimitWebDomain: -1}
		_, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, int32(5), child.LimitWebDomain)
	})

	t.Run("flags cannot exceed the parent", func(t *testing.T) {
		parent := &model.Client{LimitSSL: "n", LimitCGI: "y", ForceSuexec: "y"}
		child := &model.Client{LimitSSL: "y", LimitCGI: "y", ForceSuexec: "n"}
		clamped, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, "n", child.LimitSSL)
		require.Equal(t, "y", child.LimitCGI)
		require.Equal(t, "y", child.ForceSuexec, "parent-enforced suexec wins")
		require.Contains(t, clamped, "limit_ssl")
		require.Contains(t, clamped, "force_suexec")
	})

	t.Run("cron frequency cannot be more frequent than the parent", func(t *testing.T) {
		parent := &model.Client{LimitCronFrequency: 15}
		child := &model.Client{LimitCronFrequency: 1}
		_, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, int32(15), child.LimitCronFrequency)
	})

	t.Run("cron type cannot be less restrictive", func(t *testing.T) {
		parent := &model.Client{LimitCronType: "url"}
		child := &model.Client{LimitCronType: "full"}
		_, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, "url", child.LimitCronType)
	})

	t.Run("option and server lists intersect with the parent", func(t *testing.T) {
		parent := &model.Client{WebPHPOptions: "php-fpm,fast-cgi", WebServers: "1"}
		child := &model.Client{WebPHPOptions: "php-fpm,mod", WebServers: "1,2"}
		clamped, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Equal(t, "php-fpm", child.WebPHPOptions)
		require.Equal(t, "1", child.WebServers)
		require.Contains(t, clamped, "web_php_options")
		require.Contains(t, clamped, "web_servers")
	})

	t.Run("within-parent limits stay untouched", func(t *testing.T) {
		parent := &model.Client{LimitWebDomain: 10, LimitSSL: "y", WebServers: "1,2"}
		child := &model.Client{LimitWebDomain: 3, LimitSSL: "y", WebServers: "2"}
		clamped, err := CapToParent(child, parent)
		require.NoError(t, err)
		require.Empty(t, clamped)
		require.Equal(t, int32(3), child.LimitWebDomain)
	})
}
