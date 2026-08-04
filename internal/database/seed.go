package database

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// defaultServerConfig is the server.config INI seeded for a fresh local
// server: the complete [web] and [dns] sections of ISPConfig's
// server.ini.master, minus the apache/php5-legacy keys and with modern
// Debian/Ubuntu nginx + php-fpm paths, so getconf and the nginx/bind modules
// read it unchanged.
//
//go:embed server_config.ini
var defaultServerConfig string

// defaultSystemConfig is the panel-wide INI seeded into sys_ini, copied
// key-for-key from ISPConfig's install/tpl/system.ini.master for the keys
// this port reads.
//
// Without it a fresh install has an empty sys_ini and behaves differently
// from ISPConfig in a way that shows up in the data: dbname_prefix is what
// makes a client database "c1_shop" instead of "shop", and an empty value
// silently drops the prefix. The legacy inserts the row empty in its SQL dump
// too — exactly as our embedded dump does — and then writes this template
// over it from the installer (installer_base.lib.php:431). This is that step.
//
//go:embed system_config.ini
var defaultSystemConfig string

// Seed finishes a fresh install after Migrate executed the DDL: it sets the
// admin password (bcrypt) on the admin sys_user (id 1, inserted by the dump
// with an unusable placeholder), repairs its groups list ('1,2' in the dump,
// but only sys_group 1 exists) and creates the local server row (web + dns
// roles) with the default config INI. The admin group and essential
// sys_config defaults (db.db_version, interface.session_timeout) already
// come from the embedded dump.
//
// adminPassword is used as-is when non-empty (unattended installs);
// otherwise a random one is generated. The password in use is returned and
// the caller must print it exactly once.
func Seed(db *gorm.DB, hostname, adminPassword string) (string, error) {
	if adminPassword == "" {
		var err error
		adminPassword, err = RandomPassword(20)
		if err != nil {
			return "", err
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing admin password: %w", err)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.SysUser{}).Where("userid = 1").Updates(map[string]any{
			"passwort": string(hash),
			// The dump sets groups='1,2' but only inserts sys_group 1;
			// drop the orphan group reference.
			"groups": "1",
		})
		if res.Error != nil {
			return fmt.Errorf("setting admin password: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("admin sys_user (id 1) not found; schema seed data is incomplete")
		}

		server := model.Server{
			SysUserID:    1,
			SysGroupID:   1,
			SysPermUser:  "riud",
			SysPermGroup: "riud",
			SysPermOther: "r",
			ServerName:   hostname,
			WebServer:    1,
			DNSServer:    1,
			DBServer:     1,
			Config:       defaultServerConfig,
			DBVersion:    MinDBVersion,
			Active:       1,
		}
		if err := tx.Create(&server).Error; err != nil {
			return fmt.Errorf("creating local server row: %w", err)
		}

		if _, err := EnsureSystemConfig(tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return adminPassword, nil
}

// EnsureSystemConfig fills sys_ini row 1 with the panel defaults when it is
// still empty, inserting the row if it is missing entirely.
//
// It is deliberately separate from Seed and idempotent, because it has to run
// on more than a fresh install: every go-ispconfig created before the panel
// seeded this row has an empty sys_ini, and nothing would ever fill it —
// `migrate` skips Seed once a schema exists. Those installs are the ones
// silently creating client databases without the c[CLIENTID] prefix.
//
// The empty guard is what makes it safe to run every time: an adopted
// ISPConfig3 database, or one an operator has already configured, keeps its
// own values.
// It reports whether it actually wrote, so a caller can tell the operator the
// truth instead of "no seed data written" after having written some.
func EnsureSystemConfig(db *gorm.DB) (bool, error) {
	res := db.Model(&model.SysIni{}).
		Where("sysini_id = ? AND (config IS NULL OR config = '')", 1).
		Update("config", defaultSystemConfig)
	if res.Error != nil {
		return false, fmt.Errorf("seeding sys_ini config: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		return true, nil
	}
	// Zero rows means either "already configured" or "no row at all", and an
	// UPDATE cannot tell them apart — checking is what stops the seed
	// reporting success while having written nothing.
	var n int64
	if err := db.Model(&model.SysIni{}).Where("sysini_id = ?", 1).Count(&n).Error; err != nil {
		return false, fmt.Errorf("checking sys_ini row: %w", err)
	}
	if n > 0 {
		return false, nil // already carries a configuration; leave it alone
	}
	err := db.Create(&model.SysIni{SysIniID: 1, Config: defaultSystemConfig}).Error
	if err != nil {
		return false, fmt.Errorf("creating sys_ini row: %w", err)
	}
	return true, nil
}

// RandomPassword returns n characters from an unambiguous alphanumeric
// alphabet using crypto/rand (shared by the seed and the installer's
// generated DB credentials).
func RandomPassword(n int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("generating password: %w", err)
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}
