//go:build integration

package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestSiteUserModelsRoundTrip inserts one realistic fixture row into
// ftp_user and shell_user on a migrated MariaDB schema and reads it back
// (add-ftp-shell-module task 1.1): the GORM column mappings must survive a
// real insert/scan cycle, not only the static DDL comparison in
// model_test.go. Notably `expires` is a nullable datetime and must
// round-trip both as NULL and as a value.
func TestSiteUserModelsRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "siteuserrt")
	MariaDBExec(t, container, "CREATE DATABASE siteuserrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/siteuserrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.Local)
	ftp := model.FTPUser{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: 1, ParentDomainID: 1,
		Username: "web1_alice", UsernamePrefix: "web1_",
		Password: "$1$abcdefgh$hash", QuotaSize: 2048, Active: "y",
		UID: "web1", GID: "client1", Dir: "/var/www/clients/client1/web1",
		QuotaFiles: -1, ULRatio: -1, DLRatio: 2,
		ULBandwidth: 100, DLBandwidth: -1,
		Expires: &expires, UserType: "user", UserConfig: "",
	}
	require.NoError(t, db.Create(&ftp).Error)
	var gotFTP model.FTPUser
	require.NoError(t, db.Take(&gotFTP, ftp.FTPUserID).Error)
	assert.Equal(t, ftp.Username, gotFTP.Username)
	assert.Equal(t, ftp.Password, gotFTP.Password)
	assert.Equal(t, ftp.Dir, gotFTP.Dir)
	assert.Equal(t, ftp.QuotaSize, gotFTP.QuotaSize)
	assert.Equal(t, ftp.DLRatio, gotFTP.DLRatio)
	assert.Equal(t, ftp.ULBandwidth, gotFTP.ULBandwidth)
	assert.Equal(t, "user", gotFTP.UserType)
	require.NotNil(t, gotFTP.Expires)
	assert.Equal(t, expires.UTC(), gotFTP.Expires.UTC())

	// A never-expiring account keeps expires NULL.
	noExpiry := ftp
	noExpiry.FTPUserID, noExpiry.Username, noExpiry.Expires = 0, "web1_bob", nil
	require.NoError(t, db.Create(&noExpiry).Error)
	var gotNoExpiry model.FTPUser
	require.NoError(t, db.Take(&gotNoExpiry, noExpiry.FTPUserID).Error)
	assert.Nil(t, gotNoExpiry.Expires)

	shell := model.ShellUser{
		SysUserID: 1, SysGroupID: 1,
		SysPermUser: "riud", SysPermGroup: "riud", SysPermOther: "",
		ServerID: 1, ParentDomainID: 1,
		Username: "web1_carol", UsernamePrefix: "web1_",
		Password: "$6$salt$hash", QuotaSize: -1, Active: "y",
		PUser: "web1", PGroup: "client1", Shell: "/bin/bash",
		Dir: "/var/www/clients/client1/web1", Chroot: "jailkit",
		SSHRsa: "ssh-rsa AAAAB3NzaC1yc2E carol@example.com",
	}
	require.NoError(t, db.Create(&shell).Error)
	var gotShell model.ShellUser
	require.NoError(t, db.Take(&gotShell, shell.ShellUserID).Error)
	assert.Equal(t, shell.Username, gotShell.Username)
	assert.Equal(t, shell.Password, gotShell.Password)
	assert.Equal(t, shell.PUser, gotShell.PUser)
	assert.Equal(t, shell.PGroup, gotShell.PGroup)
	assert.Equal(t, shell.Chroot, gotShell.Chroot)
	assert.Equal(t, shell.SSHRsa, gotShell.SSHRsa)
	assert.Equal(t, int64(-1), gotShell.QuotaSize)
}
