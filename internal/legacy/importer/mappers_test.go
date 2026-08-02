package importer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/legacy/client"
)

// clientRec mirrors a legacytest client fixture record.
func clientRec() client.Record {
	return client.Record{
		"client_id": "7", "username": "client7", "contact_name": "Client Seven",
		"company_name": "Seven Ltd", "email": "seven@example.com", "language": "en",
		"parent_client_id": "1", "limit_client": "0", "limit_web_domain": "20",
		"sys_userid": "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "ri", "sys_perm_other": "",
		"unknown_new_column": "ignored",
	}
}

func TestMapClient(t *testing.T) {
	c, err := MapClient(clientRec())
	require.NoError(t, err)

	require.Zero(t, c.ClientID, "legacy primary keys are never preserved")
	require.Equal(t, "client7", c.Username)
	require.Equal(t, "Client Seven", c.ContactName)
	require.Equal(t, "seven@example.com", c.Email)
	require.Equal(t, uint32(1), c.ParentClientID, "legacy value kept for the planner remap")
	require.Equal(t, int32(20), c.LimitWebDomain)
	require.Equal(t, uint32(3), c.SysUserID)
	require.Equal(t, uint32(4), c.SysGroupID)
	require.Equal(t, "riud", c.SysPermUser)
	require.Equal(t, "ri", c.SysPermGroup)
	require.Equal(t, "", c.SysPermOther)
}

func TestMapWebDomain(t *testing.T) {
	d, err := MapWebDomain(client.Record{
		"domain_id": "42", "server_id": "3", "parent_domain_id": "10",
		"domain": "site42.example.com", "type": "vhost", "active": "y",
		"document_root": "/var/www/clients/client4/web42",
		"system_user":   "web42", "system_group": "client4",
		"hd_quota": "-1", "traffic_quota": "1000",
		"ssl": "y", "ssl_letsencrypt": "y",
		"ssl_cert": "-----BEGIN CERTIFICATE-----abc", "ssl_key": "-----BEGIN KEY-----def",
		"ssl_bundle": "-----BEGIN CERTIFICATE-----bundle",
		"sys_userid": "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
		"added_date": "", "last_quota_notification": "",
	})
	require.NoError(t, err)

	require.Zero(t, d.DomainID)
	require.Equal(t, "site42.example.com", d.Domain)
	require.Equal(t, "vhost", d.Type)
	require.Equal(t, uint32(10), d.ParentDomainID, "legacy value kept for the planner remap")
	require.Equal(t, uint32(3), d.ServerID, "legacy value kept for the server remap")
	require.Equal(t, int64(-1), d.HdQuota)
	require.Equal(t, "y", d.SSL)
	require.Equal(t, "y", d.SSLLetsencrypt)
	require.Equal(t, "-----BEGIN CERTIFICATE-----abc", d.SSLCert)
	require.Equal(t, "-----BEGIN KEY-----def", d.SSLKey)
	require.Equal(t, "-----BEGIN CERTIFICATE-----bundle", d.SSLBundle)
	require.Nil(t, d.AddedDate, "empty legacy date stays NULL")
	require.Nil(t, d.LastQuotaNotification)
}

func TestMapWebFolderAndUser(t *testing.T) {
	f, err := MapWebFolder(client.Record{
		"web_folder_id": "1", "server_id": "1", "parent_domain_id": "42",
		"path": "protected", "active": "y",
		"sys_userid": "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	})
	require.NoError(t, err)
	require.Zero(t, f.WebFolderID)
	require.Equal(t, "protected", f.Path)
	require.Equal(t, int32(42), f.ParentDomainID)

	u, err := MapWebFolderUser(client.Record{
		"web_folder_user_id": "1", "server_id": "1", "web_folder_id": "1",
		"username": "folderuser1", "password": "$6$abc$hash", "active": "y",
		"sys_userid": "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	})
	require.NoError(t, err)
	require.Zero(t, u.WebFolderUserID)
	require.Equal(t, "$6$abc$hash", u.Password, "crypt hash carried verbatim")
	require.Equal(t, int32(1), u.WebFolderID)
}

func TestMapDNS(t *testing.T) {
	z, err := MapDNSSoa(client.Record{
		"id": "12", "server_id": "1", "origin": "example.com.",
		"ns": "ns1.example.com.", "mbox": "hostmaster.example.com.",
		"serial": "2024010101", "refresh": "7200", "retry": "540",
		"expire": "604800", "minimum": "3600", "ttl": "3600", "active": "Y",
		"dnssec_initialized": "N",
		"sys_userid":         "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	})
	require.NoError(t, err)
	require.Zero(t, z.ID)
	require.Equal(t, "example.com.", z.Origin)
	require.Equal(t, uint32(2024010101), z.Serial)
	require.Equal(t, "Y", z.Active)

	rr, err := MapDNSRr(client.Record{
		"id": "5", "server_id": "1", "zone": "12", "name": "www", "type": "A",
		"data": "192.0.2.1", "aux": "0", "ttl": "3600", "active": "Y",
		"stamp": "", "serial": "",
		"sys_userid": "3", "sys_groupid": "4",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	})
	require.NoError(t, err)
	require.Zero(t, rr.ID)
	require.Equal(t, uint32(12), rr.Zone, "legacy zone id kept for the planner remap")
	require.Equal(t, "A", rr.Type)
	require.Nil(t, rr.Stamp)
	require.Nil(t, rr.Serial)

	s, err := MapDNSSlave(client.Record{
		"id": "1", "server_id": "1", "origin": "slave.example.net.",
		"ns": "192.0.2.53", "active": "Y",
		"sys_userid": "1", "sys_groupid": "1",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	})
	require.NoError(t, err)
	require.Zero(t, s.ID)
	require.Equal(t, "slave.example.net.", s.Origin)

	tpl, err := MapDNSTemplate(client.Record{
		"template_id": "1", "name": "Default", "visible": "Y",
		"fields": "DOMAIN,IP", "template": "[ZONE]\norigin={DOMAIN}.",
	})
	require.NoError(t, err)
	require.Zero(t, tpl.TemplateID)
	require.Equal(t, "Default", tpl.Name)
}

func TestMapTolerance(t *testing.T) {
	t.Run("unparseable numeric keeps zero", func(t *testing.T) {
		c, err := MapClient(client.Record{"username": "x", "limit_web_domain": "not-a-number"})
		require.NoError(t, err)
		require.Zero(t, c.LimitWebDomain)
		require.Equal(t, "x", c.Username)
	})

	t.Run("unknown columns ignored", func(t *testing.T) {
		c, err := MapClient(client.Record{"username": "x", "future_column_33": "y"})
		require.NoError(t, err)
		require.Equal(t, "x", c.Username)
	})
}

func TestDerivedSysUserAndGroup(t *testing.T) {
	c, err := MapClient(clientRec())
	require.NoError(t, err)

	g := DeriveSysGroup(c)
	require.Equal(t, "client7", g.Name)
	require.Zero(t, g.ClientID, "filled by apply once the local client id exists")

	u := DeriveSysUser(c)
	require.Equal(t, "client7", u.Username)
	require.Equal(t, "user", u.Typ)
	require.EqualValues(t, 1, u.Active)
	require.Equal(t, PlaceholderHash, u.Passwort)
	require.False(t, auth.VerifyPassword("anything", u.Passwort),
		"placeholder must be unusable for login")
	require.False(t, auth.VerifyPassword(PlaceholderHash, u.Passwort),
		"even the literal placeholder string must not log in")
}
