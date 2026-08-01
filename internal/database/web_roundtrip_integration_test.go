//go:build integration

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestWebModelsRoundTrip inserts one realistic fixture row per web table
// (web_domain, web_folder, web_folder_user, server_php) into a migrated
// MariaDB schema and reads it back (add-web-nginx-module task 1.1): the GORM
// column mappings must survive a real insert/scan cycle, not only the static
// DDL comparison of model_test.go.
func TestWebModelsRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "webrt")
	MariaDBExec(t, container, "CREATE DATABASE webrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/webrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	domain := model.WebDomain{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: 1, IPAddress: "*", Domain: "example.com", Type: "vhost",
		DocumentRoot: "/var/www/clients/client1/web1",
		SystemUser:   "web1", SystemGroup: "client1",
		// Enum columns are NOT NULL: strict-mode MariaDB rejects the empty
		// string, so every enum needs a valid value in the fixture.
		CGI: "n", SSI: "n", Suexec: "y", Ruby: "n", Python: "n", Perl: "n",
		SSLLetsencryptExclude: "n", PHPFPMChroot: "n", BackupEncrypt: "n",
		TrafficQuotaLock: "n", EnablePagespeed: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
		PHP: "php-fpm", PHPFPMUseSocket: "y", PM: "dynamic",
		PMMaxChildren: 10, PMStartServers: 2, PMMinSpareServers: 1, PMMaxSpareServers: 5,
		PMProcessIdleTimeout: 10,
		SSL:                  "n", SSLLetsencrypt: "n", RewriteToHTTPS: "n",
		SeoRedirect: "non_www_to_www", Subdomain: "www", Active: "y",
		HTTPPort: 80, HTTPSPort: 443,
		NginxDirectives: "client_max_body_size 100M;\nfastcgi_read_timeout 300;",
	}
	require.NoError(t, db.Create(&domain).Error)
	var gotDomain model.WebDomain
	require.NoError(t, db.Take(&gotDomain, domain.DomainID).Error)
	assert.Equal(t, domain.Domain, gotDomain.Domain)
	assert.Equal(t, domain.DocumentRoot, gotDomain.DocumentRoot)
	assert.Equal(t, domain.NginxDirectives, gotDomain.NginxDirectives)
	assert.Equal(t, domain.PMMaxChildren, gotDomain.PMMaxChildren)
	assert.Equal(t, domain.HTTPSPort, gotDomain.HTTPSPort)

	folder := model.WebFolder{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, ParentDomainID: int32(domain.DomainID),
		Path: "/admin", Active: "y",
	}
	require.NoError(t, db.Create(&folder).Error)
	var gotFolder model.WebFolder
	require.NoError(t, db.Take(&gotFolder, folder.WebFolderID).Error)
	assert.Equal(t, folder.Path, gotFolder.Path)
	assert.Equal(t, folder.ParentDomainID, gotFolder.ParentDomainID)

	folderUser := model.WebFolderUser{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, WebFolderID: int32(folder.WebFolderID),
		Username: "alice", Password: "$1$abcdefgh$hash", Active: "y",
	}
	require.NoError(t, db.Create(&folderUser).Error)
	var gotFolderUser model.WebFolderUser
	require.NoError(t, db.Take(&gotFolderUser, folderUser.WebFolderUserID).Error)
	assert.Equal(t, folderUser.Username, gotFolderUser.Username)
	assert.Equal(t, folderUser.Password, gotFolderUser.Password)

	php := model.ServerPHP{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Name: "PHP 8.2",
		PHPFPMInitScript: "php8.2-fpm",
		PHPFPMIniDir:     "/etc/php/8.2/fpm",
		PHPFPMPoolDir:    "/etc/php/8.2/fpm/pool.d",
		PHPFPMSocketDir:  "/var/lib/php8.2-fpm",
		Active:           "y",
	}
	require.NoError(t, db.Create(&php).Error)
	var gotPHP model.ServerPHP
	require.NoError(t, db.Take(&gotPHP, php.ServerPHPID).Error)
	assert.Equal(t, php.PHPFPMPoolDir, gotPHP.PHPFPMPoolDir)
	assert.Equal(t, php.PHPFPMInitScript, gotPHP.PHPFPMInitScript)
}
