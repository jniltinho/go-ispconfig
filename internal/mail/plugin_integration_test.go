//go:build integration

package mail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// setupMailDB boots a migrated MariaDB for the mail plugin suites.
func setupMailDB(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, suffix)
	database.MariaDBExec(t, container, "CREATE DATABASE "+suffix+" CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/" + suffix + "?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "mail1.test", "seed-pw-123456")
	require.NoError(t, err)
	return db
}

// TestResolveUIDGID covers the -1 resolution paths (task 3.1): getconf
// fallback with silent write-back, and virtual uid/gid maps following the
// web_domain parent chain on the same server.
func TestResolveUIDGID(t *testing.T) {
	db := setupMailDB(t, "mailuid")
	ctx := context.Background()

	mk := func(email string) *model.MailUser {
		u := &model.MailUser{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Email: email, Login: email, UID: -1, GID: -1,
			Maildir: "/var/vmail/x/" + email, MaildirFormat: "maildir",
			ForwardInLda: "n", Autoresponder: "n", MoveJunk: "y", Postfix: "y",
			Greylisting: "n", Access: "y", AutoresponderSubject: "s",
			DisableIMAP: "n", DisablePOP3: "n", DisableDeliver: "n", DisableSMTP: "n",
			DisableSieve: "n", DisableSieveFilter: "n", DisableLda: "n",
			DisableLmtp: "n", DisableDoveadm: "n", DisableQuotaStatus: "n",
			DisableIndexerWorker: "n", DisableReplicator: "n", BackupInterval: "none",
			BackupCopies: 1,
		}
		require.NoError(t, db.Create(u).Error)
		return u
	}

	newRow := func(u *model.MailUser) row {
		return row{
			"mailuser_id": float64(u.MailuserID), "email": u.Email,
			"uid": float64(u.UID), "gid": float64(u.GID),
			"server_id": float64(u.ServerID),
		}
	}

	t.Run("getconf fallback with silent write-back", func(t *testing.T) {
		u := mk("plain@nomap.test")
		p := NewPlugin(db, nil, nil, 1, nil)
		p.LoadConfig = func(context.Context) (getconf.MailConfig, error) {
			return getconf.DefaultMailConfig(), nil // virtual maps off
		}
		cfg, _ := p.config(ctx)
		uid, gid := p.resolveUIDGID(ctx, cfg, newRow(u))
		assert.EqualValues(t, 5000, uid)
		assert.EqualValues(t, 5000, gid)

		var got model.MailUser
		require.NoError(t, db.Take(&got, u.MailuserID).Error)
		assert.EqualValues(t, 5000, got.UID, "written back")
		assert.EqualValues(t, 5000, got.GID)
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = 'mail_user'").Count(&n).Error)
		assert.Zero(t, n, "write-back must not journal a datalog row")
	})

	t.Run("virtual maps resolve via web_domain parent chain", func(t *testing.T) {
		// Parent vhost with the system user; child subdomain without one.
		parent := model.WebDomain{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Domain: "mapped.test", Type: "vhost", SystemUser: "web1",
			CGI: "n", SSI: "n", Suexec: "y", Subdomain: "none", Ruby: "n",
			Python: "n", Perl: "n", SSL: "n", SSLLetsencrypt: "n",
			SSLLetsencryptExclude: "n", RewriteToHTTPS: "n", PHPFPMUseSocket: "y",
			EnablePagespeed: "n", PHPFPMChroot: "n", PM: "ondemand",
			BackupEncrypt: "n", Active: "y", TrafficQuotaLock: "n",
			ProxyProtocol: "n", DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
		}
		require.NoError(t, db.Create(&parent).Error)
		child := parent
		child.DomainID = 0
		child.Domain = "sub.mapped.test"
		child.SystemUser = ""
		child.ParentDomainID = parent.DomainID
		require.NoError(t, db.Create(&child).Error)

		u := mk("user@sub.mapped.test")
		p := NewPlugin(db, nil, nil, 1, nil)
		p.LookupUserUID = func(name string) (int64, bool) {
			require.Equal(t, "web1", name)
			return 1234, true
		}
		cfg := getconf.DefaultMailConfig()
		cfg.MailboxVirtualUidgidMaps = "y"
		uid, gid := p.resolveUIDGID(ctx, cfg, newRow(u))
		assert.EqualValues(t, 1234, uid, "uid mapped from the parent web domain's system user")
		assert.EqualValues(t, 5000, gid, "gid falls back to mailuser_gid")
	})

	t.Run("virtual maps skip web domain on another server", func(t *testing.T) {
		other := model.WebDomain{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 9, Domain: "other.test", Type: "vhost", SystemUser: "web9",
			CGI: "n", SSI: "n", Suexec: "y", Subdomain: "none", Ruby: "n",
			Python: "n", Perl: "n", SSL: "n", SSLLetsencrypt: "n",
			SSLLetsencryptExclude: "n", RewriteToHTTPS: "n", PHPFPMUseSocket: "y",
			EnablePagespeed: "n", PHPFPMChroot: "n", PM: "ondemand",
			BackupEncrypt: "n", Active: "y", TrafficQuotaLock: "n",
			ProxyProtocol: "n", DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
		}
		require.NoError(t, db.Create(&other).Error)

		u := mk("user@other.test")
		p := NewPlugin(db, nil, nil, 1, nil)
		p.LookupUserUID = func(string) (int64, bool) { t.Fatal("must not look up cross-server"); return 0, false }
		cfg := getconf.DefaultMailConfig()
		cfg.MailboxVirtualUidgidMaps = "y"
		uid, _ := p.resolveUIDGID(ctx, cfg, newRow(u))
		assert.EqualValues(t, 5000, uid, "cross-server web domain never maps")
	})
}
