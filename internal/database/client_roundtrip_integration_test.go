//go:build integration

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestClientModelsRoundTrip inserts one realistic fixture row per
// client-module table (client_template, client_template_assigned,
// client_message_template) into a migrated MariaDB schema and reads it
// back, plus a country lookup from the seeded rows (add-client-module
// task 1.1). Client/SysUser/SysGroup round-trips are covered by the
// repository and importer integration suites.
func TestClientModelsRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "clientrt")
	MariaDBExec(t, container, "CREATE DATABASE clientrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/clientrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	tpl := model.ClientTemplate{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		TemplateName: "Starter", TemplateType: "m",
		LimitWebDomain: 5, LimitWebSubdomain: 10, LimitWebAliasdomain: 2,
		LimitDNSZone: 3, LimitDNSSlaveZone: 0, LimitDNSRecord: -1,
		LimitMaildomain: 1, LimitMailbox: 10, LimitMailalias: -1,
		LimitMailaliasdomain: -1, LimitMailforward: -1, LimitMailcatchall: -1,
		LimitMailfilter: -1, LimitFetchmail: -1, LimitMailquota: -1,
		LimitFTPUser: 2, LimitShellUser: 0, LimitWebdavUser: 0,
		LimitDatabase: 3, LimitDatabasePostgresql: -1, LimitDatabaseUser: -1,
		LimitDatabaseQuota: -1, LimitCron: 0, LimitCronFrequency: 5,
		LimitTrafficQuota: -1, LimitClient: 0, LimitAps: -1,
		LimitXMPPDomain: -1, LimitXMPPUser: -1, LimitMailmailinglist: -1,
		WebServers: "1", DNSServers: "1", MailServers: "1", DBServers: "1",
		WebPHPOptions: "php-fpm", SSHChroot: "no,jailkit",
		// NOT NULL enums need explicit valid values under strict mode.
		LimitMailBackup: "y", LimitRelayhost: "n",
		LimitXMPPMuc: "n", LimitXMPPAnon: "n", LimitXMPPVjud: "n",
		LimitXMPPProxy: "n", LimitXMPPStatus: "n", LimitXMPPPastebin: "n",
		LimitXMPPHttparchive: "n",
		LimitCGI:             "n", LimitSSI: "n", LimitPerl: "n",
		LimitRuby: "n", LimitPython: "n", ForceSuexec: "y",
		LimitHterror: "n", LimitWildcard: "n", LimitSSL: "y",
		LimitSSLLetsencrypt: "y", LimitBackup: "y",
		LimitDirectiveSnippets: "n", LimitCronType: "url",
	}
	require.NoError(t, db.Create(&tpl).Error)
	var gotTpl model.ClientTemplate
	require.NoError(t, db.Take(&gotTpl, tpl.TemplateID).Error)
	assert.Equal(t, tpl.TemplateName, gotTpl.TemplateName)
	assert.Equal(t, int32(5), gotTpl.LimitWebDomain)
	assert.Equal(t, int32(-1), gotTpl.LimitDNSRecord)
	assert.Equal(t, "y", gotTpl.LimitSSL)
	assert.Equal(t, "php-fpm", gotTpl.WebPHPOptions)
	assert.Equal(t, "m", gotTpl.TemplateType)

	assigned := model.ClientTemplateAssigned{ClientID: 7, ClientTemplateID: int32(tpl.TemplateID)}
	require.NoError(t, db.Create(&assigned).Error)
	var gotAssigned model.ClientTemplateAssigned
	require.NoError(t, db.Take(&gotAssigned, assigned.AssignedTemplateID).Error)
	assert.Equal(t, int64(7), gotAssigned.ClientID)
	assert.Equal(t, int32(tpl.TemplateID), gotAssigned.ClientTemplateID)

	msg := model.ClientMessageTemplate{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		TemplateType: "welcome", TemplateName: "Welcome mail",
		Subject: "Welcome {username}",
		Message: "Hello {contact_name},\nyour login is {username} / {password}.",
	}
	require.NoError(t, db.Create(&msg).Error)
	var gotMsg model.ClientMessageTemplate
	require.NoError(t, db.Take(&gotMsg, msg.ClientMessageTemplateID).Error)
	assert.Equal(t, "welcome", gotMsg.TemplateType)
	assert.Contains(t, gotMsg.Message, "{password}")

	// country is seeded by the embedded dump: read-only lookup must scan
	// (incl. NULLable iso3/numcode columns).
	var countries []model.Country
	require.NoError(t, db.Order("printable_name").Limit(5).Find(&countries).Error)
	require.NotEmpty(t, countries, "country table must be seeded by the dump")
	var de model.Country
	require.NoError(t, db.Where("iso = ?", "DE").Take(&de).Error)
	assert.Equal(t, "Germany", de.PrintableName)
	assert.Equal(t, "y", de.EU)
}
