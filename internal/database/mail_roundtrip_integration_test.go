//go:build integration

package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestMailModelsRoundTrip inserts one realistic fixture row per mail
// table (mail_domain, mail_user, mail_forwarding, mail_transport,
// mail_access) into a migrated MariaDB schema and reads it back
// (add-mail-module task 1.1). Strict mode makes this catch missing enum
// defaults and misnamed columns (incl. the dashed disable* columns).
func TestMailModelsRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "mailrt")
	MariaDBExec(t, container, "CREATE DATABASE mailrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/mailrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	dom := model.MailDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Domain: "example.com",
		DKIM: "y", DKIMSelector: "default",
		DKIMPrivate: "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----",
		DKIMPublic:  "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----",
		Active:      "y", LocalDelivery: "y",
	}
	require.NoError(t, db.Create(&dom).Error)
	var gotDom model.MailDomain
	require.NoError(t, db.Take(&gotDom, dom.DomainID).Error)
	assert.Equal(t, "example.com", gotDom.Domain)
	assert.Equal(t, "y", gotDom.DKIM)
	assert.Contains(t, gotDom.DKIMPrivate, "PRIVATE KEY")

	start := time.Date(2026, 8, 1, 8, 0, 0, 0, time.Local)
	end := time.Date(2026, 8, 15, 18, 0, 0, 0, time.Local)
	usr := model.MailUser{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Email: "user1@example.com", Login: "user1@example.com",
		Password: "$6$saltsalt$hashhashhash", Name: "User One",
		UID: 5000, GID: 5000,
		Maildir: "/var/vmail/example.com/user1", MaildirFormat: "maildir",
		Quota: 1048576, CC: "copy@example.com", ForwardInLda: "y",
		Autoresponder: "y", AutoresponderStartDate: &start, AutoresponderEndDate: &end,
		AutoresponderSubject: "Out of office", AutoresponderText: "Back soon",
		MoveJunk: "y", Postfix: "y", Greylisting: "n", Access: "y",
		DisableIMAP: "n", DisablePOP3: "n", DisableDeliver: "n", DisableSMTP: "n",
		DisableSieve: "n", DisableSieveFilter: "n", DisableLda: "n", DisableLmtp: "n",
		DisableDoveadm: "n", DisableQuotaStatus: "n", DisableIndexerWorker: "n",
		DisableReplicator: "n", BackupInterval: "none", BackupCopies: 1,
	}
	require.NoError(t, db.Create(&usr).Error)
	var gotUsr model.MailUser
	require.NoError(t, db.Take(&gotUsr, usr.MailuserID).Error)
	assert.Equal(t, "user1@example.com", gotUsr.Email)
	assert.EqualValues(t, 1048576, gotUsr.Quota)
	require.NotNil(t, gotUsr.AutoresponderStartDate)
	assert.Equal(t, start.Format("2006-01-02 15:04"), gotUsr.AutoresponderStartDate.Format("2006-01-02 15:04"))
	assert.Nil(t, gotUsr.LastAccess, "NULL survives the round trip")
	assert.Equal(t, "n", gotUsr.DisableSieveFilter, "dashed column mapped")

	fwd := model.MailForwarding{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Source: "alias@example.com", Destination: "user1@example.com",
		Type: "alias", Active: "y", AllowSendAs: "y", Greylisting: "n",
	}
	require.NoError(t, db.Create(&fwd).Error)
	var gotFwd model.MailForwarding
	require.NoError(t, db.Take(&gotFwd, fwd.ForwardingID).Error)
	assert.Equal(t, "alias", gotFwd.Type)

	tr := model.MailTransport{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Domain: "relay.example.net", Transport: "smtp:[10.0.0.5]:25",
		SortOrder: 5, Active: "y",
	}
	require.NoError(t, db.Create(&tr).Error)
	var gotTr model.MailTransport
	require.NoError(t, db.Take(&gotTr, tr.TransportID).Error)
	assert.Equal(t, "smtp:[10.0.0.5]:25", gotTr.Transport)

	acc := model.MailAccess{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Source: "spammer@bad.example", Access: "REJECT",
		Type: "sender", Active: "y",
	}
	require.NoError(t, db.Create(&acc).Error)
	var gotAcc model.MailAccess
	require.NoError(t, db.Take(&gotAcc, acc.AccessID).Error)
	assert.Equal(t, "REJECT", gotAcc.Access)
	assert.Equal(t, "sender", gotAcc.Type)
}
