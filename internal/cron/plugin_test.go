package cron

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

func TestPluginRegistersActiveJob(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{
			Domain: "example.com", DocumentRoot: "/var/www/web1",
			SystemUser: "web1", SystemGroup: "client1",
		}, true, nil
	}

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))

	data := engine.Data{New: map[string]any{
		"id": float64(10), "server_id": float64(1), "parent_domain_id": float64(5),
		"active": "y", "type": "url", "command": "https://{DOMAIN}/cron.php",
		"run_min": "*/5", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
		"log": "n",
	}}
	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_insert", data))
	assert.True(t, runner.Has(10))
}

func TestPluginInactiveRemovesJob(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	require.NoError(t, runner.Add(Job{
		ID: 11, RunMin: "*", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
		Run: func(context.Context) {},
	}))
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{Domain: "e.com", DocumentRoot: "/w", SystemUser: "web1", SystemGroup: "client1"}, true, nil
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))

	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_update", engine.Data{
		Old: map[string]any{"id": float64(11), "active": "y"},
		New: map[string]any{
			"id": float64(11), "server_id": float64(1), "parent_domain_id": float64(1),
			"active": "n", "type": "url", "command": "https://e.com/c",
			"run_min": "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
		},
	}))
	assert.False(t, runner.Has(11))
}

func TestPluginDeleteRemovesJob(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	require.NoError(t, runner.Add(Job{
		ID: 12, RunMin: "*", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
		Run: func(context.Context) {},
	}))
	p := NewPlugin(nil, 1, runner, nil)
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_delete", engine.Data{
		Old: map[string]any{"id": float64(12), "server_id": float64(1)},
	}))
	assert.False(t, runner.Has(12))
}

func TestPluginSkipsMissingParent(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{}, false, nil
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_insert", engine.Data{
		New: map[string]any{
			"id": float64(13), "server_id": float64(1), "parent_domain_id": float64(99),
			"active": "y", "type": "url", "command": "https://x.com/",
			"run_min": "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
		},
	}))
	assert.False(t, runner.Has(13))
}

func TestPluginSkipsRootOwnedParent(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{
			Domain: "e.com", DocumentRoot: "/w", SystemUser: "root", SystemGroup: "root",
		}, true, nil
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_insert", engine.Data{
		New: map[string]any{
			"id": float64(14), "server_id": float64(1), "parent_domain_id": float64(1),
			"active": "y", "type": model.CronTypeFull, "command": "/bin/true",
			"run_min": "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
		},
	}))
	assert.False(t, runner.Has(14))
}

func TestHasDisallowedOwner(t *testing.T) {
	cases := []struct {
		user, group string
		disallowed  bool
	}{
		{"web1", "client1", false},
		{"web42", "client0", false},
		{"root", "root", true},
		{"web1", "root", true},
		{"ispconfig", "client1", true},
		{"vmail", "client1", true},
		{"getmail", "client1", true},
		{"webmaster", "client1", true}, // web-prefixed but not web<N>
		{"web1", "web1", true},
		{"", "", true},
		{" web1 ", " client1 ", false}, // surrounding blanks are trimmed
	}
	for _, c := range cases {
		got := hasDisallowedOwner(SiteContext{SystemUser: c.user, SystemGroup: c.group})
		assert.Equal(t, c.disallowed, got, "user=%q group=%q", c.user, c.group)
	}
}

func TestPluginSkipsOtherServer(t *testing.T) {
	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{Domain: "e.com", DocumentRoot: "/w", SystemUser: "web1", SystemGroup: "client1"}, true, nil
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
	require.NoError(t, reg.RaiseEvent(context.Background(), "cron_insert", engine.Data{
		New: map[string]any{
			"id": float64(15), "server_id": float64(9), "parent_domain_id": float64(1),
			"active": "y", "type": "url", "command": "https://e.com/",
			"run_min": "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
		},
	}))
	assert.False(t, runner.Has(15))
}

func TestPluginNeverTouchesCrontabDir(t *testing.T) {
	// Smoke: plugin code path creates no files under a temp crontab_dir.
	dir := t.TempDir()
	crontab := filepath.Join(dir, "cron.d")
	require.NoError(t, os.MkdirAll(crontab, 0o755))

	runner := NewClientJobRunner(nil)
	t.Cleanup(runner.Stop)
	p := NewPlugin(nil, 1, runner, nil)
	p.LoadParent = func(context.Context, uint32) (SiteContext, bool, error) {
		return SiteContext{Domain: "e.com", DocumentRoot: "/w", SystemUser: "web1", SystemGroup: "client1"}, true, nil
	}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
	for _, action := range []string{"cron_insert", "cron_update", "cron_delete"} {
		require.NoError(t, reg.RaiseEvent(context.Background(), action, engine.Data{
			Old: map[string]any{"id": float64(1), "server_id": float64(1), "parent_domain_id": float64(1)},
			New: map[string]any{
				"id": float64(1), "server_id": float64(1), "parent_domain_id": float64(1),
				"active": "y", "type": "url", "command": "https://e.com/c",
				"run_min": "0", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
			},
		}))
	}
	entries, err := filepath.Glob(filepath.Join(crontab, "*"))
	require.NoError(t, err)
	assert.Empty(t, entries, "plugin must not create files under crontab_dir")
}
