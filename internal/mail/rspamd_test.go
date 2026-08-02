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

func TestClassifyIdentity(t *testing.T) {
	id, ok := classifyIdentity("spamfilter_users_insert", row{"email": "u@example.com", "id": float64(3)})
	require.True(t, ok)
	assert.Equal(t, "spamfilter_user", id.typ)
	assert.False(t, id.isDomain)
	assert.Equal(t, "u@example.com", id.name)

	id, ok = classifyIdentity("spamfilter_users_update", row{"email": "@example.com", "id": float64(4)})
	require.True(t, ok)
	assert.True(t, id.isDomain)
	assert.Equal(t, "example.com", id.name)

	id, ok = classifyIdentity("mail_forwarding_delete", row{"source": "alias@example.com", "forwarding_id": float64(9)})
	require.True(t, ok)
	assert.Equal(t, "mail_forwarding", id.typ)

	for _, email := range []string{"@", "*@", ""} {
		_, ok = classifyIdentity("spamfilter_users_insert", row{"email": email})
		assert.False(t, ok, "global identity %q ignored", email)
	}
	_, ok = classifyIdentity("mail_get_insert", row{"email": "x@y.z"})
	assert.False(t, ok, "unknown event skipped")
}

func TestIdnEncodeAndValidEmail(t *testing.T) {
	assert.Equal(t, "u@xn--exmple-cua.com", idnEncode("u@exämple.com"))
	assert.Equal(t, "xn--exmple-cua.com", idnEncode("exämple.com"))
	assert.True(t, validEmail("user@example.com"))
	assert.True(t, validEmail("@example.com"), "domain filter form")
	assert.False(t, validEmail("no-at-sign"))
	assert.False(t, validEmail("a..b@example.com"))
	assert.False(t, validEmail("u@nodots"))
}

func TestFormatFloatLikePHP(t *testing.T) {
	assert.Equal(t, "6", formatFloat(6.0))
	assert.Equal(t, "6.5", formatFloat(6.5))
	assert.Equal(t, "1015", formatFloat(1015.0))
	assert.Equal(t, "0.1", formatFloat(0.1))
}

func TestUserSettingsNoopWithoutRspamdDir(t *testing.T) {
	base := NewPlugin(nil, nil, &fakeRunner{}, 1, nil)
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) {
		return getconf.DefaultMailConfig(), nil
	}
	r := NewRspamdPlugin(base, "")
	r.RspamdDir = t.TempDir() + "/does-not-exist"
	require.NoError(t, r.userSettingsUpdate(context.Background(), "spamfilter_users_insert",
		engine.Data{New: map[string]any{"email": "u@example.com", "id": float64(1)}}))
}

func TestUserSettingsDeleteRemovesFile(t *testing.T) {
	exec := &recordExec{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)
	base := NewPlugin(nil, services, &fakeRunner{}, 1, nil)
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) {
		return getconf.DefaultMailConfig(), nil
	}
	r := NewRspamdPlugin(base, "")
	r.RspamdDir = t.TempDir()
	require.NoError(t, os.MkdirAll(r.usersDir(), 0o755))
	file := r.settingsFile("u@example.com")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	require.NoError(t, r.userSettingsUpdate(context.Background(), "spamfilter_users_delete",
		engine.Data{Old: map[string]any{"email": "u@example.com", "id": float64(1)}}))
	assert.NoFileExists(t, file)
	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, []string{"rspamd/reload"}, exec.runs)
}

func TestServerUpdateWritesLocalDSnippets(t *testing.T) {
	exec := &recordExec{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)
	base := NewPlugin(nil, services, &fakeRunner{}, 1, nil)
	cfg := getconf.DefaultMailConfig()
	cfg.RspamdRedisPasswd = "sekret"
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) { return cfg, nil }
	r := NewRspamdPlugin(base, "")
	r.RspamdDir = t.TempDir()
	require.NoError(t, os.MkdirAll(r.localD(), 0o755))

	require.NoError(t, r.serverUpdate(context.Background(), "server_ip_update", engine.Data{}))

	for _, f := range serverLocalD {
		assert.FileExists(t, r.localD()+"/"+f)
	}
	redis, err := os.ReadFile(r.localD() + "/redis.conf")
	require.NoError(t, err)
	assert.Contains(t, string(redis), `servers = "127.0.0.1";`)
	assert.Contains(t, string(redis), `password = "sekret";`, "real ini key rendered (PHP read the wrong one)")
	fi, err := os.Stat(r.localD() + "/redis.conf")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm(), "password-bearing file protected")

	dkim, err := os.ReadFile(r.localD() + "/dkim_signing.conf")
	require.NoError(t, err)
	assert.Contains(t, string(dkim), "path_map")

	opts, err := os.ReadFile(r.localD() + "/options.inc")
	require.NoError(t, err)
	assert.Contains(t, string(opts), "local_addrs")

	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, []string{"rspamd/reload"}, exec.runs)
}

func TestServerEventsAnnouncedByModule(t *testing.T) {
	// The rspamd plugin's server subscriptions must load against the
	// mail module's announcements.
	reg := engine.NewRegistry(nil)
	base := NewPlugin(nil, engine.NewServices(&recordExec{}, nil), &fakeRunner{}, 1, nil)
	base.LoadConfig = func(context.Context) (getconf.MailConfig, error) {
		return getconf.DefaultMailConfig(), nil
	}
	require.NoError(t, reg.Load(
		[]engine.Module{NewModule()},
		[]engine.Plugin{NewRspamdPlugin(base, "")},
	))
}
