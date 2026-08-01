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
		adminPassword, err = randomPassword(20)
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
			Config:       defaultServerConfig,
			DBVersion:    MinDBVersion,
			Active:       1,
		}
		if err := tx.Create(&server).Error; err != nil {
			return fmt.Errorf("creating local server row: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return adminPassword, nil
}

// randomPassword returns n characters from an unambiguous alphanumeric
// alphabet using crypto/rand.
func randomPassword(n int) (string, error) {
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
