//go:build integration

package mail

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// TestRspamdUserSettings covers the DB-backed settings paths (task 6.1):
// policy-derived thresholds in the rendered conf, greylisting
// inheritance, and the domain fan-out to children without their own
// spamfilter_users row.
func TestRspamdUserSettings(t *testing.T) {
	db := setupMailDB(t, "rspamduser")
	ctx := context.Background()

	kill, tag, grey := 15.0, 6.5, 0.5
	pol := model.SpamfilterPolicy{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		PolicyName: "Normal", RspamdGreylisting: "y",
		RspamdSpamTagLevel: &tag, RspamdSpamKillLevel: &kill,
		RspamdSpamTagMethod: "rewrite_subject", RspamdSpamGreylistingLevel: &grey,
	}
	require.NoError(t, db.Create(&pol).Error)

	mkPlugin := func() (*RspamdPlugin, *engine.Services, *recordExec) {
		exec := &recordExec{}
		services := engine.NewServices(exec, nil)
		RegisterServices(services)
		base := NewPlugin(db, services, &fakeRunner{}, 1, nil)
		base.LoadConfig = func(context.Context) (getconf.MailConfig, error) {
			return getconf.DefaultMailConfig(), nil
		}
		r := NewRspamdPlugin(base, "")
		r.RspamdDir = t.TempDir()
		require.NoError(t, os.MkdirAll(r.usersDir(), 0o755))
		return r, services, exec
	}

	t.Run("spamfilter user renders policy thresholds", func(t *testing.T) {
		r, services, exec := mkPlugin()
		require.NoError(t, r.userSettingsUpdate(ctx, "spamfilter_users_insert", engine.Data{
			New: map[string]any{
				"id": float64(11), "email": "user1@example.com",
				"policy_id": float64(pol.ID), "priority": float64(7), "local": "Y",
			},
		}))
		out, err := os.ReadFile(r.settingsFile("user1@example.com"))
		require.NoError(t, err)
		conf := string(out)
		assert.Contains(t, conf, "ispc_spamfilter_user_11 {")
		assert.Contains(t, conf, "priority = 27;", "20 + row priority")
		assert.Contains(t, conf, `rcpt = "user1@example.com";`, "local=Y matches recipient")
		assert.Contains(t, conf, `reject = 15;`)
		assert.Contains(t, conf, `"rewrite subject" = 6.5;`)
		assert.Contains(t, conf, "greylist = 0.5;")
		services.ProcessDelayedActions(ctx)
		assert.Equal(t, []string{"rspamd/reload"}, exec.runs)
	})

	t.Run("policy_id 0 removes the file", func(t *testing.T) {
		r, _, _ := mkPlugin()
		file := r.settingsFile("drop@example.com")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
		require.NoError(t, r.userSettingsUpdate(ctx, "spamfilter_users_update", engine.Data{
			New: map[string]any{
				"id": float64(12), "email": "drop@example.com", "policy_id": float64(0),
			},
		}))
		assert.NoFileExists(t, file)
	})

	t.Run("domain identity fans out to children", func(t *testing.T) {
		// Domain spamfilter user + one mailbox without its own row.
		usr := model.MailUser{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Email: "child@fan.example", Login: "child@fan.example",
			Maildir: "/var/vmail/fan.example/child", MaildirFormat: "maildir",
			ForwardInLda: "n", Autoresponder: "n", MoveJunk: "y", Postfix: "y",
			Greylisting: "n", Access: "y", AutoresponderSubject: "s",
			DisableIMAP: "n", DisablePOP3: "n", DisableDeliver: "n", DisableSMTP: "n",
			DisableSieve: "n", DisableSieveFilter: "n", DisableLda: "n",
			DisableLmtp: "n", DisableDoveadm: "n", DisableQuotaStatus: "n",
			DisableIndexerWorker: "n", DisableReplicator: "n", BackupInterval: "none",
			BackupCopies: 1, UID: 5000, GID: 5000,
		}
		require.NoError(t, db.Create(&usr).Error)
		// The event mirrors a real DB row (the children's policy lookup
		// resolves through it).
		sfu := model.SpamfilterUser{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Priority: 5, PolicyID: pol.ID, Email: "@fan.example", Local: "Y",
		}
		require.NoError(t, db.Create(&sfu).Error)

		r, _, _ := mkPlugin()
		require.NoError(t, r.userSettingsUpdate(ctx, "spamfilter_users_insert", engine.Data{
			New: map[string]any{
				"id": float64(sfu.ID), "email": "@fan.example",
				"policy_id": float64(pol.ID), "priority": float64(5),
			},
		}))
		domainConf, err := os.ReadFile(r.settingsFile("fan.example"))
		require.NoError(t, err)
		assert.Contains(t, string(domainConf), "priority = 15;", "10 + priority for domains")
		childConf, err := os.ReadFile(r.settingsFile("child@fan.example"))
		require.NoError(t, err, "child mailbox re-rendered by the domain fan-out")
		assert.Contains(t, string(childConf), "ispc_mail_user_", "child rendered as mail_user identity")
		assert.Contains(t, string(childConf), `rcpt = "child@fan.example";`)
	})
}
