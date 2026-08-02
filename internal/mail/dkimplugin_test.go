package mail

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// testDkim builds the plugin with temp dkim/rspamd dirs, real services
// registry and fake runner.
func testDkim(t *testing.T) (*DkimPlugin, *recordExec, getconf.MailConfig) {
	t.Helper()
	exec := &recordExec{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)
	base := NewPlugin(nil, services, &fakeRunner{}, 1, nil)
	cfg := getconf.DefaultMailConfig()
	cfg.DKIMPath = t.TempDir() + "/dkim"
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) { return cfg, nil }
	d := NewDkimPlugin(base)
	d.RspamdLocalDir = t.TempDir()
	d.UserExists = func(string) bool { return false } // root fallback
	return d, exec, cfg
}

// domainPayload builds a mail_domain row.
func domainPayload(domain, active, dkim, selector, priv string) map[string]any {
	return map[string]any{
		"domain_id": float64(1), "domain": domain, "active": active,
		"dkim": dkim, "dkim_selector": selector, "dkim_private": priv,
	}
}

func TestDkimInsertWritesKeysAndMaps(t *testing.T) {
	d, exec, cfg := testDkim(t)
	priv, pub, err := GenerateDKIMKey(1024)
	require.NoError(t, err)

	require.NoError(t, d.domainInsert(context.Background(), engine.Data{
		New: domainPayload("example.com", "y", "y", "default", priv),
	}))

	gotPriv, err := os.ReadFile(cfg.DKIMPath + "/example.com.private")
	require.NoError(t, err)
	assert.Equal(t, priv, string(gotPriv))
	gotPub, err := os.ReadFile(cfg.DKIMPath + "/example.com.public")
	require.NoError(t, err)
	assert.Equal(t, pub, string(gotPub), "public derived from the private key")
	fi, err := os.Stat(cfg.DKIMPath + "/example.com.private")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm())

	domains, err := os.ReadFile(d.RspamdLocalDir + "/dkim_domains.map")
	require.NoError(t, err)
	assert.Equal(t, "example.com "+cfg.DKIMPath+"/example.com.private\n", string(domains))
	selectors, err := os.ReadFile(d.RspamdLocalDir + "/dkim_selectors.map")
	require.NoError(t, err)
	assert.Equal(t, "example.com default\n", string(selectors))

	d.base.services.ProcessDelayedActions(context.Background())
	assert.Equal(t, []string{"rspamd/reload"}, exec.runs)
}

func TestDkimUpdateTransitions(t *testing.T) {
	d, _, cfg := testDkim(t)
	priv, _, err := GenerateDKIMKey(1024)
	require.NoError(t, err)
	ctx := context.Background()

	enabled := domainPayload("example.com", "y", "y", "default", priv)
	require.NoError(t, d.domainInsert(ctx, engine.Data{New: enabled}))

	t.Run("selector change rewrites the selector map", func(t *testing.T) {
		changed := domainPayload("example.com", "y", "y", "k2026", priv)
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: enabled, New: changed}))
		sel, err := os.ReadFile(d.RspamdLocalDir + "/dkim_selectors.map")
		require.NoError(t, err)
		assert.Equal(t, "example.com k2026\n", string(sel))
	})

	t.Run("rename removes old materials and writes new", func(t *testing.T) {
		renamed := domainPayload("new.com", "y", "y", "default", priv)
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: enabled, New: renamed}))
		assert.NoFileExists(t, cfg.DKIMPath+"/example.com.private")
		assert.FileExists(t, cfg.DKIMPath+"/new.com.private")
		domains, err := os.ReadFile(d.RspamdLocalDir + "/dkim_domains.map")
		require.NoError(t, err)
		assert.Equal(t, "new.com "+cfg.DKIMPath+"/new.com.private\n", string(domains))
	})

	t.Run("dkim disable removes keys and map lines", func(t *testing.T) {
		on := domainPayload("new.com", "y", "y", "default", priv)
		off := domainPayload("new.com", "y", "n", "default", priv)
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: on, New: off}))
		assert.NoFileExists(t, cfg.DKIMPath+"/new.com.private")
		domains, err := os.ReadFile(d.RspamdLocalDir + "/dkim_domains.map")
		require.NoError(t, err)
		assert.Empty(t, string(domains))
	})

	t.Run("domain deactivate removes materials", func(t *testing.T) {
		on := domainPayload("re.com", "y", "y", "default", priv)
		require.NoError(t, d.domainInsert(ctx, engine.Data{New: on}))
		offDomain := domainPayload("re.com", "n", "y", "default", priv)
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: on, New: offDomain}))
		assert.NoFileExists(t, cfg.DKIMPath+"/re.com.private")

		// Re-enable restores them (and resync keeps them).
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: offDomain, New: on}))
		assert.FileExists(t, cfg.DKIMPath+"/re.com.private")
		require.NoError(t, d.domainUpdate(ctx, engine.Data{Old: on, New: on}))
		assert.FileExists(t, cfg.DKIMPath+"/re.com.private")
	})
}

func TestDkimDeleteAndGuards(t *testing.T) {
	d, _, cfg := testDkim(t)
	priv, _, err := GenerateDKIMKey(1024)
	require.NoError(t, err)
	ctx := context.Background()

	on := domainPayload("gone.com", "y", "y", "default", priv)
	require.NoError(t, d.domainInsert(ctx, engine.Data{New: on}))
	require.NoError(t, d.domainDelete(ctx, engine.Data{Old: on}))
	assert.NoFileExists(t, cfg.DKIMPath+"/gone.com.private")
	assert.NoFileExists(t, cfg.DKIMPath+"/gone.com.public")

	// content_filter != rspamd → plugin skips entirely.
	cfgAmavis := cfg
	cfgAmavis.ContentFilter = "amavis"
	d.base.LoadConfig = func(context.Context) (getconf.MailConfig, error) { return cfgAmavis, nil }
	require.NoError(t, d.domainInsert(ctx, engine.Data{
		New: domainPayload("skipped.com", "y", "y", "default", priv),
	}))
	assert.NoFileExists(t, cfg.DKIMPath+"/skipped.com.private")
}
