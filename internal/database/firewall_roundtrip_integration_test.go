//go:build integration

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestFirewallModelRoundTrip inserts a realistic firewall row into a
// migrated MariaDB schema and reads it back (add-firewall-module task
// 1.1). The active enum default must survive the round trip; sys_*
// riud stamps must land on the right columns.
func TestFirewallModelRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "fwrt")
	MariaDBExec(t, container, "CREATE DATABASE fwrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/fwrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	fw := model.Firewall{
		SysUserID:    1,
		SysGroupID:   1,
		SysPermUser:  "riud",
		SysPermGroup: "riud",
		ServerID:     1,
		TCPPort:      "21,22,80,443",
		UDPPort:      "53",
		Active:       "y",
	}
	require.NoError(t, db.Create(&fw).Error)
	require.NotZero(t, fw.FirewallID, "primary key assigned on insert")

	var got model.Firewall
	require.NoError(t, db.Take(&got, fw.FirewallID).Error)
	assert.Equal(t, "21,22,80,443", got.TCPPort)
	assert.Equal(t, "53", got.UDPPort)
	assert.Equal(t, "y", got.Active)
	assert.EqualValues(t, 1, got.ServerID)
	assert.Equal(t, "riud", got.SysPermUser)
}