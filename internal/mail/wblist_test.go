package mail

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// TestWblistGoldenMatchesPHP renders the committed fixtures through the
// Go template path and compares byte-identically to the PHP outputs.
func TestWblistGoldenMatchesPHP(t *testing.T) {
	raw, err := os.ReadFile("golden/wblist_fixtures.json")
	require.NoError(t, err)
	var fixtures map[string]map[string]string
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	require.NotEmpty(t, fixtures)

	src, _, err := mastertpl.Load("rspamd_wblist.inc.conf.master", "")
	require.NoError(t, err)
	for name, f := range fixtures {
		t.Run(name, func(t *testing.T) {
			tpl := mastertpl.New(src)
			for k, v := range f {
				tpl.SetVar(k, v)
			}
			got, err := tpl.Render()
			require.NoError(t, err)
			want, err := os.ReadFile("golden/wblist-" + name + ".conf")
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

func TestNormalizeWblistAddr(t *testing.T) {
	assert.Equal(t, "@example.com", normalizeWblistAddr("example.com"), "bare domain gains @")
	assert.Equal(t, "@example.com", normalizeWblistAddr("*@example.com"), "*@ collapses")
	assert.Equal(t, "user@example.com", normalizeWblistAddr("user@example.com"))
	assert.Equal(t, "", normalizeWblistAddr("not valid.."))
	assert.Equal(t, "", normalizeWblistAddr(""))
}

func testWblistPlugin(t *testing.T) (*RspamdPlugin, *engine.Services, *recordExec) {
	t.Helper()
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
	return r, services, exec
}

func TestMailAccessGlobalWblist(t *testing.T) {
	r, services, exec := testWblistPlugin(t)
	ctx := context.Background()

	// Sender block: B semantics with elevated priority (+30).
	require.NoError(t, r.wblistUpdate(ctx, "mail_access_update", engine.Data{
		New: map[string]any{
			"access_id": float64(3), "source": "spammer@bad.example",
			"access": "REJECT", "type": "sender", "active": "y", "priority": float64(0),
		},
	}))
	out, err := os.ReadFile(r.wblistFile(true, 3))
	require.NoError(t, err)
	assert.Contains(t, string(out), "global_wblist-3 {")
	assert.Contains(t, string(out), "priority = 30;")
	assert.Contains(t, string(out), `from = "spammer@bad.example";`)
	assert.Contains(t, string(out), "R_DUMMY = 999.0;", "blacklist semantics")

	// Client IP entry uses the ip field.
	require.NoError(t, r.wblistUpdate(ctx, "mail_access_insert", engine.Data{
		New: map[string]any{
			"access_id": float64(4), "source": "192.0.2.7",
			"access": "OK", "type": "client", "active": "y", "priority": float64(5),
		},
	}))
	out, err = os.ReadFile(r.wblistFile(true, 4))
	require.NoError(t, err)
	assert.Contains(t, string(out), `ip = "192.0.2.7";`)
	assert.Contains(t, string(out), "want_spam = yes;", "whitelist semantics")

	// Hostname client entry.
	require.NoError(t, r.wblistUpdate(ctx, "mail_access_insert", engine.Data{
		New: map[string]any{
			"access_id": float64(5), "source": "relay.partner.example",
			"access": "OK", "type": "client", "active": "y", "priority": float64(1),
		},
	}))
	out, err = os.ReadFile(r.wblistFile(true, 5))
	require.NoError(t, err)
	assert.Contains(t, string(out), `hostname = "relay.partner.example";`)

	// Inactive row removes the file.
	require.NoError(t, r.wblistUpdate(ctx, "mail_access_update", engine.Data{
		New: map[string]any{
			"access_id": float64(3), "source": "spammer@bad.example",
			"access": "REJECT", "type": "sender", "active": "n",
		},
	}))
	assert.NoFileExists(t, r.wblistFile(true, 3))

	// Delete removes too.
	require.NoError(t, r.wblistDelete(ctx, "mail_access_delete", engine.Data{
		Old: map[string]any{"access_id": float64(4)},
	}))
	assert.NoFileExists(t, r.wblistFile(true, 4))

	services.ProcessDelayedActions(ctx)
	assert.Equal(t, []string{"rspamd/reload"}, exec.runs, "deduped reload")
}
